package wa

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"

	appstore "github.com/frontyard/whodat/internal/store"
)

// onMessage persists an incoming message and publishes UI events.
// Runs on the serial DB worker.
func (s *Service) onMessage(ctx context.Context, e *events.Message) {
	chatJID := s.cid(e.Info.Chat.String())
	if chatJID == "" || e.Info.ID == "" {
		return
	}
	// Sender can carry a device part; strip it so reaction keys and contact
	// rows match the canonical non-AD form everywhere else.
	senderJID := s.cid(e.Info.Sender.ToNonAD().String())
	kind := jidKind(e.Info.Chat)

	// Call log stubs → TCall row.
	if IsCallLog(e.Message) {
		s.persistCallStub(ctx, chatJID, senderJID, kind, e)
		return
	}

	msg, act := s.convert(chatJID, e.Info.ID, senderJID, e.Info.PushName, e.Info.IsFromMe, e.Info.Timestamp, e.Message)
	if act != nil && act.kind == actPollVote {
		act.voteEvt = e // decryption needs the live event, not a reconstruction
	}
	s.applyAction(ctx, chatJID, msg, act, kind)

	// Learn push names and LID↔PN pairs from traffic.
	if e.Info.PushName != "" && !e.Info.IsFromMe && senderJID != "" {
		_ = s.db.UpsertContact(ctx, &appstore.Contact{JID: senderJID, PushName: e.Info.PushName})
	}
	s.learnAlt(ctx, chatJID)
	s.learnAlt(ctx, senderJID)
}

// persistCallStub stores a server call-log stub, deduplicating against the
// local "call-*" row the call manager writes at hangup (different ID for the
// same call) and counting only missed incoming calls toward unread.
func (s *Service) persistCallStub(ctx context.Context, chatJID, senderJID appstore.PeerID, kind appstore.ChatKind, e *events.Message) {
	cl := e.Message.GetCallLogMesssage()
	// A locally-written call row for this direction within the window means
	// we already logged this call — the local row is richer.
	if recent, err := s.db.FindRecentCall(ctx, chatJID, e.Info.Timestamp.Add(-2*time.Minute), e.Info.IsFromMe); err == nil &&
		recent != nil && strings.HasPrefix(recent.ID, "call-") {
		return
	}
	text, missed := callStubText(cl, e.Info.IsFromMe)
	m := &appstore.Message{
		ChatJID: chatJID, ID: e.Info.ID, SenderJID: senderJID,
		SenderName: e.Info.PushName, TS: e.Info.Timestamp,
		Type: appstore.TCall, Text: text, Duration: int(cl.GetDurationSecs()),
		FromMe: e.Info.IsFromMe,
	}
	m.MediaMIME = "call/audio"
	if cl.GetIsVideo() {
		m.MediaMIME = "call/video"
	}
	s.persistMessage(ctx, m, kind, missed)
}

// callStubText renders the call-log stub and reports whether it counts as
// unread (missed/silenced incoming calls only — ones we saw live don't).
func callStubText(cl *waE2E.CallLogMessage, fromMe bool) (string, bool) {
	kind := "Voice"
	if cl.GetIsVideo() {
		kind = "Video"
	}
	dur := ""
	if d := cl.GetDurationSecs(); d > 0 {
		dur = fmt.Sprintf(" · %d:%02d", d/60, d%60)
	}
	switch cl.GetCallOutcome() {
	case waE2E.CallLogMessage_MISSED,
		waE2E.CallLogMessage_SILENCED_BY_DND,
		waE2E.CallLogMessage_SILENCED_UNKNOWN_CALLER:
		if fromMe {
			return "No answer", false
		}
		return "Missed " + strings.ToLower(kind) + " call", true
	case waE2E.CallLogMessage_REJECTED:
		return "Declined " + strings.ToLower(kind) + " call", false
	default:
		if fromMe {
			return kind + " call" + dur, false
		}
		return kind + " call" + dur, false
	}
}

// learnAlt records a LID↔PN pair whenever we encounter either form —
// input is always this account's namespaced id.
func (s *Service) learnAlt(ctx context.Context, jidStr appstore.PeerID) {
	j, err := types.ParseJID(jidStr.Wire())
	if err != nil {
		return
	}
	switch j.Server {
	case types.HiddenUserServer:
		if pn, err := s.Client().Store.LIDs.GetPNForLID(ctx, j); err == nil && !pn.IsEmpty() {
			_ = s.db.UpsertAltJID(ctx, s.cid(j.String()), s.cid(pn.String()))
		}
	case types.DefaultUserServer:
		if lid, err := s.Client().Store.LIDs.GetLIDForPN(ctx, j); err == nil && !lid.IsEmpty() {
			_ = s.db.UpsertAltJID(ctx, s.cid(lid.String()), s.cid(j.String()))
		}
	}
}

// canon maps a chat id to its canonical (PN) form for event consumers.
func (s *Service) canon(ctx context.Context, jid appstore.PeerID) appstore.PeerID {
	return s.db.CanonicalJID(ctx, jid)
}

// applyAction routes a converted message into storage + bus.
func (s *Service) applyAction(ctx context.Context, chatJID appstore.PeerID, m *appstore.Message, act *msgAction, kind appstore.ChatKind) {
	if act == nil {
		return
	}
	switch act.kind {
	case actInsert:
		s.persistMessage(ctx, m, kind, !m.FromMe)
	case actReact:
		s.applyReaction(ctx, chatJID, act)
	case actRevoke:
		// Capture the media path first — SetRevoked clears it from the row,
		// then unlink the file so deleted media can't be reopened from disk.
		var oldPath string
		if target, err := s.db.GetMessage(ctx, chatJID, act.targetID); err == nil && target != nil {
			oldPath = target.MediaPath
		}
		if err := s.db.SetRevoked(ctx, chatJID, act.targetID); err == nil {
			if oldPath != "" {
				_ = os.Remove(oldPath)
			}
			s.publishMsgUpdate(ctx, chatJID, act.targetID)
		}
	case actEdit:
		if err := s.db.ApplyEdit(ctx, chatJID, act.targetID, act.newText); err == nil {
			_ = s.db.SetLinkPreview(ctx, chatJID, act.targetID, act.pv)
			s.publishMsgUpdate(ctx, chatJID, act.targetID)
		}
	case actPollVote:
		s.applyPollVote(ctx, chatJID, act)
	case actDisapp:
		_ = s.db.SetDisappSecs(ctx, chatJID, int(act.disappSec))
		text := "Disappearing messages turned off"
		if act.disappSec > 0 {
			text = "Disappearing messages turned on"
		}
		m := &appstore.Message{
			ChatJID: chatJID,
			ID:      fmt.Sprintf("disapp-%d-%d", act.disappSec, time.Now().UnixNano()),
			TS:      time.Now(), Type: appstore.TSystem, Text: text,
		}
		s.persistMessage(ctx, m, kind, false)
	}
}

// applyPollVote decrypts a poll update and rewrites the voter's ballot.
func (s *Service) applyPollVote(ctx context.Context, chatJID appstore.PeerID, act *msgAction) {
	if act.voteEvt == nil {
		return // history-sync votes can't be decrypted — no live event
	}
	poll, err := s.db.GetMessage(ctx, chatJID, act.targetID)
	if err != nil || poll == nil || len(poll.PollOptions) == 0 {
		return
	}
	vote, err := s.Client().DecryptPollVote(ctx, act.voteEvt)
	if err != nil {
		s.log.Warnf("poll vote decrypt: %v", err)
		return
	}
	hashes := whatsmeow.HashPollOptions(poll.PollOptions)
	opts := []string{}
	for _, sel := range vote.GetSelectedOptions() {
		for i, h := range hashes {
			if string(h) == string(sel) {
				opts = append(opts, poll.PollOptions[i])
				break
			}
		}
	}
	voter := act.senderJID
	if voter == "" {
		voter = s.OwnID()
	}
	if err := s.db.VotePoll(ctx, chatJID, act.targetID, voter, opts); err == nil {
		s.publishMsgUpdate(ctx, chatJID, act.targetID)
	}
}

// persistMessage inserts the row, bumps the chat summary, publishes events.
// bumpUnread controls the unread counter explicitly — call stubs only count
// when they were actually missed.
func (s *Service) persistMessage(ctx context.Context, m *appstore.Message, kind appstore.ChatKind, bumpUnread bool) {
	if err := s.db.EnsureChat(ctx, m.ChatJID, kind); err != nil {
		return
	}
	s.sink.MessageSaved(ctx, m, preview(m), bumpUnread)
	// The event lands under the stored JID — the host resolves lid/pn
	// aliases to the chat row — but the list still refreshes under the
	// canonical form too.
	if cj := s.canon(ctx, m.ChatJID); cj != m.ChatJID {
		s.sink.ChatChanged(cj)
	}
}

// applyReaction folds an emoji reaction into the target message's map.
func (s *Service) applyReaction(ctx context.Context, chatJID appstore.PeerID, act *msgAction) {
	target, err := s.db.GetMessage(ctx, chatJID, act.targetID)
	if err != nil || target == nil {
		return
	}
	// A sender can only hold one reaction; remove from all emoji first.
	target.Reactions = appstore.FoldReaction(target.Reactions, act.senderJID, act.emoji)
	if err := s.db.SetReactions(ctx, chatJID, act.targetID, target.Reactions); err == nil {
		s.sink.MessageChanged(s.canon(ctx, chatJID), target)
	}
}

// publishMsgUpdate re-reads a row and republishes it for the UI.
func (s *Service) publishMsgUpdate(ctx context.Context, chatJID appstore.PeerID, msgID string) {
	m, err := s.db.GetMessage(ctx, chatJID, msgID)
	if err != nil || m == nil {
		return
	}
	cj := s.canon(ctx, chatJID)
	s.sink.MessageChanged(cj, m)
	s.sink.ChatChanged(cj)
}

var statusRank = map[appstore.MsgStatus]int{
	appstore.SNone:      0,
	appstore.SPending:   1,
	appstore.SSent:      2,
	appstore.SDelivered: 3,
	appstore.SPlayed:    4,
	appstore.SRead:      5,
	appstore.SFailed:    -1,
}

// onReceipt upgrades delivery status on our outgoing messages.
func (s *Service) onReceipt(ctx context.Context, e *events.Receipt) {
	var st appstore.MsgStatus
	switch e.Type {
	case types.ReceiptTypeRead, types.ReceiptTypeReadSelf:
		st = appstore.SRead
	case types.ReceiptTypePlayed, types.ReceiptTypePlayedSelf:
		st = appstore.SPlayed
	case types.ReceiptTypeDelivered:
		st = appstore.SDelivered
	default:
		return
	}
	chatJID := s.cid(e.MessageSource.Chat.String())
	for _, id := range e.MessageIDs {
		cur, err := s.db.GetMessage(ctx, chatJID, id)
		if err != nil || cur == nil || !cur.FromMe {
			continue
		}
		if statusRank[cur.Status] >= statusRank[st] {
			continue // never downgrade
		}
		if err := s.db.SetStatus(ctx, chatJID, id, st); err == nil {
			s.sink.Publish(Event{Kind: EvReceipt, ChatJID: s.canon(ctx, chatJID),
				Message: &appstore.Message{ChatJID: chatJID, ID: id, Status: st}})
		}
	}
}

// syncContacts pulls whatsmeow's contact store into our DB.
func (s *Service) syncContacts(ctx context.Context) {
	if s.Client().Store.Contacts == nil {
		return
	}
	contacts, err := s.Client().Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return
	}
	var pns []types.JID
	for jid := range contacts {
		if jid.Server == types.DefaultUserServer {
			pns = append(pns, jid)
		}
	}
	if m, err := s.Client().Store.LIDs.GetManyLIDsForPNs(ctx, pns); err == nil {
		for pn, lid := range m {
			_ = s.db.UpsertAltJID(ctx, s.cid(lid.String()), s.cid(pn.String()))
		}
	}
	// Reverse pass: resolve every LID-keyed chat we already know about, so
	// non-contact LID conversations merge with their PN counterparts too.
	// ChatIDs spans every account — only this account's rows apply.
	if chatIDs, err := s.db.ChatIDs(ctx); err == nil {
		var lids []types.JID
		for _, cj := range chatIDs {
			if cj.Acct() != s.acct {
				continue
			}
			if j, err := types.ParseJID(cj.Wire()); err == nil && j.Server == types.HiddenUserServer {
				lids = append(lids, j)
			}
		}
		if rev, ok := s.Client().Store.LIDs.(store.LIDBatchReverseStore); ok {
			if m, err := rev.GetManyPNsForLIDs(ctx, lids); err == nil {
				for lid, pn := range m {
					_ = s.db.UpsertAltJID(ctx, s.cid(lid.String()), s.cid(pn.String()))
				}
			}
		}
	}
	for jid, ci := range contacts {
		_ = s.db.UpsertContact(ctx, &appstore.Contact{
			JID:          s.cid(jid.String()),
			FullName:     ci.FullName,
			PushName:     ci.PushName,
			BusinessName: ci.BusinessName,
		})
		// Mirror under the LID JID so @lid chats resolve display names.
		if lid, err := s.Client().Store.LIDs.GetLIDForPN(ctx, jid); err == nil && !lid.IsEmpty() {
			_ = s.db.UpsertContact(ctx, &appstore.Contact{
				JID:          s.cid(lid.String()),
				FullName:     ci.FullName,
				PushName:     ci.PushName,
				BusinessName: ci.BusinessName,
			})
			_ = s.db.UpsertAltJID(ctx, s.cid(lid.String()), s.cid(jid.String()))
		}
	}
	s.sink.Publish(Event{Kind: EvContactsUpdated})
}

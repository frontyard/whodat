package wa

import (
	"context"
	"database/sql"
	"time"

	"github.com/polymorfa/hypermeow/proto/waHistorySync"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"

	appstore "github.com/frontyard/whodat/internal/store"
)

const historyChunk = 400 // messages per transaction — keeps UI responsive

// onHistorySync ingests the post-pair history dump into the local DB.
// Runs on its own goroutine (a full dump can take minutes — past dbq's 60s
// task cap); each conversation commits as one dbDo task so writes stay
// serialized and the wipe gate drops them if logout lands mid-sync.
// EvSyncDone is enqueued last — the queue is FIFO, so it fires after every
// conversation task has committed.
func (s *Service) onHistorySync(e *events.HistorySync) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	data := e.Data
	if data == nil {
		return
	}
	s.sink.Publish(Event{Kind: EvSyncStart})
	for _, conv := range data.GetConversations() {
		select {
		case <-ctx.Done():
			s.dbDo(func(context.Context) {
				s.sink.Publish(Event{Kind: EvSyncDone})
			})
			return
		default:
		}
		conv := conv
		s.dbDo(func(c context.Context) { s.ingestConversation(c, conv) })
	}
	s.dbDo(func(c context.Context) {
		s.syncContacts(c)
		s.sink.Publish(Event{Kind: EvSyncDone})
	})
}

func (s *Service) ingestConversation(ctx context.Context, conv *waHistorySync.Conversation) {
	jid, err := types.ParseJID(conv.GetID())
	if err != nil {
		return
	}
	cjid := s.cid(jid.String())
	kind := jidKind(jid)
	s.learnAlt(ctx, cjid)
	name := conv.GetName()
	if name == "" {
		name = conv.GetDisplayName()
	}

	// Chat row + flags.
	chat := &appstore.Chat{
		JID: cjid, Kind: kind, Name: name,
		Archived: conv.GetArchived(),
		Pinned:   conv.GetPinned() > 0,
		Muted:    conv.GetMuteEndTime() > uint64(time.Now().Unix()),
	}
	if err := s.db.UpsertChat(ctx, chat); err != nil {
		return
	}
	_ = s.db.SetChatFlags(ctx, cjid, &chat.Pinned, &chat.Archived, &chat.Muted)
	unread := int(conv.GetUnreadCount())
	if conv.GetMarkedAsUnread() && unread == 0 {
		unread = 1
	}
	_ = s.db.SetUnread(ctx, cjid, unread)

	// Messages arrive newest-first; insert in chunks. Targeted mutations
	// (edits/revokes/reactions) run in a second pass, oldest first — a
	// mutation precedes its target in newest-first order and would drop on
	// a missing row, and a sender's latest reaction must win. Inline
	// wmi.reactions are seeded directly onto the row before insert.
	msgs := conv.GetMessages()
	var newest *appstore.Message
	var deferred []*msgAction
	pushNames := map[appstore.PeerID]string{}
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		_ = appstore.EnsureChatTx(ctx, tx, cjid, kind)
		alts := s.db.AltJIDsTx(ctx, tx, cjid)
		for i, hm := range msgs {
			wmi := hm.GetMessage()
			m, act := s.webMsgToStore(cjid, wmi)
			if act == nil {
				continue
			}
			switch act.kind {
			case actInsert:
				if m == nil {
					break
				}
				s.seedReactions(m, wmi.GetReactions(), s.OwnID())
				if err := appstore.InsertMessageTx(ctx, tx, m); err != nil {
					return err
				}
				if i == 0 {
					newest = m
				}
				if !m.FromMe && m.SenderName != "" && m.SenderJID != "" {
					pushNames[m.SenderJID] = m.SenderName
				}
			case actRevoke, actEdit, actReact:
				deferred = append(deferred, act)
			}
			if i%historyChunk == historyChunk-1 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
			}
		}
		for i := len(deferred) - 1; i >= 0; i-- {
			act := deferred[i]
			switch act.kind {
			case actRevoke:
				_ = s.db.SetRevokedTx(ctx, tx, alts, act.targetID)
			case actEdit:
				_ = s.db.ApplyEditTx(ctx, tx, alts, act.targetID, act.newText)
				_ = s.db.SetLinkPreviewTx(ctx, tx, alts, act.targetID, act.pv)
			case actReact:
				s.applyReactionTx(ctx, tx, alts, act)
			}
		}
		return nil
	})
	if err != nil {
		return
	}
	// Learn push names for senders missing from the contact store —
	// fills names for non-address-book chats and @lid conversations.
	for senderJID, pushName := range pushNames {
		_ = s.db.UpsertContact(ctx, &appstore.Contact{JID: senderJID, PushName: pushName})
		s.learnAlt(ctx, senderJID)
	}
	if newest != nil {
		_ = s.db.TouchChat(ctx, cjid, preview(newest), newest.TS, false)
	}
	s.sink.ChatChanged(cjid)
}

// seedReactions applies a history dump's inline reactions to the row before
// insert. Own reactions key under OwnID so the UI's "You" display matches
// the live-event path. Empty text = a removal entry — nothing to seed.
func (s *Service) seedReactions(m *appstore.Message, rs []*waWeb.Reaction, ownID appstore.PeerID) {
	for _, r := range rs {
		emoji := r.GetText()
		if emoji == "" {
			continue
		}
		var reactor appstore.PeerID
		if raw := r.GetKey().GetParticipant(); raw != "" {
			reactor = s.cid(raw)
		} else if r.GetKey().GetFromMe() && ownID != "" {
			reactor = ownID
		} else {
			reactor = s.cid(r.GetKey().GetRemoteJID())
		}
		if reactor == "" {
			continue
		}
		if m.Reactions == nil {
			m.Reactions = map[string][]appstore.PeerID{}
		}
		m.Reactions[emoji] = append(m.Reactions[emoji], reactor)
	}
}

// applyReactionTx folds a reaction into the target inside the ingest
// transaction — mirrors applyReaction but every read/write goes through tx.
func (s *Service) applyReactionTx(ctx context.Context, tx *sql.Tx, alts []appstore.PeerID, act *msgAction) {
	target, err := s.db.GetMessageTx(ctx, tx, alts, act.targetID)
	if err != nil || target == nil {
		return
	}
	target.Reactions = appstore.FoldReaction(target.Reactions, act.senderJID, act.emoji)
	_ = s.db.SetReactionsTx(ctx, tx, alts, act.targetID, target.Reactions)
}

// webMsgToStore adapts a history-sync WebMessageInfo into a store.Message.
// chatJID arrives namespaced; the wire sender is prefixed here.
func (s *Service) webMsgToStore(chatJID appstore.PeerID, wmi *waWeb.WebMessageInfo) (*appstore.Message, *msgAction) {
	if wmi == nil || wmi.GetKey() == nil {
		return nil, &msgAction{kind: actSkip}
	}
	key := wmi.GetKey()
	sender := key.GetParticipant()
	if sender == "" {
		// Group senders live in the top-level participant field; the key's
		// participant is only set for directly-addressed messages.
		sender = wmi.GetParticipant()
	}
	if sender == "" && !key.GetFromMe() {
		sender = key.GetRemoteJID()
	}
	return s.convert(
		chatJID,
		key.GetID(),
		s.cid(sender),
		wmi.GetPushName(),
		key.GetFromMe(),
		time.Unix(int64(wmi.GetMessageTimestamp()), 0),
		wmi.GetMessage(),
	)
}

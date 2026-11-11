package wa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/appstate"
	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"

	"github.com/frontyard/whodat/internal/protocol"
	appstore "github.com/frontyard/whodat/internal/store"
)

// ---- Chat state (app-state sync) ------------------------------------------

// SetChatPin pins/unpins a chat — locally and on the account's other devices.
func (s *Service) SetChatPin(ctx context.Context, chatJID appstore.PeerID, pin bool) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	if err := s.Client().SendAppState(ctx, appstate.BuildPin(jid, pin)); err != nil {
		return err
	}
	return s.flagAndNotify(ctx, chatJID, &pin, nil, nil)
}

// SetChatArchive archives/unarchives a chat on every device.
func (s *Service) SetChatArchive(ctx context.Context, chatJID appstore.PeerID, arch bool) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	ts, key := s.lastMsgRef(ctx, chatJID)
	if err := s.Client().SendAppState(ctx, appstate.BuildArchive(jid, arch, ts, key)); err != nil {
		return err
	}
	return s.flagAndNotify(ctx, chatJID, nil, &arch, nil)
}

// SetChatMute mutes/unmutes a chat on every device (0 duration = indefinite).
func (s *Service) SetChatMute(ctx context.Context, chatJID appstore.PeerID, mute bool) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	if err := s.Client().SendAppState(ctx, appstate.BuildMute(jid, mute, 0)); err != nil {
		return err
	}
	return s.flagAndNotify(ctx, chatJID, nil, nil, &mute)
}

// MarkChatUnreadSync mirrors "mark as unread" to the account's other devices.
func (s *Service) MarkChatUnreadSync(ctx context.Context, chatJID appstore.PeerID, unread bool) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	ts, key := s.lastMsgRef(ctx, chatJID)
	if err := s.Client().SendAppState(ctx, appstate.BuildMarkChatAsRead(jid, !unread, ts, key)); err != nil {
		return err
	}
	n := 0
	if unread {
		n = 1
	}
	if err := s.db.SetUnread(ctx, chatJID, n); err != nil {
		return err
	}
	s.sink.ChatChanged(chatJID)
	return nil
}

// DeleteChat removes the conversation on every device (and locally).
func (s *Service) DeleteChat(ctx context.Context, chatJID appstore.PeerID) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	ts, key := s.lastMsgRef(ctx, chatJID)
	if err := s.Client().SendAppState(ctx, appstate.BuildDeleteChat(jid, ts, key, true)); err != nil {
		return err
	}
	return s.deleteChatLocal(ctx, chatJID, true)
}

// deleteChatLocal drops local rows/files and notifies the UI.
func (s *Service) deleteChatLocal(ctx context.Context, chatJID appstore.PeerID, media bool) error {
	paths, err := s.db.DeleteChatRows(ctx, chatJID)
	if err != nil {
		return err
	}
	if media {
		for _, p := range paths {
			_ = os.Remove(p)
		}
	}
	cj := s.canon(ctx, chatJID)
	s.sink.Publish(Event{Kind: EvChatGone, ChatJID: cj})
	s.sink.ChatChanged(cj)
	return nil
}

func (s *Service) flagAndNotify(ctx context.Context, chatJID appstore.PeerID, p, a, m *bool) error {
	if err := s.db.SetChatFlags(ctx, chatJID, p, a, m); err != nil {
		return err
	}
	s.sink.ChatChanged(chatJID)
	return nil
}

// lastMsgRef returns the newest message's timestamp + protocol key for
// app-state patches that anchor on "last message" (archive/delete/mark-read).
func (s *Service) lastMsgRef(ctx context.Context, chatJID appstore.PeerID) (time.Time, *waCommon.MessageKey) {
	msgs, err := s.db.LatestMessages(ctx, chatJID, 1)
	if err != nil || len(msgs) == 0 {
		return time.Now(), nil
	}
	m := msgs[0]
	chat, _ := s.jid(chatJID)
	sender, err := s.jid(m.SenderJID)
	if err != nil {
		sender = chat
	}
	return m.TS, s.Client().BuildMessageKey(chat, sender, m.ID)
}

// SendStar stars/unstars a message — synced to other devices via app state.
func (s *Service) SendStar(ctx context.Context, chatJID appstore.PeerID, m *appstore.Message, star bool) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	sender, err := s.jid(m.SenderJID)
	if err != nil {
		sender = jid
	}
	if err := s.Client().SendAppState(ctx, appstate.BuildStar(jid, sender, m.ID, m.FromMe, star)); err != nil {
		return err
	}
	if err := s.db.SetStarred(ctx, chatJID, m.ID, star); err == nil {
		s.publishMsgUpdate(ctx, chatJID, m.ID)
	}
	return nil
}

// ---- Disappearing messages -------------------------------------------------

// SetDisappearing sets the per-chat disappearing timer (0 disables).
func (s *Service) SetDisappearing(ctx context.Context, chatJID appstore.PeerID, secs int) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	if err := s.Client().SetDisappearingTimer(ctx, jid, time.Duration(secs)*time.Second, time.Now()); err != nil {
		return err
	}
	_ = s.db.SetDisappSecs(ctx, chatJID, secs)
	s.sink.ChatChanged(chatJID)
	return nil
}

// ---- Blocking --------------------------------------------------------------

// SetBlocked blocks/unblocks a user.
func (s *Service) SetBlocked(ctx context.Context, jidStr appstore.PeerID, block bool) error {
	jid, err := s.jid(jidStr)
	if err != nil {
		return err
	}
	action := events.BlocklistChangeActionUnblock
	if block {
		action = events.BlocklistChangeActionBlock
	}
	if _, err := s.Client().UpdateBlocklist(ctx, jid, action, ""); err != nil {
		return err
	}
	return s.db.SetBlocked(ctx, jidStr, block)
}

// IsBlocked reports whether a JID is on our blocklist.
func (s *Service) IsBlocked(ctx context.Context, jidStr appstore.PeerID) bool {
	c, err := s.db.GetContact(ctx, jidStr)
	return err == nil && c != nil && c.Blocked
}

// ---- Polls -----------------------------------------------------------------

// SendPoll creates a poll message.
func (s *Service) SendPoll(ctx context.Context, chatJID appstore.PeerID, question string, options []string, max int) (*appstore.Message, error) {
	jid, err := s.jid(chatJID)
	if err != nil {
		return nil, err
	}
	if len(options) < 2 {
		return nil, errors.New("poll needs at least two options")
	}
	wm := s.Client().BuildPollCreation(question, options, max)
	resp, err := s.Client().SendMessage(ctx, jid, wm)
	if err != nil {
		return nil, err
	}
	m := &appstore.Message{
		ChatJID: chatJID, ID: resp.ID, SenderJID: s.OwnID(), TS: resp.Timestamp,
		Type: appstore.TPoll, Text: question, PollOptions: options, PollMax: max,
		FromMe: true, Status: appstore.SSent,
	}
	s.persistMessage(ctx, m, jidKind(jid), false)
	return m, nil
}

// VotePoll casts the user's ballot (empty opts = retract).
func (s *Service) VotePoll(ctx context.Context, chatJID appstore.PeerID, poll *appstore.Message, opts []string) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	sender, err := s.jid(poll.SenderJID)
	if err != nil {
		sender = jid
	}
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat: jid, Sender: sender,
			IsFromMe: poll.FromMe, IsGroup: jid.Server == types.GroupServer,
		},
		ID:        types.MessageID(poll.ID),
		Timestamp: poll.TS,
	}
	wm, err := s.Client().BuildPollVote(ctx, info, opts)
	if err != nil {
		return err
	}
	if _, err := s.Client().SendMessage(ctx, jid, wm); err != nil {
		return err
	}
	if err := s.db.VotePoll(ctx, chatJID, poll.ID, s.OwnID(), opts); err == nil {
		s.publishMsgUpdate(ctx, chatJID, poll.ID)
	}
	return nil
}

// PollBallot returns option → voter map for rendering a poll. Aggregate
// "~N" pseudo-voter rows (aggregate counts) aren't real ids and drop out.
func (s *Service) PollBallot(ctx context.Context, chatJID appstore.PeerID, pollID string) (map[string][]appstore.PeerID, error) {
	votes, err := s.db.PollVotes(ctx, chatJID, pollID)
	if err != nil {
		return nil, err
	}
	out := map[string][]appstore.PeerID{}
	for opt, voters := range votes {
		for _, v := range voters {
			if id, err := appstore.ParsePeerID(v); err == nil {
				out[opt] = append(out[opt], id)
			}
		}
	}
	return out, nil
}

// ---- Groups ----------------------------------------------------------------

// CreateGroup creates a group with the given participants — peer ids
// whose wire form may be a JID or bare phone digits normalized to PN JIDs.
func (s *Service) CreateGroup(ctx context.Context, subject string, participants []appstore.PeerID) (*protocol.GroupInfo, error) {
	var jids []types.JID
	for _, p := range participants {
		w := p.Wire()
		if !strings.Contains(w, "@") {
			w = digitsOnly(w)
		}
		j, err := ParseJID(w)
		if err != nil {
			continue
		}
		jids = append(jids, j)
	}
	if len(jids) == 0 {
		return nil, errors.New("no valid participants")
	}
	gi, err := s.Client().CreateGroup(ctx, whatsmeow.ReqCreateGroup{Name: subject, Participants: jids})
	if err != nil {
		return nil, err
	}
	return &protocol.GroupInfo{ID: s.cid(gi.JID.String()), Name: gi.Name}, nil
}

// JoinGroupByLink joins a group via its invite code (wa.me/grp.join/…).
func (s *Service) JoinGroupByLink(ctx context.Context, code string) (types.JID, error) {
	return s.Client().JoinGroupWithLink(ctx, code)
}

// LeaveGroup leaves a group, then records it locally — the chat keeps its
// history but is flagged left (composer off, Delete in the menu) and gets
// a transcript marker.
func (s *Service) LeaveGroup(ctx context.Context, jidStr appstore.PeerID) error {
	jid, err := s.jid(jidStr)
	if err != nil {
		return err
	}
	if err := s.Client().LeaveGroup(ctx, jid); err != nil {
		return err
	}
	s.applyMembership(ctx, jidStr, true, "You left")
	return nil
}

// applyMembership flags the chat left/rejoined and drops a system row so
// the transcript shows it. The RecentSystemText window dedupes the leave
// op racing the server's own "remove" notification, and re-adds racing a
// rejoin — nothing worse than a near-duplicate marker can result.
func (s *Service) applyMembership(ctx context.Context, jidStr appstore.PeerID, left bool, text string) {
	if err := s.db.SetChatLeft(ctx, jidStr, left); err != nil {
		s.log.Warnf("set left flag: %v", err)
	}
	if s.db.RecentSystemText(ctx, jidStr, text, time.Now().Add(-30*time.Second)) {
		s.sink.ChatChanged(jidStr)
		return
	}
	m := &appstore.Message{
		ChatJID: jidStr, ID: fmt.Sprintf("grp-%d", time.Now().UnixNano()),
		TS: time.Now(), Type: appstore.TSystem, Text: text,
	}
	s.persistMessage(ctx, m, appstore.KindGroup, false)
}

// GroupInviteLink returns (or resets) the group's invite link.
func (s *Service) GroupInviteLink(ctx context.Context, jidStr appstore.PeerID, reset bool) (string, error) {
	jid, err := s.jid(jidStr)
	if err != nil {
		return "", err
	}
	return s.Client().GetGroupInviteLink(ctx, jid, reset)
}

// SetGroupName renames a group.
func (s *Service) SetGroupName(ctx context.Context, jidStr appstore.PeerID, name string) error {
	jid, err := s.jid(jidStr)
	if err != nil {
		return err
	}
	return s.Client().SetGroupName(ctx, jid, name)
}

// SetGroupDescription edits the group description.
func (s *Service) SetGroupDescription(ctx context.Context, jidStr appstore.PeerID, desc string) error {
	jid, err := s.jid(jidStr)
	if err != nil {
		return err
	}
	return s.Client().SetGroupDescription(ctx, jid, desc)
}

// UpdateGroupParticipants applies an add/remove/promote/demote.
func (s *Service) UpdateGroupParticipants(ctx context.Context, jidStr appstore.PeerID, participants []appstore.PeerID, action protocol.ParticipantChange) error {
	jid, err := s.jid(jidStr)
	if err != nil {
		return err
	}
	var js []types.JID
	for _, p := range participants {
		j, err := s.jid(p)
		if err == nil {
			js = append(js, j)
		}
	}
	if len(js) == 0 {
		return errors.New("no valid participants")
	}
	var act whatsmeow.ParticipantChange
	switch action {
	case protocol.ParticipantAdd:
		act = whatsmeow.ParticipantChangeAdd
	case protocol.ParticipantRemove:
		act = whatsmeow.ParticipantChangeRemove
	case protocol.ParticipantPromote:
		act = whatsmeow.ParticipantChangePromote
	case protocol.ParticipantDemote:
		act = whatsmeow.ParticipantChangeDemote
	default:
		return fmt.Errorf("unknown participant action %d", action)
	}
	_, err = s.Client().UpdateGroupParticipants(ctx, jid, js, act)
	return err
}

// ---- Account ---------------------------------------------------------------

// LinkedDevices lists JIDs of devices on the account.
func (s *Service) LinkedDevices(ctx context.Context) ([]types.JID, error) {
	if !s.Paired() {
		return nil, errors.New("not paired")
	}
	return s.Client().GetUserDevices(ctx, []types.JID{s.Client().Store.ID.ToNonAD()})
}

// Sessions implements protocol.SessionLister — the account's companion
// devices as display strings.
func (s *Service) Sessions(ctx context.Context) ([]string, error) {
	devs, err := s.LinkedDevices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(devs))
	for _, d := range devs {
		out = append(out, d.String())
	}
	return out, nil
}

// PrivacySettings implements protocol.PrivacyManager — the typed
// settings struct flattened to the dialog's name→value keys.
func (s *Service) PrivacySettings(ctx context.Context) map[string]string {
	p := s.Client().GetPrivacySettings(ctx)
	return map[string]string{
		"last":         string(p.LastSeen),
		"profile":      string(p.Profile),
		"status":       string(p.Status),
		"readreceipts": string(p.ReadReceipts),
		"groupadd":     string(p.GroupAdd),
	}
}

// SetPrivacy changes one privacy setting.
func (s *Service) SetPrivacy(ctx context.Context, name, value string) error {
	_, err := s.Client().SetPrivacySetting(ctx, types.PrivacySettingType(name), types.PrivacySetting(value))
	return err
}

// VerifyCodes returns the E2E verification codes for a peer. The API keys
// identities by LID — PN JIDs resolve through the LID store first.
func (s *Service) VerifyCodes(ctx context.Context, jidStr appstore.PeerID) (*whatsmeow.IdentityVerificationCodes, error) {
	jid, err := s.jid(jidStr)
	if err != nil {
		return nil, err
	}
	if jid.Server == types.DefaultUserServer {
		if lid, err := s.Client().Store.LIDs.GetLIDForPN(ctx, jid); err == nil && !lid.IsEmpty() {
			jid = lid
		}
	}
	return s.Client().GetIdentityVerificationCodes(ctx, jid)
}

// VerifyCode implements protocol.Verifier — the peer's numeric safety code.
func (s *Service) VerifyCode(ctx context.Context, chatID appstore.PeerID) (string, error) {
	codes, err := s.VerifyCodes(ctx, chatID)
	if err != nil {
		return "", err
	}
	if codes == nil {
		return "", errors.New("empty codes")
	}
	return codes.NumericCode, nil
}

// ContactLink implements protocol.ContactLinker — our wa.me profile link.
func (s *Service) ContactLink(ctx context.Context) (string, error) {
	return s.Client().GetContactQRLink(ctx, false)
}

// StatusPrivacy returns who can see our status updates.
func (s *Service) StatusPrivacy(ctx context.Context) ([]types.StatusPrivacy, error) {
	return s.Client().GetStatusPrivacy(ctx)
}

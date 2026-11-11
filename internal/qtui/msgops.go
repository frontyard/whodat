package qtui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/frontyard/whodat/internal/core"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// registerMsgOps adds message-action ops (reactions/edit/revoke/star/delete/
// forward/polls/emoji) — called from registerOps alongside registerChatOps.
func (h *Host) registerMsgOps() {
	h.ops["emojiData"] = h.opEmojiData
	h.ops["noteEmoji"] = h.opNoteEmoji
	h.ops["sendReaction"] = h.opSendReaction
	h.ops["sendStar"] = h.opSendStar
	h.ops["sendEdit"] = h.opSendEdit
	h.ops["sendRevoke"] = h.opSendRevoke
	h.ops["deleteForMe"] = h.opDeleteForMe
	h.ops["forwardTargets"] = h.opForwardTargets
	h.ops["forward"] = h.opForward
	h.ops["sendPoll"] = h.opSendPoll
	h.ops["votePoll"] = h.opVotePoll
}

// recentEmoji is the session-scoped "recently used" picker row (newest first).
var recentEmoji []string

func noteRecent(e string) {
	next := []string{e}
	for _, r := range recentEmoji {
		if r != e {
			next = append(next, r)
		}
	}
	if len(next) > 16 {
		next = next[:16]
	}
	recentEmoji = next
}

// opEmojiData returns picker categories + recents for EmojiPicker.qml.
func (h *Host) opEmojiData(_ *Host, _ json.RawMessage) (any, error) {
	type cat struct {
		Name string `json:"name"`
		Set  string `json:"set"`
	}
	var cats []cat
	for _, c := range core.EmojiCats {
		cats = append(cats, cat{Name: c.Name, Set: c.Set})
	}
	names := map[string]string{}
	for _, c := range core.EmojiCats {
		for _, e := range strings.Fields(c.Set) {
			if n := core.EmojiNames[e]; n != "" {
				names[e] = n
			}
		}
	}
	return map[string]any{"cats": cats, "names": names, "recents": recentEmoji}, nil
}

// opNoteEmoji records a picker choice in the recents row.
func (h *Host) opNoteEmoji(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		Emoji string `json:"emoji"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	noteRecent(a.Emoji)
	return map[string]any{}, nil
}

// msg fetches a stored message for an action op. A missing row is an
// error — callers dereference the message unconditionally.
func (h *Host) msg(ctx context.Context, jid store.PeerID, id string) (*store.Message, error) {
	m, err := h.app.DB.GetMessage(ctx, jid, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, fmt.Errorf("message %s not found", id)
	}
	return m, nil
}

// opSendReaction toggles an emoji on a message — picking the emoji you
// already applied removes it.
// args: {jid, msgId, emoji}
func (h *Host) opSendReaction(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID   store.PeerID `json:"jid"`
		MsgID string       `json:"msgId"`
		Emoji string       `json:"emoji"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, err := h.msg(ctx, a.JID, a.MsgID)
	if err != nil {
		return nil, err
	}
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	// The backend resolves add-vs-remove from the row's reaction state —
	// WhatsApp replaces any prior reaction with the new emoji.
	if err := svc.SendReaction(ctx, a.JID, m, a.Emoji); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opSendStar toggles the starred flag.
// args: {jid, msgId, on}
func (h *Host) opSendStar(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID   store.PeerID `json:"jid"`
		MsgID string       `json:"msgId"`
		On    bool         `json:"on"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	m, err := h.msg(ctx, a.JID, a.MsgID)
	if err != nil {
		return nil, err
	}
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	if err := svc.SendStar(ctx, a.JID, m, a.On); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opSendEdit edits a text message.
// args: {jid, msgId, text}
func (h *Host) opSendEdit(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID   store.PeerID `json:"jid"`
		MsgID string       `json:"msgId"`
		Text  string       `json:"text"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, err := h.msg(ctx, a.JID, a.MsgID)
	if err != nil {
		return nil, err
	}
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	if err := svc.SendEdit(ctx, a.JID, m, a.Text); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opSendRevoke deletes a message for everyone.
// args: {jid, msgId}
func (h *Host) opSendRevoke(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID   store.PeerID `json:"jid"`
		MsgID string       `json:"msgId"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, err := h.msg(ctx, a.JID, a.MsgID)
	if err != nil {
		return nil, err
	}
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	if err := svc.SendRevoke(ctx, a.JID, m); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opDeleteForMe removes the message locally and drops its row.
// args: {jid, msgId}
func (h *Host) opDeleteForMe(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID   store.PeerID `json:"jid"`
		MsgID string       `json:"msgId"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	paths, err := h.app.DB.DeleteMessage(ctx, a.JID, a.MsgID)
	if err != nil {
		return nil, err
	}
	for _, p := range paths {
		_ = os.Remove(p)
	}
	mt(func() { h.msgs.Remove(a.MsgID) })
	return map[string]any{}, nil
}

// opForwardTargets lists chats the forward dialog can pick from.
// args: {exceptJid?} → {chats:[{jid,name,avatarPath,isGroup}]}
func (h *Host) opForwardTargets(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		ExceptJID store.PeerID `json:"exceptJid"`
	}
	_ = json.Unmarshal(args, &a)
	chats, err := h.app.DB.ListChats(context.Background(), false)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, c := range chats {
		if c.JID == a.ExceptJID {
			continue
		}
		out = append(out, map[string]any{
			"jid":        c.JID,
			"name":       h.chatTitle(c),
			"avatarPath": c.AvatarPath,
			"isGroup":    c.Kind == store.KindGroup,
		})
	}
	return map[string]any{"chats": out}, nil
}

// opForward forwards a message to another chat. Same-account targets take
// the protocol's native forward (carries the "forwarded" flag); crossing
// accounts resends the content through the target's normal send path — one
// account's stanza can't ride another's wire connection.
// args: {fromJid, msgId, toJid}
func (h *Host) opForward(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		FromJID store.PeerID `json:"fromJid"`
		MsgID   string       `json:"msgId"`
		ToJID   store.PeerID `json:"toJid"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m, err := h.msg(ctx, a.FromJID, a.MsgID)
	if err != nil {
		return nil, err
	}
	src, err := svcForChat(h.app, a.FromJID)
	if err != nil {
		return nil, err
	}
	dst, err := svcForChat(h.app, a.ToJID)
	if err != nil {
		return nil, err
	}
	if src.Acct() == dst.Acct() {
		if err := dst.Forward(ctx, a.ToJID, m); err != nil {
			return nil, err
		}
		return map[string]any{}, nil
	}
	return map[string]any{}, h.forwardConvert(ctx, src, dst, a.ToJID, m)
}

// forwardConvert resends m's content via dst's send path when the wire
// formats don't match (different account or protocol).
func (h *Host) forwardConvert(ctx context.Context, src, dst protocol.Service, toJID store.PeerID, m *store.Message) error {
	var sent *store.Message
	var err error
	switch m.Type {
	case store.TImage, store.TVideo, store.TAudio, store.TPTT, store.TDocument, store.TSticker:
		path := m.MediaPath
		if path == "" && src.CanFetchMedia(m) {
			if p, ferr := src.FetchMedia(ctx, m); ferr == nil {
				path = p
			}
		}
		if path == "" {
			break // fall back to the caption/text path below
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		mm := *m
		mm.ChatJID = toJID // the row must land under the target chat, not the source's
		mm.MediaPath = path
		mm.Proto = nil      // source-protocol payload mustn't leak into the dst row
		mm.Forwarded = true // copied into the persisted row by SendMedia
		mm.ForwardedFrom = m.SenderName
		sent, err = dst.SendMedia(ctx, toJID, &mm, data)
	case store.TPoll:
		if len(m.PollOptions) > 0 {
			sent, err = dst.SendPoll(ctx, toJID, m.Text, m.PollOptions, m.PollMax)
			break
		}
		fallthrough // poll with no options → send its question as text
	default:
		if strings.TrimSpace(m.Text) == "" {
			return fmt.Errorf("nothing to forward")
		}
		sent, err = dst.SendText(ctx, toJID, m.Text, nil, nil)
	}
	if err != nil {
		return err
	}
	if sent == nil {
		// media fell through to text, or a send path returned no row
		if strings.TrimSpace(m.Text) == "" {
			return fmt.Errorf("nothing to forward")
		}
		if sent, err = dst.SendText(ctx, toJID, m.Text, nil, nil); err != nil {
			return err
		}
	}
	// SendText/SendPoll build fresh rows without the flag — patch it after;
	// the service's insert was already enqueued, so this lands after it on
	// the shared serial writer.
	if !sent.Forwarded {
		_ = h.app.DB.Do(func(ctx context.Context) {
			_ = h.app.DB.SetForwarded(ctx, toJID, sent.ID, m.SenderName)
		})
	}
	return nil
}

// opSendPoll creates a single-select poll.
// args: {jid, question, options:[…]}
func (h *Host) opSendPoll(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID      store.PeerID `json:"jid"`
		Question string       `json:"question"`
		Options  []string     `json:"options"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	var opts []string
	for _, o := range a.Options {
		if o != "" {
			opts = append(opts, o)
		}
	}
	if len(opts) < 2 {
		return nil, fmt.Errorf("a poll needs at least two options")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	m, err := svc.SendPoll(ctx, a.JID, a.Question, opts, 1)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": m.ID}, nil
}

// opVotePoll casts/toggles the user's ballot for an option.
// args: {jid, msgId, option}
func (h *Host) opVotePoll(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID    store.PeerID `json:"jid"`
		MsgID  string       `json:"msgId"`
		Option string       `json:"option"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	m, err := h.msg(ctx, a.JID, a.MsgID)
	if err != nil {
		return nil, err
	}
	votes, _ := h.app.DB.PollVotes(ctx, a.JID, a.MsgID)
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	own := svc.OwnID()
	var opts []string
	mine := false
	for o, vs := range votes {
		for _, v := range vs {
			if v == own.String() {
				opts = append(opts, o)
				if o == a.Option {
					mine = true
				}
			}
		}
	}
	if mine {
		next := opts[:0]
		for _, o := range opts {
			if o != a.Option {
				next = append(next, o)
			}
		}
		opts = next
	} else if m.PollMax == 1 {
		opts = []string{a.Option}
	} else {
		opts = append(opts, a.Option)
	}
	if err := svc.VotePoll(ctx, a.JID, m, opts); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// (end of ops)

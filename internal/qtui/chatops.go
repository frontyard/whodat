package qtui

import (
	"context"
	"encoding/json"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/frontyard/whodat/internal/core"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// registerChatOps adds the session-2 ops to the table (called from registerOps).
func (h *Host) registerChatOps() {
	h.ops["openChat"] = h.opOpenChat
	h.ops["closeChat"] = h.opCloseChat
	h.ops["setFilter"] = h.opSetFilter
	h.ops["setArchivedMode"] = h.opSetArchivedMode
	h.ops["chatMenu"] = h.opChatMenu
	h.ops["chatAction"] = h.opChatAction
	h.ops["jumpTo"] = h.opJumpTo
	h.ops["markRead"] = h.opMarkRead
	h.ops["sendMessage"] = h.opSendMessage
	h.ops["sendTyping"] = h.opSendTyping
	h.ops["chatMembers"] = h.opChatMembers
}

// opOpenChat loads a conversation: seeds the message model, sets header
// state, marks read, subscribes presence.
// args: {jid} → {} — the visible result is state + model reset, not the reply.
func (h *Host) opOpenChat(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID store.PeerID `json:"jid"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	go func() {
		ctx := context.Background()
		c, err := h.app.DB.GetChat(ctx, a.JID)
		if err != nil || c == nil {
			return
		}
		msgs, err := h.app.DB.LatestMessages(ctx, a.JID, pageSize)
		if err != nil {
			return
		}
		_ = h.app.DB.SetUnread(ctx, a.JID, 0)
		h.app.Bus.Publish(protocol.Event{Kind: protocol.EvChatUpdated, ChatJID: a.JID})

		svc := h.app.SvcFor(a.JID)
		isGroup := c.Kind == store.KindGroup
		if !isGroup && svc != nil {
			// Presence subscriptions go to the canonical PN JID — a LID key
			// subscribes nothing.
			_ = svc.SubscribePresence(ctx, h.app.DB.CanonicalJID(ctx, a.JID))
		}
		name := c.Name
		if name == "" {
			name = h.app.NumberOf(ctx, a.JID)
			if svc != nil {
				_ = svc.UpdateChatName(ctx, a.JID)
			}
		}
		sub := h.app.NumberOf(ctx, c.JID)
		if isGroup {
			sub = "Group"
		}
		e2e := h.e2eState(a.JID)
		mt(func() {
			h.msgs.SetChat(a.JID, isGroup)
			h.msgs.seed(msgs)
			h.subBase = sub
			h.setState("chatJID", a.JID)
			h.setState("chatTitle", name)
			h.setState("chatSub", sub)
			h.setState("chatIsGroup", isGroup)
			h.setState("chatLeft", c.Left)
			h.setState("chatAvatarPath", c.AvatarPath)
			h.clearTyping()
			caps := protocol.Caps{}
			if svc != nil {
				caps = svc.Caps()
			}
			h.setState("chatCapsCalls", caps.Calls)
			h.setState("chatCapsGroupCalls", caps.GroupCalls)
			h.setState("chatCapsVideo", caps.VideoCalls)
			h.setState("chatCapsPolls", caps.Polls)
			h.setState("chatCapsStar", caps.Star)
			h.setState("chatCapsReact", caps.Reactions)
			h.setState("chatE2EE", e2e)
			h.markRead(msgs)
		})
	}()
	return map[string]any{}, nil
}

// opCloseChat returns to the placeholder.
func (h *Host) opCloseChat(_ *Host, _ json.RawMessage) (any, error) {
	mt(func() {
		h.msgs.SetChat("", false)
		h.setState("chatJID", "")
		h.setState("chatTitle", "")
		h.setState("chatSub", "")
		h.setState("chatLeft", false)
		h.setState("chatAvatarPath", "")
		h.clearTyping()
		h.setState("chatE2EE", "unknown")
	})
	return map[string]any{}, nil
}

// markRead sends read receipts, grouping rows by their stored ChatJID —
// merged LID/PN chats hold rows keyed to either, and receipts must target
// the real conversation JID. Runs off-thread.
func (h *Host) markRead(msgs []*store.Message) {
	jid := h.msgs.jid
	groups := map[store.PeerID][]*store.Message{}
	for _, m := range msgs {
		if !m.FromMe {
			groups[m.ChatJID] = append(groups[m.ChatJID], m)
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for cj, g := range groups {
			if svc := h.app.SvcFor(cj); svc != nil {
				if err := svc.MarkRead(ctx, cj, g); err != nil {
					log.Printf("markread %s: %v", cj, err)
				}
			}
		}
		_ = h.app.DB.SetUnread(ctx, jid, 0)
		h.app.Bus.Publish(protocol.Event{Kind: protocol.EvChatUpdated, ChatJID: jid})
	}()
}

// opSendMessage sends a text message, optionally quoting another and
// carrying "@token"→peer mention pairs picked in the composer.
// args: {jid, text, quotedId?, mentions?:[{token,id}]}
func (h *Host) opSendMessage(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID      store.PeerID `json:"jid"`
		Text     string       `json:"text"`
		QuotedID string       `json:"quotedId"`
		Mentions []struct {
			Token string       `json:"token"`
			ID    store.PeerID `json:"id"`
		} `json:"mentions"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	var quoted *store.Message
	if a.QuotedID != "" {
		if q, err := h.app.DB.GetMessage(context.Background(), a.JID, a.QuotedID); err == nil {
			quoted = q
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	mentions := make([]protocol.Mention, len(a.Mentions))
	for i, m := range a.Mentions {
		mentions[i] = protocol.Mention{Token: m.Token, ID: m.ID}
	}
	m, err := svc.SendText(ctx, a.JID, a.Text, mentions, quoted)
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": m.ID}, nil
}

// opChatMembers returns the mention-picker entries for a group chat.
// args: {jid} → {members:[{pid,name,sub,token}]}
func (h *Host) opChatMembers(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID store.PeerID `json:"jid"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	empty := map[string]any{"members": []any{}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := h.app.DB.GetChat(ctx, a.JID)
	if err != nil || c == nil || c.Kind != store.KindGroup {
		return empty, nil
	}
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	gi, err := svc.GetGroupInfo(ctx, a.JID)
	if err != nil || gi == nil {
		return nil, err
	}
	var members []map[string]any
	for _, p := range gi.Participants {
		name := h.app.ContactName(ctx, p.ID)
		sub := p.Phone
		if sub == "" {
			sub = h.app.NumberOf(ctx, p.ID)
		}
		if (name == "" || name == sub) && p.Display != "" {
			name = p.Display // anonymous-participant name
		}
		if name == "" {
			name = sub
		}
		if name == "" {
			continue
		}
		token := "@" + name
		if ct, cerr := h.app.DB.GetContact(ctx, p.ID); cerr == nil && ct != nil && ct.Username != "" {
			token = "@" + ct.Username // a real handle links up without a user id
		}
		// Mentions go on the wire in the group's addressing mode — a LID
		// member carried as PN renders "@Unknown" on receiving clients.
		pid := p.WireID
		if pid == "" {
			pid = p.ID
		}
		members = append(members, map[string]any{
			"pid": pid, "name": name, "sub": sub, "token": token,
		})
	}
	sort.SliceStable(members, func(i, j int) bool {
		return strings.ToLower(members[i]["name"].(string)) < strings.ToLower(members[j]["name"].(string))
	})
	return map[string]any{"members": members}, nil
}

// opSendTyping pulses the typing indicator (throttled client-side).
// args: {jid, composing}
func (h *Host) opSendTyping(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID       store.PeerID `json:"jid"`
		Composing bool         `json:"composing"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	if err := svc.SendTyping(ctx, a.JID, a.Composing); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opMarkRead re-triggers receipts for the visible tail — called when the
// window regains focus while a chat is open.
func (h *Host) opMarkRead(_ *Host, _ json.RawMessage) (any, error) {
	mt(func() {
		var tail []*store.Message
		for i := 0; i < len(h.msgs.items) && i < 50; i++ {
			tail = append(tail, h.msgs.items[i].m)
		}
		h.markRead(tail)
	})
	return map[string]any{}, nil
}

// opSetFilter applies the sidebar search text.
// args: {text}
func (h *Host) opSetFilter(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	mt(func() { h.chats.SetFilter(a.Text) })
	return map[string]any{}, nil
}

// opSetArchivedMode toggles the archived-only sidebar view.
// args: {on: bool}
func (h *Host) opSetArchivedMode(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		On bool `json:"on"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	mt(func() { h.chats.SetArchivedMode(a.On) })
	return map[string]any{}, nil
}

// opChatMenu returns the context-menu entries for a chat.
// args: {jid} → {items:[{id,label}]}
func (h *Host) opChatMenu(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID store.PeerID `json:"jid"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	c, err := h.app.DB.GetChat(context.Background(), a.JID)
	if err != nil || c == nil {
		return map[string]any{"items": []any{}}, nil
	}
	var items []map[string]string
	for _, it := range h.app.ChatMenuItems(c) {
		items = append(items, map[string]string{"act": it.ID, "label": it.Label})
	}
	return map[string]any{"items": items}, nil
}

// opChatAction executes a menu action; "info"/"disapp" round-trip back to
// QML for their dialogs.
// args: {jid, id} → {dialog:"info"|"disapp"} when UI-side
func (h *Host) opChatAction(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID store.PeerID `json:"jid"`
		ID  string       `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	c, err := h.app.DB.GetChat(context.Background(), a.JID)
	if err != nil || c == nil {
		return nil, err
	}
	switch a.ID {
	case core.ActChatInfo, core.ActDisapp:
		return map[string]any{"dialog": a.ID, "disappSecs": c.DisappSecs}, nil
	}
	h.app.RunChatAction(c, a.ID)
	return map[string]any{}, nil
}

// opJumpTo pages history until the message is loaded and returns its index.
// args: {id} → {index}
func (h *Host) opJumpTo(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	h.msgs.JumpTo(a.ID, func(idx int) {
		n := h.toastSeq.Add(1)
		mt(func() { h.setState("jumpTo", map[string]any{"n": n, "index": idx}) })
	})
	return map[string]any{"started": true}, nil
}

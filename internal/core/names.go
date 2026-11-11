package core

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/frontyard/whodat/internal/config"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// ChatName resolves a display name for a chat JID.
func (a *App) ChatName(ctx context.Context, jid store.PeerID) string {
	if c, err := a.DB.GetChat(ctx, jid); err == nil && c != nil && c.Name != "" {
		return c.Name
	}
	return a.ContactName(ctx, jid)
}

// peerIDs returns the id-form helper of the service owning jid, or nil
// when the backend has a single id space (no alias semantics).
func (a *App) peerIDs(jid store.PeerID) protocol.PeerIDs {
	if f, ok := a.SvcFor(jid).(protocol.PeerIDs); ok {
		return f
	}
	return nil
}

// ContactName resolves a contact/chat participant JID to its best display
// name, falling back to the real phone number for opaque ids (a WhatsApp
// LID's user part is an identifier, not a number).
func (a *App) ContactName(ctx context.Context, jid store.PeerID) string {
	if n := a.ContactDisplay(ctx, jid); n != "" {
		return n
	}
	alts := a.DB.AltJIDs(ctx, jid)
	if f := a.peerIDs(jid); f != nil {
		if alt := f.AltJID(ctx, jid); alt != "" {
			alts = append(alts, alt)
			if n := a.ContactDisplay(ctx, alt); n != "" {
				return n
			}
			// Prefer real digits over opaque ids — adopt the numbered
			// counterpart's (a LID's PN, or a PN dressed in LID digits).
			if nj := f.NumberJID(ctx, jid); nj != "" {
				return PhoneOf(nj)
			}
		}
	}
	for _, alt := range alts {
		if alt != jid {
			if n := a.ContactDisplay(ctx, alt); n != "" {
				return n
			}
		}
	}
	return a.PeerLabel(jid)
}

// PeerLabel renders an id for display when no name exists: the raw peer id
// when it isn't JID-shaped ("wa2:12345" → "12345"), "+phone" for WhatsApp
// JIDs ("wa:1234@…" → "+1234"). Only JID-shaped tails phone-format — a
// numeric peer id isn't a phone number.
func (a *App) PeerLabel(jid store.PeerID) string {
	tail := jid.Wire()
	if a.Svc(jid.Acct()) != nil && !strings.ContainsRune(tail, '@') {
		return tail
	}
	return PhoneOf(jid)
}

// ReactorName renders one reaction sender for the "who reacted" display:
// "You" for our own JID in either PN or LID form (group reactions key
// senders by LID), otherwise the contact name.
func (a *App) ReactorName(ctx context.Context, jid store.PeerID) string {
	var own store.PeerID
	if svc := a.SvcFor(jid); svc != nil {
		own = svc.OwnID()
	}
	if own == "" {
		return a.ContactName(ctx, jid)
	}
	me := a.DB.AltJIDs(ctx, own)
	if f := a.peerIDs(own); f != nil {
		if alt := f.AltJID(ctx, own); alt != "" {
			me = append(me, alt)
		}
	}
	for _, j := range me {
		if JIDUser(j) == JIDUser(jid) {
			return "You"
		}
	}
	return a.ContactName(ctx, jid)
}

// ReactorAvatar finds a cached profile-pic path for a reaction sender,
// probing the peer's alternate id forms (WhatsApp group reactions key
// senders by LID while avatars cache under the PN id). "" → initials.
func (a *App) ReactorAvatar(ctx context.Context, jid store.PeerID) string {
	forms := append([]store.PeerID{jid}, a.DB.AltJIDs(ctx, jid)...)
	if f := a.peerIDs(jid); f != nil {
		if alt := f.AltJID(ctx, jid); alt != "" {
			forms = append(forms, alt)
		}
	}
	for _, j := range forms {
		if c, err := a.DB.GetContact(ctx, j); err == nil && c != nil && c.AvatarPath != "" {
			return c.AvatarPath
		}
		if c, err := a.DB.GetChat(ctx, j); err == nil && c != nil && c.AvatarPath != "" {
			return c.AvatarPath
		}
		if p := config.AvatarPath(j); p != "" {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

// ContactDisplay returns the stored contact's display name, or "".
func (a *App) ContactDisplay(ctx context.Context, jid store.PeerID) string {
	if c, err := a.DB.GetContact(ctx, jid); err == nil && c != nil {
		if n := c.DisplayName(); n != c.JID.String() {
			return n
		}
	}
	return ""
}

// SenderName picks the display name for a message sender (groups):
// address-book name, then push name captured on the message, then number.
func (a *App) SenderName(ctx context.Context, m *store.Message) string {
	if n := a.ContactDisplay(ctx, m.SenderJID); n != "" {
		return n
	}
	if m.SenderName != "" {
		return m.SenderName
	}
	return a.ContactName(ctx, m.SenderJID)
}

var mentionRe = regexp.MustCompile(`@(\d{5,})`)

// mentionSpans finds "@<digits>" tokens at token boundaries — an address
// like "user@12345" is not a mention.
func mentionSpans(text string) [][2]int {
	var out [][2]int
	for _, loc := range mentionRe.FindAllStringIndex(text, -1) {
		if loc[0] > 0 {
			if c := text[loc[0]-1]; c == '@' || c == '_' ||
				('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9') {
				continue
			}
		}
		out = append(out, [2]int{loc[0], loc[1]})
	}
	return out
}

// mentionName resolves "@<digits>" to "@Name" (or "" to keep the token).
// The digits are the mentioned peer's user part — the owning service
// lists which id forms to probe.
func (a *App) mentionName(ctx context.Context, chatJID store.PeerID, digits string) string {
	var forms []store.PeerID
	if f := a.peerIDs(chatJID); f != nil {
		forms = f.PeerIDForms(digits)
	}
	for _, cand := range forms {
		if name := a.ContactName(ctx, cand); name != "" && name != "+"+digits {
			return "@" + name
		}
	}
	return ""
}

// ResolveMentions rewrites "@<digits>" mention tokens to "@Name" (or the
// real number when no contact name is known).
func (a *App) ResolveMentions(ctx context.Context, chatJID store.PeerID, text string) string {
	spans := mentionSpans(text)
	if len(spans) == 0 {
		return text
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(text[last:sp[0]])
		if name := a.mentionName(ctx, chatJID, text[sp[0]+1:sp[1]]); name != "" {
			b.WriteString(name)
		} else {
			b.WriteString(text[sp[0]:sp[1]])
		}
		last = sp[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

// MentionsHTML renders a text body to bubble HTML: escapes, linkifies
// URLs and resolves "@<digits>" tokens to bold "@Name" mentions.
func (a *App) MentionsHTML(ctx context.Context, chatJID store.PeerID, text string) string {
	var b strings.Builder
	rest := text
	for len(rest) > 0 {
		var ms []int
		if sp := mentionSpans(rest); len(sp) > 0 {
			ms = []int{sp[0][0], sp[0][1]}
		}
		us := urlRe.FindStringIndex(rest)
		var loc []int
		mention := false
		switch {
		case ms == nil && us == nil:
			b.WriteString(Esc(rest))
			return b.String()
		case ms == nil:
			loc = us
		case us == nil || ms[0] < us[0]:
			loc, mention = ms, true
		default:
			loc = us
		}
		b.WriteString(Esc(rest[:loc[0]]))
		tok := rest[loc[0]:loc[1]]
		if mention {
			if name := a.mentionName(ctx, chatJID, tok[1:]); name != "" {
				b.WriteString("<b>" + Esc(name) + "</b>")
			} else {
				b.WriteString(Esc(tok))
			}
		} else {
			b.WriteString(linkURL(tok))
		}
		rest = rest[loc[1]:]
	}
	return b.String()
}

// NumberOf renders a JID as "+phone", resolving opaque ids to their
// numbered counterpart first (a WhatsApp LID to its PN).
func (a *App) NumberOf(ctx context.Context, jid store.PeerID) string {
	if f := a.peerIDs(jid); f != nil {
		if nj := f.NumberJID(ctx, jid); nj != "" {
			return PhoneOf(nj)
		}
	}
	return a.PeerLabel(jid)
}

// MarkChatRead clears the unread badge and sends read receipts.
func (a *App) MarkChatRead(jid store.PeerID) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		msgs, _ := a.DB.LatestMessages(ctx, jid, 200)
		var in []*store.Message
		for _, m := range msgs {
			if !m.FromMe {
				in = append(in, m)
			}
		}
		if svc := a.SvcFor(jid); svc != nil {
			_ = svc.MarkRead(ctx, jid, in)
		}
		_ = a.DB.SetUnread(ctx, jid, 0)
		a.Bus.Publish(protocol.Event{Kind: protocol.EvChatUpdated, ChatJID: jid})
	}()
}

// MarkChatUnread sets a local unread badge without sending receipts.
func (a *App) MarkChatUnread(jid store.PeerID) {
	go func() {
		_ = a.DB.SetUnread(context.Background(), jid, 1)
		a.Bus.Publish(protocol.Event{Kind: protocol.EvChatUpdated, ChatJID: jid})
	}()
}

// RefreshAvatar downloads the JID's profile picture, caches it, updates the DB
// and notifies the UI via the bus. An unchanged picture is a no-op (the
// cached file stays); a changed one lands on a fresh filename so the UI's
// path-keyed image caches re-read it.
func (a *App) RefreshAvatar(ctx context.Context, jid store.PeerID) error {
	if _, busy := a.avInflight.LoadOrStore(jid, struct{}{}); busy {
		return nil
	}
	defer a.avInflight.Delete(jid)

	svc := a.SvcFor(jid)
	if svc == nil {
		return fmt.Errorf("no service for %s", jid)
	}
	url, err := svc.GetProfilePictureURL(ctx, jid)
	if err != nil {
		return err
	}
	if url == "" {
		// An empty path (not an error) means "no picture" — drop the stale
		// cache.
		return a.ClearAvatar(ctx, jid)
	}
	var data []byte
	if strings.HasPrefix(url, "http") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("avatar fetch: %s", resp.Status)
		}
		if data, err = io.ReadAll(io.LimitReader(resp.Body, 8<<20)); err != nil {
			return err
		}
	} else {
		// Local path — protocols that download photos to disk hand back a
		// file location instead of a URL.
		if data, err = os.ReadFile(url); err != nil {
			return err
		}
	}
	c, err := a.DB.GetChat(ctx, jid)
	if err != nil {
		return err
	}
	if c != nil {
		if cur, rerr := os.ReadFile(c.AvatarPath); rerr == nil && bytes.Equal(cur, data) {
			return nil
		}
	}
	dir := config.AvatarAcctDir(jid.Acct())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := config.AvatarPath(jid)
	if c != nil && c.AvatarPath != "" {
		// Re-fetch over an existing picture — a fresh filename, or the
		// image caches keep showing the old file's bytes.
		path = config.AvatarPathVersioned(jid)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	if c == nil {
		// No chat row (own account, group-only participant) — stash the
		// file on the contact row, the fallback slot ReactorAvatar reads.
		return a.DB.UpsertContact(ctx, &store.Contact{JID: jid, AvatarPath: path})
	}
	if c.AvatarPath != "" && c.AvatarPath != path {
		_ = os.Remove(c.AvatarPath)
	}
	if err := a.DB.SetChatAvatar(ctx, jid, path); err != nil {
		return err
	}
	a.Bus.Publish(protocol.Event{Kind: protocol.EvChatUpdated, ChatJID: jid})
	return nil
}

// ClearAvatar drops the cached profile picture for jid (the peer removed
// their picture) and notifies the UI via the bus.
func (a *App) ClearAvatar(ctx context.Context, jid store.PeerID) error {
	c, err := a.DB.GetChat(ctx, jid)
	if err != nil || c == nil || c.AvatarPath == "" {
		return err
	}
	if err := a.DB.SetChatAvatar(ctx, jid, ""); err != nil {
		return err
	}
	_ = os.Remove(c.AvatarPath)
	a.Bus.Publish(protocol.Event{Kind: protocol.EvChatUpdated, ChatJID: jid})
	return nil
}

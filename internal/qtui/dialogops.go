package qtui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	qt "github.com/mappu/miqt/qt6"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/frontyard/whodat/internal/config"
	"github.com/frontyard/whodat/internal/core"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// registerDialogOps adds settings/account/info-dialog ops.
func (h *Host) registerDialogOps() {
	h.ops["getConfig"] = h.opGetConfig
	h.ops["setConfig"] = h.opSetConfig
	h.ops["linkedDevices"] = h.opLinkedDevices
	h.ops["privacyGet"] = h.opPrivacyGet
	h.ops["privacySet"] = h.opPrivacySet
	h.ops["contactQR"] = h.opContactQR
	h.ops["chatInfo"] = h.opChatInfo
	h.ops["starredMessages"] = h.opStarredMessages
	h.ops["verifyCodes"] = h.opVerifyCodes
	h.ops["setDisapp"] = h.opSetDisapp
	h.ops["setBlocked"] = h.opSetBlocked
	h.ops["groupInfo"] = h.opGroupInfo
	h.ops["groupRename"] = h.opGroupRename
	h.ops["groupInvite"] = h.opGroupInvite
	h.ops["groupUpdate"] = h.opGroupUpdate
	h.ops["searchContacts"] = h.opSearchContacts
	h.ops["resolvePeer"] = h.opResolvePeer
	h.ops["openOrCreateChat"] = h.opOpenOrCreateChat
	h.ops["createGroup"] = h.opCreateGroup
	h.ops["windowActive"] = h.opWindowActive
}

// opGetConfig returns the settings dialog's view of the config.
func (h *Host) opGetConfig(_ *Host, _ json.RawMessage) (any, error) {
	p := h.app.Cfg.Snapshot()
	return map[string]any{
		"theme":           p.Theme,
		"notifications":   p.Notifications,
		"closeToTray":     p.CloseToTray,
		"autostart":       p.Autostart,
		"showGroupCalls":  p.ShowGroupCalls,
		"autoDownloadMax": p.AutoDownloadMax,
		"chatStyle":       p.ChatStyle,
	}, nil
}

// opSetConfig persists one settings key and applies the side effects.
// args: {key, value} — keys mirror getConfig.
func (h *Host) opSetConfig(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		Key   string          `json:"key"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	c := h.app.Cfg
	boolean := func(dst *bool) error { return json.Unmarshal(a.Value, dst) }
	var apply func(p *config.Prefs)
	switch a.Key {
	case "theme":
		var v string
		if err := json.Unmarshal(a.Value, &v); err != nil {
			return nil, err
		}
		apply = func(p *config.Prefs) { p.Theme = v }
		mt(func() { h.applyDesktopTheme() })
	case "notifications":
		var v bool
		if err := boolean(&v); err != nil {
			return nil, err
		}
		apply = func(p *config.Prefs) { p.Notifications = v }
	case "closeToTray":
		var v bool
		if err := boolean(&v); err != nil {
			return nil, err
		}
		apply = func(p *config.Prefs) { p.CloseToTray = v }
		mt(func() {
			h.setState("closeToTray", v)
			// Without a tray there's nowhere to close to — keeping the
			// flag off would strand the app windowless after a close.
			if h.tray != nil {
				qt.QGuiApplication_SetQuitOnLastWindowClosed(!v)
			}
		})
	case "autostart":
		var v bool
		if err := json.Unmarshal(a.Value, &v); err != nil {
			return nil, err
		}
		if err := core.SetAutostart(v); err != nil {
			return nil, err
		}
		apply = func(p *config.Prefs) { p.Autostart = v }
	case "showGroupCalls":
		var v bool
		if err := boolean(&v); err != nil {
			return nil, err
		}
		apply = func(p *config.Prefs) { p.ShowGroupCalls = v }
		mt(func() { h.setState("showGroupCalls", v) })
	case "chatStyle":
		var v string
		if err := json.Unmarshal(a.Value, &v); err != nil {
			return nil, err
		}
		if v != "bubbles" && v != "flat" {
			return nil, fmt.Errorf("invalid chatStyle %q", v)
		}
		apply = func(p *config.Prefs) { p.ChatStyle = v }
		mt(func() { h.setState("chatStyle", v) })
	default:
		return nil, fmt.Errorf("unknown config key %q", a.Key)
	}
	if err := c.Update(apply); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// svcForAcct resolves an optional {acct} arg to its service, falling back
// to the primary account — account-scoped ops take the acct the dialog
// was opened for so a non-primary account edits its own settings.
func (h *Host) svcForAcct(args json.RawMessage) (protocol.Service, string, error) {
	var a struct {
		Acct string `json:"acct"`
	}
	if len(args) > 0 {
		_ = json.Unmarshal(args, &a)
	}
	if a.Acct != "" {
		if s := h.app.Svc(a.Acct); s != nil {
			return s, a.Acct, nil
		}
		return nil, a.Acct, fmt.Errorf("no such account")
	}
	s := h.app.Primary()
	if s == nil {
		return nil, "", fmt.Errorf("no account")
	}
	return s, s.Acct(), nil
}

// opLinkedDevices lists the account's sessions/companion devices.
// args: {acct?} → {devices}
func (h *Host) opLinkedDevices(_ *Host, args json.RawMessage) (any, error) {
	svc, acct, err := h.svcForAcct(args)
	if err != nil {
		return nil, err
	}
	l, ok := svc.(protocol.SessionLister)
	if !ok {
		return nil, fmt.Errorf("not supported on %s", acct)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := l.Sessions(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"devices": out}, nil
}

// opPrivacyGet returns the account's privacy dropdown values — gated
// behind Caps.Privacy on the QML side.
func (h *Host) opPrivacyGet(_ *Host, args json.RawMessage) (any, error) {
	svc, acct, err := h.svcForAcct(args)
	if err != nil {
		return nil, err
	}
	m, ok := svc.(protocol.PrivacyManager)
	if !ok {
		return nil, fmt.Errorf("not supported on %s", acct)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p := m.PrivacySettings(ctx)
	out := make(map[string]any, len(p))
	for k, v := range p {
		out[k] = v
	}
	return out, nil
}

// opPrivacySet changes one privacy setting.
// args: {name, value, acct?}
func (h *Host) opPrivacySet(_ *Host, args json.RawMessage) (any, error) {
	svc, acct, err := h.svcForAcct(args)
	if err != nil {
		return nil, err
	}
	m, ok := svc.(protocol.PrivacyManager)
	if !ok {
		return nil, fmt.Errorf("not supported on %s", acct)
	}
	var a struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := m.SetPrivacy(ctx, a.Name, a.Value); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opContactQR renders the personal contact QR to a cache PNG.
// args: {acct?} → {path}
func (h *Host) opContactQR(_ *Host, args json.RawMessage) (any, error) {
	svc, acct, err := h.svcForAcct(args)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	l, ok := svc.(protocol.ContactLinker)
	if !ok {
		return nil, fmt.Errorf("not supported on %s", acct)
	}
	link, err := l.ContactLink(ctx)
	if err != nil {
		return nil, err
	}
	png, err := qrcode.Encode(link, qrcode.Medium, 256)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(config.CacheDir(), "myqr-"+acct+".png")
	if err := os.WriteFile(path, png, 0o600); err != nil {
		return nil, err
	}
	return map[string]any{"path": path}, nil
}

// opChatInfo returns the info dialog's payload for a chat.
// args: {jid} → header + DM extras (about/blocked) — participants come via groupInfo.
func (h *Host) opChatInfo(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID store.PeerID `json:"jid"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx := context.Background()
	c, err := h.app.DB.GetChat(ctx, a.JID)
	if err != nil || c == nil {
		return nil, fmt.Errorf("chat not found")
	}
	starred, _ := h.app.DB.StarredMessages(ctx, a.JID)
	out := map[string]any{
		"jid":          c.JID,
		"name":         h.chatTitle(c),
		"phone":        h.app.NumberOf(ctx, c.JID),
		"avatarPath":   c.AvatarPath,
		"isGroup":      c.Kind == store.KindGroup,
		"disappSecs":   c.DisappSecs,
		"disappText":   core.DisappText(c.DisappSecs),
		"starredCount": len(starred),
	}
	if ct, err := h.app.DB.GetContact(ctx, c.JID); err == nil && ct != nil {
		out["about"] = ct.About
		out["blocked"] = ct.Blocked
	}
	return out, nil
}

// opStarredMessages lists a chat's starred messages for the dialog.
// args: {jid} → {items:[{id,who,text,day}]}
func (h *Host) opStarredMessages(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID store.PeerID `json:"jid"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	msgs, err := h.app.DB.StarredMessages(context.Background(), a.JID)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, m := range msgs {
		who := m.SenderName
		if m.FromMe {
			who = "You"
		} else if who == "" {
			who = h.app.NumberOf(context.Background(), m.SenderJID)
		}
		text := m.Text
		if text == "" {
			text = core.PreviewText(m)
		} else {
			text = h.app.ResolveMentions(context.Background(), m.ChatJID, text)
		}
		out = append(out, map[string]any{
			"id":   m.ID,
			"who":  who,
			"text": text,
			"day":  core.FmtDay(m.TS),
		})
	}
	return map[string]any{"items": out}, nil
}

// opVerifyCodes fetches the E2E safety numbers for a peer.
// args: {jid} → {code}
func (h *Host) opVerifyCodes(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID store.PeerID `json:"jid"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	v, ok := svc.(protocol.Verifier)
	if !ok {
		return nil, fmt.Errorf("encryption verification not supported")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	code, err := v.VerifyCode(ctx, a.JID)
	if err != nil || code == "" {
		if err == nil {
			err = fmt.Errorf("empty codes")
		}
		return nil, err
	}
	return map[string]any{"code": fmtCode(code)}, nil
}

// fmtCode groups a 60-digit safety number like WhatsApp does.
func fmtCode(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 {
			if i%20 == 0 {
				b.WriteByte('\n')
			} else if i%4 == 0 {
				b.WriteByte(' ')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// opSetDisapp applies a disappearing-message timer.
// args: {jid, secs}
func (h *Host) opSetDisapp(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID  store.PeerID `json:"jid"`
		Secs int          `json:"secs"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	if err := svc.SetDisappearing(ctx, a.JID, a.Secs); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opSetBlocked toggles the contact block flag.
// args: {jid, on}
func (h *Host) opSetBlocked(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID store.PeerID `json:"jid"`
		On  bool         `json:"on"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	if err := svc.SetBlocked(ctx, a.JID, a.On); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opGroupInfo fetches participants + our admin flag for the info dialog.
// args: {jid} → {participants:[{jid,phone,name,isAdmin,isMe}],weAdmin}
func (h *Host) opGroupInfo(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID store.PeerID `json:"jid"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	gi, err := svc.GetGroupInfo(ctx, a.JID)
	if err != nil || gi == nil {
		return nil, err
	}
	weAdmin := false
	var parts []map[string]any
	for _, p := range gi.Participants {
		if p.IsMe && p.IsAdmin {
			weAdmin = true
		}
		name := h.app.ContactName(ctx, p.ID)
		num := p.Phone
		if num == "" {
			num = h.app.NumberOf(ctx, p.ID)
		}
		if name == num && p.Display != "" {
			name = p.Display // anonymous-participant name
		}
		parts = append(parts, map[string]any{
			"jid":     p.ID,
			"phone":   num,
			"name":    name,
			"isAdmin": p.IsAdmin,
			"isMe":    p.IsMe,
		})
	}
	return map[string]any{"participants": parts, "weAdmin": weAdmin, "description": gi.Description}, nil
}

// opGroupRename renames a group.
// args: {jid, name}
func (h *Host) opGroupRename(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID  store.PeerID `json:"jid"`
		Name string       `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	if err := svc.SetGroupName(ctx, a.JID, a.Name); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opGroupInvite returns (and copies nowhere — QML copies) the invite link.
// args: {jid} → {link}
func (h *Host) opGroupInvite(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID store.PeerID `json:"jid"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	link, err := svc.GroupInviteLink(ctx, a.JID, false)
	if err != nil {
		return nil, err
	}
	return map[string]any{"link": link}, nil
}

// opGroupUpdate applies an add/remove/promote/demote.
// args: {jid, participant, action:"add"|"remove"|"promote"|"demote"}
func (h *Host) opGroupUpdate(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID         store.PeerID `json:"jid"`
		Participant store.PeerID `json:"participant"`
		Action      string       `json:"action"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	var act protocol.ParticipantChange
	switch a.Action {
	case "add":
		act = protocol.ParticipantAdd
	case "remove":
		act = protocol.ParticipantRemove
	case "promote":
		act = protocol.ParticipantPromote
	case "demote":
		act = protocol.ParticipantDemote
	default:
		return nil, fmt.Errorf("unknown action %q", a.Action)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	svc, err := svcForChat(h.app, a.JID)
	if err != nil {
		return nil, err
	}
	if err := svc.UpdateGroupParticipants(ctx, a.JID, []store.PeerID{a.Participant}, act); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

// opSearchContacts queries the contact store for the new-chat/new-group
// pickers. args: {q, acct?} → {items:[{jid,name,sub}]} — acct keeps the
// group picker inside the chosen account's namespace (a group is created
// on one account only).
func (h *Host) opSearchContacts(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		Q    string `json:"q"`
		Acct string `json:"acct"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx := context.Background()
	// No query cap: the account filter below must see every match — a
	// shared LIMIT lets a larger account's contacts crowd the chosen
	// account's out of the result entirely.
	cts, err := h.app.DB.SearchContacts(ctx, a.Q, 0)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, c := range cts {
		if a.Acct != "" && h.app.AcctOf(c.JID) != a.Acct {
			continue
		}
		name := c.DisplayName()
		if name == c.JID.String() {
			name = h.app.NumberOf(ctx, c.JID)
		}
		sub := h.app.NumberOf(ctx, c.JID)
		if c.Phone != "" {
			sub = "+" + c.Phone
		}
		out = append(out, map[string]any{"jid": c.JID, "name": name, "sub": sub})
		if len(out) == 40 {
			break
		}
	}
	return map[string]any{"items": out}, nil
}

// opResolvePeer resolves a protocol handle (@username, phone) to a chat id
// on the given account. args: {acct, query} → {ok, jid}
func (h *Host) opResolvePeer(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		Acct  string `json:"acct"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	svc := h.app.Svc(a.Acct)
	if svc == nil {
		return map[string]any{"ok": false}, nil
	}
	r, ok := svc.(protocol.PeerResolver)
	if !ok {
		return map[string]any{"ok": false}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	jid, err := r.ResolvePeer(ctx, strings.TrimSpace(a.Query))
	if err != nil {
		return map[string]any{"ok": false, "err": err.Error()}, nil
	}
	return map[string]any{"ok": true, "jid": jid}, nil
}

// opOpenOrCreateChat ensures a chat row exists then reports it back —
// the QML side follows with openChat.
// args: {jid} → {jid}
func (h *Host) opOpenOrCreateChat(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID store.PeerID `json:"jid"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx := context.Background()
	c, err := h.app.DB.GetChat(ctx, a.JID)
	if err != nil {
		return nil, err
	}
	if c == nil {
		c = &store.Chat{JID: a.JID, Kind: store.KindDM, Name: h.app.NumberOf(ctx, a.JID)}
		if err := h.app.DB.UpsertChat(ctx, c); err != nil {
			return nil, err
		}
		h.app.Bus.Publish(protocol.Event{Kind: protocol.EvChatUpdated, ChatJID: a.JID})
	}
	return map[string]any{"jid": a.JID}, nil
}

// opCreateGroup creates a group with the given members.
// args: {name, numbers:[…], acct} → {jid} — members are raw handles;
// each service normalizes its own id form.
func (h *Host) opCreateGroup(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		Name    string `json:"name"`
		Numbers string `json:"numbers"`
		Acct    string `json:"acct"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	svc := h.app.Svc(a.Acct)
	if svc == nil {
		svc = h.app.Primary()
	}
	if svc == nil {
		return nil, fmt.Errorf("no account")
	}
	// Raw handles go to the service — the backend normalizes its own id
	// form (phone digits → JIDs).
	// Picked contacts arrive as store JIDs; CanonicalJID swaps a LID for
	// its phone-number counterpart, the form group ops expect.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var parts []store.PeerID
	for _, p := range strings.Split(a.Numbers, ",") {
		if p = strings.TrimSpace(p); p != "" {
			if id, err := store.ParsePeerID(p); err == nil {
				parts = append(parts, h.app.DB.CanonicalJID(ctx, id))
			}
		}
	}
	if strings.TrimSpace(a.Name) == "" {
		return nil, fmt.Errorf("name the group")
	}
	gi, err := svc.CreateGroup(ctx, strings.TrimSpace(a.Name), parts)
	if err != nil {
		return nil, err
	}
	return map[string]any{"jid": gi.ID}, nil
}

// opWindowActive records window focus for notification suppression.
// args: {active}
func (h *Host) opWindowActive(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		Active bool `json:"active"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	mt(func() { h.setState("windowActive", a.Active) })
	return map[string]any{}, nil
}

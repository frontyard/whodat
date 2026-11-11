package wa

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/frontyard/whodat/internal/linkprev"
	"github.com/frontyard/whodat/internal/media"
	"github.com/frontyard/whodat/internal/protocol"
	appstore "github.com/frontyard/whodat/internal/store"
)

// ParseJID normalises "1234", "1234@s.whatsapp.net", "…@g.us" etc.
func ParseJID(s string) (types.JID, error) {
	if !strings.Contains(s, "@") {
		s += "@" + types.DefaultUserServer
	}
	return types.ParseJID(s)
}

// digitsOnly strips a phone-ish string to its digits.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SendText sends a plain or quoted text message and records it locally.
// Mentions rewrite "@name" composer tokens into "@<digits>" wire form and
// fill ContextInfo.MentionedJID.
func (s *Service) SendText(ctx context.Context, chatJID appstore.PeerID, text string, mentions []protocol.Mention, quoted *appstore.Message) (*appstore.Message, error) {
	jid, err := s.jid(chatJID)
	if err != nil {
		return nil, err
	}
	text, mentioned := s.applyMentions(ctx, text, mentions)
	var ci *waE2E.ContextInfo
	if quoted != nil && quoted.ID != "" {
		ci = &waE2E.ContextInfo{
			StanzaID:      proto.String(quoted.ID),
			Participant:   proto.String(quoted.SenderJID.Wire()),
			QuotedMessage: unmarshalProto(quoted.Proto),
		}
	}
	if len(mentioned) > 0 {
		if ci == nil {
			ci = &waE2E.ContextInfo{}
		}
		ci.MentionedJID = mentioned
	}
	// WhatsApp link previews are generated sender-side: fetch the page's
	// Open Graph data and attach it to an ExtendedTextMessage. Best-effort —
	// a failed fetch must never hold the text itself back.
	raw := linkprev.FirstURL(text)
	var pv *linkprev.Preview
	if raw != "" {
		pv, _ = linkprev.Fetch(ctx, raw)
	}
	var wm *waE2E.Message
	if ci != nil || pv != nil {
		ext := &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: ci}
		if pv != nil {
			ext.MatchedText = proto.String(raw)
			ext.Title = proto.String(pv.Title)
			ext.Description = proto.String(pv.Desc)
			ext.JPEGThumbnail = pv.Thumb
			ext.PreviewType = waE2E.ExtendedTextMessage_NONE.Enum()
		}
		wm = &waE2E.Message{ExtendedTextMessage: ext}
	} else {
		wm = &waE2E.Message{Conversation: proto.String(text)}
	}
	resp, err := s.Client().SendMessage(ctx, jid, wm)
	if err != nil {
		return nil, err
	}
	m := &appstore.Message{
		ChatJID: chatJID, ID: resp.ID, SenderJID: s.OwnID(), TS: resp.Timestamp,
		Type: appstore.TText, Text: text, FromMe: true, Status: appstore.SSent,
	}
	if quoted != nil {
		m.QuotedID, m.QuotedSender, m.QuotedText = quoted.ID, quoted.SenderJID, quoted.Text
	}
	if pv != nil {
		m.Preview = &appstore.LinkPreview{
			URL: pv.URL, Title: pv.Title, Desc: pv.Desc, Site: pv.Site,
		}
		if len(pv.Thumb) > 0 {
			if path, err := media.StageBytes(s.acct, pv.Thumb, ".jpg"); err == nil {
				m.Preview.Thumb = path
			}
		}
	}
	if blob, merr := proto.Marshal(wm); merr == nil {
		m.Proto = blob
	}
	s.persistMessage(ctx, m, jidKind(jid), false)
	return m, nil
}

var waMentionRe = regexp.MustCompile(`@(\d{5,})`)

// applyMentions rewrites composer "@name" tokens to "@<digits>" wire form
// and returns the MentionedJID list — each target plus its LID/PN
// counterpart, so the notification resolves whichever id form the group
// addresses. Hand-typed "@digits" tokens get the same treatment.
func (s *Service) applyMentions(ctx context.Context, text string, mentions []protocol.Mention) (string, []string) {
	seen := map[string]bool{}
	var jids []string
	add := func(j types.JID) {
		if j.IsEmpty() || seen[j.String()] {
			return
		}
		seen[j.String()] = true
		jids = append(jids, j.String())
	}
	// Longest token first — "@Alina B" must claim its span before "@Alina"
	// matches inside it. Rewritten spans can't re-match later tokens.
	ms := append([]protocol.Mention(nil), mentions...)
	sort.SliceStable(ms, func(i, j int) bool { return len(ms[i].Token) > len(ms[j].Token) })
	orig := text
	for _, m := range ms {
		tj, err := s.jid(m.ID)
		if err != nil || tj.User == "" {
			continue
		}
		spans := protocol.TokenSpans(text, m.Token)
		if len(spans) == 0 {
			continue // the composer token was edited away
		}
		text = replaceSpans(text, spans, "@"+tj.User)
		add(tj)
		add(s.altJID(ctx, tj))
	}
	// Hand-typed "@digits" — scanned on the ORIGINAL text, not the
	// rewritten one: a picked "@name" rewritten to "@<lid digits>" would
	// otherwise mint "<lid digits>@s.whatsapp.net", a phantom PN that
	// claims the same span as the real LID and renders "@Unknown" on
	// receiving clients. Typed digits that name a known LID keep the LID
	// form for the same reason; anything else is assumed to be a PN.
	for _, dm := range waMentionRe.FindAllStringSubmatch(orig, -1) {
		d := dm[1]
		lid := types.NewJID(d, types.HiddenUserServer)
		if c := s.Client(); c != nil && c.Store != nil && c.Store.LIDs != nil {
			if _, err := c.Store.LIDs.GetPNForLID(ctx, lid); err == nil {
				add(lid)
				continue
			}
		}
		pn := types.NewJID(d, types.DefaultUserServer)
		add(pn)
		add(s.altJID(ctx, pn))
	}
	return text, jids
}

// replaceSpans swaps the given text spans for wire.
func replaceSpans(text string, spans [][2]int, wire string) string {
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(text[last:sp[0]])
		b.WriteString(wire)
		last = sp[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

func unmarshalProto(blob []byte) *waE2E.Message {
	if len(blob) == 0 {
		return nil
	}
	m := &waE2E.Message{}
	if err := proto.Unmarshal(blob, m); err != nil {
		return nil
	}
	return m
}

// SendReaction toggles an emoji on a message: the row's reaction state
// decides whether this sends the emoji or retracts our existing one
// (WhatsApp is one-reaction-per-user; "" at the wire means remove).
func (s *Service) SendReaction(ctx context.Context, chatJID appstore.PeerID, target *appstore.Message, emoji string) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	sender, err := s.jid(target.SenderJID)
	if err != nil {
		sender = jid
	}
	wire := emoji
	if emoji != "" {
		for _, r := range target.Reactions[emoji] {
			if r == s.OwnID() {
				wire = ""
				break
			}
		}
	}
	wm := s.Client().BuildReaction(jid, sender, target.ID, wire)
	if _, err := s.Client().SendMessage(ctx, jid, wm); err != nil {
		return err
	}
	// Mirror locally so the UI updates immediately.
	s.applyReaction(ctx, chatJID, &msgAction{
		kind: actReact, targetID: target.ID, emoji: wire, senderJID: s.OwnID(),
	})
	return nil
}

// SendEdit rewrites one of our own messages.
func (s *Service) SendEdit(ctx context.Context, chatJID appstore.PeerID, target *appstore.Message, newText string) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	if !target.FromMe {
		return errors.New("can only edit own messages")
	}
	newContent := &waE2E.Message{Conversation: proto.String(newText)}
	wm := s.Client().BuildEdit(jid, target.ID, newContent)
	if _, err := s.Client().SendMessage(ctx, jid, wm); err != nil {
		return err
	}
	if err := s.db.ApplyEdit(ctx, chatJID, target.ID, newText); err == nil {
		// The wire edit carries no preview fields — drop the local card
		// when the link it belonged to left the text.
		if target.Preview != nil && !strings.Contains(newText, target.Preview.URL) {
			_ = s.db.SetLinkPreview(ctx, chatJID, target.ID, nil)
		}
		s.publishMsgUpdate(ctx, chatJID, target.ID)
	}
	return nil
}

// SendRevoke deletes a message for everyone (own) — server decides eligibility.
func (s *Service) SendRevoke(ctx context.Context, chatJID appstore.PeerID, target *appstore.Message) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	if _, err := s.Client().RevokeMessage(ctx, jid, target.ID); err != nil {
		return err
	}
	if err := s.db.SetRevoked(ctx, chatJID, target.ID); err == nil {
		if target.MediaPath != "" {
			_ = os.Remove(target.MediaPath)
		}
		if target.MediaThumb != "" {
			_ = os.Remove(target.MediaThumb)
		}
		s.publishMsgUpdate(ctx, chatJID, target.ID)
	}
	return nil
}

// Forward resends a stored message into another chat. Media is re-uploaded
// when we still have the file — stored CDN directPaths expire, so replaying
// the original proto produces undeliverable media. The quote context is
// stripped either way: a forwarded bubble must not drag its old reply chain.
func (s *Service) Forward(ctx context.Context, targetChatJID appstore.PeerID, m *appstore.Message) error {
	jid, err := s.jid(targetChatJID)
	if err != nil {
		return err
	}
	var wm *waE2E.Message
	if m.MediaPath != "" {
		if data, rerr := os.ReadFile(m.MediaPath); rerr == nil {
			if up, uerr := s.Client().Upload(ctx, data, mediaTypeFor(m.Type)); uerr == nil {
				wm = buildMediaMessage(m, &up)
				markForwarded(wm)
			}
		}
	}
	if wm == nil {
		if orig := unmarshalProto(m.Proto); orig != nil {
			wm = orig
			stripQuoteContext(wm)
			markForwarded(wm)
		} else {
			wm = &waE2E.Message{Conversation: proto.String(m.Text)}
		}
	}
	resp, err := s.Client().SendMessage(ctx, jid, wm)
	if err != nil {
		return err
	}
	// Own sends aren't echoed back to this session — persist the forwarded
	// copy or the forward looks like a silent no-op in the target chat.
	row := *m
	row.ChatJID = targetChatJID
	row.ID = resp.ID
	row.SenderJID = s.OwnID()
	row.TS = resp.Timestamp
	row.FromMe = true
	row.Status = appstore.SSent
	row.Forwarded = true
	row.ForwardedFrom = m.SenderName // WA's wire flag carries no origin — we know the source
	row.QuotedID, row.QuotedSender, row.QuotedText = "", "", ""
	row.Reactions = nil
	row.Edited, row.Revoked, row.Starred, row.ViewOnce = false, false, false, false
	row.ExpiresAt = time.Time{}
	if blob, merr := proto.Marshal(wm); merr == nil {
		row.Proto = blob
	}
	s.persistMessage(ctx, &row, jidKind(jid), false)
	return nil
}

// contextInfoOf returns the embeddable message's ContextInfo slot for the
// same set of types markForwarded/stripQuoteContext walk.
func contextInfoOf(wm *waE2E.Message) (get func() *waE2E.ContextInfo, set func(*waE2E.ContextInfo)) {
	switch {
	case wm.GetExtendedTextMessage() != nil:
		m := wm.GetExtendedTextMessage()
		return m.GetContextInfo, func(c *waE2E.ContextInfo) { m.ContextInfo = c }
	case wm.GetImageMessage() != nil:
		m := wm.GetImageMessage()
		return m.GetContextInfo, func(c *waE2E.ContextInfo) { m.ContextInfo = c }
	case wm.GetVideoMessage() != nil:
		m := wm.GetVideoMessage()
		return m.GetContextInfo, func(c *waE2E.ContextInfo) { m.ContextInfo = c }
	case wm.GetAudioMessage() != nil:
		m := wm.GetAudioMessage()
		return m.GetContextInfo, func(c *waE2E.ContextInfo) { m.ContextInfo = c }
	case wm.GetDocumentMessage() != nil:
		m := wm.GetDocumentMessage()
		return m.GetContextInfo, func(c *waE2E.ContextInfo) { m.ContextInfo = c }
	case wm.GetStickerMessage() != nil:
		m := wm.GetStickerMessage()
		return m.GetContextInfo, func(c *waE2E.ContextInfo) { m.ContextInfo = c }
	}
	return nil, nil
}

func markForwarded(wm *waE2E.Message) {
	get, set := contextInfoOf(wm)
	if set == nil {
		return
	}
	ctx := get()
	if ctx == nil {
		ctx = &waE2E.ContextInfo{}
	}
	ctx.IsForwarded = proto.Bool(true)
	set(ctx)
}

// stripQuoteContext removes reply-chain fields so a forward doesn't carry
// its original quoted message/embedded metadata into the new chat.
func stripQuoteContext(wm *waE2E.Message) {
	get, set := contextInfoOf(wm)
	if set == nil {
		return
	}
	ctx := get()
	if ctx == nil {
		return
	}
	ctx.StanzaID = nil
	ctx.Participant = nil
	ctx.QuotedMessage = nil
	ctx.QuotedAd = nil
	set(ctx)
}

// mediaTypeFor maps a stored message type to the upload media class.
func mediaTypeFor(t appstore.MsgType) whatsmeow.MediaType {
	switch t {
	case appstore.TImage:
		return whatsmeow.MediaImage
	case appstore.TVideo:
		return whatsmeow.MediaVideo
	case appstore.TAudio, appstore.TPTT:
		return whatsmeow.MediaAudio
	default:
		return whatsmeow.MediaDocument
	}
}

// SendMedia uploads payload and sends the appropriate media message.
func (s *Service) SendMedia(ctx context.Context, chatJID appstore.PeerID, m *appstore.Message, data []byte) (*appstore.Message, error) {
	jid, err := s.jid(chatJID)
	if err != nil {
		return nil, err
	}
	up, err := s.Client().Upload(ctx, data, mediaTypeFor(m.Type))
	if err != nil {
		return nil, err
	}
	wm := buildMediaMessage(m, &up)
	resp, err := s.Client().SendMessage(ctx, jid, wm)
	if err != nil {
		return nil, err
	}
	m.ID = resp.ID
	m.TS = resp.Timestamp
	m.SenderJID = s.OwnID()
	m.FromMe = true
	m.Status = appstore.SSent
	if blob, merr := proto.Marshal(wm); merr == nil {
		m.Proto = blob
	}
	s.persistMessage(ctx, m, jidKind(jid), false)
	return m, nil
}

func buildMediaMessage(m *appstore.Message, up *whatsmeow.UploadResponse) *waE2E.Message {
	caption := proto.String(m.Text)
	switch m.Type {
	case appstore.TImage:
		return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
			Caption: caption, Mimetype: proto.String(m.MediaMIME),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: &up.FileLength,
			Width:      proto.Uint32(uint32(m.Width)), Height: proto.Uint32(uint32(m.Height)),
		}}
	case appstore.TVideo:
		return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
			Caption: caption, Mimetype: proto.String(m.MediaMIME),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: &up.FileLength,
			Width:      proto.Uint32(uint32(m.Width)), Height: proto.Uint32(uint32(m.Height)),
			Seconds: proto.Uint32(uint32(m.Duration)),
		}}
	case appstore.TAudio, appstore.TPTT:
		return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
			Mimetype: proto.String(m.MediaMIME),
			URL:      &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: &up.FileLength,
			Seconds:    proto.Uint32(uint32(m.Duration)),
			PTT:        proto.Bool(m.Type == appstore.TPTT),
			Waveform:   m.Waveform,
		}}
	case appstore.TSticker:
		return &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
			Mimetype: proto.String(m.MediaMIME),
			URL:      &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: &up.FileLength,
			Width:      proto.Uint32(uint32(m.Width)), Height: proto.Uint32(uint32(m.Height)),
		}}
	default: // document
		return &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
			Caption: caption, Mimetype: proto.String(m.MediaMIME),
			FileName: proto.String(m.MediaName), Title: proto.String(m.MediaName),
			URL: &up.URL, DirectPath: &up.DirectPath, MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256,
			FileLength: &up.FileLength,
		}}
	}
}

// MarkRead sends read receipts for the given message IDs.
func (s *Service) MarkRead(ctx context.Context, chatJID appstore.PeerID, msgs []*appstore.Message) error {
	if len(msgs) == 0 {
		return nil
	}
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	ids := make([]types.MessageID, 0, len(msgs))
	var latest time.Time
	sender := jid
	for _, m := range msgs {
		if m.FromMe {
			continue
		}
		ids = append(ids, m.ID)
		if m.TS.After(latest) {
			latest = m.TS
			if m.SenderJID != "" {
				if sj, err := s.jid(m.SenderJID); err == nil {
					sender = sj
				}
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return s.Client().MarkRead(ctx, ids, latest, jid, sender)
}

// SendTyping publishes composing/paused state for a chat.
func (s *Service) SendTyping(ctx context.Context, chatJID appstore.PeerID, composing bool) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	state := types.ChatPresencePaused
	if composing {
		state = types.ChatPresenceComposing
	}
	return s.Client().SendChatPresence(ctx, jid, state, types.ChatPresenceMediaText)
}

// SubscribePresence asks the server for a contact's online status (DMs only).
func (s *Service) SubscribePresence(ctx context.Context, jidStr appstore.PeerID) error {
	jid, err := s.jid(jidStr)
	if err != nil {
		return err
	}
	return s.Client().SubscribePresence(ctx, jid)
}

// SetChatPresence marks the app globally online/offline.
func (s *Service) SetPresence(ctx context.Context, online bool) error {
	p := types.PresenceUnavailable
	if online {
		p = types.PresenceAvailable
	}
	return s.Client().SendPresence(ctx, p)
}

// GetProfilePictureURL fetches the full-size avatar URL for a JID.
func (s *Service) GetProfilePictureURL(ctx context.Context, jidStr appstore.PeerID) (string, error) {
	jid, err := s.jid(jidStr)
	if err != nil {
		return "", err
	}
	info, err := s.Client().GetProfilePictureInfo(ctx, jid, &whatsmeow.GetProfilePictureParams{Preview: false})
	if err != nil {
		return "", err
	}
	if info == nil {
		return "", errors.New("no picture")
	}
	return info.URL, nil
}

// GetGroupInfo fetches group metadata in normalized form — the participant
// id prefers the real number over the LID (group ops address the PN).
// WireID always carries the group's addressing-mode jid (mentions must
// use that form or receiving clients can't match them to the roster).
func (s *Service) GetGroupInfo(ctx context.Context, jidStr appstore.PeerID) (*protocol.GroupInfo, error) {
	jid, err := s.jid(jidStr)
	if err != nil {
		return nil, err
	}
	gi, err := s.Client().GetGroupInfo(ctx, jid)
	if err != nil {
		return nil, err
	}
	out := &protocol.GroupInfo{ID: s.cid(gi.JID.String()), Name: gi.Name, Description: gi.Topic}
	var ownUser string
	if s.Client().Store.ID != nil {
		ownUser = s.Client().Store.ID.ToNonAD().User
	}
	for _, p := range gi.Participants {
		id := p.JID
		if !p.PhoneNumber.IsEmpty() {
			id = p.PhoneNumber // real number, not the LID
		}
		out.Participants = append(out.Participants, protocol.GroupParticipant{
			ID:      s.cid(id.String()),
			Phone:   "+" + id.User,
			Display: p.DisplayName,
			IsAdmin: p.IsAdmin || p.IsSuperAdmin,
			IsMe:    id.User == ownUser && ownUser != "",
			WireID:  s.cid(p.JID.String()),
		})
	}
	return out, nil
}

// IsOnWhatsApp checks whether phone numbers are registered.
func (s *Service) IsOnWhatsApp(ctx context.Context, phones []string) (map[string]bool, error) {
	res, err := s.Client().IsOnWhatsApp(ctx, phones)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(res))
	for _, r := range res {
		out[r.Query] = r.IsIn
	}
	return out, nil
}

// UpdateChatName resolves a contact/group display name into our DB.
func (s *Service) UpdateChatName(ctx context.Context, chatJID appstore.PeerID) error {
	jid, err := s.jid(chatJID)
	if err != nil {
		return err
	}
	if jid.Server == types.GroupServer {
		gi, err := s.Client().GetGroupInfo(ctx, jid)
		if err != nil {
			return err
		}
		return s.setChatName(ctx, chatJID, appstore.KindGroup, gi.Name)
	}
	name := contactDisplayName(s.db.GetContact(ctx, chatJID))
	if name == "" {
		// DM keys can be PN or LID depending on which side initiated —
		// try the counterpart via the session's LID map.
		if alt := s.altJID(ctx, jid); !alt.IsEmpty() {
			name = contactDisplayName(s.db.GetContact(ctx, s.cid(alt.String())))
		}
	}
	if name == "" {
		return nil
	}
	return s.setChatName(ctx, chatJID, appstore.KindDM, name)
}

func contactDisplayName(c *appstore.Contact, err error) string {
	if err != nil || c == nil {
		return ""
	}
	if dn := c.DisplayName(); dn != c.JID.String() {
		return dn
	}
	return ""
}

func (s *Service) setChatName(ctx context.Context, chatJID appstore.PeerID, kind appstore.ChatKind, name string) error {
	if err := s.db.UpsertChat(ctx, &appstore.Chat{JID: chatJID, Kind: kind, Name: name}); err != nil {
		return err
	}
	s.sink.ChatChanged(chatJID)
	return nil
}

// altJID maps a JID between its PN and LID forms via the session LID map.
func (s *Service) altJID(ctx context.Context, jid types.JID) types.JID {
	c := s.Client()
	if c == nil || c.Store == nil || c.Store.LIDs == nil {
		return types.JID{}
	}
	lids := c.Store.LIDs
	if jid.Server == types.HiddenUserServer {
		if alt, err := lids.GetPNForLID(ctx, jid); err == nil {
			return alt
		}
		return types.JID{}
	}
	if alt, err := lids.GetLIDForPN(ctx, jid); err == nil && !alt.IsEmpty() {
		return alt
	}
	// Some nodes carry a LID's digits under @s.whatsapp.net; probe as LID too.
	lidJID := types.NewJID(jid.User, types.HiddenUserServer)
	if alt, err := lids.GetPNForLID(ctx, lidJID); err == nil {
		return alt
	}
	return types.JID{}
}

// ---- protocol.PeerIDs --------------------------------------------------------

// PeerIDForms lists the JID forms a bare user part may appear under —
// the PN and LID servers — so display code can probe both id spaces.
func (s *Service) PeerIDForms(user string) []appstore.PeerID {
	return []appstore.PeerID{
		s.cid(types.NewJID(user, types.DefaultUserServer).String()),
		s.cid(types.NewJID(user, types.HiddenUserServer).String()),
	}
}

// AltJID implements protocol.PeerIDs — the peer's LID↔PN counterpart as
// a string, "" when unknown.
func (s *Service) AltJID(ctx context.Context, jidStr appstore.PeerID) appstore.PeerID {
	jid, err := s.jid(jidStr)
	if err != nil {
		return ""
	}
	return s.cid(s.altJID(ctx, jid).String())
}

// NumberJID implements protocol.PeerIDs — the id form carrying the real
// phone number: the PN for a LID, itself for a PN, "" otherwise.
func (s *Service) NumberJID(ctx context.Context, jidStr appstore.PeerID) appstore.PeerID {
	jid, err := s.jid(jidStr)
	if err != nil {
		return ""
	}
	if jid.Server == types.DefaultUserServer {
		return s.cid(jid.String())
	}
	if jid.Server == types.HiddenUserServer {
		if alt := s.altJID(ctx, jid); alt.Server == types.DefaultUserServer {
			return s.cid(alt.String())
		}
	}
	return ""
}

// ResolvePeer implements protocol.PeerResolver — a phone number becomes a
// chat JID when it's registered on WhatsApp.
func (s *Service) ResolvePeer(ctx context.Context, query string) (appstore.PeerID, error) {
	num := digitsOnly(query)
	if num == "" {
		return "", errors.New("no digits in query")
	}
	res, err := s.IsOnWhatsApp(ctx, []string{num})
	if err != nil {
		return "", err
	}
	if !res[num] {
		return "", fmt.Errorf("%s is not on WhatsApp", num)
	}
	return s.cid(types.NewJID(num, types.DefaultUserServer).String()), nil
}

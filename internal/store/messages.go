package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// MsgType classifies how a message row is rendered.
type MsgType string

const (
	TText     MsgType = "text"
	TImage    MsgType = "image"
	TVideo    MsgType = "video"
	TAudio    MsgType = "audio"
	TPTT      MsgType = "ptt" // voice note
	TDocument MsgType = "document"
	TSticker  MsgType = "sticker"
	TLocation MsgType = "location"
	TContact  MsgType = "contact"
	TCall     MsgType = "call"
	TPoll     MsgType = "poll"
	TSystem   MsgType = "system" // group events, crypto notices
	TRevoked  MsgType = "revoked"
)

// NotifyFallback is the notification body for messages without text.
func NotifyFallback(t MsgType) string {
	switch t {
	case TImage:
		return "Photo"
	case TVideo:
		return "Video"
	case TAudio:
		return "Audio"
	case TPTT:
		return "Voice message"
	case TDocument:
		return "Document"
	case TSticker:
		return "Sticker"
	case TLocation:
		return "Location"
	case TContact:
		return "Contact"
	case TCall:
		return "Call"
	case TPoll:
		return "Poll"
	case TRevoked:
		return "Message deleted"
	default:
		return "Message"
	}
}

// MsgStatus tracks outgoing delivery state.
type MsgStatus string

const (
	SNone      MsgStatus = ""
	SPending   MsgStatus = "pending"
	SSent      MsgStatus = "sent"
	SDelivered MsgStatus = "delivered"
	SPlayed    MsgStatus = "played"
	SRead      MsgStatus = "read"
	SFailed    MsgStatus = "failed"
)

// Message is a single rendered row.
type Message struct {
	ChatJID       PeerID
	ID            string
	SenderJID     PeerID
	SenderName    string
	TS            time.Time
	Type          MsgType
	Text          string
	MediaPath     string
	MediaMIME     string
	MediaName     string // original filename (documents)
	MediaSize     int64
	Duration      int // seconds, audio/video
	Width         int
	Height        int
	QuotedID      string
	QuotedSender  PeerID
	QuotedText    string
	FromMe        bool
	Status        MsgStatus
	Edited        bool
	Revoked       bool
	Starred       bool
	ViewOnce      bool                // received "view once" media
	ExpiresAt     time.Time           // disappearing-message deadline (zero = permanent)
	PollOptions   []string            // TPoll: option names in order
	PollMax       int                 // TPoll: max selectable options (0 = any)
	Reactions     map[string][]PeerID // emoji -> sender JIDs
	Waveform      []byte              // voice-note peak amplitudes
	Proto         []byte              // protocol-owned payload (WA: serialized waE2E.Message; TG: download ref)
	HTML          string              // pre-rendered rich text (TG entities); empty = derive from Text
	Forwarded     bool                // arrived via a forward, or is our forwarded copy
	ForwardedFrom string              // origin name when the protocol exposes it (TG) or it's our copy
	Preview       *LinkPreview        // link-preview card attached to the text

	RowID int64 // sqlite rowid — pagination tie-breaker, never persisted by callers
}

// LinkPreview is the normalized "link card" attached to a text message.
// WhatsApp delivers sender-resolved title/description/thumbnail inline.
type LinkPreview struct {
	URL     string          `json:"url"`
	Title   string          `json:"title,omitempty"`
	Desc    string          `json:"desc,omitempty"`
	Site    string          `json:"site,omitempty"`
	Thumb   string          `json:"thumb,omitempty"`   // local file path once downloaded
	Ref     json.RawMessage `json:"ref,omitempty"`     // protocol-owned thumb download locator (TG photo ref)
	Large   bool            `json:"large,omitempty"`   // banner-style card (TG large media)
	Pending bool            `json:"pending,omitempty"` // server is still resolving the page
}

// pollJSON is the on-disk shape of poll_options.
type pollJSON struct {
	O []string `json:"o,omitempty"`
	M int      `json:"m,omitempty"`
}

const msgCols = `chat_jid,id,sender_jid,sender_name,ts,type,text,media_path,media_mime,
media_name,media_size,duration,width,height,quoted_id,quoted_sender,quoted_text,
from_me,status,edited,revoked,starred,view_once,expires_at,poll_options,reactions,waveform,proto,html,forwarded,forwarded_from,
link_preview,preview_url`

// msgSelCols prefixes rowid for SELECTs (scanMessage reads it first).
const msgSelCols = `rowid,` + msgCols

func scanMessage(row interface{ Scan(...any) error }) (*Message, error) {
	var m Message
	var typ, status string
	var ts int64
	var fromMe, edited, revoked, starred, viewOnce, forwarded int
	var expiresAt sql.NullInt64
	var reactions, pollOpts, linkPrev, prevURL string
	err := row.Scan(&m.RowID, &m.ChatJID, &m.ID, &m.SenderJID, &m.SenderName, &ts, &typ, &m.Text,
		&m.MediaPath, &m.MediaMIME, &m.MediaName, &m.MediaSize, &m.Duration, &m.Width, &m.Height,
		&m.QuotedID, &m.QuotedSender, &m.QuotedText, &fromMe, &status, &edited, &revoked,
		&starred, &viewOnce, &expiresAt, &pollOpts, &reactions, &m.Waveform, &m.Proto, &m.HTML, &forwarded,
		&m.ForwardedFrom, &linkPrev, &prevURL)
	if err != nil {
		return nil, err
	}
	m.TS = time.Unix(ts, 0)
	m.Type, m.Status = MsgType(typ), MsgStatus(status)
	m.FromMe, m.Edited, m.Revoked, m.Starred, m.ViewOnce = fromMe != 0, edited != 0, revoked != 0, starred != 0, viewOnce != 0
	m.Forwarded = forwarded != 0
	if expiresAt.Valid {
		m.ExpiresAt = time.Unix(expiresAt.Int64, 0)
	}
	if pollOpts != "" {
		var pj pollJSON
		if json.Unmarshal([]byte(pollOpts), &pj) == nil {
			m.PollOptions, m.PollMax = pj.O, pj.M
		}
	}
	if reactions != "" {
		_ = json.Unmarshal([]byte(reactions), &m.Reactions)
	}
	if linkPrev != "" {
		var pv LinkPreview
		if json.Unmarshal([]byte(linkPrev), &pv) == nil {
			if pv.URL == "" {
				pv.URL = prevURL
			}
			m.Preview = &pv
		}
	}
	if m.Revoked {
		m.Type = TRevoked
	}
	return &m, nil
}

func msgVals(m *Message) []any {
	reactions, _ := json.Marshal(m.Reactions)
	var pollOpts []byte
	if len(m.PollOptions) > 0 {
		pollOpts, _ = json.Marshal(pollJSON{O: m.PollOptions, M: m.PollMax})
	}
	if m.Revoked {
		m.Type = TRevoked
	}
	var expires any
	if !m.ExpiresAt.IsZero() {
		expires = m.ExpiresAt.Unix()
	}
	var linkPrev, prevURL string
	if m.Preview != nil {
		if b, err := json.Marshal(m.Preview); err == nil {
			linkPrev, prevURL = string(b), m.Preview.URL
		}
	}
	return []any{m.ChatJID, m.ID, m.SenderJID, m.SenderName, m.TS.Unix(), string(m.Type), m.Text,
		m.MediaPath, m.MediaMIME, m.MediaName, m.MediaSize, m.Duration, m.Width, m.Height,
		m.QuotedID, m.QuotedSender, m.QuotedText, m.FromMe, string(m.Status),
		m.Edited, m.Revoked, m.Starred, m.ViewOnce, expires, string(pollOpts), string(reactions), m.Waveform, m.Proto, m.HTML, m.Forwarded, m.ForwardedFrom,
		linkPrev, prevURL}
}

// statusRank maps a status value to its monotonic progression rank. Used in
// upserts so echoes/resyncs can never downgrade read → delivered → sent.
// 'failed' ranks lowest: a later 'sent' echo proves the message did go out.
const statusRank = `(CASE %s WHEN 'read' THEN 5 WHEN 'played' THEN 4 WHEN 'delivered' THEN 3 WHEN 'sent' THEN 2 WHEN 'pending' THEN 1 ELSE 0 END)`

// keepStatusRank is the ON CONFLICT assignment that never downgrades status.
var keepStatusRank = fmt.Sprintf(`status=CASE WHEN %s >= %s THEN excluded.status ELSE messages.status END`,
	fmt.Sprintf(statusRank, "excluded.status"),
	fmt.Sprintf(statusRank, "messages.status"))

// insertSQL is shared by InsertMessage and its Tx variant — keep the two
// from drifting again.
var insertSQL = `
INSERT INTO messages (` + msgCols + `)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(chat_jid,id) DO UPDATE SET
  text=excluded.text, type=excluded.type, edited=excluded.edited,
  html=CASE WHEN excluded.html!='' THEN excluded.html ELSE messages.html END,
  revoked=excluded.revoked, from_me=excluded.from_me,
  sender_jid=CASE WHEN excluded.sender_jid!='' THEN excluded.sender_jid ELSE messages.sender_jid END,
  sender_name=CASE WHEN excluded.sender_name!='' THEN excluded.sender_name ELSE messages.sender_name END,
  quoted_id=excluded.quoted_id, quoted_sender=excluded.quoted_sender, quoted_text=excluded.quoted_text,
  media_mime=excluded.media_mime, media_name=excluded.media_name, media_size=excluded.media_size,
  duration=excluded.duration, width=excluded.width, height=excluded.height,
  poll_options=excluded.poll_options, view_once=excluded.view_once, expires_at=excluded.expires_at,
  waveform=CASE WHEN excluded.waveform IS NOT NULL THEN excluded.waveform ELSE messages.waveform END,
  ` + keepStatusRank + `,
  reactions=CASE WHEN messages.reactions IN ('','null','{}') THEN excluded.reactions ELSE messages.reactions END,
  media_path=CASE WHEN excluded.media_path!='' THEN excluded.media_path ELSE messages.media_path END,
  proto=CASE WHEN excluded.proto IS NOT NULL THEN excluded.proto ELSE messages.proto END,
  forwarded=MAX(messages.forwarded, excluded.forwarded),
  forwarded_from=CASE WHEN excluded.forwarded_from!='' THEN excluded.forwarded_from ELSE messages.forwarded_from END,
  link_preview=CASE
    WHEN excluded.link_preview='' THEN messages.link_preview
    WHEN messages.link_preview='' OR json_extract(excluded.link_preview,'$.thumb') IS NOT NULL
      THEN excluded.link_preview
    ELSE json_set(excluded.link_preview,'$.thumb',json_extract(messages.link_preview,'$.thumb')) END,
  preview_url=CASE WHEN excluded.preview_url!='' THEN excluded.preview_url ELSE messages.preview_url END
WHERE NOT (messages.revoked=1 AND excluded.revoked=0)`

// InsertMessage stores a message; conflict = update mutable fields.
func (d *DB) InsertMessage(ctx context.Context, m *Message) error {
	_, err := d.sql.ExecContext(ctx, insertSQL, msgVals(m)...)
	return err
}

// insertMessageTx is the Tx variant used by history sync batches.
func InsertMessageTx(ctx context.Context, tx *sql.Tx, m *Message) error {
	_, err := tx.ExecContext(ctx, insertSQL, msgVals(m)...)
	return err
}

// TouchChatTx is the Tx variant of TouchChat.
func TouchChatTx(ctx context.Context, tx *sql.Tx, jid PeerID, preview string, ts time.Time, incUnread bool) error {
	inc := 0
	if incUnread {
		inc = 1
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE chats SET last_msg_ts=?, last_msg_preview=?, unread=unread+? WHERE jid=?`,
		ts.Unix(), preview, inc, jid)
	return err
}

// EnsureChatTx is the Tx variant of EnsureChat.
func EnsureChatTx(ctx context.Context, tx *sql.Tx, jid PeerID, kind ChatKind) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO chats (jid,kind) VALUES (?,?) ON CONFLICT(jid) DO NOTHING`, jid, string(kind))
	return err
}

// GetMessage fetches one message or nil.
func (d *DB) GetMessage(ctx context.Context, chatJID PeerID, id string) (*Message, error) {
	clause, args := d.colIN(ctx, "chat_jid", chatJID)
	m, err := scanMessage(d.sql.QueryRowContext(ctx,
		`SELECT `+msgSelCols+` FROM messages WHERE `+clause+` AND id=?`, append(args, id)...))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

// MessagesBefore returns up to limit messages before the (ts, rowid) keyset,
// newest first. Same-second timestamps no longer swallow rows — rowid breaks
// the tie. Pass math.MaxInt64 as beforeRowID for the first page.
func (d *DB) MessagesBefore(ctx context.Context, chatJID PeerID, before time.Time, beforeRowID int64, limit int) ([]*Message, error) {
	clause, args := d.colIN(ctx, "chat_jid", chatJID)
	// Disappearing messages past their deadline never render — the sweep
	// deletes the rows/files, but the filter is the hard guarantee.
	now := time.Now().Unix()
	rows, err := d.sql.QueryContext(ctx, `
SELECT `+msgSelCols+` FROM messages WHERE `+clause+`
AND (expires_at IS NULL OR expires_at>?)
AND (ts<? OR (ts=? AND rowid<?))
ORDER BY ts DESC, rowid DESC LIMIT ?`,
		append(args, now, before.Unix(), before.Unix(), beforeRowID, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// scanMessages drains a message query into rows (newest first).
func scanMessages(rows *sql.Rows) ([]*Message, error) {
	var out []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// LatestMessages returns the newest `limit` messages (newest first). No
// upper time bound — a skewed sender clock beyond the old now+1h horizon
// used to hide the newest rows entirely.
func (d *DB) LatestMessages(ctx context.Context, chatJID PeerID, limit int) ([]*Message, error) {
	clause, args := d.colIN(ctx, "chat_jid", chatJID)
	now := time.Now().Unix()
	rows, err := d.sql.QueryContext(ctx, `
SELECT `+msgSelCols+` FROM messages WHERE `+clause+`
AND (expires_at IS NULL OR expires_at>?)
ORDER BY ts DESC, rowid DESC LIMIT ?`,
		append(args, now, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// GetMessageByProto fetches the message carrying this proto payload —
// poll/media ids that protocol updates address instead of the message id.
func (d *DB) GetMessageByProto(ctx context.Context, chatJID PeerID, proto []byte) (*Message, error) {
	clause, args := d.colIN(ctx, "chat_jid", chatJID)
	m, err := scanMessage(d.sql.QueryRowContext(ctx,
		`SELECT `+msgSelCols+` FROM messages WHERE `+clause+` AND proto=?`,
		append(args, proto)...))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

// FindRecentCall returns the newest call-log row in the chat after `after`
// matching direction — used to dedupe locally-written call rows against
// server call-log sync events.
func (d *DB) FindRecentCall(ctx context.Context, chatJID PeerID, after time.Time, fromMe bool) (*Message, error) {
	clause, args := d.colIN(ctx, "chat_jid", chatJID)
	m, err := scanMessage(d.sql.QueryRowContext(ctx,
		`SELECT `+msgSelCols+` FROM messages WHERE `+clause+` AND type=? AND ts>=? AND from_me=?
ORDER BY ts DESC, rowid DESC LIMIT 1`,
		append(args, string(TCall), after.Unix(), fromMe)...))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

// msgWhere builds `chat_jid IN (alts) AND id=?` with args for chatJID + id.
func (d *DB) msgWhere(ctx context.Context, chatJID PeerID, id string) (string, []any) {
	return msgWhereAlts(d.AltJIDs(ctx, chatJID), id)
}

// msgWhereAlts is msgWhere with the alt-JID list already resolved — Tx
// variants resolve alts through the transaction.
func msgWhereAlts(alts []PeerID, id string) (string, []any) {
	if len(alts) == 1 {
		return "chat_jid=? AND id=?", []any{alts[0], id}
	}
	return "chat_jid IN (?,?) AND id=?", []any{alts[0], alts[1], id}
}

// SetStatus updates delivery state on an outgoing message.
func (d *DB) SetStatus(ctx context.Context, chatJID PeerID, id string, status MsgStatus) error {
	clause, args := d.msgWhere(ctx, chatJID, id)
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET status=? WHERE `+clause+` AND from_me=1`, append([]any{status}, args...)...)
	return err
}

// SetRevoked marks a message revoked (delete-for-everyone) and clears its
// body, media references, waveform, proto payload, and reactions — nothing
// of the deleted content may linger in the DB. The caller unlinks the file
// on disk (it knows the old MediaPath).
func (d *DB) SetRevoked(ctx context.Context, chatJID PeerID, id string) error {
	clause, args := d.msgWhere(ctx, chatJID, id)
	_, err := d.sql.ExecContext(ctx, `
UPDATE messages SET revoked=1, type=?, text='', media_path='', media_mime='',
  media_name='', media_size=0, duration=0, width=0, height=0,
  waveform=NULL, proto=NULL, reactions='', html='', link_preview='', preview_url=''
WHERE `+clause, append([]any{string(TRevoked)}, args...)...)
	return err
}

// ApplyEdit rewrites a message's body after a MESSAGE_EDIT protocol event.
func (d *DB) ApplyEdit(ctx context.Context, chatJID PeerID, id, newText string) error {
	clause, args := d.msgWhere(ctx, chatJID, id)
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET text=?, edited=1 WHERE `+clause, append([]any{newText}, args...)...)
	return err
}

// SetReactions replaces the reactions JSON for a message.
func (d *DB) SetReactions(ctx context.Context, chatJID PeerID, id string, reactions map[string][]PeerID) error {
	clause, args := d.msgWhere(ctx, chatJID, id)
	data, _ := json.Marshal(reactions)
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET reactions=? WHERE `+clause, append([]any{string(data)}, args...)...)
	return err
}

// SetMediaPath stores the local path once media has been downloaded.
func (d *DB) SetMediaPath(ctx context.Context, chatJID PeerID, id, path string) error {
	clause, args := d.msgWhere(ctx, chatJID, id)
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET media_path=? WHERE `+clause, append([]any{path}, args...)...)
	return err
}

// DeleteMessage removes a row entirely (local delete). Returns media paths
// for the caller to unlink — a deleted message's file must not linger on
// disk (same contract as ClearChatMessages).
func (d *DB) DeleteMessage(ctx context.Context, chatJID PeerID, id string) ([]string, error) {
	clause, args := d.msgWhere(ctx, chatJID, id)
	paths := d.mediaPathsIn(ctx, clause, args)
	_, err := d.sql.ExecContext(ctx, `DELETE FROM messages WHERE `+clause, args...)
	return paths, err
}

// SetStarred toggles the star flag on a message (own actions and synced
// Star app-state events share this).
func (d *DB) SetStarred(ctx context.Context, chatJID PeerID, id string, starred bool) error {
	clause, args := d.msgWhere(ctx, chatJID, id)
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET starred=? WHERE `+clause, append([]any{starred}, args...)...)
	return err
}

// SetForwarded marks a row as a forwarded copy — cross-protocol resends
// persist through SendText, whose wire send can't carry the flag.
func (d *DB) SetForwarded(ctx context.Context, chatJID PeerID, id, from string) error {
	clause, args := d.msgWhere(ctx, chatJID, id)
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET forwarded=1, forwarded_from=? WHERE `+clause,
		append([]any{from}, args...)...)
	return err
}

// SetLinkPreview replaces a row's link card (webpage-resolution updates,
// edits that add or drop a link); nil clears it.
func (d *DB) SetLinkPreview(ctx context.Context, chatJID PeerID, id string, pv *LinkPreview) error {
	clause, args := d.msgWhere(ctx, chatJID, id)
	var data, u string
	if pv != nil {
		if b, err := json.Marshal(pv); err == nil {
			data, u = string(b), pv.URL
		}
	}
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET link_preview=?, preview_url=? WHERE `+clause,
		append([]any{data, u}, args...)...)
	return err
}

// SetPreviewThumb records the downloaded card image inside the preview JSON.
func (d *DB) SetPreviewThumb(ctx context.Context, chatJID PeerID, id, path string) error {
	clause, args := d.msgWhere(ctx, chatJID, id)
	_, err := d.sql.ExecContext(ctx,
		`UPDATE messages SET link_preview=json_set(link_preview,'$.thumb',?)
WHERE `+clause+` AND link_preview!=''`, append([]any{path}, args...)...)
	return err
}

// MessagesByPreviewURL returns rows whose link card points at url — the
// updateWebPage match path, scoped to one account's chat namespace.
func (d *DB) MessagesByPreviewURL(ctx context.Context, acct, url string) ([]*Message, error) {
	rows, err := d.sql.QueryContext(ctx,
		`SELECT `+msgSelCols+` FROM messages WHERE preview_url=? AND chat_jid LIKE ?`,
		url, acct+":%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessages(rows)
}

// mediaPathsIn returns media and preview-thumbnail paths of every message
// matching the clause — callers unlink them after deletion.
func (d *DB) mediaPathsIn(ctx context.Context, clause string, args []any) []string {
	all := append(append([]any{}, args...), args...)
	rows, err := d.sql.QueryContext(ctx,
		`SELECT media_path FROM messages WHERE media_path!='' AND `+clause+`
UNION ALL
SELECT CASE WHEN json_valid(link_preview) THEN json_extract(link_preview,'$.thumb') END
FROM messages WHERE json_valid(link_preview) AND `+clause,
		all...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil && p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ClearChatMessages deletes a chat's message history but keeps the chat row
// (WhatsApp's "Clear chat"). Returns media paths for the caller to unlink.
func (d *DB) ClearChatMessages(ctx context.Context, jid PeerID) ([]string, error) {
	clause, args := d.colIN(ctx, "chat_jid", jid)
	paths := d.mediaPathsIn(ctx, clause, args)
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM messages WHERE `+clause, args...); err != nil {
		return nil, err
	}
	chatClause, chatArgs := d.colIN(ctx, "poll_chat", jid)
	if _, err := d.sql.ExecContext(ctx, `DELETE FROM poll_votes WHERE `+chatClause, chatArgs...); err != nil {
		return paths, err
	}
	if err := d.SetUnread(ctx, jid, 0); err != nil {
		return paths, err
	}
	jidClause, jidArgs := d.colIN(ctx, "jid", jid)
	_, err := d.sql.ExecContext(ctx,
		`UPDATE chats SET last_msg_preview='' WHERE `+jidClause, jidArgs...)
	return paths, err
}

// DeleteChatRows removes the whole conversation (messages, votes, chat row).
// Returns media paths for the caller to unlink.
func (d *DB) DeleteChatRows(ctx context.Context, jid PeerID) ([]string, error) {
	paths, err := d.ClearChatMessages(ctx, jid)
	if err != nil {
		return paths, err
	}
	clause, args := d.colIN(ctx, "jid", jid)
	_, err = d.sql.ExecContext(ctx, `DELETE FROM chats WHERE `+clause, args...)
	return paths, err
}

// DeleteExpired removes messages whose disappearing deadline has passed.
// Returns media paths for the caller to unlink. Rows deleted here must also
// be filtered in queries — a sweep isn't guaranteed to have run yet.
func (d *DB) DeleteExpired(ctx context.Context, now time.Time) ([]string, error) {
	paths := d.mediaPathsIn(ctx, `expires_at IS NOT NULL AND expires_at<=?`, []any{now.Unix()})
	res, err := d.sql.ExecContext(ctx,
		`DELETE FROM messages WHERE expires_at IS NOT NULL AND expires_at<=?`, now.Unix())
	if err != nil {
		return paths, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return paths, nil
	}
	return nil, nil
}

// RecentSystemText reports whether a TSystem row with this exact text exists
// in the chat since `since` — dedupes repeated crypto/identity notices that
// re-fire on every connect.
func (d *DB) RecentSystemText(ctx context.Context, chatJID PeerID, text string, since time.Time) bool {
	clause, args := d.colIN(ctx, "chat_jid", chatJID)
	var n int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM messages WHERE `+clause+` AND type=? AND text=? AND ts>=?`,
		append(args, string(TSystem), text, since.Unix())...).Scan(&n)
	return err == nil && n > 0
}

// HasRealMessages reports whether the chat holds at least one non-system
// row — a conversation consisting solely of crypto notices and decrypt
// placeholders never carried real traffic.
func (d *DB) HasRealMessages(ctx context.Context, chatJID PeerID) bool {
	clause, args := d.colIN(ctx, "chat_jid", chatJID)
	var n int
	err := d.sql.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM messages WHERE `+clause+` AND type!=?)`,
		append(args, string(TSystem))...).Scan(&n)
	return err == nil && n != 0
}

// StarredMessages returns a chat's starred messages, oldest first.
func (d *DB) StarredMessages(ctx context.Context, chatJID PeerID) ([]*Message, error) {
	clause, args := d.colIN(ctx, "chat_jid", chatJID)
	rows, err := d.sql.QueryContext(ctx,
		`SELECT `+msgSelCols+` FROM messages WHERE `+clause+` AND starred=1 ORDER BY ts ASC, rowid ASC`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return out, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Poll votes ----------------------------------------------------------------

// VotePoll replaces a voter's ballot (empty opts = retract).
func (d *DB) VotePoll(ctx context.Context, chatJID PeerID, pollID string, voter PeerID, opts []string) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM poll_votes WHERE poll_chat=? AND poll_id=? AND voter=?`,
			chatJID, pollID, voter); err != nil {
			return err
		}
		for _, o := range opts {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO poll_votes (poll_chat,poll_id,voter,opt_name) VALUES (?,?,?,?)`,
				chatJID, pollID, voter, o); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetPollCounts replaces a poll's ballot with aggregate server counts —
// a backend that reports only totals stores N pseudo-voter rows
// per option; the owner's own choice lands under its real id so the UI can
// mark it. mine is the list of chosen option names.
func (d *DB) SetPollCounts(ctx context.Context, chatJID PeerID, pollID string, counts map[string]int, mine []string, own PeerID) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM poll_votes WHERE poll_chat=? AND poll_id=?`, chatJID, pollID); err != nil {
			return err
		}
		for opt, n := range counts {
			for i := 0; i < n; i++ {
				voter := fmt.Sprintf("~%d", i)
				if i == 0 {
					for _, mo := range mine {
						if mo == opt && own != "" {
							voter = own.String()
							break
						}
					}
				}
				if _, err := tx.ExecContext(ctx,
					`INSERT OR REPLACE INTO poll_votes (poll_chat,poll_id,voter,opt_name) VALUES (?,?,?,?)`,
					chatJID, pollID, voter, opt); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// PollVotes returns option name → voter JIDs for a poll message.
func (d *DB) PollVotes(ctx context.Context, chatJID PeerID, pollID string) (map[string][]string, error) {
	clause, args := d.colIN(ctx, "poll_chat", chatJID)
	rows, err := d.sql.QueryContext(ctx,
		`SELECT opt_name,voter FROM poll_votes WHERE `+clause+` AND poll_id=?`,
		append(args, pollID)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var o, v string
		if err := rows.Scan(&o, &v); err != nil {
			return nil, err
		}
		out[o] = append(out[o], v)
	}
	return out, rows.Err()
}

// Transaction variants — history sync applies edits/revokes/reactions inside
// the ingest transaction. They take a pre-resolved alt-JID list because the
// alt_jids lookup itself must run through the transaction.

// GetMessageTx fetches one message inside tx (alts = AltJIDsTx result), or nil.
func (d *DB) GetMessageTx(ctx context.Context, tx *sql.Tx, alts []PeerID, id string) (*Message, error) {
	clause, args := msgWhereAlts(alts, id)
	m, err := scanMessage(tx.QueryRowContext(ctx,
		`SELECT `+msgSelCols+` FROM messages WHERE `+clause, args...))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

// SetRevokedTx is SetRevoked inside tx.
func (d *DB) SetRevokedTx(ctx context.Context, tx *sql.Tx, alts []PeerID, id string) error {
	clause, args := msgWhereAlts(alts, id)
	_, err := tx.ExecContext(ctx, `
UPDATE messages SET revoked=1, type=?, text='', media_path='', media_mime='',
  media_name='', media_size=0, duration=0, width=0, height=0,
  waveform=NULL, proto=NULL, reactions='', html='', link_preview='', preview_url=''
WHERE `+clause, append([]any{string(TRevoked)}, args...)...)
	return err
}

// SetLinkPreviewTx is SetLinkPreview inside tx.
func (d *DB) SetLinkPreviewTx(ctx context.Context, tx *sql.Tx, alts []PeerID, id string, pv *LinkPreview) error {
	clause, args := msgWhereAlts(alts, id)
	var data, u string
	if pv != nil {
		if b, err := json.Marshal(pv); err == nil {
			data, u = string(b), pv.URL
		}
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE messages SET link_preview=?, preview_url=? WHERE `+clause,
		append([]any{data, u}, args...)...)
	return err
}

// ApplyEditTx is ApplyEdit inside tx.
func (d *DB) ApplyEditTx(ctx context.Context, tx *sql.Tx, alts []PeerID, id, newText string) error {
	clause, args := msgWhereAlts(alts, id)
	_, err := tx.ExecContext(ctx,
		`UPDATE messages SET text=?, edited=1 WHERE `+clause, append([]any{newText}, args...)...)
	return err
}

// SetReactionsTx is SetReactions inside tx.
func (d *DB) SetReactionsTx(ctx context.Context, tx *sql.Tx, alts []PeerID, id string, reactions map[string][]PeerID) error {
	clause, args := msgWhereAlts(alts, id)
	data, _ := json.Marshal(reactions)
	_, err := tx.ExecContext(ctx,
		`UPDATE messages SET reactions=? WHERE `+clause, append([]any{string(data)}, args...)...)
	return err
}

// FoldReaction moves sender's reaction to emoji on the reactions map —
// one reaction per sender, latest wins (an empty emoji is a removal).
// Mutates and returns the map; shared by the live, history-ingest, and
// poll-update paths so the fold can't drift.
func FoldReaction(reactions map[string][]PeerID, sender PeerID, emoji string) map[string][]PeerID {
	if reactions == nil {
		reactions = map[string][]PeerID{}
	}
	for e, senders := range reactions {
		next := senders[:0]
		for _, s := range senders {
			if s != sender {
				next = append(next, s)
			}
		}
		if len(next) == 0 {
			delete(reactions, e)
		} else {
			reactions[e] = next
		}
	}
	if emoji != "" {
		reactions[emoji] = append(reactions[emoji], sender)
	}
	return reactions
}

// ChatIDs returns all known chat JIDs (e.g. for avatar prefetch).
func (d *DB) ChatIDs(ctx context.Context) ([]PeerID, error) {
	rows, err := d.sql.QueryContext(ctx, `SELECT jid FROM chats`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PeerID
	for rows.Next() {
		var j PeerID
		if err := rows.Scan(&j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// ChatKind classifies a conversation.
type ChatKind string

const (
	KindDM         ChatKind = "dm"
	KindGroup      ChatKind = "group"
	KindBroadcast  ChatKind = "broadcast"
	KindNewsletter ChatKind = "newsletter"
)

// Chat is a sidebar row.
type Chat struct {
	RowID          int64 // sqlite rowid — creation order, sort tie-breaker, never persisted by callers
	JID            PeerID
	Kind           ChatKind
	Name           string
	AvatarPath     string
	LastMsgTS      time.Time
	LastMsgPreview string
	Unread         int
	Pinned         bool
	Archived       bool
	Muted          bool
	DisappSecs     int  // disappearing-message timer (0 = off)
	Left           bool // group we've left/been removed from (or that was deleted)
}

// Contact is an address-book / push-name record.
type Contact struct {
	JID          PeerID
	PushName     string
	FullName     string
	BusinessName string
	AvatarPath   string
	About        string // "about" status line
	Blocked      bool
	Username     string // protocol handle, when the peer has one
	Phone        string // real phone digits when the peer shares them
}

// DisplayName picks the best available name. PushName (what the person
// calls themselves) wins over the address-book name — a saved name can be
// stale or wrong, e.g. an own second account filed under a real name.
func (c *Contact) DisplayName() string {
	if c.PushName != "" {
		return c.PushName
	}
	if c.FullName != "" {
		return c.FullName
	}
	if c.BusinessName != "" {
		return c.BusinessName
	}
	if c.Username != "" {
		return "@" + c.Username
	}
	return string(c.JID)
}

func scanChat(row interface{ Scan(...any) error }) (*Chat, error) {
	var c Chat
	var kind string
	var ts int64
	var pinned, archived, muted, disapp, leftg int
	err := row.Scan(&c.RowID, &c.JID, &kind, &c.Name, &c.AvatarPath, &ts,
		&c.LastMsgPreview, &c.Unread, &pinned, &archived, &muted, &disapp, &leftg)
	if err != nil {
		return nil, err
	}
	c.Kind = ChatKind(kind)
	c.LastMsgTS = time.Unix(ts, 0)
	c.Pinned, c.Archived, c.Muted = pinned != 0, archived != 0, muted != 0
	c.DisappSecs = disapp
	c.Left = leftg != 0
	return &c, nil
}

const chatCols = `jid,kind,name,avatar_path,last_msg_ts,last_msg_preview,unread,pinned,archived,muted,disapp_secs,left_group`

// chatColsJoined resolves the display name: contact names win (address-book
// names and push names beat stale number fallbacks); chats.name is the
// fallback for groups and non-contacts.
const chatColsJoined = `c.rowid,c.jid,c.kind,
COALESCE(NULLIF(ct.full_name,''),NULLIF(ct.push_name,''),NULLIF(ct.business_name,''),NULLIF(c.name,''),''),
c.avatar_path,c.last_msg_ts,c.last_msg_preview,c.unread,c.pinned,c.archived,c.muted,c.disapp_secs,c.left_group`

const chatsJoined = ` FROM chats c LEFT JOIN contacts ct ON ct.jid=c.jid`

// ListChats returns chats for the sidebar: pinned first, then most-recent
// activity (last_msg_ts), rowid breaking ties. LID and PN chats belonging
// to the same person (per alt_jids) are merged. DM rows surface only once
// they've carried a real (non-system) message — a chat built solely out of
// crypto notices or decrypt placeholders is a phantom, not a conversation.
// Pinned and unread flags are deliberate user marks and always show; groups
// are memberships, listed regardless.
func (d *DB) ListChats(ctx context.Context, includeArchived bool) ([]*Chat, error) {
	q := `SELECT ` + chatColsJoined + chatsJoined + ` WHERE `
	if !includeArchived {
		q += `c.archived=0 AND `
	}
	q += `(c.kind!=? OR c.pinned=1 OR c.unread>0 OR EXISTS(
		SELECT 1 FROM messages m WHERE m.chat_jid=c.jid AND m.type!=?))`
	q += ` ORDER BY c.pinned DESC, c.last_msg_ts DESC, c.rowid DESC`
	rows, err := d.sql.QueryContext(ctx, q, string(KindDM), string(TSystem))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Chat
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return d.mergeByCanonical(ctx, out), nil
}

// GetChat fetches one chat or nil; LID/PN pairs merge into a single view.
func (d *DB) GetChat(ctx context.Context, jid PeerID) (*Chat, error) {
	clause, args := d.jidIN(ctx, jid)
	rows, err := d.sql.QueryContext(ctx,
		`SELECT `+chatColsJoined+chatsJoined+` WHERE `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var in []*Chat
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		in = append(in, c)
	}
	merged := d.mergeByCanonical(ctx, in)
	if len(merged) == 0 {
		return nil, nil
	}
	return merged[0], nil
}

// UpsertChat inserts or updates mutable chat fields, preserving counters.
func (d *DB) UpsertChat(ctx context.Context, c *Chat) error {
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO chats (jid,kind,name,avatar_path) VALUES (?,?,?,?)
ON CONFLICT(jid) DO UPDATE SET
  kind=CASE WHEN excluded.kind!='' THEN excluded.kind ELSE chats.kind END,
  name=CASE WHEN excluded.name!='' THEN excluded.name ELSE chats.name END,
  avatar_path=CASE WHEN excluded.avatar_path!='' THEN excluded.avatar_path ELSE chats.avatar_path END`,
		c.JID, string(c.Kind), c.Name, c.AvatarPath)
	return err
}

// TouchChat updates the last-message summary shown in the sidebar.
func (d *DB) TouchChat(ctx context.Context, jid PeerID, preview string, ts time.Time, incUnread bool) error {
	inc := 0
	if incUnread {
		inc = 1
	}
	_, err := d.sql.ExecContext(ctx, `
UPDATE chats SET last_msg_ts=?, last_msg_preview=?, unread=unread+? WHERE jid=?`,
		ts.Unix(), preview, inc, jid)
	return err
}

// TotalUnread sums unread across unmuted, unarchived chats (badge count).
func (d *DB) TotalUnread(ctx context.Context) (int, error) {
	var n int
	err := d.sql.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(unread),0) FROM chats WHERE muted=0 AND archived=0`).Scan(&n)
	return n, err
}

// EnsureChat inserts a stub chat row if none exists.
func (d *DB) EnsureChat(ctx context.Context, jid PeerID, kind ChatKind) error {
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO chats (jid,kind) VALUES (?,?) ON CONFLICT(jid) DO NOTHING`, jid, string(kind))
	return err
}

// SetUnread directly sets the unread counter (on the chat and its LID/PN pair).
func (d *DB) SetUnread(ctx context.Context, jid PeerID, n int) error {
	clause, args := d.colIN(ctx, "jid", jid)
	args = append([]any{n}, args...)
	_, err := d.sql.ExecContext(ctx, `UPDATE chats SET unread=? WHERE `+clause, args...)
	return err
}

// SetChatFlags updates pinned/archived/muted flags (on the chat and its pair).
func (d *DB) SetChatFlags(ctx context.Context, jid PeerID, pinned, archived, muted *bool) error {
	clause, args := d.colIN(ctx, "jid", jid)
	for col, v := range map[string]*bool{"pinned": pinned, "archived": archived, "muted": muted} {
		if v == nil {
			continue
		}
		if _, err := d.sql.ExecContext(ctx, `UPDATE chats SET `+col+`=? WHERE `+clause,
			append([]any{*v}, args...)...); err != nil {
			return err
		}
	}
	return nil
}

// SetChatLeft records whether we're still a participant of a group —
// left, removed, or the group deleted (on the chat and its pair).
func (d *DB) SetChatLeft(ctx context.Context, jid PeerID, left bool) error {
	clause, args := d.colIN(ctx, "jid", jid)
	_, err := d.sql.ExecContext(ctx, `UPDATE chats SET left_group=? WHERE `+clause,
		append([]any{left}, args...)...)
	return err
}

// SetChatAvatar updates the cached avatar path (on the chat and its pair).
func (d *DB) SetChatAvatar(ctx context.Context, jid PeerID, path string) error {
	clause, args := d.colIN(ctx, "jid", jid)
	args = append([]any{path}, args...)
	_, err := d.sql.ExecContext(ctx, `UPDATE chats SET avatar_path=? WHERE `+clause, args...)
	return err
}

// SetDisappSecs records the chat's disappearing-message timer (0 = off).
func (d *DB) SetDisappSecs(ctx context.Context, jid PeerID, secs int) error {
	clause, args := d.colIN(ctx, "jid", jid)
	_, err := d.sql.ExecContext(ctx,
		`UPDATE chats SET disapp_secs=? WHERE `+clause, append([]any{secs}, args...)...)
	return err
}

// Alt-JID mapping (LID ↔ PN) -----------------------------------------------

// UpsertAltJID records a LID↔PN pair for the same identity.
func (d *DB) UpsertAltJID(ctx context.Context, lid, pn PeerID) error {
	_, err := d.sql.ExecContext(ctx,
		`INSERT INTO alt_jids (lid,pn) VALUES (?,?) ON CONFLICT(lid) DO UPDATE SET pn=excluded.pn`,
		lid, pn)
	return err
}

// altJIDPairs returns the full mapping keyed by LID (full JID strings).
// Only mergeByCanonical needs the whole table — single-JID lookups use
// lookupPN/lookupLID below.
func (d *DB) altJIDPairs(ctx context.Context) map[PeerID]PeerID {
	rows, err := d.sql.QueryContext(ctx, `SELECT lid,pn FROM alt_jids`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[PeerID]PeerID{}
	for rows.Next() {
		var lid, pn PeerID
		if rows.Scan(&lid, &pn) == nil {
			out[lid] = pn
		}
	}
	return out
}

// queryer is satisfied by both *sql.DB and *sql.Tx — lets single-row helpers
// run inside or outside a transaction.
type queryer interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
}

// lookupPN resolves a LID JID to its phone-number JID (single row).
func (d *DB) lookupPN(ctx context.Context, lid PeerID) PeerID {
	return d.lookupPNq(ctx, d.sql, lid)
}

func (d *DB) lookupPNq(ctx context.Context, q queryer, lid PeerID) PeerID {
	var pn PeerID
	err := q.QueryRowContext(ctx, `SELECT pn FROM alt_jids WHERE lid=?`, lid).Scan(&pn)
	if err != nil {
		return ""
	}
	return pn
}

// lookupLID resolves a phone-number JID to its LID counterpart (single row).
func (d *DB) lookupLID(ctx context.Context, pn PeerID) PeerID {
	return d.lookupLIDq(ctx, d.sql, pn)
}

func (d *DB) lookupLIDq(ctx context.Context, q queryer, pn PeerID) PeerID {
	var lid PeerID
	err := q.QueryRowContext(ctx, `SELECT lid FROM alt_jids WHERE pn=?`, pn).Scan(&lid)
	if err != nil {
		return ""
	}
	return lid
}

// jidUser extracts the user part of a peer id — drops the account prefix,
// server and WhatsApp device part ("wa2:123:4@…" → "123").
func jidUser(jid PeerID) string {
	u := jid.Wire()
	if i := strings.IndexByte(u, '@'); i > 0 {
		u = u[:i]
	}
	if i := strings.IndexByte(u, ':'); i >= 0 {
		u = u[:i] // device part
	}
	return u
}

// acctUser extracts "acct:user" — the merge key: the account prefix stays
// (different accounts never fold together), the server and a WhatsApp
// device part drop ("wa:123:4@…" → "wa:123").
func acctUser(jid PeerID) string {
	return jid.Acct() + ":" + jidUser(jid)
}

// CanonicalJID returns the PN JID when the chat is LID-keyed and mapped.
func (d *DB) CanonicalJID(ctx context.Context, jid PeerID) PeerID {
	if !strings.HasSuffix(string(jid), "@lid") {
		return jid
	}
	if pn := d.lookupPN(ctx, jid); pn != "" {
		return pn
	}
	return jid
}

// AltJIDs returns the chat's JID plus its LID/PN counterpart when known.
func (d *DB) AltJIDs(ctx context.Context, jid PeerID) []PeerID {
	return d.altJIDsQ(ctx, d.sql, jid)
}

// AltJIDsTx is the transaction variant of AltJIDs — the alt_jids lookups
// must go through tx (the single connection is held by the transaction).
func (d *DB) AltJIDsTx(ctx context.Context, tx *sql.Tx, jid PeerID) []PeerID {
	return d.altJIDsQ(ctx, tx, jid)
}

func (d *DB) altJIDsQ(ctx context.Context, q queryer, jid PeerID) []PeerID {
	if strings.HasSuffix(string(jid), "@lid") {
		if pn := d.lookupPNq(ctx, q, jid); pn != "" {
			return []PeerID{jid, pn}
		}
		return []PeerID{jid}
	}
	if strings.HasSuffix(string(jid), "@s.whatsapp.net") {
		if lid := d.lookupLIDq(ctx, q, jid); lid != "" {
			return []PeerID{jid, lid}
		}
	}
	return []PeerID{jid}
}

// colIN builds a `col IN (...)` clause expanded over LID/PN pairs.
func (d *DB) colIN(ctx context.Context, col string, jid PeerID) (string, []any) {
	alts := d.AltJIDs(ctx, jid)
	if len(alts) == 1 {
		return col + "=?", []any{jid}
	}
	return col + " IN (?,?)", []any{alts[0], alts[1]}
}

func (d *DB) jidIN(ctx context.Context, jid PeerID) (string, []any) {
	return d.colIN(ctx, "c.jid", jid)
}

// mergeByCanonical folds chats that resolve to the same person (LID↔PN) into
// one row: unread sums, newest preview wins, the PN JID is preferred.
func (d *DB) mergeByCanonical(ctx context.Context, in []*Chat) []*Chat {
	pairs := d.altJIDPairs(ctx)
	lidToPNUser := map[string]string{}
	for lid, pn := range pairs {
		lidToPNUser[acctUser(lid)] = acctUser(pn)
	}
	canon := func(jid PeerID) string {
		u := acctUser(jid)
		if pn, ok := lidToPNUser[u]; ok {
			return pn
		}
		return u
	}
	seen := map[string]*Chat{}
	var out []*Chat
	for _, c := range in {
		k := canon(c.JID)
		m := seen[k]
		if m == nil {
			seen[k] = c
			out = append(out, c)
			continue
		}
		m.Unread += c.Unread
		if c.RowID < m.RowID {
			m.RowID = c.RowID // pair counts as created when its first row appeared
		}
		if c.LastMsgTS.After(m.LastMsgTS) {
			m.LastMsgTS, m.LastMsgPreview = c.LastMsgTS, c.LastMsgPreview
		}
		m.Pinned = m.Pinned || c.Pinned
		m.Archived = m.Archived && c.Archived
		m.Muted = m.Muted && c.Muted
		m.Left = m.Left || c.Left
		if !strings.HasSuffix(string(c.JID), "@lid") && strings.HasSuffix(string(m.JID), "@lid") {
			m.JID = c.JID
		}
		if m.AvatarPath == "" {
			m.AvatarPath = c.AvatarPath
		}
		if (m.Name == "" || m.Name == "+"+jidUser(m.JID)) && c.Name != "" {
			m.Name = c.Name
		}
	}
	return out
}

// Contacts ----------------------------------------------------------------

// UpsertContact saves name fields without clobbering non-empty values.
func (d *DB) UpsertContact(ctx context.Context, c *Contact) error {
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO contacts (jid,push_name,full_name,business_name,avatar_path,username,phone) VALUES (?,?,?,?,?,?,?)
ON CONFLICT(jid) DO UPDATE SET
  push_name=CASE WHEN excluded.push_name!='' THEN excluded.push_name ELSE contacts.push_name END,
  full_name=CASE WHEN excluded.full_name!='' THEN excluded.full_name ELSE contacts.full_name END,
  business_name=CASE WHEN excluded.business_name!='' THEN excluded.business_name ELSE contacts.business_name END,
  avatar_path=CASE WHEN excluded.avatar_path!='' THEN excluded.avatar_path ELSE contacts.avatar_path END,
  username=CASE WHEN excluded.username!='' THEN excluded.username ELSE contacts.username END,
  phone=CASE WHEN excluded.phone!='' THEN excluded.phone ELSE contacts.phone END`,
		c.JID, c.PushName, c.FullName, c.BusinessName, c.AvatarPath, c.Username, c.Phone)
	return err
}

// GetContact returns a contact or nil.
func (d *DB) GetContact(ctx context.Context, jid PeerID) (*Contact, error) {
	var c Contact
	err := d.sql.QueryRowContext(ctx,
		`SELECT jid,push_name,full_name,business_name,avatar_path,about,blocked,username,phone FROM contacts WHERE jid=?`, jid).
		Scan(&c.JID, &c.PushName, &c.FullName, &c.BusinessName, &c.AvatarPath, &c.About, &c.Blocked, &c.Username, &c.Phone)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &c, err
}

// SetContactAbout updates the "about" status line (UserAbout events).
func (d *DB) SetContactAbout(ctx context.Context, jid PeerID, about string) error {
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO contacts (jid,about) VALUES (?,?)
ON CONFLICT(jid) DO UPDATE SET about=excluded.about`, jid, about)
	return err
}

// SetBlocked records whether a contact is on the blocklist.
func (d *DB) SetBlocked(ctx context.Context, jid PeerID, blocked bool) error {
	_, err := d.sql.ExecContext(ctx, `
INSERT INTO contacts (jid,blocked) VALUES (?,?)
ON CONFLICT(jid) DO UPDATE SET blocked=excluded.blocked`, jid, blocked)
	return err
}

// SearchContacts finds contacts/chats by name fragment for the new-chat dialog.
// A non-positive limit returns every match — callers that filter rows
// afterwards (e.g. by account) must use it, or the cap drops matches first.
func (d *DB) SearchContacts(ctx context.Context, q string, limit int) ([]*Contact, error) {
	query := `
SELECT jid,push_name,full_name,business_name,avatar_path,about,blocked,username,phone FROM contacts
WHERE push_name LIKE ? OR full_name LIKE ? OR business_name LIKE ? OR jid LIKE ? OR username LIKE ? OR phone LIKE ?
ORDER BY COALESCE(NULLIF(full_name,''),NULLIF(push_name,''),NULLIF(business_name,''),NULLIF(username,''),jid) COLLATE NOCASE`
	like := "%" + q + "%"
	args := []any{like, like, like, like, like, like}
	if limit > 0 {
		query += "\nLIMIT ?"
		args = append(args, limit)
	}
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.JID, &c.PushName, &c.FullName, &c.BusinessName, &c.AvatarPath,
			&c.About, &c.Blocked, &c.Username, &c.Phone); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

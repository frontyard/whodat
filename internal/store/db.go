package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the application database (chat history, contacts, metadata).
// It is separate from whatsmeow's own device/crypto store.
type DB struct {
	sql *sql.DB

	// dbq serialises application-DB writes. Backends deliver events on
	// several goroutines, so writes were interleaved nondeterministically
	// (a touch could commit before its insert) and blocked event delivery.
	dbq    chan func()
	done   chan struct{} // closed by worker exit — Close waits on it
	wipeMu sync.Mutex
	wiping map[string]bool // per-account drop gates for DoFor
	closed bool            // set under wipeMu by Close — enqueue drops
}

// Open opens (and migrates) the app database at path.
func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sqldb.SetMaxOpenConns(1) // serialise writers; WAL still allows concurrent reads
	db := &DB{sql: sqldb, dbq: make(chan func(), 1024), done: make(chan struct{}), wiping: map[string]bool{}}
	go db.worker()
	if err := db.migrate(); err != nil {
		sqldb.Close()
		return nil, err
	}
	// Idempotent index — outside migrate() so existing DBs pick it up too.
	if _, err := sqldb.Exec(`CREATE INDEX IF NOT EXISTS idx_alt_jids_pn ON alt_jids(pn)`); err != nil {
		sqldb.Close()
		return nil, err
	}
	// Message history is private: never leave the file group/world-readable.
	if err := os.Chmod(path, 0o600); err != nil && !os.IsNotExist(err) {
		sqldb.Close()
		return nil, err
	}
	return db, nil
}

// Close stops the write worker, drains nothing (queued tasks run to
// completion first — the worker exits when the channel is closed and
// empty), then closes the database.
func (d *DB) Close() error {
	d.wipeMu.Lock()
	if !d.closed {
		d.closed = true
		close(d.dbq)
	}
	d.wipeMu.Unlock()
	<-d.done
	return d.sql.Close()
}

const schemaVersion = 12

func (d *DB) migrate() error {
	var v int
	if err := d.sql.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	if v >= schemaVersion {
		return nil
	}
	_, err := d.sql.Exec(`
CREATE TABLE IF NOT EXISTS chats (
  jid              TEXT PRIMARY KEY,
  kind             TEXT NOT NULL DEFAULT 'dm',
  name             TEXT NOT NULL DEFAULT '',
  avatar_path      TEXT NOT NULL DEFAULT '',
  last_msg_ts      INTEGER NOT NULL DEFAULT 0,
  last_msg_preview TEXT NOT NULL DEFAULT '',
  unread           INTEGER NOT NULL DEFAULT 0,
  pinned           INTEGER NOT NULL DEFAULT 0,
  archived         INTEGER NOT NULL DEFAULT 0,
  muted            INTEGER NOT NULL DEFAULT 0,
  disapp_secs      INTEGER NOT NULL DEFAULT 0,
  left_group       INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
  chat_jid      TEXT NOT NULL,
  id            TEXT NOT NULL,
  sender_jid    TEXT NOT NULL DEFAULT '',
  sender_name   TEXT NOT NULL DEFAULT '',
  ts            INTEGER NOT NULL,
  type          TEXT NOT NULL DEFAULT 'text',
  text          TEXT NOT NULL DEFAULT '',
  media_path    TEXT NOT NULL DEFAULT '',
  media_thumb   TEXT NOT NULL DEFAULT '',
  media_mime    TEXT NOT NULL DEFAULT '',
  media_name    TEXT NOT NULL DEFAULT '',
  media_size    INTEGER NOT NULL DEFAULT 0,
  duration      INTEGER NOT NULL DEFAULT 0,
  width         INTEGER NOT NULL DEFAULT 0,
  height        INTEGER NOT NULL DEFAULT 0,
  quoted_id     TEXT NOT NULL DEFAULT '',
  quoted_sender TEXT NOT NULL DEFAULT '',
  quoted_text   TEXT NOT NULL DEFAULT '',
  from_me       INTEGER NOT NULL DEFAULT 0,
  status        TEXT NOT NULL DEFAULT '',
  edited        INTEGER NOT NULL DEFAULT 0,
  revoked       INTEGER NOT NULL DEFAULT 0,
  starred       INTEGER NOT NULL DEFAULT 0,
  view_once     INTEGER NOT NULL DEFAULT 0,
  expires_at    INTEGER,
  poll_options  TEXT NOT NULL DEFAULT '',
  reactions     TEXT NOT NULL DEFAULT '',
  waveform      BLOB,
  proto         BLOB,
  html          TEXT NOT NULL DEFAULT '',
  forwarded     INTEGER NOT NULL DEFAULT 0,
  forwarded_from TEXT NOT NULL DEFAULT '',
  link_preview  TEXT NOT NULL DEFAULT '',
  preview_url   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (chat_jid, id)
);
CREATE INDEX IF NOT EXISTS idx_msgs_chat_ts ON messages(chat_jid, ts DESC);
CREATE TABLE IF NOT EXISTS contacts (
  jid           TEXT PRIMARY KEY,
  push_name     TEXT NOT NULL DEFAULT '',
  full_name     TEXT NOT NULL DEFAULT '',
  business_name TEXT NOT NULL DEFAULT '',
  avatar_path   TEXT NOT NULL DEFAULT '',
  about         TEXT NOT NULL DEFAULT '',
  blocked       INTEGER NOT NULL DEFAULT 0,
  username      TEXT NOT NULL DEFAULT '',
  phone         TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS kv (k TEXT PRIMARY KEY, v TEXT);
CREATE TABLE IF NOT EXISTS alt_jids (
  lid TEXT PRIMARY KEY,
  pn  TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS poll_votes (
  poll_chat TEXT NOT NULL,
  poll_id   TEXT NOT NULL,
  voter     TEXT NOT NULL,
  opt_name  TEXT NOT NULL,
  PRIMARY KEY (poll_chat, poll_id, voter, opt_name)
);
`)
	if err != nil {
		return err
	}
	// v2 → v3: new columns/tables for existing databases. CREATE TABLE IF NOT
	// EXISTS above already covers fresh files; these guard older ones.
	for _, c := range [][3]string{
		{"chats", "disapp_secs", "ALTER TABLE chats ADD COLUMN disapp_secs INTEGER NOT NULL DEFAULT 0"},
		{"messages", "view_once", "ALTER TABLE messages ADD COLUMN view_once INTEGER NOT NULL DEFAULT 0"},
		{"messages", "expires_at", "ALTER TABLE messages ADD COLUMN expires_at INTEGER"},
		{"messages", "poll_options", "ALTER TABLE messages ADD COLUMN poll_options TEXT NOT NULL DEFAULT ''"},
		{"contacts", "about", "ALTER TABLE contacts ADD COLUMN about TEXT NOT NULL DEFAULT ''"},
		{"contacts", "blocked", "ALTER TABLE contacts ADD COLUMN blocked INTEGER NOT NULL DEFAULT 0"},
		{"messages", "html", "ALTER TABLE messages ADD COLUMN html TEXT NOT NULL DEFAULT ''"},
		{"contacts", "username", "ALTER TABLE contacts ADD COLUMN username TEXT NOT NULL DEFAULT ''"},
		{"contacts", "phone", "ALTER TABLE contacts ADD COLUMN phone TEXT NOT NULL DEFAULT ''"},
		{"messages", "forwarded", "ALTER TABLE messages ADD COLUMN forwarded INTEGER NOT NULL DEFAULT 0"},
		{"messages", "forwarded_from", "ALTER TABLE messages ADD COLUMN forwarded_from TEXT NOT NULL DEFAULT ''"},
		{"messages", "link_preview", "ALTER TABLE messages ADD COLUMN link_preview TEXT NOT NULL DEFAULT ''"},
		{"messages", "preview_url", "ALTER TABLE messages ADD COLUMN preview_url TEXT NOT NULL DEFAULT ''"},
		{"messages", "media_thumb", "ALTER TABLE messages ADD COLUMN media_thumb TEXT NOT NULL DEFAULT ''"},
		{"chats", "left_group", "ALTER TABLE chats ADD COLUMN left_group INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := d.addColumn(c[0], c[1], c[2]); err != nil {
			return err
		}
	}
	// v3 → v4: drop protocol stubs that stored as empty system rows — they
	// rendered as invisible gaps at the bottom of the message list.
	if _, err := d.sql.Exec(`DELETE FROM messages WHERE type='system' AND text=''`); err != nil {
		return err
	}
	_, err = d.sql.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
	return err
}

// addColumn runs ddl only when the column is absent — ALTER TABLE has no
// IF NOT EXISTS, so inspect pragma_table_info first.
func (d *DB) addColumn(table, col, ddl string) error {
	var n int
	if err := d.sql.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, col).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := d.sql.Exec(ddl)
	return err
}

// KV helpers ---------------------------------------------------------------

// GetKV returns a stored string value or "".
func (d *DB) GetKV(ctx context.Context, k string) (string, error) {
	var v string
	err := d.sql.QueryRowContext(ctx, `SELECT v FROM kv WHERE k=?`, k).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetKV stores a string value.
func (d *DB) SetKV(ctx context.Context, k, v string) error {
	_, err := d.sql.ExecContext(ctx, `INSERT INTO kv(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

// worker drains the serialised write queue until Close drops it.
func (d *DB) worker() {
	defer close(d.done)
	for fn := range d.dbq {
		fn()
	}
}

// Do enqueues a DB-bound task on the serial writer, reporting whether it
// was accepted — tasks posted after Close are dropped (a late write must
// not panic on the closed channel), and callers that need the task's
// results must not wait on a task that will never run.
func (d *DB) Do(fn func(ctx context.Context)) bool {
	d.wipeMu.Lock()
	defer d.wipeMu.Unlock()
	if d.closed {
		return false
	}
	d.dbq <- func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		fn(ctx)
	}
	return true
}

// DoFor is Do gated on the account's wipe flag — during a logout wipe new
// writes are dropped: they come from a dead session and would commit after
// the purge, resurrecting rows (an in-flight history sync can enqueue for
// seconds).
func (d *DB) DoFor(acct string, fn func(ctx context.Context)) {
	d.wipeMu.Lock()
	defer d.wipeMu.Unlock()
	if d.wiping[acct] || d.closed {
		return
	}
	d.dbq <- func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		fn(ctx)
	}
}

// SetWiping opens/closes an account's write gate. The gate is set before the
// purge runs and cleared when a new session pairs — a dead session writes
// nothing.
func (d *DB) SetWiping(acct string, on bool) {
	d.wipeMu.Lock()
	defer d.wipeMu.Unlock()
	if on {
		d.wiping[acct] = true
	} else {
		delete(d.wiping, acct)
	}
}

// PurgeAccount deletes every application row owned by acct. Every stored
// id is namespaced ("acct:<id>"), so ownership is a prefix match; kv keys
// are prefix-scoped the same way. Files are not touched here — they live
// under account-scoped dirs the caller RemoveAlls.
func (d *DB) PurgeAccount(ctx context.Context, acct string) error {
	like := acct + ":%"

	err := d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE chat_jid LIKE ?`, like); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM poll_votes WHERE poll_chat LIKE ?`, like); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM chats WHERE jid LIKE ?`, like); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM contacts WHERE jid LIKE ?`, like); err != nil {
			return err
		}
		// alt_jids rows are namespaced like everything else.
		if _, err := tx.ExecContext(ctx, `DELETE FROM alt_jids WHERE lid LIKE ?`, like); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM kv WHERE k LIKE ?`, like)
		return err
	})
	if err != nil {
		return err
	}
	if _, err := d.sql.ExecContext(ctx, `VACUUM`); err != nil {
		return err
	}
	_, err = d.sql.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

// PurgeAll deletes every application row and reclaims the freed pages —
// the local data wipe run on logout. The file and schema stay; VACUUM +
// checkpoint ensure deleted rows aren't left sitting in free pages or WAL.
func (d *DB) PurgeAll(ctx context.Context) error {
	if err := d.Tx(ctx, func(tx *sql.Tx) error {
		for _, t := range []string{
			"messages", "poll_votes", "chats", "contacts", "alt_jids", "kv",
		} {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+t); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if _, err := d.sql.ExecContext(ctx, `VACUUM`); err != nil {
		return err
	}
	_, err := d.sql.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

// Exec exposes raw exec for specialised queries (history sync batches).
func (d *DB) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return d.sql.ExecContext(ctx, q, args...)
}

// Tx runs fn inside a transaction.
func (d *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

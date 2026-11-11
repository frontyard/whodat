package wa

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"
	_ "modernc.org/sqlite"

	"github.com/frontyard/whodat/internal/config"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// TestWipeLocalData exercises the full logout wipe: app-DB purge, whatsmeow
// table purge (lid map / privacy tokens lack FK cascades), and removal of
// media, avatars, QR caches and status.json — while unrelated files and
// non-account dirs (extracted QML) survive.
func TestWipeLocalData(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(tmp, "state"))
	ctx := context.Background()

	// App DB with a chat + message + contact.
	if err := os.MkdirAll(config.DataDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	appDB, err := store.Open(filepath.Join(config.DataDir(), "whodat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer appDB.Close()
	jid := store.PeerID("wa:w@s.whatsapp.net")
	_ = appDB.UpsertChat(ctx, &store.Chat{JID: jid, Name: "c"})
	_ = appDB.InsertMessage(ctx, &store.Message{ChatJID: jid, ID: "m1", TS: time.Now()})
	_ = appDB.UpsertContact(ctx, &store.Contact{JID: jid})

	// wastore.db stand-in: real schema lives in hypermeow migrations; the
	// wipe only needs the table names to enumerate.
	waDB := filepath.Join(config.DataDir(), "wastore.db")
	if err := os.MkdirAll(filepath.Dir(waDB), 0o700); err != nil {
		t.Fatal(err)
	}
	seed, err := sql.Open("sqlite", "file:"+waDB)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		`CREATE TABLE whatsmeow_device (jid TEXT PRIMARY KEY)`,
		`CREATE TABLE whatsmeow_lid_map (lid TEXT PRIMARY KEY, pn TEXT)`,
		`CREATE TABLE whatsmeow_privacy_tokens (our_jid TEXT, their_jid TEXT, token BLOB)`,
		`CREATE TABLE whatsmeow_version (version INTEGER, compat INTEGER)`,
		`INSERT INTO whatsmeow_version VALUES (19, 8)`,
		`INSERT INTO whatsmeow_device VALUES ('me@s.whatsapp.net')`,
		`INSERT INTO whatsmeow_lid_map VALUES ('1@lid','1@s.whatsapp.net')`,
		`INSERT INTO whatsmeow_privacy_tokens VALUES ('a','b','t')`,
		`CREATE TABLE not_whatsmeow (x TEXT)`,
		`INSERT INTO not_whatsmeow VALUES ('keep')`,
	} {
		if _, err := seed.ExecContext(ctx, ddl); err != nil {
			t.Fatalf("seed %q: %v", ddl, err)
		}
	}
	seed.Close()

	// Files: media, avatar, QR caches, status.json — plus a QML file that
	// must survive (it's extracted program code, not account data).
	dirs := []string{
		config.MediaAcctDir(acctWA),
		config.MediaAcctDir("wa9"), // a peer account's dir — must survive
		config.AvatarAcctDir(acctWA),
		config.CacheDir(),
		filepath.Join(config.CacheDir(), "qml", "v1"),
		config.StateDir(),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p string) {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(config.MediaAcctDir(acctWA), "att.jpg"))
	write(filepath.Join(config.MediaAcctDir("wa9"), "t1.jpg")) // other account's file survives
	write(filepath.Join(config.AvatarAcctDir(acctWA), "a1.jpg"))
	write(filepath.Join(config.CacheDir(), "qr-wa-1.png"))
	write(filepath.Join(config.CacheDir(), "qr-wa9-1.png")) // another account's in-flight QR survives
	write(filepath.Join(config.CacheDir(), "myqr-wa.png"))
	qmlFile := filepath.Join(config.CacheDir(), "qml", "v1", "Main.qml")
	write(qmlFile)
	write(filepath.Join(config.StateDir(), "status.json"))

	s := &Service{
		sink: protocol.NewSink(acctWA, appDB, protocol.NewBus()),
		db:   appDB,
		waDB: waDB,
		acct: acctWA,
		log:  waLog.Noop,
	}
	s.wipeLocalData(ctx)

	// App DB empty.
	if c, _ := appDB.GetChat(ctx, jid); c != nil {
		t.Fatal("chat row survived wipe")
	}
	if m, _ := appDB.GetMessage(ctx, jid, "m1"); m != nil {
		t.Fatal("message row survived wipe")
	}
	if c, _ := appDB.GetContact(ctx, jid); c != nil {
		t.Fatal("contact row survived wipe")
	}
	// whatsmeow_* tables empty; unrelated table untouched.
	chk, err := sql.Open("sqlite", "file:"+waDB)
	if err != nil {
		t.Fatal(err)
	}
	defer chk.Close()
	for _, tbl := range []string{"whatsmeow_device", "whatsmeow_lid_map", "whatsmeow_privacy_tokens"} {
		var n int
		if err := chk.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tbl).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d rows, err %v", tbl, n, err)
		}
	}
	// The schema version marker survives — without it sqlstore replays all
	// upgrades onto existing tables on the next start.
	var ver int
	if err := chk.QueryRowContext(ctx, `SELECT version FROM whatsmeow_version`).Scan(&ver); err != nil || ver != 19 {
		t.Fatalf("version row: %d, err %v", ver, err)
	}
	var n int
	if err := chk.QueryRowContext(ctx, `SELECT COUNT(*) FROM not_whatsmeow`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("unrelated table: %d rows, err %v", n, err)
	}
	// This account's media/avatar dirs are empty and recreated.
	for _, d := range []string{config.MediaAcctDir(acctWA), config.AvatarAcctDir(acctWA)} {
		ents, err := os.ReadDir(d)
		if err != nil || len(ents) != 0 {
			t.Fatalf("%s: %d entries, err %v", d, len(ents), err)
		}
	}
	if _, err := os.Stat(filepath.Join(config.MediaAcctDir("wa9"), "t1.jpg")); err != nil {
		t.Fatal("other account's media was deleted")
	}
	for _, p := range []string{
		filepath.Join(config.CacheDir(), "qr-wa-1.png"),
		filepath.Join(config.CacheDir(), "myqr-wa.png"),
		filepath.Join(config.StateDir(), "status.json"),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s still exists", p)
		}
	}
	if _, err := os.Stat(filepath.Join(config.CacheDir(), "qr-wa9-1.png")); err != nil {
		t.Fatal("other account's QR was deleted")
	}
	if _, err := os.Stat(qmlFile); err != nil {
		t.Fatal("QML cache was deleted")
	}

	// Writes from the dead session are gated off — a straggler (e.g. an
	// in-flight history sync) must not resurrect rows after the purge.
	s.dbDo(func(ctx context.Context) {
		_ = s.db.InsertMessage(ctx, &store.Message{ChatJID: jid, ID: "ghost", TS: time.Now()})
	})
	flushed := make(chan struct{})
	s.db.Do(func(context.Context) { close(flushed) })
	<-flushed
	if m, _ := appDB.GetMessage(ctx, jid, "ghost"); m != nil {
		t.Fatal("write during wipe resurrected a row")
	}

	// PairSuccess clears the gate — the next session writes normally.
	s.dispatch(&events.PairSuccess{})
	s.dbDo(func(ctx context.Context) {
		_ = s.db.InsertMessage(ctx, &store.Message{ChatJID: jid, ID: "m2", TS: time.Now()})
	})
	flushed2 := make(chan struct{})
	s.db.Do(func(context.Context) { close(flushed2) })
	<-flushed2
	if m, _ := appDB.GetMessage(ctx, jid, "m2"); m == nil {
		t.Fatal("writes still gated after re-pair")
	}
}

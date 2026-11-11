package store

import (
	"context"
	"testing"
	"time"
)

// TestPurgeAll seeds one row per table, wipes, and expects an empty but
// still-usable database.
func TestPurgeAll(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	jid := PeerID("wa:w@s.whatsapp.net")

	_ = d.UpsertChat(ctx, &Chat{JID: jid, Kind: KindDM, Name: "c"})
	_ = d.InsertMessage(ctx, msg(jid, "m1", time.Now()))
	_ = d.UpsertContact(ctx, &Contact{JID: jid, PushName: "p"})
	_ = d.UpsertAltJID(ctx, "111@lid", jid)
	_ = d.SetKV(ctx, "k", "v")
	poll := msg(jid, "p1", time.Now())
	poll.Type = TPoll
	_ = d.InsertMessage(ctx, poll)
	_ = d.VotePoll(ctx, jid, "p1", "v", []string{"A"})

	if err := d.PurgeAll(ctx); err != nil {
		t.Fatalf("purge: %v", err)
	}
	for _, tbl := range []string{
		"messages", "poll_votes", "chats", "contacts", "alt_jids", "kv",
	} {
		var n int
		if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+tbl).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		if n != 0 {
			t.Fatalf("%s: %d rows survived purge", tbl, n)
		}
	}
	// The store must keep working — a new session writes into the same file.
	if err := d.InsertMessage(ctx, msg(jid, "m2", time.Now())); err != nil {
		t.Fatalf("insert after purge: %v", err)
	}
}

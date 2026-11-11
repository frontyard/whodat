package store

import (
	"context"
	"testing"
	"time"
)

// TestPurgeAccount seeds rows for two accounts — "wa:"-prefixed WhatsApp
// ids and "wa2:"-prefixed ones — then purges each and expects the other's
// rows to survive untouched.
func TestPurgeAccount(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()

	seed := func() {
		for _, jid := range []PeerID{"wa:w@s.whatsapp.net", "wa:grp@g.us"} {
			_ = d.UpsertChat(ctx, &Chat{JID: jid, Kind: KindDM})
			m := msg(jid, "m1", time.Now())
			m.MediaPath = "/media/" + jid.String() + ".jpg"
			_ = d.InsertMessage(ctx, m)
			_ = d.UpsertContact(ctx, &Contact{JID: jid})
		}
		for _, jid := range []PeerID{"wa2:777000", "wa2:-1001"} {
			_ = d.UpsertChat(ctx, &Chat{JID: jid, Kind: KindDM})
			m := msg(jid, "m1", time.Now())
			m.MediaPath = "/media/wa2.jpg"
			_ = d.InsertMessage(ctx, m)
			_ = d.UpsertContact(ctx, &Contact{JID: jid})
		}
		_ = d.InsertMessage(ctx, msg("wa2:777000", "m2", time.Now()))
		_ = d.UpsertAltJID(ctx, "wa:1@lid", "wa:w@s.whatsapp.net")
		_ = d.SetKV(ctx, "wa:sync", "1")
		_ = d.SetKV(ctx, "wa2:self", "2")
	}
	seed()

	// Purge WhatsApp — only wa: rows go.
	if err := d.PurgeAccount(ctx, "wa"); err != nil {
		t.Fatalf("purge wa: %v", err)
	}
	for _, jid := range []PeerID{"wa:w@s.whatsapp.net", "wa:grp@g.us"} {
		if c, _ := d.GetChat(ctx, jid); c != nil {
			t.Fatalf("wa chat survived: %s", jid)
		}
		if m, _ := d.GetMessage(ctx, jid, "m1"); m != nil {
			t.Fatalf("wa message survived: %s", jid)
		}
		if c, _ := d.GetContact(ctx, jid); c != nil {
			t.Fatalf("wa contact survived: %s", jid)
		}
	}
	for _, jid := range []PeerID{"wa2:777000", "wa2:-1001"} {
		if c, _ := d.GetChat(ctx, jid); c == nil {
			t.Fatalf("wa2 chat purged: %s", jid)
		}
		if m, _ := d.GetMessage(ctx, jid, "m1"); m == nil {
			t.Fatalf("wa2 message purged: %s", jid)
		}
	}
	var n int
	if err := d.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM alt_jids`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("alt_jids survived: %d err %v", n, err)
	}
	if v, _ := d.GetKV(ctx, "wa:sync"); v != "" {
		t.Fatal("wa kv survived")
	}
	if v, _ := d.GetKV(ctx, "wa2:self"); v != "2" {
		t.Fatal("wa2 kv purged")
	}

	// Purge the second account — only wa2: rows go.
	if err := d.PurgeAccount(ctx, "wa2"); err != nil {
		t.Fatalf("purge wa2: %v", err)
	}
	for _, jid := range []PeerID{"wa2:777000", "wa2:-1001"} {
		if c, _ := d.GetChat(ctx, jid); c != nil {
			t.Fatalf("wa2 chat survived: %s", jid)
		}
	}
}

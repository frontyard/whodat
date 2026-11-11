package store

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"
)

// openTemp opens a migrated DB under t.TempDir().
func openTemp(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func msg(chat PeerID, id string, ts time.Time) *Message {
	return &Message{ChatJID: chat, ID: id, TS: ts, Type: TText, Text: "m" + id}
}

func TestStatusNeverDowngrades(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	ts := time.Now().Truncate(time.Second)

	ins := func(st MsgStatus) {
		t.Helper()
		m := msg("a@s.whatsapp.net", "X1", ts)
		m.FromMe = true
		m.Status = st
		if err := d.InsertMessage(ctx, m); err != nil {
			t.Fatalf("insert %s: %v", st, err)
		}
	}
	ins(SSent)
	ins(SRead) // upgrade: fine
	got, _ := d.GetMessage(ctx, "a@s.whatsapp.net", "X1")
	if got.Status != SRead {
		t.Fatalf("upgrade read lost: %s", got.Status)
	}
	ins(SSent) // echo/resync must not downgrade
	got, _ = d.GetMessage(ctx, "a@s.whatsapp.net", "X1")
	if got.Status != SRead {
		t.Fatalf("downgrade to sent: %s", got.Status)
	}
	ins(SDelivered) // also must not downgrade
	got, _ = d.GetMessage(ctx, "a@s.whatsapp.net", "X1")
	if got.Status != SRead {
		t.Fatalf("downgrade to delivered: %s", got.Status)
	}
}

func TestKeysetPaginationNoLoss(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	ts := time.Now().Truncate(time.Second)
	jid := PeerID("b@s.whatsapp.net")
	// 12 messages sharing the same second — ts<? paging would lose 8.
	for i := 0; i < 12; i++ {
		m := msg(jid, string(rune('a'+i))+id(i), ts)
		if err := d.InsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	first, err := d.MessagesBefore(ctx, jid, time.Now().Add(time.Hour), math.MaxInt64, 5)
	if err != nil || len(first) != 5 {
		t.Fatalf("page1: %d %v", len(first), err)
	}
	last := first[len(first)-1]
	second, err := d.MessagesBefore(ctx, jid, last.TS, last.RowID, 20)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, m := range first {
		seen[m.ID] = true
	}
	for _, m := range second {
		seen[m.ID] = true
	}
	if len(seen) != 12 {
		t.Fatalf("lost %d messages across pages", 12-len(seen))
	}
}

func id(i int) string { return "-" + string(rune('0'+i)) }

func TestSetRevokedClearsMedia(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	jid := PeerID("c@s.whatsapp.net")
	m := msg(jid, "R1", time.Now())
	m.Type = TImage
	m.MediaPath = "/x/y.jpg"
	m.MediaMIME = "image/jpeg"
	m.Waveform = []byte{1, 2}
	m.Proto = []byte{9}
	if err := d.InsertMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := d.SetRevoked(ctx, jid, "R1"); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetMessage(ctx, jid, "R1")
	if !got.Revoked || got.Type != TRevoked {
		t.Fatalf("not revoked: %+v", got)
	}
	if got.MediaPath != "" || got.MediaMIME != "" || got.Proto != nil || got.Waveform != nil || got.Text != "" {
		t.Fatalf("media/text lingered: %+v", got)
	}
}

func TestFindRecentCall(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	jid := PeerID("d@s.whatsapp.net")
	old := msg(jid, "call-old", time.Now().Add(-time.Hour))
	old.Type = TCall
	recent := msg(jid, "call-abc", time.Now())
	recent.Type = TCall
	recent.FromMe = true
	_ = d.InsertMessage(ctx, old)
	_ = d.InsertMessage(ctx, recent)

	got, err := d.FindRecentCall(ctx, jid, time.Now().Add(-2*time.Minute), true)
	if err != nil || got == nil || got.ID != "call-abc" {
		t.Fatalf("find: %v %+v", err, got)
	}
	got, _ = d.FindRecentCall(ctx, jid, time.Now().Add(-2*time.Minute), false)
	if got != nil {
		t.Fatalf("direction mismatch: %+v", got)
	}
}

func TestPollVotes(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	jid := PeerID("p@s.whatsapp.net")
	poll := msg(jid, "poll1", time.Now())
	poll.Type = TPoll
	poll.PollOptions = []string{"A", "B"}
	if err := d.InsertMessage(ctx, poll); err != nil {
		t.Fatal(err)
	}
	if err := d.VotePoll(ctx, jid, "poll1", "v1", []string{"A"}); err != nil {
		t.Fatal(err)
	}
	if err := d.VotePoll(ctx, jid, "poll1", "v2", []string{"A", "B"}); err != nil {
		t.Fatal(err)
	}
	votes, _ := d.PollVotes(ctx, jid, "poll1")
	if len(votes["A"]) != 2 || len(votes["B"]) != 1 {
		t.Fatalf("votes: %v", votes)
	}
	// Ballot replacement — v1 switches to B.
	if err := d.VotePoll(ctx, jid, "poll1", "v1", []string{"B"}); err != nil {
		t.Fatal(err)
	}
	votes, _ = d.PollVotes(ctx, jid, "poll1")
	if len(votes["A"]) != 1 || len(votes["B"]) != 2 {
		t.Fatalf("replaced: %v", votes)
	}
	// Retract.
	if err := d.VotePoll(ctx, jid, "poll1", "v1", nil); err != nil {
		t.Fatal(err)
	}
	votes, _ = d.PollVotes(ctx, jid, "poll1")
	if len(votes["B"]) != 1 {
		t.Fatalf("retract: %v", votes)
	}
}

func TestDisappearingFilterAndSweep(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	jid := PeerID("g@s.whatsapp.net")
	expired := msg(jid, "old", time.Now().Add(-time.Hour))
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	live := msg(jid, "live", time.Now())
	live.ExpiresAt = time.Now().Add(time.Hour)
	plain := msg(jid, "plain", time.Now())
	for _, m := range []*Message{expired, live, plain} {
		if err := d.InsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	// Expired rows never surface even before the sweep runs.
	got, _ := d.MessagesBefore(ctx, jid, time.Now().Add(time.Hour), math.MaxInt64, 50)
	for _, m := range got {
		if m.ID == "old" {
			t.Fatal("expired message rendered")
		}
	}
	paths, err := d.DeleteExpired(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	_ = paths
	m, _ := d.GetMessage(ctx, jid, "old")
	if m != nil {
		t.Fatal("sweep kept expired row")
	}
	m, _ = d.GetMessage(ctx, jid, "live")
	if m == nil {
		t.Fatal("sweep killed live row")
	}
}

func TestClearAndDeleteChat(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	jid := PeerID("x@s.whatsapp.net")
	_ = d.UpsertChat(ctx, &Chat{JID: jid, Kind: KindDM})
	_ = d.InsertMessage(ctx, msg(jid, "m1", time.Now()))
	_ = d.InsertMessage(ctx, msg(jid, "m2", time.Now().Add(time.Second)))
	_ = d.SetUnread(ctx, jid, 3)

	if _, err := d.ClearChatMessages(ctx, jid); err != nil {
		t.Fatal(err)
	}
	msgs, _ := d.MessagesBefore(ctx, jid, time.Now().Add(time.Hour), math.MaxInt64, 50)
	if len(msgs) != 0 {
		t.Fatal("clear left messages")
	}
	c, _ := d.GetChat(ctx, jid)
	if c == nil || c.Unread != 0 || c.LastMsgPreview != "" {
		t.Fatalf("chat state after clear: %+v", c)
	}
	_ = d.InsertMessage(ctx, msg(jid, "m3", time.Now()))
	if _, err := d.DeleteChatRows(ctx, jid); err != nil {
		t.Fatal(err)
	}
	c, _ = d.GetChat(ctx, jid)
	if c != nil {
		t.Fatal("delete left chat row")
	}
}

func TestChatFlagsDisapp(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	jid := PeerID("d2@s.whatsapp.net")
	_ = d.UpsertChat(ctx, &Chat{JID: jid, Kind: KindDM})
	if err := d.SetDisappSecs(ctx, jid, 86400); err != nil {
		t.Fatal(err)
	}
	c, _ := d.GetChat(ctx, jid)
	if c.DisappSecs != 86400 {
		t.Fatalf("disapp: %d", c.DisappSecs)
	}
}

func TestBlockedAndAbout(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	jid := PeerID("ct@s.whatsapp.net")
	_ = d.UpsertContact(ctx, &Contact{JID: jid, FullName: "C"})
	if err := d.SetBlocked(ctx, jid, true); err != nil {
		t.Fatal(err)
	}
	if err := d.SetContactAbout(ctx, jid, "busy"); err != nil {
		t.Fatal(err)
	}
	c, _ := d.GetContact(ctx, jid)
	if !c.Blocked || c.About != "busy" {
		t.Fatalf("contact: %+v", c)
	}
	if err := d.SetBlocked(ctx, jid, false); err != nil {
		t.Fatal(err)
	}
	c, _ = d.GetContact(ctx, jid)
	if c.Blocked {
		t.Fatal("unblock stuck")
	}
}

func TestAltJIDMerge(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	lid := PeerID("wa:111@lid")
	pn := PeerID("wa:555@s.whatsapp.net")
	if err := d.UpsertAltJID(ctx, lid, pn); err != nil {
		t.Fatal(err)
	}
	if c := d.CanonicalJID(ctx, lid); c != pn {
		t.Fatalf("canonical: %s", c)
	}
	if c := d.CanonicalJID(ctx, pn); c != pn {
		t.Fatalf("canonical pn: %s", c)
	}
	if alts := d.AltJIDs(ctx, pn); len(alts) != 2 {
		t.Fatalf("alts: %v", alts)
	}
	// Messages under both keys surface on one lookup.
	_ = d.InsertMessage(ctx, msg(lid, "m1", time.Now()))
	got, _ := d.GetMessage(ctx, pn, "m1")
	if got == nil {
		t.Fatal("PN lookup missed LID-keyed message")
	}
}

func TestSearchContactsOrder(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	for _, c := range []*Contact{
		{JID: "1@s.whatsapp.net", FullName: "Zed"},
		{JID: "2@s.whatsapp.net", FullName: "Anna"},
		{JID: "3@s.whatsapp.net", PushName: "mike"},
	} {
		if err := d.UpsertContact(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	// 'a' also matches every jid via s.whatsapp.net — all three return,
	// but they must arrive name-ordered (Anna, mike, Zed).
	res, err := d.SearchContacts(ctx, "a", 10)
	if err != nil || len(res) != 3 {
		t.Fatalf("search: %d %v", len(res), err)
	}
	want := []string{"Anna", "mike", "Zed"}
	for i, w := range want {
		if res[i].DisplayName() != w {
			t.Fatalf("order[%d]: %s want %s", i, res[i].DisplayName(), w)
		}
	}
}

// Re-inserts (echoes, resends, resyncs) carry nil Reactions — the upsert
// must not clobber stored reactions with 'null'.
func TestUpsertKeepsReactions(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	jid, id := PeerID("a@s.whatsapp.net"), "R1"

	if err := d.InsertMessage(ctx, msg(jid, id, time.Now().Truncate(time.Second))); err != nil {
		t.Fatal(err)
	}
	want := map[string][]PeerID{"👍": {"wa:u@s.whatsapp.net"}}
	if err := d.SetReactions(ctx, jid, id, want); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertMessage(ctx, msg(jid, id, time.Now().Truncate(time.Second))); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMessage(ctx, jid, id)
	if err != nil || len(got.Reactions["👍"]) != 1 {
		t.Fatalf("reactions lost on re-insert: %v %v", got.Reactions, err)
	}

	// Empty existing row backfills from a seeded insert (history resync).
	if err := d.InsertMessage(ctx, msg(jid, "R2", time.Now())); err != nil {
		t.Fatal(err)
	}
	seeded := msg(jid, "R2", time.Now())
	seeded.Reactions = want
	if err := d.InsertMessage(ctx, seeded); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetMessage(ctx, jid, "R2")
	if len(got.Reactions["👍"]) != 1 {
		t.Fatalf("seeded reactions not backfilled: %v", got.Reactions)
	}

	// But a non-empty stored set always wins over the excluded payload.
	if err := d.SetReactions(ctx, jid, "R2", map[string][]PeerID{"❤️": {"x@s.whatsapp.net"}}); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertMessage(ctx, seeded); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetMessage(ctx, jid, "R2")
	if len(got.Reactions["❤️"]) != 1 || got.Reactions["👍"] != nil {
		t.Fatalf("live reactions overwritten by stale payload: %v", got.Reactions)
	}
}

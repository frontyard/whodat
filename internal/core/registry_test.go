package core

import (
	"path/filepath"
	"testing"

	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// fakeSvc satisfies protocol.Service via the embedded interface — tests
// override only the methods they touch.
type fakeSvc struct {
	protocol.Service
	acct  string
	proto string
	caps  protocol.Caps
}

func (f *fakeSvc) Acct() string { return f.acct }
func (f *fakeSvc) Protocol() string {
	if f.proto != "" {
		return f.proto
	}
	return f.acct
}
func (f *fakeSvc) Caps() protocol.Caps { return f.caps }
func (f *fakeSvc) OwnID() store.PeerID { return store.PeerID("me-" + f.acct) }
func (f *fakeSvc) Paired() bool        { return true }
func newTestApp(t *testing.T) *App {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	waS := &fakeSvc{acct: "wa", caps: protocol.FullCaps()}
	wa9S := &fakeSvc{acct: "wa9", caps: protocol.Caps{Pin: true, Mute: true, DeleteChat: true}}
	return New(
		map[string]protocol.Service{"wa": waS, "wa9": wa9S},
		[]string{"wa"},
		nil, db, nil, nil,
	)
}

func TestSvcForRouting(t *testing.T) {
	a := newTestApp(t)
	cases := map[string]string{
		"1234@s.whatsapp.net":   "wa", // bare → primary
		"1234:7@s.whatsapp.net": "wa", // device part — "1234" isn't an account
		"grp-1@g.us":            "wa",
		"wa9:777000":            "wa9",
		"wa9:-100123":           "wa9",
	}
	for id, want := range cases {
		if got := a.SvcFor(store.PeerID(id)); got == nil || got.Acct() != want {
			t.Fatalf("SvcFor(%q) = %v want %s", id, got, want)
		}
	}
	if a.AcctOf(store.PeerID("wa9:777000")) != "wa9" || a.AcctOf(store.PeerID("x@s.whatsapp.net")) != "wa" {
		t.Fatal("AcctOf")
	}
	// An account-shaped prefix that isn't registered is a dead/foreign
	// account — nil, never the primary service.
	if got := a.SvcFor(store.PeerID("wa2:123@s.whatsapp.net")); got != nil {
		t.Fatalf("SvcFor dead account = %v", got.Acct())
	}
	if a.AcctOf(store.PeerID("wa99:777000")) != "" {
		t.Fatal("AcctOf dead account")
	}
}

func TestAnyPairedAndPrimary(t *testing.T) {
	a := newTestApp(t)
	if !a.AnyPaired() {
		t.Fatal("no acct paired")
	}
	if a.Primary().Acct() != "wa" {
		t.Fatalf("primary: %s", a.Primary().Acct())
	}
}

// TestChatMenuItemsCaps: capability flags filter menu actions — the wa9
// fake supports no disappearing/block/leave, so those entries are absent.
func TestChatMenuItemsCaps(t *testing.T) {
	a := newTestApp(t)
	has := func(items []MenuItem, id string) bool {
		for _, it := range items {
			if it.ID == id {
				return true
			}
		}
		return false
	}

	wa := &store.Chat{JID: "w@s.whatsapp.net", Kind: store.KindDM}
	items := a.ChatMenuItems(wa)
	for _, want := range []string{ActPin, ActArchive, ActMute, ActDisapp, ActBlock, ActDelete} {
		if !has(items, want) {
			t.Fatalf("wa menu missing %s", want)
		}
	}

	wa9 := &store.Chat{JID: "wa9:777000", Kind: store.KindDM}
	items = a.ChatMenuItems(wa9)
	if !has(items, ActPin) || !has(items, ActMute) || !has(items, ActDelete) {
		t.Fatalf("wa9 menu missing supported items")
	}
	for _, absent := range []string{ActArchive, ActDisapp, ActBlock} {
		if has(items, absent) {
			t.Fatalf("wa9 menu offered unsupported %s", absent)
		}
	}
}

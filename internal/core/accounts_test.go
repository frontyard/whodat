package core

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

func TestNextAcct(t *testing.T) {
	taken := map[string]bool{}
	if got := protocol.NextAcct("wa", taken); got != "wa" {
		t.Fatalf("first: %q", got)
	}
	taken["wa"] = true
	if got := protocol.NextAcct("wa", taken); got != "wa2" {
		t.Fatalf("second: %q", got)
	}
	taken["wa2"] = true
	taken["wa3"] = true
	if got := protocol.NextAcct("wa", taken); got != "wa4" {
		t.Fatalf("fourth: %q", got)
	}
}

func TestAccountsRoundTrip(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	recs := []store.AccountRec{{Acct: "wa", Proto: "whatsapp"}, {Acct: "wa2", Proto: "whatsapp"}}
	if err := db.SetAccounts(ctx, recs); err != nil {
		t.Fatal(err)
	}
	got, err := db.Accounts(ctx)
	if err != nil || len(got) != 2 || got[0].Acct != "wa" || got[1].Acct != "wa2" {
		t.Fatalf("accounts: %+v err %v", got, err)
	}
}

func TestAddRemoveAccount(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var registered, deregistered []string
	protocol.RegisterBackend(protocol.Backend{
		Proto: "testwa", Name: "Test WA", AcctBase: "twa", Multi: true,
		New: func(ctx context.Context, acct, dataDir string, db *store.DB, bus *protocol.Bus) (protocol.Service, error) {
			return &fakeSvc{acct: acct, proto: "testwa"}, nil
		},
	})
	protocol.RegisterBackend(protocol.Backend{
		Proto: "testsingle", Name: "Single", AcctBase: "tsg", Multi: false,
		New: func(ctx context.Context, acct, dataDir string, db *store.DB, bus *protocol.Bus) (protocol.Service, error) {
			return &fakeSvc{acct: acct, proto: "testsingle"}, nil
		},
	})

	a := New(map[string]protocol.Service{}, nil, nil, db, nil, nil)
	a.DataDir = t.TempDir()
	a.OnRegister = func(s protocol.Service) { registered = append(registered, s.Acct()) }
	a.OnDeregister = func(acct string) { deregistered = append(deregistered, acct) }
	ctx := context.Background()

	svc1, err := a.AddAccount(ctx, "testwa")
	if err != nil || svc1.Acct() != "twa" {
		t.Fatalf("add1: %v %v", svc1, err)
	}
	svc2, err := a.AddAccount(ctx, "testwa")
	if err != nil || svc2.Acct() != "twa2" {
		t.Fatalf("add2: %v %v", svc2, err)
	}
	if len(registered) != 2 || registered[0] != "twa" || registered[1] != "twa2" {
		t.Fatalf("registered: %v", registered)
	}

	// Routing: prefixed → its account, bare → primary (first registered).
	if a.SvcFor(store.PeerID("twa2:123")).Acct() != "twa2" || a.SvcFor(store.PeerID("123@x")).Acct() != "twa" {
		t.Fatal("routing")
	}
	recs, _ := db.Accounts(ctx)
	if len(recs) != 2 || recs[0].Acct != "twa" || recs[1].Acct != "twa2" {
		t.Fatalf("persisted: %+v", recs)
	}

	// Non-multi backend refuses a second live account.
	if _, err := a.AddAccount(ctx, "testsingle"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddAccount(ctx, "testsingle"); err == nil {
		t.Fatal("single-account backend allowed a second account")
	}

	// Removing the primary promotes the next account for Primary(), but
	// bare ids belong to the non-multi backend — a multi backend's account
	// only ever claims its own prefix.
	a.RemoveAccount(ctx, "twa")
	if len(deregistered) != 1 || deregistered[0] != "twa" {
		t.Fatalf("deregistered: %v", deregistered)
	}
	if a.Primary().Acct() != "twa2" || a.SvcFor(store.PeerID("123@x")).Acct() != "tsg" {
		t.Fatal("primary after remove")
	}
	recs, _ = db.Accounts(ctx)
	if len(recs) != 2 || recs[0].Acct != "twa2" {
		t.Fatalf("persisted after remove: %+v", recs)
	}
}

// TestAddAccountConcurrent hammers AddAccount from parallel goroutines:
// every returned service must own a distinct account id, and the registry
// must hold exactly that service — a check→allocate race lets two callers
// pick the same id and silently overwrite each other's live service.
func TestAddAccountConcurrent(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	protocol.RegisterBackend(protocol.Backend{
		Proto: "testconc", Name: "Conc", AcctBase: "tcc", Multi: true,
		New: func(ctx context.Context, acct, dataDir string, db *store.DB, bus *protocol.Bus) (protocol.Service, error) {
			return &fakeSvc{acct: acct, proto: "testconc"}, nil
		},
	})

	a := New(map[string]protocol.Service{}, nil, nil, db, nil, nil)
	a.DataDir = t.TempDir()
	ctx := context.Background()

	const n = 8
	svcs := make([]protocol.Service, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			svcs[i], errs[i] = a.AddAccount(ctx, "testconc")
		}(i)
	}
	wg.Wait()

	seen := map[string]bool{}
	for i := range svcs {
		if errs[i] != nil {
			t.Fatalf("add %d: %v", i, errs[i])
		}
		acct := svcs[i].Acct()
		if seen[acct] {
			t.Fatalf("two calls allocated %q", acct)
		}
		seen[acct] = true
		if got := a.Svc(acct); got != svcs[i] {
			t.Fatalf("registry holds a different service for %q — a live service was overwritten", acct)
		}
	}
}

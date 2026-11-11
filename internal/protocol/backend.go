package protocol

import (
	"context"
	"sort"
	"strconv"

	"github.com/frontyard/whodat/internal/store"
)

// Backend describes a protocol implementation the app can add accounts for.
// Backends self-register (init()) so the protocol picker lists whatever is
// compiled in — a new protocol is RegisterBackend + the Service impl.
type Backend struct {
	Proto    string // stable protocol key stored in the accounts table: "whatsapp"
	Name     string // display name: "WhatsApp"
	AcctBase string // account-id prefix allocated by NextAcct: "wa"
	// Multi allows more than one live account of this protocol. A backend
	// opts in once every id it stores or publishes is namespaced
	// ("<acct>:<id>").
	Multi bool

	// ScanHint — optional instruction shown under the pairing QR
	// ("Scan the QR code with WhatsApp → Linked devices.").
	ScanHint string

	// New instantiates one account. acct is the allocated account id ("wa",
	// "wa2"); dataDir is the app data dir — the backend picks its own
	// session filename inside it. The returned service's Acct() must equal
	// acct; events publish through bus.For(acct).
	New func(ctx context.Context, acct, dataDir string, db *store.DB, bus *Bus) (Service, error)
}

var backends = map[string]Backend{}

// RegisterBackend adds a protocol; called from each backend package's init.
func RegisterBackend(b Backend) { backends[b.Proto] = b }

// BackendOf returns the registered backend for a protocol key.
func BackendOf(proto string) (Backend, bool) { b, ok := backends[proto]; return b, ok }

// Backends lists every registered protocol, sorted for a stable UI order.
func Backends() []Backend {
	out := make([]Backend, 0, len(backends))
	for _, b := range backends {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// NextAcct allocates an account id for a protocol: the bare base ("wa")
// when free, else the smallest "base+N" suffix not in use ("wa2",
// "wa3").
func NextAcct(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		if cand := base + strconv.Itoa(i); !taken[cand] {
			return cand
		}
	}
}

// Stoppable is an optional Service interface: backends with background
// goroutines (sweepers, pollers) release them when the account is removed
// from the registry — called after the caller's own logout/wipe.
type Stoppable interface {
	Stop()
}

// (An in-progress auth flow is torn down by Disconnect + Logout from the
// authCancel op — the account is deleted, so there's no keep-alive
// "cancel but stay registered" path.)

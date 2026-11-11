// Package core holds the UI-agnostic glue between the backend services and
// the Qt/QML frontend. Everything here is pure Go —
// no widget imports — so the UI shares name resolution, formatting, menu
// models and avatar caching.
package core

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"

	"github.com/frontyard/whodat/internal/call"
	"github.com/frontyard/whodat/internal/config"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// App is the shared backend context handed to every frontend.
// Toast is installed by the UI layer (never nil in practice).
type App struct {
	DB    *store.DB
	Cfg   *config.Config
	CM    *call.Manager
	Bus   *protocol.Bus
	Toast func(msg string)
	// NotifPaused suppresses desktop notifications while set — toggled from
	// the tray menu. Session-only; the persistent toggle is Cfg.Notifications.
	NotifPaused atomic.Bool

	// avInflight dedupes concurrent RefreshAvatar runs per jid — events,
	// sidebar prefetch and the context menu can race on the same peer.
	avInflight sync.Map

	// DataDir is where backends keep their session stores — set by the
	// wiring layer before AddAccount is reachable.
	DataDir string
	// OnRegister/OnDeregister let the wiring layer attach per-account
	// resources (call providers) whenever the registry changes.
	OnRegister   func(protocol.Service)
	OnDeregister func(acct string)

	// Svcs is the account registry: account id → service. order fixes a
	// deterministic primary (first registered — "wa" on existing installs).
	// svcMu guards both — ops mutate the registry off the Qt thread while
	// the event loop reads it there. Read Svcs via Svc/EachSvc, never raw.
	Svcs  map[string]protocol.Service
	order []string
	svcMu sync.RWMutex
}

// New builds the shared context; the UI installs Toast afterwards.
func New(svcs map[string]protocol.Service, order []string, bus *protocol.Bus,
	db *store.DB, cfg *config.Config, cm *call.Manager) *App {
	return &App{DB: db, Cfg: cfg, CM: cm, Bus: bus, Svcs: svcs, order: order}
}

// Svc returns the service for acct — thread-safe accessor over Svcs.
func (a *App) Svc(acct string) protocol.Service {
	a.svcMu.RLock()
	defer a.svcMu.RUnlock()
	return a.Svcs[acct]
}

// EachSvc runs fn for every registered account in primary-first order,
// outside the registry lock.
func (a *App) EachSvc(fn func(acct string, svc protocol.Service)) {
	a.svcMu.RLock()
	type kv struct {
		acct string
		svc  protocol.Service
	}
	var all []kv
	for _, acct := range a.order {
		if s := a.Svcs[acct]; s != nil {
			all = append(all, kv{acct, s})
		}
	}
	a.svcMu.RUnlock()
	for _, e := range all {
		fn(e.acct, e.svc)
	}
}

// Primary returns the default account — used for account-level actions
// that take no chat id (logout, pairing).
func (a *App) Primary() protocol.Service {
	a.svcMu.RLock()
	defer a.svcMu.RUnlock()
	for _, acct := range a.order {
		if s, ok := a.Svcs[acct]; ok {
			return s
		}
	}
	for _, s := range a.Svcs {
		return s
	}
	return nil
}

// AcctOf returns the account prefix of a chat-scoped id when it names a
// registered account; an account-shaped but unregistered prefix is a
// dead/foreign id → ""; prefix-less forms (device JIDs, raw wire ids)
// attribute to the bare/primary account.
func (a *App) AcctOf(id store.PeerID) string {
	a.svcMu.RLock()
	defer a.svcMu.RUnlock()
	if _, ok := a.Svcs[id.Acct()]; ok {
		return id.Acct()
	}
	if accountShapedPrefix(id.Acct()) {
		return "" // id belongs to a removed/foreign account
	}
	if s := a.bareLocked(); s != nil {
		return s.Acct()
	}
	return ""
}

// SvcFor resolves the service owning a chat-scoped id. Ids carrying a
// registered account prefix route to it; an account-shaped prefix that
// isn't registered means a dead/foreign account and gets nil rather than
// the wrong service; only truly prefix-less ids (device JIDs, raw wire
// forms) fall through to bareLocked.
func (a *App) SvcFor(id store.PeerID) protocol.Service {
	a.svcMu.RLock()
	defer a.svcMu.RUnlock()
	if s, ok := a.Svcs[id.Acct()]; ok {
		return s
	}
	if accountShapedPrefix(id.Acct()) {
		return nil
	}
	return a.bareLocked()
}

// accountShapedPrefix reports whether a prefix looks like an account name —
// letter-led alphanumeric, same rule ParsePeerID enforces — so a digit-led
// device part ("1234:40@…") is never mistaken for an account.
func accountShapedPrefix(acct string) bool {
	if acct == "" || acct[0] < 'a' || acct[0] > 'z' {
		return false
	}
	for i := 1; i < len(acct); i++ {
		c := acct[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// primaryLocked is Primary under svcMu.
func (a *App) primaryLocked() protocol.Service {
	for _, acct := range a.order {
		if s, ok := a.Svcs[acct]; ok {
			return s
		}
	}
	for _, s := range a.Svcs {
		return s
	}
	return nil
}

// bareLocked resolves the fallback for ids with no usable account prefix:
// the account behind a non-multi backend owns the bare namespace (an id
// can't disambiguate which of several accounts it targets); with every
// backend namespaced it degenerates to the primary account. Registration
// order can't answer this — an early namespaced account must not absorb
// another backend's unprefixed ids.
func (a *App) bareLocked() protocol.Service {
	for _, s := range a.Svcs {
		if b, ok := protocol.BackendOf(s.Protocol()); ok && !b.Multi {
			return s
		}
	}
	return a.primaryLocked()
}

// AnyPaired reports whether any account holds an authorized session.
func (a *App) AnyPaired() bool {
	paired := false
	a.EachSvc(func(_ string, s protocol.Service) {
		if s.Paired() {
			paired = true
		}
	})
	return paired
}

// AccountInfo describes one registered account for the settings UI.
type AccountInfo struct {
	Acct   string
	Proto  string // backend key: "whatsapp"
	Name   string // display label
	Paired bool
	OwnID  string // "" until paired
}

// Accounts lists every registered account in primary-first order.
func (a *App) Accounts() []AccountInfo {
	var out []AccountInfo
	a.EachSvc(func(acct string, s protocol.Service) {
		info := AccountInfo{Acct: acct, Proto: s.Protocol(), Paired: s.Paired(), OwnID: s.OwnID().String()}
		if b, ok := protocol.BackendOf(s.Protocol()); ok {
			info.Name = b.Name
		} else {
			info.Name = s.Protocol()
		}
		out = append(out, info)
	})
	return out
}

// AddAccount allocates an id for proto, instantiates the backend's service,
// registers it and persists the account list. The caller then Connect()s.
func (a *App) AddAccount(ctx context.Context, proto string) (protocol.Service, error) {
	b, ok := protocol.BackendOf(proto)
	if !ok {
		return nil, fmt.Errorf("unknown protocol %q", proto)
	}
	// The whole check → allocate → register path must hold the lock: two
	// concurrent calls would otherwise pick the same account id and the
	// second registration would silently replace the first live service.
	a.svcMu.Lock()
	taken := make(map[string]bool, len(a.Svcs))
	live := 0
	for acct, s := range a.Svcs {
		taken[acct] = true
		if s.Protocol() == proto {
			live++
		}
	}
	if live > 0 && !b.Multi {
		a.svcMu.Unlock()
		return nil, fmt.Errorf("only one %s account is supported", b.Name)
	}
	svc, err := b.New(ctx, protocol.NextAcct(b.AcctBase, taken), a.DataDir, a.DB, a.Bus)
	if err != nil {
		a.svcMu.Unlock()
		return nil, err
	}
	a.registerLocked(svc)
	a.svcMu.Unlock()
	if a.OnRegister != nil {
		a.OnRegister(svc)
	}
	a.persistAccounts(context.Background())
	return svc, nil
}

// RemoveAccount drops acct from the registry and the persisted list. The
// caller is expected to have logged out/wiped first — and must call this
// BEFORE the service publishes EvLoggedOut, or the restart path resurrects
// the account.
func (a *App) RemoveAccount(ctx context.Context, acct string) {
	a.svcMu.Lock()
	svc, ok := a.Svcs[acct]
	if !ok {
		a.svcMu.Unlock()
		return
	}
	if st, ok := svc.(protocol.Stoppable); ok {
		st.Stop()
	}
	delete(a.Svcs, acct)
	order := a.order[:0]
	for _, id := range a.order {
		if id != acct {
			order = append(order, id)
		}
	}
	a.order = order
	a.svcMu.Unlock()
	if a.OnDeregister != nil {
		a.OnDeregister(acct)
	}
	a.persistAccounts(ctx)
}

// registerLocked inserts svc into the registry; the caller must hold svcMu.
func (a *App) registerLocked(svc protocol.Service) {
	acct := svc.Acct()
	if _, exists := a.Svcs[acct]; !exists {
		a.order = append(a.order, acct)
	}
	a.Svcs[acct] = svc
}

// register inserts a service, propagates peers and persists the list.
func (a *App) register(svc protocol.Service) {
	a.svcMu.Lock()
	a.registerLocked(svc)
	a.svcMu.Unlock()
	if a.OnRegister != nil {
		a.OnRegister(svc)
	}
	a.persistAccounts(context.Background())
}

// persistAccounts writes the registry to kv — order-preserving, paired
// only: an unlinked registration is a link-in-progress and must not
// outlive the process (a restart would resurrect it as a "not linked"
// zombie). The EvPaired handler persists once pairing lands.
func (a *App) persistAccounts(ctx context.Context) {
	if a.DB == nil {
		return
	}
	a.svcMu.RLock()
	recs := make([]store.AccountRec, 0, len(a.order))
	for _, acct := range a.order {
		if s := a.Svcs[acct]; s != nil && s.Paired() {
			recs = append(recs, store.AccountRec{Acct: acct, Proto: s.Protocol()})
		}
	}
	a.svcMu.RUnlock()
	if err := a.DB.SetAccounts(ctx, recs); err != nil {
		log.Printf("persist accounts: %v", err)
	}
}

// PersistAccounts forces a registry write — called by the UI host on
// EvPaired, the moment an account becomes worth persisting (its
// registration-time write was skipped while unpaired).
func (a *App) PersistAccounts(ctx context.Context) {
	a.persistAccounts(ctx)
}

// LoadAccounts instantiates every persisted account through its backend and
// registers it — startup path. Unknown protocols are skipped with a log.
func (a *App) LoadAccounts(ctx context.Context, recs []store.AccountRec) {
	for _, rec := range recs {
		b, ok := protocol.BackendOf(rec.Proto)
		if !ok {
			log.Printf("account %s: no backend for protocol %q — skipping", rec.Acct, rec.Proto)
			continue
		}
		svc, err := b.New(ctx, rec.Acct, a.DataDir, a.DB, a.Bus)
		if err != nil {
			log.Printf("account %s: init: %v", rec.Acct, err)
			continue
		}
		// Rows written by older builds can name an account that never
		// finished pairing — skip it rather than resurrect a "not linked"
		// zombie that QR-streams in the background every launch.
		if !svc.Paired() {
			log.Printf("account %s: never linked — dropping registration", rec.Acct)
			continue
		}
		a.register(svc)
	}
}

// ConnectAll starts every account's connection.
func (a *App) ConnectAll(ctx context.Context) {
	a.EachSvc(func(_ string, s protocol.Service) {
		go func() {
			if err := s.Connect(ctx); err != nil {
				log.Printf("%s connect: %v", s.Acct(), err)
			}
		}()
	})
}

// MediaPath resolves a message's attachment to a local path, routing the
// fetch to the owning account's backend.
func (a *App) MediaPath(ctx context.Context, m *store.Message) (string, error) {
	if m.MediaPath != "" {
		if _, err := os.Stat(m.MediaPath); err == nil {
			return m.MediaPath, nil
		}
	}
	svc := a.SvcFor(m.ChatJID)
	if svc == nil || !svc.CanFetchMedia(m) {
		return "", fmt.Errorf("no media ref for download")
	}
	return svc.FetchMedia(ctx, m)
}

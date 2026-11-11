// Package qtui implements the Qt6/QML interface.
package qtui

import (
	"context"
	"log"
	"path/filepath"

	"github.com/frontyard/whodat/internal/call"
	"github.com/frontyard/whodat/internal/config"
	"github.com/frontyard/whodat/internal/core"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"

	_ "github.com/frontyard/whodat/internal/wa" // register the WhatsApp backend
)

const appID = "io.github.frontyard.whodat"

// version is injected by the build.
var version = "dev"

// SetVersion sets the app version from main.
func SetVersion(v string) { version = v }

// Run builds everything and enters the Qt main loop. Returns exit code.
func Run() int {
	if err := config.EnsureDirs(); err != nil {
		log.Printf("config: %v", err)
	}
	cfg := config.Load()

	db, err := store.Open(filepath.Join(config.DataDir(), "whodat.db"))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	bus := protocol.NewBus()
	cm := call.New()

	app := core.New(map[string]protocol.Service{}, nil, bus, db, cfg, cm)
	app.DataDir = config.DataDir()
	// Call providers adapt a protocol's signalling — the call package owns
	// the backend→provider binding so the UI never names a concrete service.
	app.OnRegister = func(svc protocol.Service) {
		if p := call.ProviderFor(svc, bus); p != nil {
			bare := false
			if b, ok := protocol.BackendOf(svc.Protocol()); ok {
				bare = !b.Multi
			}
			cm.RegisterProvider(svc.Acct(), p, bare)
		}
	}
	app.OnDeregister = func(acct string) {
		cm.UnregisterProvider(acct)
	}

	ctx := context.Background()
	recs, err := db.Accounts(ctx)
	if err != nil {
		log.Printf("load accounts: %v", err)
	}
	app.LoadAccounts(ctx, recs)

	host := NewHost(app)
	app.Toast = host.Toast

	code := host.Run()

	app.EachSvc(func(_ string, s protocol.Service) { s.Disconnect() })
	_ = db.Close()
	return code
}

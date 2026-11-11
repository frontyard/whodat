package wa

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/polymorfa/hypermeow"
	"github.com/polymorfa/hypermeow/proto/waCompanionReg"
	wastore "github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/store/sqlstore"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	waLog "github.com/polymorfa/hypermeow/util/log"

	"github.com/frontyard/whodat/internal/config"
	"github.com/frontyard/whodat/internal/protocol"
	appstore "github.com/frontyard/whodat/internal/store"
)

// acctBase is the account-id prefix WhatsApp accounts allocate from —
// "wa", "wa2", "wa3", … Every account namespaces its ids as
// "<acct>:<wire-jid>", so several accounts can coexist without their
// chat/message rows colliding.
const acctBase = "wa"
const acctWA = acctBase // first-allocated id — wastore.db keeps the plain name

// init registers the WhatsApp backend so the account picker can add
// WhatsApp accounts through protocol.Backends().
func init() {
	protocol.RegisterBackend(protocol.Backend{
		Proto:    "whatsapp",
		Name:     "WhatsApp",
		AcctBase: acctBase,
		Multi:    true, // every stored id carries the account prefix
		ScanHint: "Scan the QR code with WhatsApp → Linked devices.",
		New: func(ctx context.Context, acct, dataDir string, db *appstore.DB, bus *protocol.Bus) (protocol.Service, error) {
			return New(ctx, acct, dataDir, db, bus)
		},
	})
}

var (
	_ protocol.Service        = (*Service)(nil)
	_ protocol.SessionLister  = (*Service)(nil)
	_ protocol.ContactLinker  = (*Service)(nil)
	_ protocol.PeerResolver   = (*Service)(nil)
	_ protocol.PrivacyManager = (*Service)(nil)
	_ protocol.Verifier       = (*Service)(nil)
	_ protocol.PeerIDs        = (*Service)(nil)
	_ protocol.Stoppable      = (*Service)(nil)
)

// Service owns the whatsmeow client, the session container, and the event bus.
type Service struct {
	// client is the active whatsmeow client — atomic because Reset() swaps
	// it on the EvLoggedOut restart path while event handlers still run on
	// the old client's goroutines. Read it via Client(), never cache it.
	client    atomic.Pointer[whatsmeow.Client]
	sink      *protocol.Sink
	container *sqlstore.Container
	db        *appstore.DB
	waDB      string // path to wastore.db — for post-Delete table purge
	acct      string // this account's id — the chat-id prefix for routing
	log       waLog.Logger

	// stopCh releases the sweeper goroutine on account removal (Stop).
	stopCh   chan struct{}
	stopOnce sync.Once

	mu       sync.Mutex
	pairing  bool
	qrCancel context.CancelFunc
}

// New opens one WhatsApp account's session container under dataDir and
// builds a client. acct is the allocated account id — "wa" keeps the
// plain wastore.db filename, later accounts get wastore-<acct>.db so
// several WhatsApp accounts can coexist. bus is the app-wide event bus;
// events published through Bus are stamped with this account's id.
func New(ctx context.Context, acct, dataDir string, db *appstore.DB, bus *Bus) (*Service, error) {
	if acct == "" {
		acct = acctWA
	}
	level := os.Getenv("WHODAT_WA_LOG")
	if level == "" {
		level = "WARN"
	}
	log := waLog.Stdout("whodat-wa-"+acct, level, false)
	waName := "wastore.db"
	if acct != acctWA {
		waName = "wastore-" + acct + ".db"
	}
	waDB := filepath.Join(dataDir, waName)
	container, err := sqlstore.New(ctx, "sqlite",
		fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", waDB), log)
	if err != nil {
		return nil, fmt.Errorf("open session store: %w", err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("get device: %w", err)
	}
	s := &Service{
		sink:      protocol.NewSink(acct, db, bus),
		container: container,
		db:        db,
		waDB:      waDB,
		acct:      acct,
		log:       log,
		stopCh:    make(chan struct{}),
	}
	// Periodic sweep for messages whose disappearing timer elapsed while the
	// app stayed open (the connect-time sweep only catches startup state).
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				s.dbDo(s.sweepExpired)
			case <-s.stopCh:
				return
			}
		}
	}()
	// The library's default companion-device props identify as "whatsmeow" —
	// that's what shows up under Linked devices on the phone. Present as
	// WhoDat, desktop.
	wastore.SetOSInfo("WhoDat", [3]uint32{1, 0, 0})
	wastore.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
	s.client.Store(whatsmeow.NewClient(device, log))
	s.Client().AddEventHandler(s.dispatch)
	s.Client().SetForceActiveDeliveryReceipts(false)
	return s, nil
}

// DB exposes the application store for derived services.
func (s *Service) DB() *appstore.DB { return s.db }

// dbDo enqueues a DB-bound task on the shared serial writer, gated on this
// account's wipe flag. During a logout wipe new writes are dropped — they
// come from a dead session and would commit after the purge, resurrecting
// rows (an in-flight history sync can enqueue for seconds).
func (s *Service) dbDo(fn func(ctx context.Context)) {
	s.sink.Do(fn)
}

// Client returns the active whatsmeow client. It can be swapped by Reset —
// call this per use instead of keeping a copy.
func (s *Service) Client() *whatsmeow.Client { return s.client.Load() }

// Stop releases the account's background goroutines — called when the
// account is removed (protocol.Stoppable).
func (s *Service) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// Paired reports whether this device already holds a session.
func (s *Service) Paired() bool {
	c := s.Client()
	return c != nil && c.Store != nil && c.Store.ID != nil
}

// Acct is the account id used as the chat-id prefix.
func (s *Service) Acct() string { return s.acct }

// cid namespaces a wire JID for the app store/bus: "wa:<jid>". Input is
// always the raw wire form — wire ids can't contain an account prefix.
func (s *Service) cid(wire string) appstore.PeerID {
	return appstore.NewID(s.acct, wire)
}

// jid parses an app-facing peer id into a wire JID. The id must belong to
// this account — a foreign account's id that routing failed on is refused
// rather than parsed as a garbage user part.
func (s *Service) jid(id appstore.PeerID) (types.JID, error) {
	acct := id.Acct()
	// A non-account-shaped prefix (empty, or digit-led like a WhatsApp
	// device part "1234:40@…") means the id is already in wire form.
	if acct != "" && acct[0] >= 'a' && acct[0] <= 'z' {
		if acct != s.acct {
			return types.JID{}, fmt.Errorf("foreign account id %q (owned by %q, this is %q)", id, acct, s.acct)
		}
		return ParseJID(id.Wire())
	}
	return ParseJID(id.String())
}

// Protocol is the human-readable protocol name.
func (s *Service) Protocol() string { return "whatsapp" }

// Caps reports this account's feature set.
func (s *Service) Caps() protocol.Caps { return protocol.FullCaps() }

// ChatE2EE reports whether the chat rides Signal-protocol E2E — every
// WhatsApp conversation does (DMs, groups, broadcast lists) except
// newsletters, which are server-visible broadcasts. protocol.ChatEncryptor.
func (s *Service) ChatE2EE(ctx context.Context, chatID appstore.PeerID) (bool, error) {
	c, err := s.db.GetChat(ctx, chatID)
	if err != nil {
		return false, err
	}
	if c == nil {
		return false, nil
	}
	return c.Kind != appstore.KindNewsletter, nil
}

// OwnID returns our account JID once paired — namespaced like every other
// app-facing id and without the device part, so it compares equal to
// sender JIDs stored by other code paths.
func (s *Service) OwnID() appstore.PeerID {
	if !s.Paired() {
		return ""
	}
	return s.cid(s.Client().Store.ID.ToNonAD().String())
}

// Connect starts the socket. If unpaired, a QR flow is started first; the
// pairing alternative is PairWithPhone after Connect returns.
func (s *Service) Connect(ctx context.Context) error {
	if s.Client().IsConnected() {
		return nil
	}
	if !s.Paired() {
		if err := s.startQR(ctx); err != nil {
			return err
		}
		// Pairing isn't syncing — the post-pair history dump publishes
		// its own EvSyncStart (onHistorySync); claiming sync here shows
		// the "Syncing messages…" pill over the QR screen.
	} else {
		s.sink.Publish(Event{Kind: EvSyncStart})
	}
	return s.Client().Connect()
}

// startQR wires the QR channel; the first code arrives right after Connect.
func (s *Service) startQR(ctx context.Context) error {
	s.mu.Lock()
	if s.pairing {
		s.mu.Unlock()
		return nil
	}
	s.pairing = true
	s.mu.Unlock()

	qrCtx, cancel := context.WithCancel(context.Background())
	s.qrCancel = cancel
	ch, err := s.Client().GetQRChannel(qrCtx)
	if err != nil {
		s.pairing = false
		return err
	}
	go func() {
		for item := range ch {
			switch item.Event {
			case whatsmeow.QRChannelEventCode:
				s.sink.Publish(Event{Kind: EvQRCode, QRCode: item.Code})
			case "success":
				s.sink.Publish(Event{Kind: EvPaired})
			case whatsmeow.QRChannelEventError, "err-unexpected-state", "err-client-outdated":
				s.sink.Publish(Event{Kind: EvLoggedOut, Err: item.Error})
			}
		}
		s.mu.Lock()
		s.pairing = false
		s.mu.Unlock()
	}()
	return nil
}

// PairWithPhone requests an 8-digit pairing code (call after Connect while unpaired).
func (s *Service) PairWithPhone(ctx context.Context, phone string) (string, error) {
	if s.Paired() {
		return "", errors.New("already paired")
	}
	return s.Client().PairPhone(ctx, phone, true, whatsmeow.PairClientFirefox, "Firefox (Linux)")
}

// Disconnect closes the socket; the session remains paired.
func (s *Service) Disconnect() { s.Client().Disconnect() }

// Logout unlinks this device and removes the local session and all local data.
func (s *Service) Logout(ctx context.Context) error {
	s.mu.Lock()
	if s.qrCancel != nil {
		s.qrCancel()
		s.qrCancel = nil
	}
	s.pairing = false
	s.mu.Unlock()

	// Capture the client — Reset() may swap s.Client() concurrently (the
	// EvLoggedOut restart path), and Delete must hit the right device row.
	c := s.Client()
	var err error
	// Server unlink only makes sense paired: a connected-but-sessionless
	// client (mid QR pair) rejects Logout outright.
	if c.Store.ID != nil {
		err = c.Logout(ctx)
	}
	c.Disconnect()
	if c.Store.ID != nil {
		if derr := c.Store.Delete(ctx); derr != nil {
			s.log.Warnf("delete device store: %v", derr)
		}
	}
	s.wipeLocalData(ctx)
	// hypermeow emits events.LoggedOut only for server-side revocations —
	// publish it ourselves so the UI returns to login and restarts pairing.
	s.sink.Publish(Event{Kind: EvLoggedOut})
	return err
}

// wipeLocalData erases every local trace of the account: app-DB rows,
// whatsmeow tables that outlive Store.Delete (lid map and privacy tokens
// have no ON DELETE CASCADE), downloaded media, avatars and cached QR
// codes. Runs before EvLoggedOut is published so the re-pair flow can't
// interleave fresh rows into a half-wiped store.
func (s *Service) wipeLocalData(ctx context.Context) {
	// Set the wipe gate, then enqueue the purge on the same serial writer:
	// anything queued before commits before the purge and gets deleted;
	// anything after is dropped by DoFor. Cleared on PairSuccess — a dead
	// session writes nothing.
	s.db.SetWiping(s.acct, true)
	done := make(chan struct{})
	if s.db.Do(func(ctx context.Context) {
		defer close(done)
		if err := s.db.PurgeAccount(ctx, s.acct); err != nil {
			s.log.Warnf("logout wipe: app db: %v", err)
		}
	}) {
		// Wait unconditionally — a ctx.Done() branch would read the results
		// while the task still writes them. The task's own 60s bound keeps
		// this finite; a dropped task (DB closed) returns false and runs
		// nothing.
		<-done
	}

	// Clear every whatsmeow_* table — Store.Delete only covers the
	// FK-cascaded ones; enumeration keeps the wipe correct across upgrades.
	// The version row is schema metadata, not session data: capture and
	// restore it. A wiped store must look like a fresh one — without the
	// marker, sqlstore reads v0 and replays CREATE TABLEs onto existing
	// tables on the next start.
	if sqldb, err := sql.Open("sqlite",
		fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", s.waDB)); err == nil {
		var ver, compat sql.NullInt32
		_ = sqldb.QueryRowContext(ctx,
			`SELECT version, compat FROM whatsmeow_version LIMIT 1`).Scan(&ver, &compat)
		var tables []string
		if rows, err := sqldb.QueryContext(ctx, `SELECT name FROM sqlite_master
			WHERE type='table' AND name LIKE 'whatsmeow\_%' ESCAPE '\'`); err == nil {
			for rows.Next() {
				var n string
				if rows.Scan(&n) == nil {
					tables = append(tables, n)
				}
			}
			rows.Close()
		}
		for _, t := range tables {
			if _, err := sqldb.ExecContext(ctx, `DELETE FROM "`+t+`"`); err != nil {
				s.log.Warnf("logout wipe: %s: %v", t, err)
			}
		}
		if ver.Valid {
			if _, err := sqldb.ExecContext(ctx,
				`INSERT INTO whatsmeow_version (version, compat) VALUES ($1, $2)`, ver.Int32, compat.Int32); err != nil {
				s.log.Warnf("logout wipe: restore version: %v", err)
			}
		}
		_, _ = sqldb.ExecContext(ctx, `VACUUM`)
		_ = sqldb.Close()
	}

	// Delete this account's files. The media and avatar dirs are shared, so
	// only paths owned by the deleted rows go — another account's files must
	// Media and avatar dirs are account-scoped — RemoveAll erases every
	// file this account ever wrote, row-tracked or orphaned, and can never
	// touch another account's files.
	for _, d := range []string{config.MediaAcctDir(s.acct), config.AvatarAcctDir(s.acct)} {
		if err := os.RemoveAll(d); err != nil {
			s.log.Warnf("logout wipe: %s: %v", d, err)
		}
		_ = os.MkdirAll(d, 0o700)
	}
	if qrs, err := filepath.Glob(filepath.Join(config.CacheDir(), "qr-"+s.acct+"-*.png")); err == nil {
		for _, p := range qrs {
			_ = os.Remove(p)
		}
	}
	_ = os.Remove(filepath.Join(config.CacheDir(), "myqr-"+s.acct+".png"))
	_ = os.Remove(filepath.Join(config.StateDir(), "status.json"))
}

// Reset tears down the client after logout/revoke and builds a fresh one —
// a logged-out client can never connect again, and nothing restarted the QR
// flow before. Callers follow with Connect() to pair a new session.
func (s *Service) Reset(ctx context.Context) error {
	s.mu.Lock()
	if s.qrCancel != nil {
		s.qrCancel()
		s.qrCancel = nil
	}
	s.pairing = false
	s.mu.Unlock()

	s.Client().Disconnect()
	device, err := s.container.GetFirstDevice(ctx)
	if err != nil {
		return fmt.Errorf("get device: %w", err)
	}
	s.client.Store(whatsmeow.NewClient(device, s.log))
	s.Client().AddEventHandler(s.dispatch)
	s.Client().SetForceActiveDeliveryReceipts(false)
	return nil
}

// dispatch is the single whatsmeow event handler; runs on wa goroutines.
func (s *Service) dispatch(evt any) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	switch e := evt.(type) {
	case *events.Connected:
		s.sink.Publish(Event{Kind: EvConnected})
		go func() {
			_ = s.Client().SendPresence(context.Background(), types.PresenceAvailable)
			s.dbDo(s.syncContacts)
		}()
		s.dbDo(func(ctx context.Context) { s.sweepExpired(ctx) })
	case *events.Disconnected:
		s.sink.Publish(Event{Kind: EvDisconnected})
	case *events.LoggedOut:
		s.wipeLocalData(ctx)
		s.sink.Publish(Event{Kind: EvLoggedOut})
	case *events.PairSuccess:
		// New session established — writes flow again (clears the logout
		// wipe gate; also the reason wiping isn't cleared in wipeLocalData).
		s.db.SetWiping(s.acct, false)
		s.sink.Publish(Event{Kind: EvPaired})
	case *events.PairError:
		s.sink.Publish(Event{Kind: EvLoggedOut, Err: e.Error})
	case *events.ManualLoginReconnect:
		// Re-pair flow requested by server after logout — the session is
		// dead but hypermeow doesn't delete the store on this path, so
		// mark it deleted before the wipe (blocks a stray save from
		// resurrecting the device row).
		if c := s.Client(); c.Store != nil && c.Store.ID != nil {
			_ = c.Store.Delete(ctx)
		}
		s.wipeLocalData(ctx)
		s.sink.Publish(Event{Kind: EvLoggedOut})
	case *events.Message:
		s.dbDo(func(ctx context.Context) { s.onMessage(ctx, e) })
	case *events.Receipt:
		s.dbDo(func(ctx context.Context) { s.onReceipt(ctx, e) })
	case *events.HistorySync:
		go s.onHistorySync(e)
	case *events.OfflineSyncCompleted:
		s.dbDo(func(c context.Context) {
			s.syncContacts(c)
			s.sink.Publish(Event{Kind: EvSyncDone})
		})
	case *events.AppStateSyncComplete:
		s.dbDo(func(c context.Context) {
			s.syncContacts(c)
			s.sink.Publish(Event{Kind: EvSyncDone})
		})
	case *events.ChatPresence:
		s.sink.Publish(Event{Kind: EvTyping, Typing: &TypingInfo{
			ChatJID:   s.canon(ctx, s.cid(e.MessageSource.Chat.String())),
			SenderJID: s.cid(e.MessageSource.Sender.ToNonAD().String()),
			Composing: e.State == types.ChatPresenceComposing,
			Recording: e.Media == types.ChatPresenceMediaAudio,
		}})
	case *events.Presence:
		s.sink.Publish(Event{Kind: EvPresence, Typing: &TypingInfo{
			ChatJID: s.canon(ctx, s.cid(e.From.String())), Online: !e.Unavailable,
			LastSeen: e.LastSeen,
		}})
	case *events.PushName:
		if e.NewPushName != "" {
			jid, name := e.JID, e.NewPushName
			s.dbDo(func(ctx context.Context) {
				_ = s.db.UpsertContact(ctx, &appstore.Contact{JID: s.cid(jid.String()), PushName: name})
				// Mirror under the LID form so LID-keyed chat rows join the
				// contact row and pick the new name up too.
				if s.Client().Store != nil && s.Client().Store.LIDs != nil {
					if lid, err := s.Client().Store.LIDs.GetLIDForPN(ctx, jid); err == nil && !lid.IsEmpty() {
						_ = s.db.UpsertContact(ctx, &appstore.Contact{JID: s.cid(lid.String()), PushName: name})
					}
				}
				s.sink.ChatChanged(s.cid(jid.String()))
			})
		}
	case *events.Picture:
		if !e.JID.IsEmpty() {
			// Core re-downloads (or clears) the cached picture; publishing
			// only EvAvatarChanged keeps this handler off the network path.
			s.sink.AvatarChanged(s.canon(ctx, s.cid(e.JID.String())), e.Remove)
		}
	case *events.GroupInfo:
		if e.Name != nil && e.Name.Name != "" {
			jid := s.cid(e.JID.String())
			s.dbDo(func(ctx context.Context) {
				_ = s.db.UpsertChat(ctx, &appstore.Chat{JID: jid, Kind: appstore.KindGroup, Name: e.Name.Name})
				s.sink.ChatChanged(jid)
			})
		}
	case *events.JoinedGroup:
		jid := s.cid(e.JID.String())
		s.dbDo(func(ctx context.Context) {
			_ = s.db.UpsertChat(ctx, &appstore.Chat{JID: jid, Kind: appstore.KindGroup, Name: e.GroupName.Name})
			s.sink.ChatChanged(jid)
		})
	case *events.DeleteForMe:
		if !e.ChatJID.IsEmpty() && e.MessageID != "" {
			cj, mid := s.cid(e.ChatJID.String()), e.MessageID
			s.dbDo(func(ctx context.Context) {
				if paths, err := s.db.DeleteMessage(ctx, cj, mid); err == nil {
					for _, p := range paths {
						_ = os.Remove(p)
					}
				}
				s.sink.ChatChanged(cj)
			})
		}
	case *events.Archive:
		arch := e.Action.GetArchived()
		jid := s.cid(e.JID.String())
		s.dbDo(func(ctx context.Context) {
			_ = s.db.SetChatFlags(ctx, jid, nil, &arch, nil)
			s.sink.ChatChanged(jid)
		})
	case *events.Pin:
		v := e.Action.GetPinned()
		jid := s.cid(e.JID.String())
		s.dbDo(func(ctx context.Context) {
			_ = s.db.SetChatFlags(ctx, jid, &v, nil, nil)
			s.sink.ChatChanged(jid)
		})
	case *events.Mute:
		v := e.Action.GetMuted()
		jid := s.cid(e.JID.String())
		s.dbDo(func(ctx context.Context) {
			_ = s.db.SetChatFlags(ctx, jid, nil, nil, &v)
			s.sink.ChatChanged(jid)
		})
	case *events.MarkChatAsRead:
		// Read/unread toggled on another device — mirror it so our badge
		// doesn't stay stale.
		read := e.Action.GetRead()
		jid := s.cid(e.JID.String())
		s.dbDo(func(ctx context.Context) {
			n := 1
			if read {
				n = 0
			}
			_ = s.db.SetUnread(ctx, jid, n)
			s.sink.ChatChanged(s.canon(ctx, jid))
		})
	case *events.ClearChat:
		jid := s.cid(e.JID.String())
		s.dbDo(func(ctx context.Context) {
			paths, err := s.db.ClearChatMessages(ctx, jid)
			if err == nil && e.DeleteMedia {
				for _, p := range paths {
					_ = os.Remove(p)
				}
			}
			cj := s.canon(ctx, jid)
			s.sink.Publish(Event{Kind: EvChatGone, ChatJID: cj})
			s.sink.ChatChanged(cj)
		})
	case *events.DeleteChat:
		jid := s.cid(e.JID.String())
		s.dbDo(func(ctx context.Context) {
			paths, err := s.db.DeleteChatRows(ctx, jid)
			if err == nil && e.DeleteMedia {
				for _, p := range paths {
					_ = os.Remove(p)
				}
			}
			cj := s.canon(ctx, jid)
			s.sink.Publish(Event{Kind: EvChatGone, ChatJID: cj})
			s.sink.ChatChanged(cj)
		})
	case *events.Star:
		if !e.ChatJID.IsEmpty() && e.MessageID != "" {
			cj, mid := s.cid(e.ChatJID.String()), e.MessageID
			starred := e.Action.GetStarred()
			s.dbDo(func(ctx context.Context) {
				if err := s.db.SetStarred(ctx, cj, mid, starred); err == nil {
					s.publishMsgUpdate(ctx, cj, mid)
				}
			})
		}
	case *events.UserAbout:
		if !e.JID.IsEmpty() {
			jid, about := s.cid(e.JID.String()), e.Status
			s.dbDo(func(ctx context.Context) {
				_ = s.db.SetContactAbout(ctx, jid, about)
				_ = s.db.SetContactAbout(ctx, s.canon(ctx, jid), about)
			})
		}
	case *events.UndecryptableMessage:
		// Keep a placeholder so the gap in the transcript is visible; the
		// real row may still arrive on retry or history sync.
		info := e.Info
		if info.ID != "" {
			s.dbDo(func(ctx context.Context) {
				m := &appstore.Message{
					ChatJID: s.cid(info.Chat.String()), ID: info.ID,
					SenderJID: s.cid(info.Sender.ToNonAD().String()), SenderName: info.PushName,
					TS: info.Timestamp, FromMe: info.IsFromMe,
					Type: appstore.TSystem,
					Text: "Couldn't decrypt this message — it may arrive after a resync",
				}
				s.persistMessage(ctx, m, jidKind(info.Chat), false)
			})
		}
	case *events.IdentityChange:
		// A peer's E2E key changed — surface once per day, not on every
		// connect. Only inside a conversation that already carried real
		// traffic: with no prior messages the notice would be the chat's
		// sole row and manufacture a phantom sidebar entry.
		jid := s.cid(e.JID.String())
		s.dbDo(func(ctx context.Context) {
			if !s.db.HasRealMessages(ctx, jid) {
				return
			}
			text := "Security code changed for this chat"
			if s.db.RecentSystemText(ctx, jid, text, time.Now().Add(-24*time.Hour)) {
				return
			}
			m := &appstore.Message{
				ChatJID: jid, ID: fmt.Sprintf("idchg-%d", e.Timestamp.UnixNano()),
				TS: e.Timestamp, Type: appstore.TSystem, Text: text,
			}
			s.persistMessage(ctx, m, jidKind(e.JID), false)
		})
	case *events.Blocklist:
		s.dbDo(func(ctx context.Context) { s.applyBlocklist(ctx, e) })
	}
}

// sweepExpired deletes overdue disappearing messages and their media files.
func (s *Service) sweepExpired(ctx context.Context) {
	paths, err := s.db.DeleteExpired(ctx, time.Now())
	if err != nil {
		return
	}
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

// applyBlocklist mirrors server blocklist events into contacts.blocked.
func (s *Service) applyBlocklist(ctx context.Context, e *events.Blocklist) {
	if e.Action == "modify" || len(e.Changes) == 0 {
		// Whole list re-requested.
		bl, err := s.Client().GetBlocklist(ctx, "")
		if err != nil {
			return
		}
		for _, it := range bl.Items {
			jid := it.PN
			if jid.IsEmpty() {
				jid = it.LID
			}
			if !jid.IsEmpty() {
				_ = s.db.SetBlocked(ctx, s.cid(jid.String()), it.Active)
			}
		}
		return
	}
	for _, ch := range e.Changes {
		_ = s.db.SetBlocked(ctx, s.cid(ch.JID.String()), ch.Action == events.BlocklistChangeActionBlock)
	}
}

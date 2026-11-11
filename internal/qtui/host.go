package qtui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"
	"github.com/mappu/miqt/qt6/multimedia"
	"github.com/mappu/miqt/qt6/qml"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/frontyard/whodat/internal/call"
	"github.com/frontyard/whodat/internal/config"
	"github.com/frontyard/whodat/internal/core"
	"github.com/frontyard/whodat/internal/media"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// Host owns the Qt application, the QML engine and the Go↔QML bridge.
// All QML-side writes happen on the Qt thread via mt(); backend callbacks
// may arrive on any goroutine and are always marshalled through it.
type Host struct {
	app         *core.App
	qa          *qt.QApplication
	eng         *qml.QQmlApplicationEngine
	cmd         *qml.QQmlPropertyMap // QML→Go: {payload: JSON{id,op,args}}
	reply       *qml.QQmlPropertyMap // Go→QML: {payload: JSON{id,ok,data|error}}
	state       *qml.QQmlPropertyMap // Go→QML: reactive state keys
	theme       *qml.QQmlPropertyMap // Go→QML: color/style roles
	chats       *ChatListModel
	msgs        *MessageListModel
	ops         map[string]opFunc
	qrSeq       atomic.Int64
	toastSeq    atomic.Int64
	showSeq     atomic.Int64
	subBase     string          // chat subtitle fallback (phone/"Group") for presence reset
	recorder    *media.Recorder // active PTT capture (nil when idle)
	recMu       sync.Mutex      // guards recorder — start/stop ops run on bridge goroutines
	tray        *qt.QSystemTrayIcon
	trayMenu    *qt.QMenu
	trayIconDK  *qt.QIcon    // white glyph, for dark panels
	trayIconLT  *qt.QIcon    // black glyph, for light panels
	trayBadgeDK *qt.QIcon    // white glyph + unread dot
	trayBadgeLT *qt.QIcon    // black glyph + unread dot
	trayUnread  int          // last published unread total (Qt thread only)
	pendingChat store.PeerID // chat a notification click should open
	appIconPath string       // bundled app icon file — fallback notification icon

	// notifChat maps daemon-issued notification ids to chats for
	// click-to-open. notifMu guards it — ActionInvoked arrives on a godbus
	// goroutine while fireNotification writes from the Qt thread.
	notifMu   sync.Mutex
	notifChat map[uint32]store.PeerID

	// rvAsked dedupes lazy avatar fetches triggered from reactorWho —
	// each reactor JID is requested at most once (Qt thread only).
	rvAsked map[store.PeerID]bool

	// acctConn tracks each account's connection state ("boot", "pair",
	// "connecting", "online") — the aggregate connState is derived from it;
	// connStates exposes the whole map to QML as JSON. acctMu guards it —
	// setAcctState runs on the Qt thread while ops read it off-thread.
	acctConn map[string]string
	acctMu   sync.Mutex

	// avLast rate-limits event-driven avatar re-fetches per chat — update
	// bursts (gap recovery, contact syncs) must not become download storms.
	avLast sync.Map

	// calls
	call      *call.Session
	callEnded atomic.Bool
	userRot   atomic.Int32
	remSess   *multimedia.QMediaCaptureSession
	locSess   *multimedia.QMediaCaptureSession
	remW      int // last remote frame dims (Qt thread only)
	remH      int
}

// NewHost builds the Qt app, bridge and QML engine but does not exec.
func NewHost(app *core.App) *Host {
	// Must be set before QApplication is constructed.
	if os.Getenv("QT_QUICK_CONTROLS_STYLE") == "" {
		_ = os.Setenv("QT_QUICK_CONTROLS_STYLE", "Material")
	}
	qa := qt.NewQApplication(os.Args)
	qt.QCoreApplication_SetApplicationName("whodat")
	qt.QGuiApplication_SetApplicationDisplayName("WhoDat")
	qt.QCoreApplication_SetOrganizationName("whodat")
	qt.QGuiApplication_SetDesktopFileName(appID)

	// App icon: ship the bundled SVG where icon themes (and our own
	// QIcon) can find it.
	iconDir := filepath.Join(config.DataDir(), "icons")
	_ = os.MkdirAll(iconDir, 0o755)
	iconPath := filepath.Join(iconDir, appID+".svg")
	_ = os.WriteFile(iconPath, core.IconSVG, 0o644)
	_ = os.WriteFile(filepath.Join(iconDir, appID+".attention.svg"), core.BadgeSVG(core.IconSVG), 0o644)
	qt.QGuiApplication_SetWindowIcon(qt.NewQIcon4(iconPath))
	// Rendered notification avatars from previous runs — dropped here so a
	// churn of picture changes can't accumulate; regenerated on demand.
	stale, _ := filepath.Glob(filepath.Join(iconDir, "notif-*.png"))
	for _, f := range stale {
		_ = os.Remove(f)
	}

	// The tray gets the simplified glyph — legible at SNI sizes where the
	// gradient tile would read as a blob. Two monochrome variants track the
	// panel mode (white on dark, black on light) — see refreshTrayIcon.
	trayDarkPath := filepath.Join(iconDir, appID+".tray-dark.svg")
	_ = os.WriteFile(trayDarkPath, core.TrayIconDarkSVG, 0o644)
	trayLightPath := filepath.Join(iconDir, appID+".tray-light.svg")
	_ = os.WriteFile(trayLightPath, core.TrayIconLightSVG, 0o644)
	trayIconDK := qt.NewQIcon4(trayDarkPath)
	trayIconLT := qt.NewQIcon4(trayLightPath)

	// Unread-indicator variants: the SNI spec's NeedsAttention status has no
	// QSystemTrayIcon API (Qt only sets it transiently during showMessage),
	// so a persistent indicator means swapping the icon itself. The tray
	// glyphs use viewBox "8 8 112 112" — the dot sits inside that space.
	trayDarkBadgePath := filepath.Join(iconDir, appID+".tray-dark.badge.svg")
	_ = os.WriteFile(trayDarkBadgePath, core.BadgeSVGAt(core.TrayIconDarkSVG, 102, 102, 17, 5), 0o644)
	trayLightBadgePath := filepath.Join(iconDir, appID+".tray-light.badge.svg")
	_ = os.WriteFile(trayLightBadgePath, core.BadgeSVGAt(core.TrayIconLightSVG, 102, 102, 17, 5), 0o644)
	trayBadgeDK := qt.NewQIcon4(trayDarkBadgePath)
	trayBadgeLT := qt.NewQIcon4(trayLightBadgePath)

	// Give the default font a color-emoji fallback: without it Qt resolves
	// emoji glyphs as monochrome outlines, so the picker/reactions/message
	// text look broken. Missing on other desktops → fontconfig falls back.
	if f := qt.QGuiApplication_Font(); f != nil {
		fams := f.Families()
		fams = append(fams, "Noto Color Emoji")
		f.SetFamilies(fams)
		qt.QGuiApplication_SetFont(f)
	}

	h := &Host{app: app, qa: qa, rvAsked: map[store.PeerID]bool{}, acctConn: map[string]string{},
		appIconPath: iconPath,
		notifChat:   map[uint32]store.PeerID{},
		trayIconDK:  trayIconDK, trayIconLT: trayIconLT, trayBadgeDK: trayBadgeDK, trayBadgeLT: trayBadgeLT}
	h.cmd = qml.NewQQmlPropertyMap()
	h.reply = qml.NewQQmlPropertyMap()
	h.state = qml.NewQQmlPropertyMap()
	h.theme = qml.NewQQmlPropertyMap()
	h.applyDesktopTheme()
	h.registerOps()

	h.cmd.OnValueChanged(func(key string, value *qt.QVariant) {
		if key == "payload" {
			h.dispatch(value.ToString())
		}
	})

	h.chats = NewChatListModel(h)
	h.msgs = NewMessageListModel(h)

	// Call sinks must exist before the engine takes them as context props.
	h.initCalls()

	h.eng = qml.NewQQmlApplicationEngine()
	ctx := h.eng.RootContext()
	ctx.SetContextProperty("Cmd", h.cmd.QObject)
	ctx.SetContextProperty("Reply", h.reply.QObject)
	ctx.SetContextProperty("App", h.state.QObject)
	ctx.SetContextProperty("Theme", h.theme.QObject)
	ctx.SetContextProperty("ChatModel", h.chats.QAbstractItemModel.QObject)
	ctx.SetContextProperty("MessageModel", h.msgs.QAbstractItemModel.QObject)
	ctx.SetContextProperty("RemoteVideoSession", h.remSess.QObject)
	ctx.SetContextProperty("LocalVideoSession", h.locSess.QObject)

	h.setState("connState", "boot")
	h.setState("connStates", "{}")
	h.setState("paired", false)
	h.setState("syncing", false)
	h.setState("toast", "")
	h.setState("qrPath", "")
	h.setState("loginStatus", "")
	h.setState("authAcct", "")
	h.setState("authStage", "")
	h.setState("authHint", "")
	h.setState("appVersion", version)
	h.setState("appIconPath", iconPath)
	h.setState("archivedCount", 0)
	h.setState("chatJID", "")
	h.setState("chatTitle", "")
	h.setState("chatSub", "")
	h.setState("chatIsGroup", false)
	h.setState("chatAvatarPath", "")
	h.setState("chatTyping", "")
	h.setState("jumpTo", "")
	prefs := h.app.Cfg.Snapshot()
	h.setState("showGroupCalls", prefs.ShowGroupCalls)
	h.setState("chatStyle", prefs.ChatStyle)
	h.setState("autoDownloadMax", int(prefs.AutoDownloadMax))
	h.setState("closeToTray", prefs.CloseToTray)
	h.setState("hasTray", false)
	h.setState("windowActive", true)
	h.setState("showSeq", 0)
	h.setState("callActive", false)
	h.setState("callIncoming", false)
	h.setState("callName", "")
	h.setState("callPhase", "")
	h.setState("callMuted", false)
	h.setState("callVideo", false)
	h.setState("callEnded", false)
	h.setState("callEndedReason", "")
	h.setState("callRemoteSize", "0x0")
	h.setState("callEmojis", "")

	h.registerCallOps()
	h.registerDialogOps()
	h.subscribe()
	h.chats.Reload()

	qmlPath := h.prepareQML()
	h.eng.Load(qt.QUrl_FromLocalFile(filepath.Join(qmlPath, "Main.qml")))
	if len(h.eng.RootObjects()) == 0 {
		log.Fatal("qtui: failed to load Main.qml")
	}

	// Initial page: paired sessions go straight to the chat list.
	h.setState("paired", h.app.AnyPaired())

	h.initTray()
	h.watchNotifications()
	h.watchNotifActions()
	h.watchDesktopTheme()
	h.watchStatus()

	go h.app.ConnectAll(context.Background())
	return h
}

// Run enters the Qt event loop.
func (h *Host) Run() int {
	return qt.QApplication_Exec()
}

// mt marshals fn onto the Qt main thread.
func mt(f func()) { mainthread.Start(f) }

// setState writes a State key — must be called on the Qt thread (use mt).
func (h *Host) setState(key string, v any) {
	var qv *qt.QVariant
	switch t := v.(type) {
	case string:
		qv = qt.NewQVariant14(t)
	case bool:
		qv = qt.NewQVariant8(t)
	case int:
		qv = qt.NewQVariant4(t)
	case fmt.Stringer:
		qv = qt.NewQVariant14(t.String())
	default:
		b, _ := json.Marshal(v)
		qv = qt.NewQVariant14(string(b))
	}
	h.state.Insert(key, qv)
}

// acctState reads one account's connection state.
func (h *Host) acctState(acct string) string {
	h.acctMu.Lock()
	defer h.acctMu.Unlock()
	return h.acctConn[acct]
}

// setAcctState records one account's state and republishes the aggregate
// connState plus the connStates map. Qt thread only.
func (h *Host) setAcctState(acct, st string) {
	h.acctMu.Lock()
	h.acctConn[acct] = st
	agg := h.connAggregateLocked()
	snap := maps.Clone(h.acctConn)
	h.acctMu.Unlock()
	h.setState("connState", agg)
	h.setState("connStates", snap)
}

// connAggregateLocked is connAggregate under acctMu.
func (h *Host) connAggregateLocked() string {
	if len(h.acctConn) == 0 {
		return "boot"
	}
	allOnline := true
	anyPair := false
	var accts []string
	// Only linked accounts drive the banner — an unpaired registration is
	// waiting for a scan, not reconnecting, and can never reach "online".
	h.app.EachSvc(func(acct string, s protocol.Service) {
		if s.Paired() {
			accts = append(accts, acct)
		}
	})
	for _, acct := range accts {
		switch h.acctConn[acct] {
		case "online":
		case "pair":
			anyPair = true
			allOnline = false
		default:
			allOnline = false
		}
	}
	if allOnline {
		return "online"
	}
	if anyPair {
		return "pair"
	}
	return "connecting"
}

// setReply writes a Reply payload — Qt thread only.
func (h *Host) setReply(v any) {
	b, _ := json.Marshal(v)
	h.reply.Insert("payload", qt.NewQVariant14(string(b)))
}

// Toast is the core.App.Toast hook — safe from any goroutine.
func (h *Host) Toast(msg string) {
	n := h.toastSeq.Add(1)
	mt(func() {
		b, _ := json.Marshal(map[string]any{"n": n, "msg": msg})
		h.state.Insert("toast", qt.NewQVariant14(string(b)))
	})
}

// subscribe fans bus events out to state/models on the Qt thread.
func (h *Host) subscribe() {
	ch, unsub := h.app.Bus.Subscribe(512)
	_ = unsub // process-lifetime subscription
	go func() {
		for e := range ch {
			e := e
			mt(func() { h.handleEvent(e) })
		}
	}()
}

func (h *Host) handleEvent(e protocol.Event) {
	switch e.Kind {
	case protocol.EvQRCode:
		h.setAcctState(e.Acct, "pair")
		// Auth events drive the login flow only while an explicit flow is
		// open for that account (authStart set authAcct first).
		// A background account's QR stream must not hijack the screen.
		if cur := h.stateValue("authAcct"); cur == e.Acct {
			h.setState("authStage", string(protocol.StageQR))
			h.renderQR(e.Acct, e.QRCode)
		}
	case protocol.EvAuthStage:
		if cur := h.stateValue("authAcct"); cur == e.Acct {
			h.setState("authStage", string(e.AuthStage))
			h.setState("authHint", e.AuthHint)
		}
	case protocol.EvPaired:
		h.setState("paired", h.app.AnyPaired())
		h.app.PersistAccounts(context.Background())
		if h.stateValue("authAcct") == e.Acct {
			h.setState("authAcct", "")
			h.setState("authStage", "")
			h.setState("authHint", "")
		}
		h.chats.Reload()
	case protocol.EvConnected:
		h.setAcctState(e.Acct, "online")
		h.setState("paired", h.app.AnyPaired())
		h.chats.Reload()
	case protocol.EvDisconnected:
		h.setAcctState(e.Acct, "connecting")
	case protocol.EvLoggedOut:
		h.setState("paired", h.app.AnyPaired())
		// The dead account may have been mid-sync — no EvSyncDone is
		// coming for it.
		h.setState("syncing", false)
		h.setAcctState(e.Acct, "pair")
		h.setState("qrPath", "")
		h.setState("loginStatus", "")
		// Logout wiped the account's local data — drop views of it, but
		// only when the open chat belonged to the wiped account.
		if h.msgs.jid == "" || h.app.AcctOf(h.msgs.jid) == e.Acct {
			h.opCloseChat(h, nil)
		}
		h.chats.Reload()
		// logoutAccount/authCancel removed the acct BEFORE the service
		// published — those stay dead. A service-initiated logout (a
		// server-side revoke reaches us here with the acct still
		// registered) drops the registration the same way: a dead
		// account is re-added through the picker, never resurrected as
		// an invisible "not linked" zombie.
		if h.app.Svc(e.Acct) != nil {
			h.app.RemoveAccount(context.Background(), e.Acct)
			h.chats.Reload()
		}
	case protocol.EvSyncStart:
		h.setState("syncing", true)
	case protocol.EvSyncDone:
		h.setState("syncing", false)
		h.chats.Reload()
	case protocol.EvMessage:
		cj := h.chatRowJID(e.ChatJID)
		h.chats.OnChatUpdated(cj)
		h.msgs.OnMessage(cj, e.Message)
		// Incoming messages in the open chat are marked read immediately —
		// transcript logic; FromMe rows are filtered inside.
		if h.sameChat(cj, h.msgs.jid) && e.Message != nil {
			h.markRead([]*store.Message{e.Message})
		}
	case protocol.EvMessageUpdated:
		// Republished row (star/react/edit/revoke) — refresh views but don't
		// re-mark-read or notify.
		cj := h.chatRowJID(e.ChatJID)
		h.chats.OnChatUpdated(cj)
		h.msgs.OnMessage(cj, e.Message)
	case protocol.EvReceipt:
		h.msgs.OnReceipt(h.chatRowJID(e.ChatJID), e.Message)
	case protocol.EvChatUpdated:
		cj := h.chatRowJID(e.ChatJID)
		h.chats.OnChatUpdated(cj)
		if h.sameChat(cj, h.msgs.jid) {
			h.refreshChatHeader()
		}
	case protocol.EvChatGone:
		cj := h.chatRowJID(e.ChatJID)
		h.chats.OnChatGone(cj)
		if h.sameChat(cj, h.msgs.jid) {
			// Cleared chats keep the (now empty) transcript; deleted chats
			// lose their row and we bail to the placeholder.
			go func() {
				c, err := h.app.DB.GetChat(context.Background(), e.ChatJID)
				if err != nil {
					return
				}
				mt(func() {
					if c == nil {
						h.opCloseChat(h, nil)
					} else {
						h.msgs.SetChat(cj, h.msgs.isGroup)
						h.msgs.seed(nil)
					}
				})
			}()
		}
	case protocol.EvTyping:
		if e.Typing != nil {
			e.Typing.ChatJID = h.chatRowJID(e.Typing.ChatJID)
		}
		h.onTyping(e.Typing)
	case protocol.EvPresence:
		if e.Typing != nil {
			e.Typing.ChatJID = h.chatRowJID(e.Typing.ChatJID)
		}
		h.onPresence(e.Typing)
	case protocol.EvContactsUpdated:
		h.chats.Reload()
	case protocol.EvAvatarChanged:
		// A peer's picture changed — re-fetch (or drop) the cached file.
		// Throttled per chat so update bursts don't become downloads; runs
		// off the Qt thread. RefreshAvatar publishes EvChatUpdated when the
		// stored path actually changes.
		cj := h.chatRowJID(e.ChatJID)
		if !h.avThrottled(cj) {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				if e.Remove {
					_ = h.app.ClearAvatar(ctx, cj)
				} else {
					_ = h.app.RefreshAvatar(ctx, cj)
				}
			}()
		}
	}
}

// avThrottled reports whether chat jid had an event-driven avatar refresh
// within the last 30s (and stores this attempt's timestamp otherwise).
func (h *Host) avThrottled(jid store.PeerID) bool {
	now := time.Now()
	if v, ok := h.avLast.Load(jid); ok {
		if t, _ := v.(time.Time); now.Sub(t) < 30*time.Second {
			return true
		}
	}
	h.avLast.Store(jid, now)
	return false
}

// sameChat reports whether two chat ids name the same conversation —
// exact match or identical after lid→pn canonicalization.
func (h *Host) sameChat(a, b store.PeerID) bool {
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return h.app.DB.CanonicalJID(ctx, a) == h.app.DB.CanonicalJID(ctx, b)
}

// errNoSvc reports an op that arrived after every account was removed —
// SvcFor returns nil only in that window, and ops must not dereference it.
var errNoSvc = errors.New("no account for chat")

// svcForChat resolves the service owning a chat-scoped id, or errNoSvc
// when no account is registered anymore.
func svcForChat(app *core.App, jid store.PeerID) (protocol.Service, error) {
	if s := app.SvcFor(jid); s != nil {
		return s, nil
	}
	return nil, errNoSvc
}

// chatRowJID maps a bus event's chat id onto the stored chat row's key.
// WhatsApp's lid/pn aliasing means updates can arrive under either form
// while the chat row lives under whichever was persisted first — without
// this, events for those chats silently miss every view.
func (h *Host) chatRowJID(jid store.PeerID) store.PeerID {
	if jid == "" {
		return jid
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if c, err := h.app.DB.GetChat(ctx, jid); err == nil && c != nil {
		return jid
	}
	for _, alt := range h.app.DB.AltJIDs(ctx, jid) {
		if alt == jid {
			continue
		}
		if c, err := h.app.DB.GetChat(ctx, alt); err == nil && c != nil {
			return alt
		}
	}
	return jid
}

// refreshChatHeader updates title/subtitle/avatar when the open chat's row
// changes (rename, avatar refresh, …).
func (h *Host) refreshChatHeader() {
	go func() {
		jid := h.msgs.jid
		c, err := h.app.DB.GetChat(context.Background(), jid)
		if err != nil || c == nil {
			return
		}
		e2e := h.e2eState(jid)
		mt(func() {
			if c.Name != "" {
				h.setState("chatTitle", c.Name)
			}
			h.setState("chatAvatarPath", c.AvatarPath)
			h.setState("chatE2EE", e2e)
		})
	}()
}

// e2eState resolves the header lock indicator — "yes"/"no" when the
// backend answers protocol.ChatEncryptor, "unknown" otherwise (no icon
// renders rather than a misleading one). Runs off-thread.
func (h *Host) e2eState(jid store.PeerID) string {
	svc := h.app.SvcFor(jid)
	if svc == nil {
		return "unknown"
	}
	ce, ok := svc.(protocol.ChatEncryptor)
	if !ok {
		return "unknown"
	}
	on, err := ce.ChatE2EE(context.Background(), jid)
	if err != nil {
		return "unknown"
	}
	if on {
		return "yes"
	}
	return "no"
}

// onTyping shows "typing…"/"recording…" for the open chat.
func (h *Host) onTyping(t *protocol.TypingInfo) {
	if t == nil || !h.sameChat(t.ChatJID, h.msgs.jid) {
		return
	}
	if !t.Composing {
		h.setState("chatTyping", "")
		return
	}
	who := ""
	if h.msgs.isGroup && t.SenderJID != "" {
		who = h.app.ContactName(context.Background(), t.SenderJID) + " is "
	}
	label := "typing…"
	if t.Recording {
		label = "recording audio…"
	}
	h.setState("chatTyping", who+label)
}

// onPresence reflects online/last-seen in the header subtitle (DMs).
func (h *Host) onPresence(t *protocol.TypingInfo) {
	if t == nil || !h.sameChat(t.ChatJID, h.msgs.jid) || h.msgs.isGroup {
		return
	}
	switch {
	case t.Online:
		h.setState("chatSub", "online")
	case !t.LastSeen.IsZero():
		h.setState("chatSub", "last seen "+fmtLastSeen(t.LastSeen))
	default:
		h.setState("chatSub", h.subBase)
	}
}

// fmtLastSeen renders a WhatsApp-style relative timestamp.
func fmtLastSeen(t time.Time) string {
	now := time.Now()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch {
	case day.Equal(today):
		return "today at " + t.Format("15:04")
	case day.Equal(today.AddDate(0, 0, -1)):
		return "yesterday at " + t.Format("15:04")
	default:
		return t.Format("02 Jan 2006 at 15:04")
	}
}

// renderQR encodes the QR string to a unique PNG in the cache dir and points
// the login page at it. Runs the encode off the Qt thread. The filename is
// account-scoped so one account's wipe can't delete another's pairing code.
func (h *Host) renderQR(acct, code string) {
	n := h.qrSeq.Add(1)
	go func() {
		png, err := qrcode.Encode(code, qrcode.Medium, 512)
		if err != nil {
			mt(func() { h.setState("loginStatus", "Could not render QR code") })
			return
		}
		dir := config.CacheDir()
		path := filepath.Join(dir, "qr-"+acct+"-"+strconv.FormatInt(n, 10)+".png")
		if err := os.WriteFile(path, png, 0o600); err != nil {
			return
		}
		hint := "Scan the QR code with your messaging app."
		if svc := h.app.Svc(acct); svc != nil {
			if b, ok := protocol.BackendOf(svc.Protocol()); ok && b.ScanHint != "" {
				hint = b.ScanHint
			}
		}
		mt(func() {
			h.setState("qrPath", path)
			h.setState("loginStatus", hint)
		})
	}()
}

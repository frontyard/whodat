package qtui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"
	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/frontyard/whodat/internal/config"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// watchNotifications: desktop notifications for
// incoming messages unless muted, disabled, or the window is focused on the
// same chat.
func (h *Host) watchNotifications() {
	ch, unsub := h.app.Bus.Subscribe(256)
	go func() {
		defer unsub()
		for e := range ch {
			if e.Kind != protocol.EvMessage || e.Message == nil || e.Message.FromMe {
				continue
			}
			m := e.Message
			if m.Type == store.TSystem {
				continue
			}
			if !h.app.Cfg.Snapshot().Notifications || h.app.NotifPaused.Load() {
				continue
			}
			ctx := context.Background()
			if c, err := h.app.DB.GetChat(ctx, h.chatRowJID(m.ChatJID)); err == nil && c != nil && c.Muted {
				continue
			}
			// Resolve to the stored row's JID — events can arrive under a
			// lid/pn alias while the open chat tracks the canonical row, and
			// the raw JID would bypass the "already viewing" suppression.
			rowJID := h.chatRowJID(m.ChatJID)
			title := h.app.ChatName(ctx, rowJID)
			body := m.Text
			if body == "" {
				body = store.NotifyFallback(m.Type)
			}
			if m.SenderName != "" {
				body = m.SenderName + ": " + body
			}
			// The notification icon is the sender's cached avatar (group
			// chats get the actual sender, not the group pic), falling back
			// to the chat's picture. DB probes stay off the Qt thread.
			avPath := ""
			if m.SenderJID != "" {
				avPath = h.app.ReactorAvatar(ctx, m.SenderJID)
			}
			if avPath == "" {
				avPath = h.app.ReactorAvatar(ctx, rowJID)
			}
			mainthread.Start(func() { h.fireNotification(title, body, rowJID, avPath) })
		}
	}()
}

// fireNotification posts a desktop notification — Qt thread. Suppressed
// when the window is focused and already viewing the chat. The D-Bus
// Notify call is primary: QSystemTrayIcon::showMessage sends an empty
// app_icon and no image hints on the wire, so it can only ever render
// text — it survives as the fallback for hosts without the freedesktop
// Notifications service.
func (h *Host) fireNotification(title, body string, jid store.PeerID, avatarPath string) {
	if sv := h.state.Value("windowActive"); sv != nil && sv.ToBool() {
		if cv := h.state.Value("chatJID"); cv != nil && cv.ToString() == jid.String() {
			return
		}
	}
	iconPath := ""
	if avatarPath != "" {
		iconPath = notifIconFile(avatarPath)
	}
	if iconPath == "" {
		iconPath = h.appIconPath
	}
	if err := h.dbusNotify(title, body, jid, iconPath); err == nil {
		return
	}
	if h.tray == nil {
		return
	}
	h.pendingChat = jid
	// The QIcon overload sends a real AttentionIconPixmap and leaves
	// AttentionIconName empty. The MessageIcon enum instead makes Qt publish
	// a theme name ("dialog-information") — hosts that can't resolve it
	// (e.g. Quickshell under Adwaita) show a checkerboard placeholder while
	// NeedsAttention is active.
	h.tray.ShowMessage3(title, body, h.currentTrayIcon(), 8000)
}

// dbusNotify posts the notification via org.freedesktop.Notifications —
// the only path that carries an image: daemons render app_icon (a path
// works everywhere a theme name does) in the icon slot. Every message is
// a fresh notification (replaces_id 0): servers that honour replacement
// update the old entry silently when its popup already expired — the
// message would never surface (DMS/Quickshell does exactly this). The
// "default" action lets a body click reopen the chat — see
// watchNotifActions. Qt thread only.
func (h *Host) dbusNotify(title, body string, jid store.PeerID, iconPath string) error {
	conn, err := portalBus()
	if err != nil {
		return err
	}
	hints := map[string]dbus.Variant{
		"desktop-entry": dbus.MakeVariant(appID),
		"urgency":       dbus.MakeVariant(byte(1)),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var id uint32
	if err := conn.Object("org.freedesktop.Notifications",
		dbus.ObjectPath("/org/freedesktop/Notifications")).
		CallWithContext(ctx, "org.freedesktop.Notifications.Notify", 0,
			"WhoDat", uint32(0), iconPath, title, body,
			[]string{"default", "Open"}, hints, 8000).Store(&id); err != nil {
		return err
	}
	h.notifMu.Lock()
	h.notifChat[id] = jid
	if len(h.notifChat) > 256 {
		h.notifChat = map[uint32]store.PeerID{}
	}
	h.notifMu.Unlock()
	return nil
}

// watchNotifActions opens the chat a notification click invoked — the
// daemon reports it as an ActionInvoked signal for the "default" action,
// and NotificationClosed reaps the id so the map can't grow.
func (h *Host) watchNotifActions() {
	conn, err := portalBus()
	if err != nil {
		return
	}
	if err := conn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0,
		"type='signal',interface='org.freedesktop.Notifications'").Err; err != nil {
		return
	}
	ch := make(chan *dbus.Signal, 16)
	conn.Signal(ch)
	go func() {
		for sig := range ch {
			if len(sig.Body) < 1 {
				continue
			}
			id, _ := sig.Body[0].(uint32)
			switch sig.Name {
			case "org.freedesktop.Notifications.NotificationClosed":
				h.notifMu.Lock()
				delete(h.notifChat, id)
				h.notifMu.Unlock()
			case "org.freedesktop.Notifications.ActionInvoked":
				if len(sig.Body) != 2 {
					continue
				}
				key, _ := sig.Body[1].(string)
				if key != "default" {
					continue
				}
				h.notifMu.Lock()
				jid, ok := h.notifChat[id]
				delete(h.notifChat, id)
				h.notifMu.Unlock()
				if !ok {
					continue
				}
				mt(func() {
					h.requestWindow()
					b, _ := json.Marshal(map[string]string{"jid": jid.String()})
					_, _ = h.opOpenChat(h, b)
				})
			}
		}
	}()
}

// notifIconSize is the notification avatar's edge in px — large enough to
// stay crisp on HiDPI daemons, small enough to keep the file lean.
const notifIconSize = 128

// notifIconFile renders avatarPath into a circular PNG under the icon
// cache dir and returns it — daemons read the icon from disk, so it must
// be a real file (DMS/Quickshell ignore image-data for the icon slot and
// show appIcon only). Keyed by source path: refreshed avatars land on
// versioned filenames, so a changed picture is a fresh cache entry.
// Qt thread only.
func notifIconFile(avatarPath string) string {
	sum := sha256.Sum256([]byte(avatarPath))
	out := filepath.Join(config.DataDir(), "icons",
		"notif-"+hex.EncodeToString(sum[:8])+".png")
	if _, err := os.Stat(out); err == nil {
		return out
	}
	pm := roundAvatarPixmap(avatarPath)
	if pm == nil || !pm.Save(out) {
		return ""
	}
	return out
}

// roundAvatarPixmap loads an avatar file into a notifIconSize square
// pixmap clipped to a circle — DMS renders the icon unmasked, so the
// rounding is baked in here. nil when the file can't be decoded.
func roundAvatarPixmap(path string) *qt.QPixmap {
	src := qt.NewQPixmap4(path)
	if src.IsNull() {
		return nil
	}
	scaled := src.Scaled3(notifIconSize, notifIconSize, qt.KeepAspectRatioByExpanding, qt.SmoothTransformation)
	// KeepAspectRatioByExpanding over-covers the long axis — center-crop.
	if scaled.Width() > notifIconSize || scaled.Height() > notifIconSize {
		scaled = scaled.Copy((scaled.Width()-notifIconSize)/2, (scaled.Height()-notifIconSize)/2,
			notifIconSize, notifIconSize)
	}
	dst := qt.NewQPixmap2(notifIconSize, notifIconSize)
	dst.FillWithFillColor(qt.NewQColor2(qt.Transparent))
	p := qt.NewQPainter2(dst.QPaintDevice)
	p.SetRenderHint2(qt.QPainter__Antialiasing, true)
	p.SetPenWithStyle(qt.NoPen)
	p.SetBrush(qt.NewQBrush7(scaled))
	p.DrawEllipse2(0, 0, notifIconSize, notifIconSize)
	p.End()
	return dst
}

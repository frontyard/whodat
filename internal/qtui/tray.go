package qtui

import (
	"context"
	"encoding/json"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/frontyard/whodat/internal/protocol"
)

// initTray creates the SNI tray icon + menu. Qt thread only.
func (h *Host) initTray() {
	if !qt.QSystemTrayIcon_IsSystemTrayAvailable() {
		return
	}
	tray := qt.NewQSystemTrayIcon()
	// The tray icons were loaded from bundled SVG paths — real pixmaps,
	// which SNI hosts always render. fromTheme relies on the host's icon
	// theme/cache and commonly yields a placeholder.
	tray.SetIcon(h.currentTrayIcon())
	tray.SetToolTip("WhoDat")

	menu := qt.NewQMenu2()
	show := menu.AddActionWithText("Show WhoDat")
	pause := menu.AddActionWithText("Pause notifications")
	pause.SetCheckable(true)
	quit := menu.AddActionWithText("Quit")
	show.OnTriggered(func() { h.requestWindow() })
	pause.OnToggled(func(checked bool) { h.app.NotifPaused.Store(checked) })
	quit.OnTriggered(func() { qt.QCoreApplication_Quit() })
	tray.SetContextMenu(menu)

	tray.OnActivated(func(reason qt.QSystemTrayIcon__ActivationReason) {
		if reason == qt.QSystemTrayIcon__Trigger || reason == qt.QSystemTrayIcon__DoubleClick {
			h.requestWindow()
		}
	})
	tray.OnMessageClicked(func() {
		h.requestWindow()
		if h.pendingChat != "" {
			jid := h.pendingChat
			h.pendingChat = ""
			b, _ := json.Marshal(map[string]string{"jid": jid.String()})
			_, _ = h.opOpenChat(h, b)
		}
	})
	tray.Show()

	h.tray = tray
	h.trayMenu = menu
	// Close-to-tray on → Main.qml's onClosing rejects the close and hides;
	// off → the window close must actually quit, so the flag follows the
	// pref instead of being unconditionally off.
	qt.QGuiApplication_SetQuitOnLastWindowClosed(!h.app.Cfg.Snapshot().CloseToTray)
	h.setState("hasTray", true)
	h.watchTrayUnread()
}

// currentTrayIcon picks the glyph matching the desktop panel mode — white
// on dark, black on light — with the unread-badge variant swapped in while
// trayUnread > 0. Falls back to the window icon if the bundled files failed
// to load. Qt thread only.
func (h *Host) currentTrayIcon() *qt.QIcon {
	icon := h.trayIconLT
	badge := h.trayBadgeLT
	if desktopDark() {
		icon = h.trayIconDK
		badge = h.trayBadgeDK
	}
	if h.trayUnread > 0 && badge != nil && !badge.IsNull() {
		return badge
	}
	if icon == nil || icon.IsNull() {
		icon = qt.QGuiApplication_WindowIcon()
	}
	return icon
}

// refreshTrayIcon re-applies the mode-appropriate glyph — called from
// applyDesktopTheme whenever the desktop scheme may have flipped.
// Qt thread only.
func (h *Host) refreshTrayIcon() {
	if h.tray != nil {
		h.tray.SetIcon(h.currentTrayIcon())
	}
}

// requestWindow asks QML to un-hide/raise the window via the showSeq counter.
// On niri/Wayland activation needs compositor IPC — handled separately.
func (h *Host) requestWindow() {
	n := h.showSeq.Add(1)
	mainthread.Start(func() {
		h.state.Insert("showSeq", qt.NewQVariant4(int(n)))
	})
	h.niriFocusWindow()
}

// watchTrayUnread drives the NeedsAttention behaviour: the tray tooltip
// gains an unread badge while any unmuted chat has unread messages.
func (h *Host) watchTrayUnread() {
	update := func() {
		n, err := h.app.DB.TotalUnread(context.Background())
		if err != nil {
			return
		}
		mainthread.Start(func() {
			tip := "WhoDat"
			if n > 0 {
				tip = "WhoDat — " + itoa(n) + " unread"
			}
			if h.tray != nil {
				h.tray.SetToolTip(tip)
			}
			if h.trayUnread != n {
				h.trayUnread = n
				h.refreshTrayIcon()
			}
			h.setState("trayUnread", n)
		})
	}
	go update()
	ch, unsub := h.app.Bus.Subscribe(64)
	go func() {
		defer unsub()
		for e := range ch {
			switch e.Kind {
			case protocol.EvChatUpdated, protocol.EvMessage, protocol.EvMessageUpdated,
				protocol.EvSyncDone, protocol.EvReceipt, protocol.EvLoggedOut:
				update()
			}
		}
	}()
}

// itoa formats small ints without importing strconv for one call site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

var _ = itoa // keep helper referenced in minimal builds

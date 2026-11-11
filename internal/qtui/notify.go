package qtui

import (
	"context"

	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// watchNotifications: system-tray notifications for
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
			mainthread.Start(func() { h.fireNotification(title, body, rowJID) })
		}
	}()
}

// fireNotification shows the tray notification — Qt thread. Suppressed when
// the window is focused and already viewing the chat.
func (h *Host) fireNotification(title, body string, jid store.PeerID) {
	if h.tray == nil {
		return
	}
	if sv := h.state.Value("windowActive"); sv != nil && sv.ToBool() {
		if cv := h.state.Value("chatJID"); cv != nil && cv.ToString() == jid.String() {
			return
		}
	}
	h.pendingChat = jid
	// The QIcon overload sends a real AttentionIconPixmap and leaves
	// AttentionIconName empty. The MessageIcon enum instead makes Qt publish
	// a theme name ("dialog-information") — hosts that can't resolve it
	// (e.g. Quickshell under Adwaita) show a checkerboard placeholder while
	// NeedsAttention is active.
	h.tray.ShowMessage3(title, body, h.currentTrayIcon(), 8000)
}

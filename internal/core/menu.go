package core

import (
	"context"
	"os"
	"time"

	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// Chat action IDs shared by both frontends. Actions with IDs not handled by
// RunChatAction ("info", "disapp") open UI-specific dialogs.
const (
	ActChatInfo   = "info"
	ActPin        = "pin"
	ActArchive    = "archive"
	ActMarkRead   = "markread"
	ActMarkUnread = "markunread"
	ActMute       = "mute"
	ActDisapp     = "disapp"
	ActBlock      = "block"
	ActClear      = "clear"
	ActLeave      = "leave"
	ActDelete     = "delete"
	ActRefreshAv  = "refreshavatar"
)

// MenuItem is a toolkit-neutral context-menu entry.
type MenuItem struct {
	ID    string
	Label string
}

// ChatMenuItems builds the shared chat actions used by the sidebar
// right-click menu and the chat ⋮ menu — group/state-aware and filtered
// through the owning account's capabilities.
// State toggles go through the service so they sync to the account's
// other devices (app-state patches), not just the local DB.
func (a *App) ChatMenuItems(c *store.Chat) []MenuItem {
	caps := protocol.Caps{}
	if svc := a.SvcFor(c.JID); svc != nil {
		caps = svc.Caps()
	}
	info := "Contact info"
	if c.Kind == store.KindGroup {
		info = "Group info"
	}
	items := []MenuItem{{ID: ActChatInfo, Label: info}}
	if caps.Pin {
		pin := "Pin chat"
		if c.Pinned {
			pin = "Unpin chat"
		}
		items = append(items, MenuItem{ID: ActPin, Label: pin})
	}
	if caps.Archive {
		arch := "Archive chat"
		if c.Archived {
			arch = "Unarchive chat"
		}
		items = append(items, MenuItem{ID: ActArchive, Label: arch})
	}
	if c.Unread > 0 {
		items = append(items, MenuItem{ID: ActMarkRead, Label: "Mark as read"})
	} else {
		items = append(items, MenuItem{ID: ActMarkUnread, Label: "Mark as unread"})
	}
	if caps.Mute {
		mute := "Mute chat"
		if c.Muted {
			mute = "Unmute chat"
		}
		items = append(items, MenuItem{ID: ActMute, Label: mute})
	}

	if caps.Disappearing && !c.Left {
		disapp := "Disappearing messages"
		if c.DisappSecs > 0 {
			disapp = "Disappearing: " + DisappText(c.DisappSecs)
		}
		items = append(items, MenuItem{ID: ActDisapp, Label: disapp})
	}

	if caps.Block && c.Kind == store.KindDM {
		bl := "Block"
		if ct, err := a.DB.GetContact(context.Background(), c.JID); err == nil && ct != nil && ct.Blocked {
			bl = "Unblock"
		}
		items = append(items, MenuItem{ID: ActBlock, Label: bl})
	}

	items = append(items, MenuItem{ID: ActClear, Label: "Clear chat"})

	if c.Kind == store.KindGroup && caps.Leave && !c.Left {
		items = append(items, MenuItem{ID: ActLeave, Label: "Leave group"})
	} else if caps.DeleteChat {
		items = append(items, MenuItem{ID: ActDelete, Label: "Delete chat"})
	}
	return items
}

// RunChatAction executes the backend part of a chat-menu action in the
// background and toasts on failure. Returns false for IDs the UI layer
// handles itself (dialogs): "info" and "disapp".
func (a *App) RunChatAction(c *store.Chat, id string) bool {
	run := func(f func(context.Context) error, pfx string) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := f(ctx); err != nil && a.Toast != nil {
				a.Toast(pfx + ": " + protocol.UserErr(err).Error())
			}
		}()
	}
	svc := a.SvcFor(c.JID)
	if svc == nil {
		return false // account removed between menu build and dispatch
	}
	switch id {
	case ActPin:
		p := !c.Pinned
		run(func(ctx context.Context) error { return svc.SetChatPin(ctx, c.JID, p) }, "Pin failed")
	case ActArchive:
		v := !c.Archived
		run(func(ctx context.Context) error { return svc.SetChatArchive(ctx, c.JID, v) }, "Archive failed")
	case ActMarkRead:
		a.MarkChatRead(c.JID)
	case ActMarkUnread:
		run(func(ctx context.Context) error { return svc.MarkChatUnreadSync(ctx, c.JID, true) }, "Failed")
	case ActMute:
		m := !c.Muted
		run(func(ctx context.Context) error { return svc.SetChatMute(ctx, c.JID, m) }, "Mute failed")
	case ActBlock:
		blocked := false
		if ct, err := a.DB.GetContact(context.Background(), c.JID); err == nil && ct != nil {
			blocked = ct.Blocked
		}
		b := !blocked
		run(func(ctx context.Context) error { return svc.SetBlocked(ctx, c.JID, b) }, "Block failed")
	case ActClear:
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			paths, err := a.DB.ClearChatMessages(ctx, c.JID)
			if err == nil {
				for _, p := range paths {
					_ = os.Remove(p)
				}
				a.Bus.Publish(protocol.Event{Kind: protocol.EvChatGone, ChatJID: c.JID})
				a.Bus.Publish(protocol.Event{Kind: protocol.EvChatUpdated, ChatJID: c.JID})
			}
		}()
	case ActLeave:
		run(func(ctx context.Context) error { return svc.LeaveGroup(ctx, c.JID) }, "Leave failed")
	case ActDelete:
		run(func(ctx context.Context) error { return svc.DeleteChat(ctx, c.JID) }, "Delete failed")
	case ActRefreshAv:
		run(func(ctx context.Context) error { return a.RefreshAvatar(ctx, c.JID) }, "Avatar refresh failed")
	default:
		return false
	}
	return true
}

// Package protocol defines the contract between a messaging backend
// (WhatsApp, …) and the app core: the event bus, the Service
// interface every account satisfies, capability flags, and the normalized
// group/auth types services exchange with the UI.
package protocol

import (
	"sync"
	"time"

	"github.com/frontyard/whodat/internal/store"
)

// EventKind identifies what an Event carries.
type EventKind int

const (
	EvQRCode          EventKind = iota // QRCode set — display/refresh QR
	EvPaired                           // pairing succeeded, waiting for connect
	EvConnected                        // online
	EvDisconnected                     // lost connection (will auto-retry)
	EvLoggedOut                        // session revoked — needs re-auth
	EvSyncStart                        // offline/history sync running
	EvSyncDone                         // sync finished
	EvMessage                          // new incoming/outgoing Message
	EvMessageUpdated                   // existing Message row changed (star/react/edit/revoke) — refresh views, never notify
	EvReceipt                          // delivery status changed on Message (only ID/status meaningful)
	EvChatUpdated                      // chat row changed (name/avatar/unread)
	EvChatGone                         // chat cleared/deleted — open views must reload
	EvTyping                           // Typing set
	EvPresence                         // contact online state changed (Typing reused: State=presence)
	EvContactsUpdated                  // contact list refreshed
	EvAvatarChanged                    // peer profile picture changed (Remove: removed)
	EvAuthStage                        // auth flow advanced — AuthStage/AuthHint/QRCode set
)

// Event is published on the bus; subscribers must not mutate shared fields.
// Acct names the account that produced it ("wa", "wa2"); empty on
// UI-originated refresh events.
type Event struct {
	Kind      EventKind
	Acct      string
	QRCode    string
	ChatJID   store.PeerID
	Message   *store.Message
	Chat      *store.Chat
	Typing    *TypingInfo
	Err       error
	AuthStage AuthStage
	AuthHint  string
	Remove    bool // EvAvatarChanged: the picture was removed, not changed
}

// TypingInfo carries chat-presence or presence state.
type TypingInfo struct {
	ChatJID    store.PeerID
	SenderJID  store.PeerID
	SenderName string
	Composing  bool      // typing vs online
	Recording  bool      // recording audio vs typing text
	Online     bool      // for EvPresence
	LastSeen   time.Time // for EvPresence when Online=false (zero = hidden)
}

// Bus is a simple fan-out pub/sub. Subscribers get buffered channels;
// slow consumers drop events rather than block the backend handler.
type Bus struct {
	mu   sync.Mutex
	subs map[uint64]chan Event
	next uint64
}

// NewBus creates an event bus.
func NewBus() *Bus { return &Bus{subs: make(map[uint64]chan Event)} }

// Subscribe returns a buffered channel and an unsubscribe func. The func
// is safe to call twice — the second call is a no-op (a plain delete+close
// would panic on a caller that unsubscribes from two cleanup paths).
func (b *Bus) Subscribe(size int) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan Event, size)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[id]; !ok {
			return
		}
		delete(b.subs, id)
		close(ch)
	}
}

// Publish delivers the event to all subscribers without blocking.
func (b *Bus) Publish(e Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// For returns a stamped bus whose Publish stamps events with acct — services
// publish through it so subscribers can attribute events to an account.
func (b *Bus) For(acct string) *StampedBus { return &StampedBus{Bus: b, acct: acct} }

// StampedBus wraps Bus, stamping Event.Acct on every publish.
type StampedBus struct {
	*Bus
	acct string
}

// Publish stamps the account then fans out.
func (b *StampedBus) Publish(e Event) {
	e.Acct = b.acct
	b.Bus.Publish(e)
}

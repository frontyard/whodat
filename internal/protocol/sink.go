package protocol

import (
	"context"

	"github.com/frontyard/whodat/internal/store"
)

// Sink couples a service's store writes with the bus events the views
// listen for — the "persist the row, then tell everyone" dance every
// backend needs. Each service holds one instead of a bare StampedBus, so
// a send or update path can't store a message and forget to notify: the
// write and the event are one call.
//
// Do is the shared serial writer (wipe-gated, same as store.DB.DoFor) —
// DB-bound work goes through it; the helpers assume they run inside it.
type Sink struct {
	acct string
	db   *store.DB
	bus  *StampedBus
}

// NewSink builds the per-account write/event handle a Service uses for
// message persistence and change notifications.
func NewSink(acct string, db *store.DB, bus *Bus) *Sink {
	return &Sink{acct: acct, db: db, bus: bus.For(acct)}
}

// Do enqueues a DB-bound task on the shared serial writer, gated on this
// account's wipe flag (store.DB.DoFor) — writes from a dead session are
// dropped instead of resurrecting purged rows.
func (s *Sink) Do(fn func(ctx context.Context)) { s.db.DoFor(s.acct, fn) }

// Publish stamps the event with this account and fans it out.
func (s *Sink) Publish(e Event) { s.bus.Publish(e) }

// MessageSaved stores a finished message row — insert it, bump the chat
// summary, tell the views. Callers own EnsureChat beforehand (the kind is
// service-specific); send paths skip it since the chat already exists.
func (s *Sink) MessageSaved(ctx context.Context, m *store.Message, preview string, bumpUnread bool) {
	_ = s.db.InsertMessage(ctx, m)
	_ = s.db.TouchChat(ctx, m.ChatJID, preview, m.TS, bumpUnread)
	s.bus.Publish(Event{Kind: EvMessage, ChatJID: m.ChatJID, Message: m})
	s.bus.Publish(Event{Kind: EvChatUpdated, ChatJID: m.ChatJID})
}

// MessageChanged republishes an existing row — the caller already applied
// the star/react/edit/revoke. Views refresh in place; nothing notifies.
func (s *Sink) MessageChanged(chatID store.PeerID, m *store.Message) {
	s.bus.Publish(Event{Kind: EvMessageUpdated, ChatJID: chatID, Message: m})
}

// ChatChanged publishes EvChatUpdated — name/avatar/unread/flags changed,
// or a write landed under a JID alias the views also display.
func (s *Sink) ChatChanged(chatID store.PeerID) {
	s.bus.Publish(Event{Kind: EvChatUpdated, ChatJID: chatID})
}

// AvatarChanged publishes EvAvatarChanged — the peer's profile picture
// changed (removed=true: it was taken down) and should be re-fetched.
func (s *Sink) AvatarChanged(chatID store.PeerID, removed bool) {
	s.bus.Publish(Event{Kind: EvAvatarChanged, ChatJID: chatID, Remove: removed})
}

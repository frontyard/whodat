package protocol

import (
	"context"
	"time"

	"github.com/frontyard/whodat/internal/store"
)

// Optional Service interfaces — backends implement the subset their
// protocol supports; the UI type-asserts before offering the feature.

// HistoryFetcher pulls older history from the server when the local store
// runs out (scroll-back past the earliest row). Returns rows inserted.
type HistoryFetcher interface {
	FetchEarlier(ctx context.Context, chatID store.PeerID, before time.Time, limit int) (int, error)
}

// PreviewThumber lazily downloads a link-preview thumbnail — the row's
// Preview.Ref locator is protocol-owned, like Proto is for media.
type PreviewThumber interface {
	FetchPreviewThumb(ctx context.Context, m *store.Message) (string, error)
}

// MediaThumber resolves a message's sender-provided inline thumbnail —
// the video poster / GIF first frame living inside the protocol-owned
// Proto payload, staged locally on first view.
type MediaThumber interface {
	FetchMediaThumb(ctx context.Context, m *store.Message) (string, error)
}

// PeerResolver turns a directory handle (phone number, @username) into a
// chat id the account can open.
type PeerResolver interface {
	ResolvePeer(ctx context.Context, query string) (store.PeerID, error)
}

// SessionLister enumerates the account's active sessions/devices as display
// strings (WhatsApp companion devices).
type SessionLister interface {
	Sessions(ctx context.Context) ([]string, error)
}

// ContactLinker returns a scannable link to the account's own profile —
// rendered as the personal QR code.
type ContactLinker interface {
	ContactLink(ctx context.Context) (string, error)
}

// PrivacyManager reads/writes the account's privacy toggles as name→value
// strings. The names are backend-defined; the dialog renders the subset
// it knows (last/profile/status/readreceipts/groupadd).
type PrivacyManager interface {
	PrivacySettings(ctx context.Context) map[string]string
	SetPrivacy(ctx context.Context, name, value string) error
}

// Verifier returns the account's E2E verification material for a peer —
// the numeric "safety number" the UI renders in grouped form.
type Verifier interface {
	VerifyCode(ctx context.Context, chatID store.PeerID) (string, error)
}

// ChatEncryptor reports whether a chat is end-to-end encrypted — the chat
// header's lock indicator. An error means "unknown": the UI renders no
// icon rather than a misleading one.
type ChatEncryptor interface {
	ChatE2EE(ctx context.Context, chatID store.PeerID) (bool, error)
}

// PeerIDs exposes a backend's peer-id forms so display code can resolve
// aliases without importing protocol libraries. WhatsApp's LID↔PN pair
// is the motivating case; single-namespace backends implement the forms
// they have and return "" for the rest.
type PeerIDs interface {
	// PeerIDForms lists the chat-id forms a bare user part (phone digits,
	// numeric id) may appear under — one entry for single-namespace
	// backends, two for WhatsApp's PN/LID split.
	PeerIDForms(user string) []store.PeerID
	// AltJID returns the peer's counterpart id in the backend's other
	// namespace (LID↔PN), or "" when none is known.
	AltJID(ctx context.Context, jid store.PeerID) store.PeerID
	// NumberJID returns the id form carrying the peer's real number —
	// the PN for a LID, itself for a PN — "" when unknown or the id
	// carries no number.
	NumberJID(ctx context.Context, jid store.PeerID) store.PeerID
}

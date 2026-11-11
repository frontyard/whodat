package protocol

import (
	"context"

	"github.com/frontyard/whodat/internal/store"
)

// Service is one logged-in account on one protocol. Chat-scoped ids carry
// the account prefix ("wa:1234@s.whatsapp.net") — the backend strips it
// before hitting the wire and adds it to everything it stores or publishes.
type Service interface {
	// Identity + lifecycle.
	Acct() string        // account id — the chat-id prefix ("wa", "wa2")
	Protocol() string    // human label: "whatsapp"
	Paired() bool        // authorized session exists
	OwnID() store.PeerID // own id in chat-id form
	Caps() Caps
	Connect(ctx context.Context) error
	Disconnect()
	Reset(ctx context.Context) error
	Logout(ctx context.Context) error // unlinks and erases this account's data only

	// Messaging.
	SendText(ctx context.Context, chatID store.PeerID, text string, mentions []Mention, quoted *store.Message) (*store.Message, error)
	SendMedia(ctx context.Context, chatID store.PeerID, m *store.Message, data []byte) (*store.Message, error)
	SendReaction(ctx context.Context, chatID store.PeerID, target *store.Message, emoji string) error
	SendStar(ctx context.Context, chatID store.PeerID, target *store.Message, star bool) error
	SendEdit(ctx context.Context, chatID store.PeerID, target *store.Message, newText string) error
	SendRevoke(ctx context.Context, chatID store.PeerID, target *store.Message) error
	Forward(ctx context.Context, toChatID store.PeerID, m *store.Message) error
	MarkRead(ctx context.Context, chatID store.PeerID, msgs []*store.Message) error
	SendTyping(ctx context.Context, chatID store.PeerID, composing bool) error
	SetPresence(ctx context.Context, online bool) error
	SubscribePresence(ctx context.Context, chatID store.PeerID) error

	// Chat + contact ops.
	SetChatPin(ctx context.Context, chatID store.PeerID, pin bool) error
	SetChatArchive(ctx context.Context, chatID store.PeerID, arch bool) error
	SetChatMute(ctx context.Context, chatID store.PeerID, mute bool) error
	SetDisappearing(ctx context.Context, chatID store.PeerID, secs int) error
	MarkChatUnreadSync(ctx context.Context, chatID store.PeerID, unread bool) error
	DeleteChat(ctx context.Context, chatID store.PeerID) error
	LeaveGroup(ctx context.Context, chatID store.PeerID) error
	UpdateChatName(ctx context.Context, chatID store.PeerID) error
	GetProfilePictureURL(ctx context.Context, id store.PeerID) (string, error)
	SetBlocked(ctx context.Context, id store.PeerID, block bool) error
	IsBlocked(ctx context.Context, id store.PeerID) bool

	// Polls.
	SendPoll(ctx context.Context, chatID store.PeerID, question string, options []string, max int) (*store.Message, error)
	VotePoll(ctx context.Context, chatID store.PeerID, poll *store.Message, opts []string) error
	PollBallot(ctx context.Context, chatID store.PeerID, pollID string) (map[string][]store.PeerID, error)

	// Groups.
	CreateGroup(ctx context.Context, subject string, participants []store.PeerID) (*GroupInfo, error)
	GetGroupInfo(ctx context.Context, chatID store.PeerID) (*GroupInfo, error)
	GroupInviteLink(ctx context.Context, chatID store.PeerID, reset bool) (string, error)
	SetGroupName(ctx context.Context, chatID store.PeerID, name string) error
	SetGroupDescription(ctx context.Context, chatID store.PeerID, desc string) error
	UpdateGroupParticipants(ctx context.Context, chatID store.PeerID, participants []store.PeerID, action ParticipantChange) error

	// Media: resolve a message's attachment to a local path, downloading
	// lazily. CanFetchMedia reports whether the row carries a download ref.
	FetchMedia(ctx context.Context, m *store.Message) (string, error)
	CanFetchMedia(m *store.Message) bool

	// Auth: stage-driven login. StartAuth kicks the flow (QR stream, waits
	// for input); SubmitAuth feeds a value for the stage; the service
	// advances via EvAuthStage events.
	StartAuth(ctx context.Context) error
	SubmitAuth(ctx context.Context, stage, value string) error
	AuthStages() []string
}

// AuthStage names a login-flow step the UI renders.
type AuthStage string

const (
	StageQR       AuthStage = "qr"       // QRCode carries the payload to render
	StagePhone    AuthStage = "phone"    // wants a phone number
	StagePairCode AuthStage = "paircode" // displays an 8-digit pairing code (AuthHint)
	StageCode     AuthStage = "code"     // wants the OTP/login code (AuthHint: info)
	StagePassword AuthStage = "password" // wants the 2FA password
	StageDone     AuthStage = "done"     // authorized
	StageFailed   AuthStage = "failed"   // AuthHint carries the error
)

// ParticipantChange is add/remove/promote/demote on a group member.
type ParticipantChange int

const (
	ParticipantAdd ParticipantChange = iota
	ParticipantRemove
	ParticipantPromote
	ParticipantDemote
)

// GroupParticipant is one member in normalized form.
type GroupParticipant struct {
	ID      store.PeerID // id the service accepts for ops (wa:<jid>)
	Phone   string       // real number/username when distinguishable from ID
	Display string       // protocol-provided display name (anonymous participants)
	IsAdmin bool
	IsMe    bool
}

// GroupInfo is normalized group metadata.
type GroupInfo struct {
	ID           store.PeerID
	Name         string
	Description  string
	Participants []GroupParticipant
}

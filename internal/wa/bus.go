package wa

import "github.com/frontyard/whodat/internal/protocol"

// The bus contract lives in internal/protocol — these aliases keep the
// package-local names readable without a second import.
type (
	Bus        = protocol.Bus
	StampedBus = protocol.StampedBus
	Event      = protocol.Event
	EventKind  = protocol.EventKind
	TypingInfo = protocol.TypingInfo
)

const (
	EvQRCode          = protocol.EvQRCode
	EvPaired          = protocol.EvPaired
	EvConnected       = protocol.EvConnected
	EvDisconnected    = protocol.EvDisconnected
	EvLoggedOut       = protocol.EvLoggedOut
	EvSyncStart       = protocol.EvSyncStart
	EvSyncDone        = protocol.EvSyncDone
	EvMessage         = protocol.EvMessage
	EvMessageUpdated  = protocol.EvMessageUpdated
	EvReceipt         = protocol.EvReceipt
	EvChatUpdated     = protocol.EvChatUpdated
	EvChatGone        = protocol.EvChatGone
	EvTyping          = protocol.EvTyping
	EvPresence        = protocol.EvPresence
	EvContactsUpdated = protocol.EvContactsUpdated
	EvAvatarChanged   = protocol.EvAvatarChanged
	EvAuthStage       = protocol.EvAuthStage
)

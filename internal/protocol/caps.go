package protocol

// Caps advertises which features a protocol account supports. The UI hides
// menu items, dialogs and buttons whose capability is false.
type Caps struct {
	// messaging
	Reactions    bool
	Edit         bool
	Revoke       bool
	Forward      bool
	Star         bool
	Polls        bool
	Typing       bool
	Presence     bool
	ReadReceipts bool

	// chat ops
	Pin          bool
	Archive      bool
	Mute         bool
	Disappearing bool
	Block        bool
	Leave        bool
	DeleteChat   bool

	// groups
	Groups      bool
	GroupInvite bool
	GroupAdmin  bool
	CreateGroup bool

	// calls
	Calls      bool
	GroupCalls bool
	VideoCalls bool

	// protocol extras (exposed via optional interfaces, not Service)
	ContactQR       bool
	LinkedDevices   bool
	Privacy         bool
	VerifyE2E       bool
	DirectorySearch bool // resolve phone/username to a chat
	AuthQR          bool
	ServerHistory   bool // can fetch older pages on demand
}

// FullCaps returns Caps with everything set — WhatsApp's current surface.
func FullCaps() Caps {
	return Caps{
		Reactions: true, Edit: true, Revoke: true, Forward: true, Star: true, Polls: true,
		Typing: true, Presence: true, ReadReceipts: true,
		Pin: true, Archive: true, Mute: true, Disappearing: true, Block: true, Leave: true, DeleteChat: true,
		Groups: true, GroupInvite: true, GroupAdmin: true, CreateGroup: true,
		Calls: true, GroupCalls: true, VideoCalls: true,
		ContactQR: true, LinkedDevices: true, Privacy: true, VerifyE2E: true,
		DirectorySearch: true, AuthQR: true,
	}
}

package call

import (
	"context"
	"io"

	"github.com/frontyard/whodat/internal/protocol"
	appstore "github.com/frontyard/whodat/internal/store"
	"github.com/frontyard/whodat/internal/wa"
)

// Provider owns call signalling for one protocol account. The Manager routes
// outgoing calls to the provider matching the chat id's account prefix and
// fans incoming calls out to the UI.
type Provider interface {
	// Place starts a 1:1 call to a chat-id target.
	Place(ctx context.Context, target appstore.PeerID, video bool) (*Session, error)
	// PlaceGroupByID starts/joins a call bound to a group chat.
	PlaceGroupByID(ctx context.Context, groupID appstore.PeerID) (*Session, error)
	// PlaceGroup rings the given targets as one call.
	PlaceGroup(ctx context.Context, targets ...appstore.PeerID) (*Session, error)
	// OnIncoming installs the incoming-call route.
	OnIncoming(fn func(*Session))
}

// ProviderFor returns the call provider a service carries. The binding of
// protocol backends to their signalling adapters lives here — this is the
// one place the call layer names a concrete service, so the UI and core
// never do.
func ProviderFor(svc protocol.Service, bus *protocol.Bus) Provider {
	if s, ok := svc.(*wa.Service); ok {
		return NewWAProvider(s, bus)
	}
	return nil
}

// conn is the transport behind a Session — signalling plus media hooks.
// Audio moves as s16le 16 kHz mono PCM out and f32 frames in; video moves as
// encoded frames ("access units") in the wire codec VideoCodec reports, so
// the session's GStreamer pipes adapt to whichever codec the protocol uses.
// Each protocol adapts its library's transport to this shape.
type conn interface {
	// signalling
	Answer() error
	Reject() error
	Hangup() error
	ID() string
	Peer() appstore.PeerID // namespaced chat id, no device part
	IsGroup() bool
	Ended() bool

	// media
	PlayAudio(r io.ReadCloser)       // outbound: s16le PCM stream
	ReceiveAudio(fn func([]float32)) // inbound: f32 frames
	SendVideoAU(au []byte) error     // one encoded frame, VideoCodec's codec
	StartVideo() error               // upgrade handshake — audio-only call adding video
	SetVideoEnabled(on bool) error   // toggle within already-negotiated video
	StopVideo() error                // signal video-off to the peer
	AcceptVideo() error              // accept the peer's video-upgrade request

	// wire properties
	VideoCodec() string  // "h264" | "vp8" — selects encode/decode pipelines
	NativeCallLog() bool // the server logs call rows itself — skip ours
	Emojis() []string    // E2E key-verification emoji (nil when unsupported)

	// events — providers map their library's types to these normalized hooks
	OnPhase(fn func(name string)) // display-ready phase text
	OnEnd(fn func(reason string))
	OnPeerAccept(fn func())
	OnKeyframeRequest(fn func())                   // peer wants an IDR
	OnVideoUpgrade(fn func())                      // peer turned its camera on mid-call
	OnOrientation(fn func(quarterTurns int))       // out-of-band rotation updates
	OnFrameAU(fn func(au []byte, orientation int)) // inbound AU; orient<0 = unchanged
}

package call

import (
	"context"
	"io"
	"strings"

	"github.com/polymorfa/hypermeow/types"
	"github.com/suhwr/meowcaller"

	"github.com/frontyard/whodat/internal/protocol"
	appstore "github.com/frontyard/whodat/internal/store"
	"github.com/frontyard/whodat/internal/wa"
)

// waProvider adapts meowcaller (WhatsApp VoIP) to the Provider interface.
// It must be created before the whatsmeow client connects — meowcaller
// installs a raw call hook on it.
type waProvider struct {
	acct string
	mc   *meowcaller.Client
	db   *appstore.DB
	bus  *protocol.Bus
}

// NewWAProvider builds the WhatsApp call provider. It binds the client
// current at construction — a Reset() swap afterwards needs a fresh
// provider (meowcaller installs its hook on the client it was given).
func NewWAProvider(waSvc *wa.Service, bus *protocol.Bus) Provider {
	return &waProvider{
		acct: waSvc.Acct(),
		mc:   meowcaller.NewClient(waSvc.Client()),
		db:   waSvc.DB(),
		bus:  bus,
	}
}

// strip drops the account prefix from an app-facing id for the wire.
func (p *waProvider) strip(id appstore.PeerID) string {
	return strings.TrimPrefix(id.String(), p.acct+":")
}

// Place starts a 1:1 call.
func (p *waProvider) Place(ctx context.Context, target appstore.PeerID, video bool) (*Session, error) {
	var c *meowcaller.Call
	var err error
	if video {
		c, err = p.mc.CallWithOptions(ctx, p.strip(target), meowcaller.CallOptions{Video: true})
	} else {
		c, err = p.mc.Call(ctx, p.strip(target))
	}
	if err != nil {
		return nil, err
	}
	return newSession(&waConn{c: c, acct: p.acct}, false, video, p.db, p.bus), nil
}

// PlaceGroupByID starts a group-bound call (experimental upstream).
func (p *waProvider) PlaceGroupByID(ctx context.Context, groupID appstore.PeerID) (*Session, error) {
	c, err := p.mc.GroupCallByID(ctx, p.strip(groupID))
	if err != nil {
		return nil, err
	}
	return newSession(&waConn{c: c, acct: p.acct}, false, false, p.db, p.bus), nil
}

// PlaceGroup rings the given targets as one call.
func (p *waProvider) PlaceGroup(ctx context.Context, targets ...appstore.PeerID) (*Session, error) {
	wire := make([]string, len(targets))
	for i, t := range targets {
		wire[i] = p.strip(t)
	}
	c, err := p.mc.GroupCall(ctx, wire...)
	if err != nil {
		return nil, err
	}
	return newSession(&waConn{c: c, acct: p.acct}, false, false, p.db, p.bus), nil
}

// OnIncoming installs the incoming-call route.
func (p *waProvider) OnIncoming(fn func(*Session)) {
	p.mc.OnIncomingCall(func(c *meowcaller.Call) {
		fn(newSession(&waConn{c: c, acct: p.acct}, true, c.IsVideo(), p.db, p.bus))
	})
}

// waConn adapts one meowcaller.Call to the normalized conn.
type waConn struct {
	c     *meowcaller.Call
	acct  string    // owning account — the peer's chat-id prefix
	oriFn func(int) // OnOrientation hook — rides inside OnVideoState
}

func (w *waConn) Answer() error { return w.c.Answer() }
func (w *waConn) Reject() error { return w.c.Reject() }
func (w *waConn) Hangup() error { return w.c.Hangup() }
func (w *waConn) ID() string    { return w.c.ID() }

// Peer returns the peer namespaced like every other chat-scoped id — the
// call-log row and name lookups route by this account's prefix.
func (w *waConn) Peer() appstore.PeerID { return appstore.NewID(w.acct, w.c.Peer().ToNonAD().String()) }
func (w *waConn) IsGroup() bool         { return w.c.Peer().Server == types.GroupServer }
func (w *waConn) Ended() bool           { return w.c.State() == meowcaller.CallPhaseEnded }

func (w *waConn) PlayAudio(r io.ReadCloser)       { w.c.Play(meowcaller.PCMStream(r)) }
func (w *waConn) ReceiveAudio(fn func([]float32)) { w.c.Receive(meowcaller.SinkFunc(fn)) }
func (w *waConn) SendVideoAU(au []byte) error     { return w.c.SendVideo(au) }
func (w *waConn) StartVideo() error               { return w.c.StartVideo() }
func (w *waConn) SetVideoEnabled(on bool) error   { return w.c.SetVideoEnabled(on) }
func (w *waConn) StopVideo() error                { return w.c.StopVideo() }
func (w *waConn) AcceptVideo() error              { return w.c.AcceptVideo() }

// VideoCodec — WhatsApp's wire codec is H.264.
func (w *waConn) VideoCodec() string { return "h264" }

// NativeCallLog — WhatsApp produces no server-side call rows; the local
// log the session writes is the only record.
func (w *waConn) NativeCallLog() bool { return false }

// Emojis — WhatsApp calls have no in-call key-verification emoji surface.
func (w *waConn) Emojis() []string { return nil }

func (w *waConn) OnPhase(fn func(string)) {
	w.c.OnStateChange(func(p meowcaller.CallPhase) { fn(phaseName(p)) })
}
func (w *waConn) OnEnd(fn func(string))       { w.c.OnEnd(fn) }
func (w *waConn) OnPeerAccept(fn func())      { w.c.OnPeerAccept(fn) }
func (w *waConn) OnKeyframeRequest(fn func()) { w.c.OnVideoKeyframeRequest(fn) }
func (w *waConn) OnVideoUpgrade(fn func()) {
	w.c.OnVideoState(func(vs meowcaller.VideoState) {
		// Video-off/update events report orientation=0 — don't let them
		// wipe out the last meaningful rotation or frames snap back.
		if vs.Orientation != 0 {
			w.emitOri(vs.Orientation & 3)
		}
		// A peer that turns its camera on mid-call asks for a video
		// upgrade (UpgradeRequestV2). Nothing accepts it automatically —
		// without the accept their sender stays gated and our inbound
		// video never flows.
		if vs.Upgrade {
			fn()
		}
	})
}
func (w *waConn) OnOrientation(fn func(int)) { w.oriFn = fn }
func (w *waConn) OnFrameAU(fn func([]byte, int)) {
	w.c.OnParticipantVideoFrame(func(f meowcaller.ParticipantVideoFrame) {
		fn(f.AccessUnit, f.Orientation)
	})
}

// emitOri forwards the video-state orientation to the OnOrientation hook —
// registered after OnVideoUpgrade, so the field rides on the conn.
func (w *waConn) emitOri(turns int) {
	if w.oriFn != nil {
		w.oriFn(turns)
	}
}

// phaseName maps meowcaller phases to display text.
func phaseName(p meowcaller.CallPhase) string {
	switch p {
	case meowcaller.CallPhaseCalling:
		return "Calling…"
	case meowcaller.CallPhaseRinging:
		return "Ringing…"
	case meowcaller.CallPhaseConnecting:
		return "Connecting…"
	case meowcaller.CallPhaseActive:
		return "In call"
	case meowcaller.CallPhaseWaitingRoom:
		return "Waiting room"
	case meowcaller.CallPhaseEnded:
		return "Call ended"
	default:
		return "Connecting…"
	}
}

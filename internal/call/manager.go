// Package call bridges the protocol call transport (meowcaller/WhatsApp)
// with GStreamer device I/O and surfaces a UI-friendly session object.
package call

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-gst/go-gst/gst"

	"github.com/frontyard/whodat/internal/protocol"
	appstore "github.com/frontyard/whodat/internal/store"
)

var gstOnce sync.Once

// Manager routes calls through per-account providers and reports incoming
// calls to the UI.
type Manager struct {
	providers map[string]Provider
	deflt     string // fallback account for ids with no registered prefix

	mu         sync.Mutex
	onIncoming func(*Session)
	active     *Session
}

// New builds an empty manager; providers register per account.
func New() *Manager {
	gstOnce.Do(func() { gst.Init(nil) })
	return &Manager{providers: map[string]Provider{}}
}

// RegisterProvider attaches a call provider to an account. bareOwner marks
// the account behind the unprefixed chat-id namespace (a non-Multi
// backend); registration order can't answer this, so an early namespaced
// account must not absorb bare ids. When no non-multi backend exists the
// earliest registration is the fallback.
func (m *Manager) RegisterProvider(acct string, p Provider, bareOwner bool) {
	m.mu.Lock()
	m.providers[acct] = p
	if bareOwner || m.deflt == "" {
		m.deflt = acct
	}
	m.mu.Unlock()
	p.OnIncoming(m.routeIncoming)
}

// UnregisterProvider detaches a removed account's provider, handing the
// bare-namespace default to the earliest remaining registration.
func (m *Manager) UnregisterProvider(acct string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.providers, acct)
	if m.deflt == acct {
		m.deflt = ""
		for k := range m.providers {
			if m.deflt == "" || k < m.deflt {
				m.deflt = k
			}
		}
	}
}

// routeIncoming is the single incoming-call fan-out: session becomes active,
// then the UI callback fires.
func (m *Manager) routeIncoming(s *Session) {
	m.setActive(s)
	m.mu.Lock()
	fn := m.onIncoming
	m.mu.Unlock()
	if fn != nil {
		fn(s)
	}
}

// SetIncomingHandler registers the UI callback for incoming calls.
func (m *Manager) SetIncomingHandler(fn func(*Session)) {
	m.mu.Lock()
	m.onIncoming = fn
	m.mu.Unlock()
}

func (m *Manager) setActive(s *Session) {
	m.mu.Lock()
	m.active = s
	m.mu.Unlock()
}

// Active returns the current call session, if any.
func (m *Manager) Active() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

// errBusy is returned when a call is already in progress.
var errBusy = errors.New("a call is already in progress")

// hasLiveCall reports whether an active session hasn't ended yet.
func (m *Manager) hasLiveCall() bool {
	s := m.Active()
	return s != nil && !s.conn.Ended()
}

// providerFor resolves the provider owning a chat-scoped id — the account
// prefix before ':', or the default when the prefix names no provider.
func (m *Manager) providerFor(id appstore.PeerID) Provider {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := strings.IndexByte(id.String(), ':'); i > 0 {
		if p, ok := m.providers[id[:i].String()]; ok {
			return p
		}
	}
	return m.providers[m.deflt]
}

// Call starts a 1:1 call to a phone number or chat-id string.
func (m *Manager) Call(ctx context.Context, target appstore.PeerID, video bool) (*Session, error) {
	if m.hasLiveCall() {
		return nil, errBusy
	}
	p := m.providerFor(target)
	if p == nil {
		return nil, fmt.Errorf("no call provider for %q", target)
	}
	s, err := p.Place(ctx, target, video)
	if err != nil {
		return nil, err
	}
	m.setActive(s)
	s.startAudio()
	return s, nil
}

// GroupCallByID starts a group-bound call.
func (m *Manager) GroupCallByID(ctx context.Context, groupID appstore.PeerID) (*Session, error) {
	if m.hasLiveCall() {
		return nil, errBusy
	}
	p := m.providerFor(groupID)
	if p == nil {
		return nil, fmt.Errorf("no call provider for %q", groupID)
	}
	s, err := p.PlaceGroupByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	m.setActive(s)
	s.startAudio()
	return s, nil
}

// GroupCall rings the given targets as one call.
func (m *Manager) GroupCall(ctx context.Context, targets ...appstore.PeerID) (*Session, error) {
	if m.hasLiveCall() {
		return nil, errBusy
	}
	m.mu.Lock()
	deflt := m.deflt
	m.mu.Unlock()
	var route appstore.PeerID
	if len(targets) > 0 {
		route = targets[0]
	} else {
		route = appstore.PeerID(deflt)
	}
	p := m.providerFor(route)
	if p == nil {
		return nil, fmt.Errorf("no call provider")
	}
	s, err := p.PlaceGroup(ctx, targets...)
	if err != nil {
		return nil, err
	}
	m.setActive(s)
	s.startAudio()
	return s, nil
}

// Session wraps one call connection plus its GStreamer media pipelines.
type Session struct {
	conn     conn
	Incoming bool
	Video    bool // video enabled (either direction)

	// UI callbacks — assigned before Answer/returned to caller.
	OnPhase       func(phase string)
	OnEnd         func(reason string)
	OnRemoteFrame func(rgba []byte, w, h int)
	OnLocalFrame  func(rgba []byte, w, h int)
	OnPeerAccept  func()

	mic      *pullPipe // mic → call
	speaker  *pushPipe // call → speaker
	cam      *pullPipe // camera → H.264 → call
	camPrev  *pullPipe // camera → RGBA preview
	viddec   *pushPipe // H.264 in → RGBA out
	viddecMu sync.Mutex
	muted    atomic.Bool
	started  bool
	startMu  sync.Mutex
	db       *appstore.DB  // call-log row
	bus      *protocol.Bus // call-log events
	videoSig bool          // we sent video-on to the peer
	videoUpg bool          // next enable needs the UpgradeRequestV2 handshake
	remOri   atomic.Int32  // peer's device orientation, quarter turns CW
	camLive  atomic.Bool   // false while user has video off (camera kept warm)
	camStopT *time.Timer   // delayed device release after disable

	answeredAt time.Time   // talk-time start (accept/answer); zero = unanswered
	hadVideo   atomic.Bool // video ever enabled — decides the log label
	loggedOnce sync.Once   // one chat-log row per session
}

func newSession(c conn, incoming, video bool, db *appstore.DB, bus *protocol.Bus) *Session {
	s := &Session{conn: c, Incoming: incoming, Video: video, videoUpg: !video,
		db: db, bus: bus}
	s.hadVideo.Store(video)
	c.OnPhase(func(name string) {
		if s.OnPhase != nil {
			s.OnPhase(name)
		}
	})
	c.OnEnd(func(reason string) {
		s.stop()
		if s.OnEnd != nil {
			s.OnEnd(reason)
		}
	})
	c.OnPeerAccept(func() {
		if s.answeredAt.IsZero() {
			s.answeredAt = time.Now()
		}
		if s.OnPeerAccept != nil {
			s.OnPeerAccept()
		}
	})
	c.OnKeyframeRequest(func() { s.forceKeyframe() })
	c.OnVideoUpgrade(func() {
		// A peer that turns its camera on mid-call asks for a video
		// upgrade (UpgradeRequestV2). Nothing accepts it automatically —
		// without the accept their sender stays gated and our inbound
		// video never flows.
		go func() {
			if err := c.AcceptVideo(); err != nil {
				log.Printf("call: AcceptVideo: %v", err)
			}
		}()
	})
	c.OnOrientation(func(turns int) { s.remOri.Store(int32(turns & 3)) })
	return s
}

// Peer returns the peer's chat-id form (no device part).
func (s *Session) Peer() appstore.PeerID { return s.conn.Peer() }

// Ended reports whether the transport can no longer produce media.
func (s *Session) Ended() bool { return s.conn.Ended() }

// RemoteOrientation returns the peer's device orientation in quarter turns
// clockwise — rotate inbound frames by this before displaying.
func (s *Session) RemoteOrientation() int { return int(s.remOri.Load()) }

// Emojis returns the call's E2E key-verification emoji — nil until the key
// exchange completes, and always nil on transports that can't produce one.
func (s *Session) Emojis() []string { return s.conn.Emojis() }

// Answer accepts an incoming call and starts audio.
func (s *Session) Answer() error {
	if err := s.conn.Answer(); err != nil {
		return err
	}
	s.answeredAt = time.Now()
	s.startAudio()
	return nil
}

// Reject declines an incoming call.
func (s *Session) Reject() error { return s.conn.Reject() }

// Hangup ends the call.
func (s *Session) Hangup() error {
	s.stop()
	return s.conn.Hangup()
}

// startAudio wires mic capture → conn.PlayAudio and conn.ReceiveAudio →
// speaker. Idempotent; safe to call from both Answer() and outgoing Call().
// Pipeline startup runs outside startMu so stop()/hangup stay responsive.
func (s *Session) startAudio() {
	s.startMu.Lock()
	if s.started {
		s.startMu.Unlock()
		return
	}
	s.started = true
	s.startMu.Unlock()

	mic, spk, pw, err := startAudioPipes(&s.muted)
	if err != nil {
		log.Printf("call: audio pipeline failed: %v", err)
	} else {
		s.startMu.Lock()
		s.mic = mic
		s.speaker = spk
		s.startMu.Unlock()
		s.conn.PlayAudio(pw)
		s.conn.ReceiveAudio(func(frame []float32) {
			spk.PushAudioF32(frame)
		})
	}
	if s.Video {
		if err := s.EnableVideo(true); err != nil {
			log.Printf("call: EnableVideo: %v", err)
		}
	}
	s.attachVideoRecv()
}

// attachVideoRecv decodes inbound H.264 AUs to RGBA frames for the UI.
// Per-frame orientation (carried in-band) is used over out-of-band
// video-state orientation — the latter is stale by comparison and
// sometimes absent entirely.
func (s *Session) attachVideoRecv() {
	s.conn.OnFrameAU(func(au []byte, orient int) {
		if orient >= 0 {
			s.remOri.Store(int32(orient & 3))
		}
		s.viddecMu.Lock()
		if s.viddec == nil {
			p, pull, err := startVideoDecPipe(s.conn.VideoCodec())
			if err != nil {
				s.viddecMu.Unlock()
				log.Printf("call: video decode pipeline failed: %v", err)
				return
			}
			s.viddec = p
			first := true
			go func() {
				for frame := range pull {
					if first {
						first = false
						log.Printf("call: video decode: first frame %dx%d", frame.W, frame.H)
					}
					if s.OnRemoteFrame != nil {
						s.OnRemoteFrame(frame.Data, frame.W, frame.H)
					}
				}
			}()
		}
		vd := s.viddec
		s.viddecMu.Unlock()
		vd.PushBytes(au)
	})
}

// EnableVideo starts/stops sending camera video.
func (s *Session) EnableVideo(on bool) error {
	if on {
		// The camera scan can take tens of seconds — run it before taking
		// startMu so stop()/hangup don't stall behind it.
		if err := s.ensureCamera(); err != nil {
			return err
		}
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()
	return s.enableVideoLocked(on)
}

// ensureCamera probes and installs the camera pipelines (outside startMu).
func (s *Session) ensureCamera() error {
	s.startMu.Lock()
	need := s.cam == nil
	s.startMu.Unlock()
	if !need {
		return nil
	}
	cam, prev, aus, err := startCameraPipe(s.conn.VideoCodec())
	if err != nil {
		return err
	}
	s.startMu.Lock()
	defer s.startMu.Unlock()
	if s.cam != nil {
		// A concurrent enable won — tear down the duplicate probe.
		cam.Stop()
		prev.Stop()
		return nil
	}
	s.cam = cam
	s.camPrev = prev
	s.startCameraSend(aus)
	s.startCamPreview(prev)
	return nil
}

// startCamPreview pumps the local RGBA preview sink to the UI.
func (s *Session) startCamPreview(prev *pullPipe) {
	prevFrames := make(chan Frame, 2)
	go prev.pullFrames(prevFrames)
	go func() {
		for f := range prevFrames {
			if s.OnLocalFrame != nil && s.camLive.Load() {
				s.OnLocalFrame(f.Data, f.W, f.H)
			}
		}
	}()
}

// enableVideoLocked does EnableVideo's work; caller holds startMu.
// Phones toggle mid-call video with Stopped(6)↔Enabled(1) — no handshake —
// because the call's video media stays negotiated; the UpgradeRequestV2
// handshake is only for calls that started audio-only (the phone ignores
// an upgrade on an already-video call and the sender stays gated).
// Video-off is only signaled if we previously signaled on — a spurious
// video-off makes the peer pause its own camera (WhatsApp syncs video
// state symmetrically), which would kill inbound video on a failed
// local-camera start.
func (s *Session) enableVideoLocked(on bool) error {
	if !on {
		s.Video = false
		s.camLive.Store(false)
		if s.videoSig {
			s.videoSig = false
			_ = s.conn.StopVideo()
		}
		// Don't release the camera yet: re-acquiring the UVC device EOSes
		// for seconds after teardown. Keep it warm; release only if it
		// stays off (privacy/LED is preserved by the delayed release).
		if s.cam != nil {
			s.armCamRelease()
		}
		return nil
	}
	s.cancelCamRelease()
	s.camLive.Store(true)
	s.Video = true
	s.hadVideo.Store(true)
	if s.videoSig {
		return nil
	}
	var err error
	if s.videoUpg {
		// Call started audio-only — the peer never negotiated video media.
		err = s.conn.StartVideo()
	} else {
		err = s.conn.SetVideoEnabled(true)
	}
	if err != nil {
		return err
	}
	s.videoSig = true
	return nil
}

// camKeepAlive is how long a disabled camera stays warm for instant re-enable.
const camKeepAlive = 10 * time.Second

// armCamRelease schedules the device release; caller holds startMu.
func (s *Session) armCamRelease() {
	s.cancelCamRelease()
	cam, prev := s.cam, s.camPrev
	s.camStopT = time.AfterFunc(camKeepAlive, func() {
		s.startMu.Lock()
		defer s.startMu.Unlock()
		s.camStopT = nil
		if s.cam == cam && s.camPrev == prev && !s.camLive.Load() {
			s.cam.Stop()
			s.camPrev.Stop()
			s.cam, s.camPrev = nil, nil
		}
	})
}

// cancelCamRelease drops a pending release; caller holds startMu.
func (s *Session) cancelCamRelease() {
	if s.camStopT != nil {
		s.camStopT.Stop()
		s.camStopT = nil
	}
}

// startCameraSend pumps encoded AUs into the call.
func (s *Session) startCameraSend(aus <-chan []byte) {
	go func() {
		errLogged := false
		for au := range aus {
			if !s.camLive.Load() {
				continue // video off — drain but don't transmit
			}
			if err := s.conn.SendVideoAU(au); err != nil && !errLogged {
				log.Printf("call: SendVideo: %v", err)
				errLogged = true
			}
		}
	}()
}

// SetMuted silences the microphone (sends silence so the stream stays alive).
func (s *Session) SetMuted(m bool) { s.muted.Store(m) }

// Muted reports the mic state.
func (s *Session) Muted() bool { return s.muted.Load() }

// ErrNoCamera is returned when no usable camera device is found.
var ErrNoCamera = errors.New("no camera device available")

func (s *Session) forceKeyframe() {
	if s.cam != nil {
		s.cam.ForceKeyUnit()
	}
}

// writeCallLog records the finished call as a chat bubble (once per session).
// A backend whose server writes call rows itself opts out via NativeCallLog —
// a local row would just duplicate the synced one.
func (s *Session) writeCallLog() {
	if s.conn.NativeCallLog() {
		return
	}
	ctx := context.Background()
	chatJID := s.db.CanonicalJID(ctx, s.conn.Peer())
	kind := appstore.KindDM
	if s.conn.IsGroup() {
		kind = appstore.KindGroup
	}
	answered := !s.answeredAt.IsZero()
	dur := 0
	if answered {
		dur = int(time.Since(s.answeredAt).Seconds())
	}
	label, mime := "Voice", "call/audio"
	if s.hadVideo.Load() {
		label, mime = "Video", "call/video"
	}
	var text string
	switch {
	case !answered && s.Incoming:
		text = "Missed " + strings.ToLower(label) + " call"
	case !answered:
		text = "No answer"
	default:
		text = label + " call"
		if dur > 0 {
			text += fmt.Sprintf(" · %d:%02d", dur/60, dur%60)
		}
	}
	var sender appstore.PeerID
	if s.Incoming {
		sender = s.conn.Peer()
	}
	m := &appstore.Message{
		ChatJID: chatJID, ID: "call-" + s.conn.ID(), SenderJID: sender,
		TS: time.Now(), Type: appstore.TCall, Text: text,
		MediaMIME: mime, Duration: dur, FromMe: !s.Incoming,
	}
	if err := s.db.EnsureChat(ctx, chatJID, kind); err != nil {
		return
	}
	if err := s.db.InsertMessage(ctx, m); err != nil {
		return
	}
	// Only an unanswered incoming call counts as unread.
	_ = s.db.TouchChat(ctx, chatJID, text, m.TS, !answered && s.Incoming)
	s.bus.Publish(protocol.Event{Kind: protocol.EvMessage, ChatJID: chatJID, Message: m})
	s.bus.Publish(protocol.Event{Kind: protocol.EvChatUpdated, ChatJID: chatJID})
}

// stop tears down all pipelines.
func (s *Session) stop() {
	s.loggedOnce.Do(s.writeCallLog)
	s.startMu.Lock()
	defer s.startMu.Unlock()
	s.cancelCamRelease()
	s.camLive.Store(false)
	for _, p := range []*pullPipe{s.mic, s.cam, s.camPrev} {
		if p != nil {
			p.Stop()
		}
	}
	s.viddecMu.Lock()
	vd := s.viddec
	s.viddec = nil
	s.viddecMu.Unlock()
	for _, p := range []*pushPipe{s.speaker, vd} {
		if p != nil {
			p.Stop()
		}
	}
	s.mic, s.cam, s.camPrev, s.speaker = nil, nil, nil, nil
	s.videoSig = false
}

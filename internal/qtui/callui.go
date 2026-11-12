package qtui

import (
	"context"
	"encoding/json"
	"fmt"
	appstore "github.com/frontyard/whodat/internal/store"
	"log"
	"strings"
	"time"
	"unsafe"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/multimedia"

	"github.com/frontyard/whodat/internal/call"
	"github.com/frontyard/whodat/internal/protocol"
)

// callui wires the call.Manager into QML: two QVideoSink-backed capture
// sessions (remote + local preview) exposed as context properties, call
// state keys driving CallDialog.qml, and ops for every button.

// initCalls installs the incoming-call handler and the video sinks.
// Qt thread only — context props must exist before the engine loads.
func (h *Host) initCalls() {
	// QML assigns session.videoOutput, which swaps the session's videoSink
	// for the VideoOutput's own — so frames must go to sess.VideoSink()
	// (the *current* sink at push time), never a pre-bound one.
	h.remSess = multimedia.NewQMediaCaptureSession()
	h.locSess = multimedia.NewQMediaCaptureSession()

	h.app.CM.SetIncomingHandler(func(s *call.Session) {
		mt(func() { h.presentIncoming(s) })
	})
}

// registerCallOps installs call-button ops.
func (h *Host) registerCallOps() {
	h.ops["startCall"] = h.opStartCall
	h.ops["answerCall"] = h.opAnswerCall
	h.ops["rejectCall"] = h.opRejectCall
	h.ops["hangupCall"] = h.opHangupCall
	h.ops["muteCall"] = h.opMuteCall
	h.ops["videoCall"] = h.opVideoCall
	h.ops["rotateVideo"] = h.opRotateVideo
}

// opStartCall dials a chat. args: {jid, video, group}
func (h *Host) opStartCall(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID   appstore.PeerID `json:"jid"`
		Video bool            `json:"video"`
		Group bool            `json:"group"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var s *call.Session
		var err error
		if a.Group {
			s, err = h.app.CM.GroupCallByID(ctx, a.JID)
		} else {
			s, err = h.app.CM.Call(ctx, a.JID, a.Video)
		}
		if err != nil {
			log.Printf("call: start failed: %v", err)
			h.Toast("Call failed: " + protocol.UserErr(err).Error())
			return
		}
		mt(func() { h.presentOutgoing(s) })
	}()
	return map[string]any{}, nil
}

// resetCallState clears the sticky per-call keys — without this a previous
// call's callEnded=true leaves the control bar hidden on the next call.
func (h *Host) resetCallState() {
	h.remW, h.remH = 0, 0
	h.setState("callEnded", false)
	h.setState("callEndedReason", "")
	h.setState("callIncoming", false)
	h.setState("callMuted", false)
	h.setState("callVideo", false)
	h.setState("callRemoteVideo", false)
	h.setState("callRemoteSize", "0x0")
	h.setState("callEmojis", "")
}

// presentIncoming opens the dialog in accept/reject mode.
func (h *Host) presentIncoming(s *call.Session) {
	h.call = s
	h.callEnded.Store(false)
	h.userRot.Store(0)
	h.wireSession(s)
	name := h.app.ChatName(context.Background(), s.Peer())
	h.resetCallState()
	h.setState("callName", name)
	h.setState("callPhase", "Incoming call")
	h.setState("callIncoming", true)
	h.setState("callVideo", s.Video)
	h.setState("callActive", true)
	h.requestWindow()
}

// presentOutgoing opens the dialog in outgoing mode (controls shown).
func (h *Host) presentOutgoing(s *call.Session) {
	h.call = s
	h.callEnded.Store(false)
	h.userRot.Store(0)
	h.wireSession(s)
	name := h.app.ChatName(context.Background(), s.Peer())
	h.resetCallState()
	h.setState("callName", name)
	h.setState("callPhase", "Calling…")
	h.setState("callVideo", s.Video)
	h.setState("callActive", true)
}

// wireSession binds the session callbacks to state + sinks. Callbacks arrive
// off-thread — state writes go through mt, video frames push directly
// (QVideoSink::setVideoFrame is documented thread-safe).
func (h *Host) wireSession(s *call.Session) {
	s.OnPhase = func(phase string) {
		mt(func() {
			h.setState("callPhase", phase)
			// The fingerprint lands with the key exchange, before "In
			// call" — every transition re-checks so it surfaces as soon
			// as it exists.
			if em := s.Emojis(); len(em) > 0 {
				h.setState("callEmojis", strings.Join(em, " "))
			}
		})
	}
	s.OnPeerAccept = func() {
		mt(func() {
			h.setState("callIncoming", false)
			h.setState("callPhase", "In call")
		})
	}
	s.OnEnd = func(reason string) {
		mt(func() {
			h.callEnded.Store(true)
			h.setState("callPhase", "Call ended")
			h.setState("callEndedReason", reason)
			h.setState("callEnded", true)
			// Clear video flags so the dialog can tear down the frames.
			h.setState("callVideo", false)
			h.setState("callRemoteVideo", false)
		})
	}
	s.OnRemoteFrame = func(rgba []byte, w, ht int) {
		if h.callEnded.Load() || w == 0 {
			return
		}
		turns := (s.RemoteOrientation() + int(h.userRot.Load())) & 3
		rgba, w, ht = call.RotateRGBA(rgba, w, ht, turns)
		// Post-rotation dims drive the window reshape: the dialog tracks the frame's real aspect so the
		// video fills it instead of pillarboxing into a fixed preset.
		mt(func() {
			if w != h.remW || ht != h.remH {
				h.remW, h.remH = w, ht
				h.setState("callRemoteSize", fmt.Sprintf("%dx%d", w, ht))
			}
			// The remote surface is shown whenever remote frames arrive —
			// this key is deliberately separate from callVideo, which the
			// camera toggle button uses for our own camera state.
			h.setState("callRemoteVideo", true)
		})
		pushRGBA(h.remSess.VideoSink(), rgba, w, ht)
	}
	s.OnLocalFrame = func(rgba []byte, w, ht int) {
		if h.callEnded.Load() || w == 0 {
			return
		}
		dw, dh := call.FitInside(w, ht, 320, 240)
		rgba = call.DownscaleRGBA(rgba, w, ht, dw, dh)
		pushRGBA(h.locSess.VideoSink(), rgba, dw, dh)
	}
	// A conn that died before its hooks were wired never fires OnEnd —
	// latch the ended state so the dialog doesn't sit open on a dead call.
	if s.Ended() {
		mt(func() {
			h.callEnded.Store(true)
			h.setState("callPhase", "Call ended")
			h.setState("callEnded", true)
		})
	}
}

// pushRGBA uploads one RGBA frame into the sink.
func pushRGBA(sink *multimedia.QVideoSink, rgba []byte, w, h int) {
	if sink == nil {
		return // QML hasn't bound its VideoOutput yet
	}
	fmt := multimedia.NewQVideoFrameFormat2(qt.NewQSize2(w, h), multimedia.QVideoFrameFormat__Format_RGBA8888)
	f := multimedia.NewQVideoFrame2(fmt)
	if !f.Map(multimedia.QVideoFrame__ReadWrite) {
		return
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(f.Bits(0))), len(rgba))
	copy(dst, rgba)
	f.Unmap()
	sink.SetVideoFrame(f)
}

func (h *Host) opAnswerCall(_ *Host, _ json.RawMessage) (any, error) {
	s := h.call
	if s == nil {
		return nil, nil
	}
	go func() {
		if err := s.Answer(); err != nil {
			h.Toast("Answer failed: " + protocol.UserErr(err).Error())
			mt(func() { h.setState("callActive", false) })
		}
	}()
	mt(func() { h.setState("callIncoming", false) })
	return map[string]any{}, nil
}

func (h *Host) opRejectCall(_ *Host, _ json.RawMessage) (any, error) {
	s := h.call
	if s == nil {
		return nil, nil
	}
	go func() { _ = s.Reject() }()
	mt(func() { h.setState("callActive", false) })
	return map[string]any{}, nil
}

func (h *Host) opHangupCall(_ *Host, _ json.RawMessage) (any, error) {
	s := h.call
	if s == nil {
		return nil, nil
	}
	h.callEnded.Store(true)
	go func() { _ = s.Hangup() }()
	mt(func() { h.setState("callActive", false) })
	return map[string]any{}, nil
}

func (h *Host) opMuteCall(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		On bool `json:"on"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	if s := h.call; s != nil {
		s.SetMuted(a.On)
	}
	mt(func() { h.setState("callMuted", a.On) })
	return map[string]any{}, nil
}

func (h *Host) opVideoCall(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		On bool `json:"on"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	s := h.call
	if s == nil {
		return nil, nil
	}
	go func() {
		if err := s.EnableVideo(a.On); err != nil {
			log.Printf("call: EnableVideo(%v): %v", a.On, err)
			h.Toast("Camera failed")
			mt(func() { h.setState("callVideo", !a.On) })
		}
	}()
	mt(func() { h.setState("callVideo", a.On) })
	return map[string]any{}, nil
}

// opRotateVideo nudges the remote rotation. args: {turns} — added CW quarter
// turns, accumulated with the peer's own orientation signal.
func (h *Host) opRotateVideo(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		Turns int `json:"turns"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	h.userRot.Add(int32(a.Turns))
	return map[string]any{}, nil
}

// (end of call ops)

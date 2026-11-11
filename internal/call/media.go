package call

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
)

// pullPipe wraps a pipeline ending in appsink; a goroutine pulls samples and
// forwards raw bytes (or decoded frames) to consumers.
type pullPipe struct {
	pipeline *gst.Pipeline
	sink     *app.Sink
	done     chan struct{}
	once     sync.Once
	muted    *atomic.Bool // when set, samples are zeroed before delivery
	enc      *gst.Element // encoder element (for key-unit events)
}

// pushPipe wraps a pipeline starting at appsrc; PushBytes feeds it.
type pushPipe struct {
	pipeline *gst.Pipeline
	src      *app.Source
	done     chan struct{}
	once     sync.Once
	audioF32 bool // reinterpret input as []float32 audio
	mu       sync.Mutex
}

func elementByName(p *gst.Pipeline, name string) *gst.Element {
	e, err := p.GetElementByName(name)
	if err != nil {
		return nil
	}
	return e
}

func launch(desc string) (*gst.Pipeline, error) {
	p, err := gst.NewPipelineFromString(desc)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// startAudioPipes builds ONE pipeline with both directions: webrtcdsp
// (capture) and webrtcechoprobe (playback) only pair for AEC when they share
// a pipeline. Returns the mic pull pipe, speaker push pipe, and the mic reader
// for meowcaller.PCMStream.
func startAudioPipes(muted *atomic.Bool) (*pullPipe, *pushPipe, io.ReadCloser, error) {
	pr, pw := io.Pipe()
	// webrtcdsp looks up its echo probe by element NAME ("webrtcechoprobe0"
	// by default) — auto-numbering drifts across pipelines, so pin it.
	// async=false on the sinks is critical: a pipeline only finishes
	// PAUSED→PLAYING once every sink prerolls — pulsesink prerolls on the
	// FIRST pushed inbound frame, so a dead inbound stream would wedge the
	// pipeline at PAUSED and take the mic with it.
	// The queue after each branch head gives capture and playback their
	// own scheduling threads — without them the clocked pulsesink starves
	// pulsesrc in the shared thread (capture underruns, garbled audio).
	desc := `pulsesrc ! queue ! audioconvert ! audioresample !
		audio/x-raw,rate=16000,channels=1 ! webrtcdsp ! volume volume=1.4 !
		audio/x-raw,format=S16LE !
		appsink name=mic emit-signals=false max-buffers=16 drop=false sync=false async=false
		appsrc name=spk is-live=true format=time do-timestamp=true ! queue !
		audio/x-raw,format=F32LE,rate=16000,channels=1,layout=interleaved !
		audioconvert ! audioresample ! volume volume=2.0 !
		webrtcechoprobe name=webrtcechoprobe0 ! pulsesink sync=true async=false`
	p, err := launch(desc)
	if err != nil {
		return nil, nil, nil, err
	}
	mic := &pullPipe{pipeline: p, sink: app.SinkFromElement(elementByName(p, "mic")),
		done: make(chan struct{}), muted: muted}
	src := app.SrcFromElement(elementByName(p, "spk"))
	src.SetCaps(gst.NewCapsFromString(`audio/x-raw,format=F32LE,rate=16000,channels=1,layout=interleaved`))
	spk := &pushPipe{pipeline: p, src: src, done: make(chan struct{}), audioF32: true}
	go watchBus(p, "audio")
	if err := p.SetState(gst.StatePlaying); err != nil {
		p.SetState(gst.StateNull) // don't drop a READY pipeline — GLib teardown crash
		return nil, nil, nil, err
	}
	go mic.pullBytes(pw)
	return mic, spk, pr, nil
}

// watchBus logs pipeline errors/EOS so silent failures are visible.
func watchBus(p *gst.Pipeline, tag string) {
	<-watchBusErr(p, tag)
}

// watchBusErr logs bus messages and returns the first error (or nil on EOS).
func watchBusErr(p *gst.Pipeline, tag string) <-chan error {
	out := make(chan error, 1)
	go func() {
		bus := p.GetBus()
		for {
			msg := bus.TimedPop(gst.ClockTimeNone)
			if msg == nil {
				out <- nil
				return
			}
			switch msg.Type() {
			case gst.MessageError:
				gerr := msg.ParseError()
				log.Printf("call: %s pipeline error (from %s): %v", tag, msg.Source(), gerr)
				out <- gerr
				return
			case gst.MessageEOS:
				log.Printf("call: %s pipeline reached EOS", tag)
				out <- nil
				return
			case gst.MessageWarning:
				log.Printf("call: %s pipeline warning (from %s): %v", tag, msg.Source(), msg.ParseWarning())
			}
		}
	}()
	return out
}

// pullSample wraps PullSample so a nil while the sink pad is still flushing
// (pre-PLAYING, or mid state transition) is retried instead of looking like
// EOS — appsink returns NULL for BOTH conditions. Real EOS is distinguished
// by IsEOS(); done ends the wait during teardown.
func (pp *pullPipe) pullSample() *gst.Sample {
	for {
		select {
		case <-pp.done:
			return nil
		default:
		}
		if s := pp.sink.PullSample(); s != nil || pp.sink.IsEOS() {
			return s
		}
		select {
		case <-pp.done:
			return nil
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// pullBytes moves sample payloads into w until stopped; muted samples are
// replaced with silence so the peer's stream stays continuous.
func (pp *pullPipe) pullBytes(w io.WriteCloser) {
	defer w.Close()
	for {
		sample := pp.pullSample()
		if sample == nil {
			return
		}
		buf := sample.GetBuffer()
		if buf == nil {
			continue
		}
		b := buf.Bytes()
		if pp.muted != nil && pp.muted.Load() {
			b = make([]byte, len(b))
		}
		if _, err := w.Write(b); err != nil {
			return
		}
	}
}

// Frame is one decoded RGBA video frame.
type Frame struct {
	Data []byte
	W, H int
}

// videoEncodeBranch returns the encoder element chain and output caps for the
// session's wire codec (WhatsApp calls carry H.264).
func videoEncodeBranch(codec string) (enc, caps string) {
	switch codec {
	case "vp8":
		return `vp8enc name=enc deadline=1 target-bitrate=700000 keyframe-max-dist=30`, `video/x-vp8`
	default: // h264
		return `x264enc name=enc tune=zerolatency speed-preset=ultrafast bitrate=700 key-int-max=30 !
			h264parse config-interval=1`, `video/x-h264,stream-format=byte-stream,alignment=au,profile=constrained-baseline`
	}
}

// videoDecodeBranch returns input caps and decoder chain for a wire codec.
func videoDecodeBranch(codec string) (caps, dec string) {
	switch codec {
	case "vp8":
		// libvpx postprocessing: deblock/demist peer-encoded frames —
		// heavily compressed senders show visible blocking on busy
		// scenes without it.
		return `video/x-vp8`, `vp8dec post-processing=true deblocking-level=8`
	default: // h264
		return `video/x-h264,stream-format=byte-stream,alignment=au`, `h264parse ! avdec_h264 max-threads=2`
	}
}

// startCameraPipe captures video, encodes wire frames and tees a raw RGBA
// preview. Each candidate is a complete source+decode prefix: decodebin
// autoplug covers most cases, but explicit jpegdec/raw paths exist for
// cameras decodebin can't handle.
func startCameraPipe(codec string) (cam *pullPipe, preview *pullPipe, aus <-chan []byte, err error) {
	sources := []string{
		`v4l2src device=/dev/video0 ! decodebin`, // primary webcam node
		`v4l2src ! image/jpeg ! jpegdec`,         // explicit MJPEG path
		`v4l2src ! decodebin`,                    // generic autoplug
		`v4l2src device=/dev/video2 ! decodebin`, // secondary/IR node
		`pipewiresrc ! decodebin`,                // portal; often no target
	}
	var lastErr error
	for _, src := range sources {
		// A fast EOS usually means the previous pipeline hasn't fully
		// released the device — retry the same source once after a beat.
		for attempt := 0; attempt < 2; attempt++ {
			if attempt > 0 {
				time.Sleep(300 * time.Millisecond)
			}
			if cam, preview, aus, err = tryCameraSource(src, codec); err == nil {
				return cam, preview, aus, nil
			}
			lastErr = err
		}
	}
	return nil, nil, nil, fmt.Errorf("%w: %v", ErrNoCamera, lastErr)
}

// tryCameraSource builds one candidate pipeline and proves it by waiting for
// the first encoded AU — a source that only *launches* produces nothing and
// must not win the fallback chain.
func tryCameraSource(src, codec string) (*pullPipe, *pullPipe, <-chan []byte, error) {
	// Probe the sensor's real size first: the encode caps preserve that
	// aspect so no letterbox bars get baked into the stream.
	sw, sh, err := probeCameraSize(src)
	if err != nil {
		log.Printf("call: camera source [%s] probe failed: %v", src, err)
		return nil, nil, nil, err
	}
	// The probe opened and released the device — give it a beat before the
	// real pipeline re-acquires it.
	time.Sleep(300 * time.Millisecond)
	tw, th := scaledSize(sw, sh, 640, 480)
	cam, preview, aus, busErr, err := tryCameraPipe(src, codec, tw, th)
	if err != nil {
		log.Printf("call: camera source [%s] failed to launch: %v", src, err)
		return nil, nil, nil, err
	}
	select {
	case first, ok := <-aus:
		if !ok {
			log.Printf("call: camera source [%s] EOS before first frame", src)
			cam.Stop()
			return nil, nil, nil, errors.New("appsink EOS before first frame")
		}
		out := make(chan []byte, 8)
		go func() {
			out <- first
			for a := range aus {
				out <- a
			}
			close(out)
		}()
		go func() { <-busErr }() // keep draining bus to log
		return cam, preview, out, nil
	case berr := <-busErr:
		log.Printf("call: camera source [%s] bus error: %v", src, berr)
		cam.Stop()
		return nil, nil, nil, berr
	case <-time.After(5 * time.Second):
		log.Printf("call: camera source [%s] timed out with no frames", src)
		cam.Stop()
		return nil, nil, nil, errors.New("no frames within 5s")
	}
}

// probeCameraSize launches the source headlessly and returns the frame size
// it negotiates, so encode caps can preserve the sensor's native aspect.
func probeCameraSize(src string) (w, h int, err error) {
	p, err := launch(src + ` ! fakesink name=probe sync=false async=false`)
	if err != nil {
		return 0, 0, err
	}
	defer p.SetState(gst.StateNull)
	busErr := watchBusErr(p, "probe")
	// PLAYING, not just PAUSED: decodebin/jpegdec in the source chain only
	// produce real caps once data actually flows.
	if err := p.SetState(gst.StatePlaying); err != nil {
		return 0, 0, err
	}
	pad := elementByName(p, "probe").GetStaticPad("sink")
	deadline := time.Now().Add(3 * time.Second)
	for {
		select {
		case berr := <-busErr:
			if berr == nil {
				berr = errors.New("EOS before caps negotiation")
			}
			return 0, 0, berr
		default:
		}
		if caps := pad.GetCurrentCaps(); caps != nil && caps.GetSize() > 0 {
			if w, h = structSize(caps.GetStructureAt(0)); w > 0 && h > 0 {
				return w, h, nil
			}
			return 0, 0, errors.New("caps without fixed dimensions")
		}
		if time.Now().After(deadline) {
			return 0, 0, errors.New("timed out waiting for caps")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// scaledSize preserves the sensor's aspect within a maxW×maxH box, rounding
// down to even dims — I420 (and x264) can't carry odd frame sizes.
func scaledSize(sw, sh, maxW, maxH int) (w, h int) {
	w, h = maxW, maxW*sh/sw
	if h > maxH {
		h = maxH
		w = sw * h / sh
	}
	if w < 16 || h < 16 {
		return 640, 480
	}
	return w &^ 1, h &^ 1
}

func tryCameraPipe(src, codec string, w, h int) (*pullPipe, *pullPipe, <-chan []byte, <-chan error, error) {
	// videorate is required before the caps filter: the camera rarely
	// offers exactly 15fps — without it negotiation fails outright.
	// The dims come from the source's real aspect (probeCameraSize), so the
	// stream carries no baked-in letterbox bars.
	enc, outCaps := videoEncodeBranch(codec)
	desc := fmt.Sprintf(src+` ! videoconvert ! videoscale ! videorate !
		video/x-raw,format=I420,width=%d,height=%d,framerate=15/1 !
		tee name=t
		t. ! queue ! `+enc+` ! `+outCaps+` !
		   appsink name=vsink emit-signals=false max-buffers=4 drop=true sync=false
		t. ! queue ! videoconvert ! video/x-raw,format=RGBA !
		   appsink name=prev emit-signals=false max-buffers=2 drop=true sync=false`, w, h)
	p, err := launch(desc)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	busErr := watchBusErr(p, "camera")
	cam := &pullPipe{pipeline: p, sink: app.SinkFromElement(elementByName(p, "vsink")),
		done: make(chan struct{}), enc: elementByName(p, "enc")}
	prev := &pullPipe{pipeline: p, sink: app.SinkFromElement(elementByName(p, "prev")),
		done: make(chan struct{})}

	if err := p.SetState(gst.StatePlaying); err != nil {
		p.SetState(gst.StateNull) // dispose cleanly — avoids READY-state teardown
		return nil, nil, nil, nil, err
	}
	auCh := make(chan []byte, 8)
	go func() {
		defer close(auCh)
		for {
			sample := cam.pullSample()
			if sample == nil {
				return
			}
			buf := sample.GetBuffer()
			if buf == nil {
				continue
			}
			b := append([]byte(nil), buf.Bytes()...)
			select {
			case auCh <- b:
			case <-cam.done:
				return
			default:
				// encoder faster than network — drop
			}
		}
	}()
	_ = prev // preview consumed via pullFrames by the session if needed
	return cam, prev, auCh, busErr, nil
}

// pullFrames yields decoded RGBA frames from a preview-style appsink.
func (pp *pullPipe) pullFrames(out chan<- Frame) {
	defer close(out)
	for {
		sample := pp.pullSample()
		if sample == nil {
			return
		}
		buf := sample.GetBuffer()
		if buf == nil {
			continue
		}
		w, h := sampleSize(sample)
		f := Frame{Data: append([]byte(nil), buf.Bytes()...), W: w, H: h}
		select {
		case out <- f:
		case <-pp.done:
			return
		}
	}
}

func sampleSize(s *gst.Sample) (w, h int) {
	caps := s.GetCaps()
	if caps == nil || caps.GetSize() == 0 {
		return 0, 0
	}
	return structSize(caps.GetStructureAt(0))
}

// structSize pulls width/height out of a caps structure (0 when unfixed).
func structSize(st *gst.Structure) (w, h int) {
	if st == nil {
		return 0, 0
	}
	if v, err := st.GetValue("width"); err == nil {
		if i, ok := v.(int); ok {
			w = i
		}
	}
	if v, err := st.GetValue("height"); err == nil {
		if i, ok := v.(int); ok {
			h = i
		}
	}
	return w, h
}

// startVideoDecPipe receives encoded frames in the wire codec and yields
// RGBA frames — Annex-B H.264 on WhatsApp calls.
func startVideoDecPipe(codec string) (*pushPipe, <-chan Frame, error) {
	inCaps, dec := videoDecodeBranch(codec)
	desc := `appsrc name=vsrc is-live=true format=time do-timestamp=true !
		` + inCaps + ` !
		` + dec + ` ! videoconvert !
		video/x-raw,format=RGBA !
		appsink name=rgba emit-signals=false max-buffers=2 drop=true sync=false`
	p, err := launch(desc)
	if err != nil {
		return nil, nil, err
	}
	src := app.SrcFromElement(elementByName(p, "vsrc"))
	src.SetCaps(gst.NewCapsFromString(inCaps))
	out := &pushPipe{pipeline: p, src: src, done: make(chan struct{})}
	sink := &pullPipe{pipeline: p, sink: app.SinkFromElement(elementByName(p, "rgba")), done: out.done}
	go watchBus(p, "viddec")
	frames := make(chan Frame, 4)
	if err := p.SetState(gst.StatePlaying); err != nil {
		p.SetState(gst.StateNull)
		return nil, nil, err
	}
	go sink.pullFrames(frames)
	return out, frames, nil
}

// DownscaleRGBA returns a nearest-neighbor scaled copy of an RGBA frame.
func DownscaleRGBA(src []byte, sw, sh, dw, dh int) []byte {
	dst := make([]byte, dw*dh*4)
	for y := 0; y < dh; y++ {
		sy := y * sh / dh
		for x := 0; x < dw; x++ {
			sx := x * sw / dw
			copy(dst[(y*dw+x)*4:], src[(sy*sw+sx)*4:(sy*sw+sx)*4+4])
		}
	}
	return dst
}

// FitInside scales sw×sh down to fit within maxW×maxH, preserving aspect.
func FitInside(sw, sh, maxW, maxH int) (dw, dh int) {
	dw, dh = sw, sh
	if dw > maxW {
		dh = dh * maxW / dw
		dw = maxW
	}
	if dh > maxH {
		dw = dw * maxH / dh
		dh = maxH
	}
	return
}

// RotateRGBA rotates an RGBA frame by quarter turns clockwise (0-3) and
// returns the new buffer plus its (possibly swapped) dimensions.
func RotateRGBA(src []byte, w, h, turns int) ([]byte, int, int) {
	switch turns & 3 {
	case 1: // 90° CW: src(x,y) → dst(H-1-y, x); new dims H×W
		dst := make([]byte, len(src))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				si, di := (y*w+x)*4, (x*h+(h-1-y))*4
				copy(dst[di:di+4], src[si:si+4])
			}
		}
		return dst, h, w
	case 2: // 180°
		dst := make([]byte, len(src))
		for i := 0; i < w*h; i++ {
			copy(dst[(w*h-1-i)*4:(w*h-i)*4], src[i*4:i*4+4])
		}
		return dst, w, h
	case 3: // 270° CW: src(x,y) → dst(y, W-1-x); new dims H×W
		dst := make([]byte, len(src))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				si, di := (y*w+x)*4, ((w-1-x)*h+y)*4
				copy(dst[di:di+4], src[si:si+4])
			}
		}
		return dst, h, w
	}
	return src, w, h
}

// PushBytes feeds an encoded access unit / encoded audio into the pipe.
func (pp *pushPipe) PushBytes(b []byte) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	if pp.src == nil {
		return
	}
	buf := gst.NewBufferFromBytes(b)
	pp.src.PushBuffer(buf)
}

// PushAudioF32 encodes an f32 frame as little-endian float32 and pushes it.
func (pp *pushPipe) PushAudioF32(frame []float32) {
	b := make([]byte, len(frame)*4)
	for i, v := range frame {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}
	pp.PushBytes(b)
}

// ForceKeyUnit asks the encoder for an IDR next frame.
func (pp *pullPipe) ForceKeyUnit() {
	if pp.enc == nil {
		return
	}
	// Custom-upstream events travel upstream — send on the src pad so it
	// propagates toward the encoder's sink (a sink-pad send goes the wrong
	// way and GStreamer drops it with a warning).
	pad := pp.enc.GetStaticPad("src")
	if pad == nil {
		return
	}
	st := gst.NewStructure("GstForceKeyUnit")
	_ = st.SetValue("all-headers", true)
	_ = st.SetValue("count", uint32(0))
	ev := gst.NewCustomEvent(gst.EventTypeCustomUpstream, st)
	pad.SendEvent(ev)
}

// Stop tears the pipeline down (safe to call twice).
func (pp *pullPipe) Stop() {
	pp.once.Do(func() {
		close(pp.done)
		pp.pipeline.SetState(gst.StateNull)
	})
}

// Stop tears the pipeline down (safe to call twice).
func (pp *pushPipe) Stop() {
	pp.once.Do(func() {
		close(pp.done)
		pp.pipeline.SetState(gst.StateNull)
	})
}

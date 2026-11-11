package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-gst/go-gst/gst"
	"github.com/go-gst/go-gst/gst/app"
)

// Recorder captures raw 16 kHz mono PCM for a voice note, then encodes
// it to ogg/opus and derives a waveform on Stop.
type Recorder struct {
	pipeline *gst.Pipeline
	sink     *app.Sink
	done     chan struct{}
	stopped  sync.Once
	vn       *VoiceNote // Stop result — computed once, returned on every call
	err      error

	mu    sync.Mutex
	pcm   bytes.Buffer
	level float64 // last RMS 0..1 for the level meter

	OnLevel func(float64) // optional, called from the pull goroutine
}

// StartRecorder begins capturing from the default input.
func StartRecorder() (*Recorder, error) {
	gst.Init(nil)
	desc := `pulsesrc ! audioconvert ! audioresample !
		audio/x-raw,format=S16LE,rate=16000,channels=1,layout=interleaved !
		appsink name=rec emit-signals=false max-buffers=64 sync=false`
	p, err := gst.NewPipelineFromString(desc)
	if err != nil {
		return nil, err
	}
	elem, _ := p.GetElementByName("rec")
	r := &Recorder{pipeline: p, sink: app.SinkFromElement(elem), done: make(chan struct{})}
	if err := p.SetState(gst.StatePlaying); err != nil {
		return nil, err
	}
	go r.pull()
	return r, nil
}

func (r *Recorder) pull() {
	for {
		sample := r.sink.PullSample()
		if sample == nil {
			// nil before EOS means the sink is still transitioning — transient,
			// not terminal. Same race class as the call pipelines.
			select {
			case <-r.done:
				return
			default:
			}
			if r.sink.IsEOS() {
				return
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		select {
		case <-r.done:
			return
		default:
		}
		buf := sample.GetBuffer()
		if buf == nil {
			continue
		}
		b := buf.Bytes()
		r.mu.Lock()
		r.pcm.Write(b)
		r.level = rms(b)
		lvl := r.level
		r.mu.Unlock()
		if r.OnLevel != nil {
			r.OnLevel(lvl)
		}
	}
}

func rms(b []byte) float64 {
	var sum float64
	n := len(b) / 2
	if n == 0 {
		return 0
	}
	for i := 0; i+1 < len(b); i += 2 {
		v := float64(int16(binary.LittleEndian.Uint16(b[i:]))) / 32768
		sum += v * v
	}
	if sum == 0 {
		return 0
	}
	return math.Sqrt(sum / float64(n))
}

// Level returns the latest RMS level.
func (r *Recorder) Level() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.level
}

// VoiceNote is the finished recording.
type VoiceNote struct {
	Ogg      []byte
	Waveform []byte
	Seconds  int
}

// Stop finishes capture and encodes the result. The whole stop+encode
// runs once — a second call (cancel racing send, a stray double tap)
// returns the same result instead of re-encoding a dead pipeline's tail.
func (r *Recorder) Stop() (*VoiceNote, error) {
	r.stopped.Do(func() {
		close(r.done)
		r.pipeline.SetState(gst.StateNull)
		r.mu.Lock()
		pcm := append([]byte(nil), r.pcm.Bytes()...)
		r.mu.Unlock()
		if len(pcm) == 0 {
			r.err = fmt.Errorf("empty recording")
			return
		}
		ogg, err := encodeOpus(pcm)
		if err != nil {
			r.err = err
		}
		r.vn = &VoiceNote{
			Ogg:      ogg,
			Waveform: waveform(pcm),
			Seconds:  len(pcm) / 2 / 16000,
		}
	})
	return r.vn, r.err
}

// encodeOpus runs an offline appsrc→opusenc→oggmux→appsink encode.
func encodeOpus(pcm []byte) ([]byte, error) {
	// layout=interleaved is required: opusenc only accepts interleaved, and a
	// capsfilter that leaves layout unfixed errors the stream on first buffer.
	desc := `appsrc name=in is-live=false format=time !
		audio/x-raw,format=S16LE,rate=16000,channels=1,layout=interleaved !
		audioconvert ! opusenc bitrate=24000 ! oggmux !
		appsink name=out sync=false`
	p, err := gst.NewPipelineFromString(desc)
	if err != nil {
		return nil, err
	}
	srcElem, _ := p.GetElementByName("in")
	src := app.SrcFromElement(srcElem)
	src.SetCaps(gst.NewCapsFromString(`audio/x-raw,format=S16LE,rate=16000,channels=1,layout=interleaved`))
	sinkElem, _ := p.GetElementByName("out")
	sink := app.SinkFromElement(sinkElem)

	if err := p.SetState(gst.StatePlaying); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			s := sink.PullSample()
			if s == nil {
				if sink.IsEOS() {
					return
				}
				time.Sleep(5 * time.Millisecond)
				continue
			}
			if b := s.GetBuffer(); b != nil {
				out.Write(b.Bytes())
			}
		}
	}()
	// Feed in 64 KiB chunks with real timestamps (32000 bytes/s at 16 kHz S16LE)
	// so oggmux can compute granule positions and the file carries a duration.
	const bytesPerSec = 32000
	for i := 0; i < len(pcm); i += 65536 {
		end := i + 65536
		if end > len(pcm) {
			end = len(pcm)
		}
		buf := gst.NewBufferFromBytes(pcm[i:end])
		buf.SetPresentationTimestamp(gst.ClockTime(uint64(i) * uint64(time.Second) / bytesPerSec))
		buf.SetDuration(gst.ClockTime(uint64(end-i) * uint64(time.Second) / bytesPerSec))
		if ret := src.PushBuffer(buf); ret != gst.FlowOK {
			p.SetState(gst.StateNull)
			return nil, fmt.Errorf("appsrc push: %v", ret)
		}
	}
	src.EndStream()
	<-done
	p.SetState(gst.StateNull)
	if out.Len() == 0 {
		return nil, fmt.Errorf("opus encode produced no data")
	}
	return out.Bytes(), nil
}

// waveform folds PCM peaks into ≤64 amplitude buckets (WhatsApp style).
func waveform(pcm []byte) []byte {
	const buckets = 64
	if len(pcm) < 2 {
		return nil
	}
	samples := len(pcm) / 2
	step := samples / buckets
	if step == 0 {
		step = 1
	}
	wf := make([]byte, 0, buckets)
	for i := 0; i < samples && len(wf) < buckets; i += step {
		var peak int32 // int32: |min int16| = 32768 overflows int16
		for j := i; j < i+step && j < samples; j++ {
			v := int32(int16(binary.LittleEndian.Uint16(pcm[j*2:])))
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		if b := peak >> 7; b > 255 {
			wf = append(wf, 255)
		} else {
			wf = append(wf, byte(b))
		}
	}
	return wf
}

// SecondsOf reports rough duration for a raw PCM blob.
func SecondsOf(pcm []byte) int { return len(pcm) / 2 / 16000 }

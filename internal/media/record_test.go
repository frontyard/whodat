package media

import (
	"bytes"
	"testing"

	"github.com/go-gst/go-gst/gst"
)

func TestEncodeOpus(t *testing.T) {
	gst.Init(nil)
	// 2 seconds of silence: S16LE 16kHz mono = 64000 bytes.
	pcm := make([]byte, 64000)
	out, err := encodeOpus(pcm)
	if err != nil {
		t.Fatalf("encodeOpus: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("OggS")) {
		t.Fatalf("output is not Ogg (first bytes: %x)", out[:min(8, len(out))])
	}
	t.Logf("encoded %d bytes of ogg/opus", len(out))
}

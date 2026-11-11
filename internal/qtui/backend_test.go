package qtui

import (
	"testing"

	"github.com/frontyard/whodat/internal/protocol"
	_ "github.com/frontyard/whodat/internal/wa"
)

func TestListBackends(t *testing.T) {
	for _, b := range protocol.Backends() {
		t.Logf("proto=%s name=%s multi=%v", b.Proto, b.Name, b.Multi)
	}
	if len(protocol.Backends()) < 1 {
		t.Fatal("expected >= 1 backend")
	}
}

package qtui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frontyard/whodat/internal/core"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// TestMsgOpsMissingMessage: a stale msgId (row absent — GetMessage returns
// (nil, nil)) must surface as a normal error from every message-action op,
// not a nil dereference or a nil *store.Message handed to the backend.
func TestMsgOpsMissingMessage(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := &Host{app: core.New(map[string]protocol.Service{}, nil, nil, db, nil, nil)}

	ops := map[string]struct {
		fn   opFunc
		args string
	}{
		"sendReaction": {h.opSendReaction, `{"jid":"j","msgId":"gone","emoji":"+"}`},
		"sendStar":     {h.opSendStar, `{"jid":"j","msgId":"gone","on":true}`},
		"sendEdit":     {h.opSendEdit, `{"jid":"j","msgId":"gone","text":"x"}`},
		"sendRevoke":   {h.opSendRevoke, `{"jid":"j","msgId":"gone"}`},
		"forward":      {h.opForward, `{"fromJid":"j","msgId":"gone","toJid":"k"}`},
		"votePoll":     {h.opVotePoll, `{"jid":"j","msgId":"gone","option":"o"}`},
	}
	for name, tc := range ops {
		if _, err := tc.fn(h, json.RawMessage(tc.args)); err == nil ||
			!strings.Contains(err.Error(), "not found") {
			t.Fatalf("%s: want a not-found error, got %v", name, err)
		}
	}
}

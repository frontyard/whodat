package wa

import (
	"context"
	"errors"
	"slices"
	"testing"

	whatsmeow "github.com/polymorfa/hypermeow"
	wastore "github.com/polymorfa/hypermeow/store"
	"github.com/polymorfa/hypermeow/types"

	"github.com/frontyard/whodat/internal/protocol"
)

// fakeLIDs is a LIDStore over lid-user → pn-user pairs.
type fakeLIDs map[string]string

func (f fakeLIDs) PutManyLIDMappings(context.Context, []wastore.LIDMapping) error {
	return nil
}
func (f fakeLIDs) PutLIDMapping(context.Context, types.JID, types.JID) error { return nil }
func (f fakeLIDs) GetPNForLID(_ context.Context, lid types.JID) (types.JID, error) {
	if pn, ok := f[lid.User]; ok {
		return types.NewJID(pn, types.DefaultUserServer), nil
	}
	return types.JID{}, errors.New("unknown lid")
}
func (f fakeLIDs) GetLIDForPN(_ context.Context, pn types.JID) (types.JID, error) {
	for l, p := range f {
		if p == pn.User {
			return types.NewJID(l, types.HiddenUserServer), nil
		}
	}
	return types.JID{}, errors.New("unknown pn")
}
func (f fakeLIDs) GetManyLIDsForPNs(_ context.Context, pns []types.JID) (map[types.JID]types.JID, error) {
	out := map[types.JID]types.JID{}
	for _, p := range pns {
		if l, err := f.GetLIDForPN(context.Background(), p); err == nil {
			out[p] = l
		}
	}
	return out, nil
}

// The "@Unknown" regression: a picked LID member used to also emit
// "<lid digits>@s.whatsapp.net" — a phantom PN claiming the same span.
func TestApplyMentionsLID(t *testing.T) {
	svc := &Service{acct: "wa"}
	svc.client.Store(&whatsmeow.Client{Store: &wastore.Device{
		LIDs: fakeLIDs{"22664592761007": "447491798746"},
	}})

	text, jids := svc.applyMentions(context.Background(),
		"hey @Selma can you view the houses?",
		[]protocol.Mention{{Token: "@Selma", ID: "wa:22664592761007@lid"}})

	if text != "hey @22664592761007 can you view the houses?" {
		t.Fatalf("text: %q", text)
	}
	if slices.Contains(jids, "22664592761007@s.whatsapp.net") {
		t.Fatalf("phantom PN minted for LID digits: %v", jids)
	}
	for _, want := range []string{"22664592761007@lid", "447491798746@s.whatsapp.net"} {
		if !slices.Contains(jids, want) {
			t.Fatalf("jids %v missing %s", jids, want)
		}
	}
}

func TestApplyMentionsPN(t *testing.T) {
	svc := &Service{acct: "wa"}
	svc.client.Store(&whatsmeow.Client{Store: &wastore.Device{
		LIDs: fakeLIDs{"22664592761007": "447491798746"},
	}})

	text, jids := svc.applyMentions(context.Background(),
		"hey @Selma ping",
		[]protocol.Mention{{Token: "@Selma", ID: "wa:447491798746@s.whatsapp.net"}})

	if text != "hey @447491798746 ping" {
		t.Fatalf("text: %q", text)
	}
	for _, want := range []string{"447491798746@s.whatsapp.net", "22664592761007@lid"} {
		if !slices.Contains(jids, want) {
			t.Fatalf("jids %v missing %s", jids, want)
		}
	}
}

// A hand-typed "@<lid digits>" must not mint the same digits as a PN.
func TestApplyMentionsTypedLIDDigits(t *testing.T) {
	svc := &Service{acct: "wa"}
	svc.client.Store(&whatsmeow.Client{Store: &wastore.Device{
		LIDs: fakeLIDs{"22664592761007": "447491798746"},
	}})

	text, jids := svc.applyMentions(context.Background(),
		"ping @22664592761007", nil)

	if text != "ping @22664592761007" {
		t.Fatalf("text: %q", text)
	}
	if slices.Contains(jids, "22664592761007@s.whatsapp.net") {
		t.Fatalf("phantom PN minted for typed LID digits: %v", jids)
	}
	if !slices.Contains(jids, "22664592761007@lid") {
		t.Fatalf("jids %v missing lid", jids)
	}
}

// A hand-typed "@<pn>" keeps the PN and gains its LID counterpart.
func TestApplyMentionsTypedPNDigits(t *testing.T) {
	svc := &Service{acct: "wa"}
	svc.client.Store(&whatsmeow.Client{Store: &wastore.Device{
		LIDs: fakeLIDs{"22664592761007": "447491798746"},
	}})

	_, jids := svc.applyMentions(context.Background(), "ping @447491798746", nil)
	for _, want := range []string{"447491798746@s.whatsapp.net", "22664592761007@lid"} {
		if !slices.Contains(jids, want) {
			t.Fatalf("jids %v missing %s", jids, want)
		}
	}
}

// Without a live client there is no LID map — must not panic, and a
// picked LID member still emits only its own JID.
func TestApplyMentionsNoClient(t *testing.T) {
	svc := &Service{acct: "wa"}
	text, jids := svc.applyMentions(context.Background(),
		"hey @Selma", []protocol.Mention{{Token: "@Selma", ID: "wa:22664592761007@lid"}})
	if text != "hey @22664592761007" {
		t.Fatalf("text: %q", text)
	}
	if len(jids) != 1 || jids[0] != "22664592761007@lid" {
		t.Fatalf("jids: %v", jids)
	}
}

package wa

import (
	"os"
	"testing"
	"time"

	"github.com/polymorfa/hypermeow/proto/waCommon"
	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/proto/waWeb"
	"google.golang.org/protobuf/proto"

	"github.com/frontyard/whodat/internal/store"
)

// tsvc drives the convert/seed helpers — the account prefix is what
// namespaces every stored id.
var tsvc = &Service{acct: "wa"}

func TestConvertText(t *testing.T) {
	wm := &waE2E.Message{Conversation: proto.String("hi")}
	m, act := tsvc.convert("c@s.whatsapp.net", "id1", "s@s.whatsapp.net", "Sam", false, time.Now(), wm)
	if act.kind != actInsert || m.Type != store.TText || m.Text != "hi" {
		t.Fatalf("convert: %+v %+v", m, act)
	}
	if m.Proto == nil {
		t.Fatal("proto payload not retained")
	}
}

func TestConvertRevoke(t *testing.T) {
	wm := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(),
		Key:  &waCommon.MessageKey{ID: proto.String("target")},
	}}
	m, act := tsvc.convert("c", "id2", "s", "", false, time.Now(), wm)
	if m != nil || act.kind != actRevoke || act.targetID != "target" {
		t.Fatalf("revoke: %+v %+v", m, act)
	}
}

func TestConvertEdit(t *testing.T) {
	wm := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
		Key:  &waCommon.MessageKey{ID: proto.String("target")},
		EditedMessage: &waE2E.Message{
			Conversation: proto.String("new body"),
		},
	}}
	_, act := tsvc.convert("c", "id3", "s", "", false, time.Now(), wm)
	if act.kind != actEdit || act.targetID != "target" || act.newText != "new body" {
		t.Fatalf("edit: %+v", act)
	}
}

func TestConvertPoll(t *testing.T) {
	wm := &waE2E.Message{PollCreationMessage: &waE2E.PollCreationMessage{
		Name: proto.String("Lunch?"),
		Options: []*waE2E.PollCreationMessage_Option{
			{OptionName: proto.String("Pizza")},
			{OptionName: proto.String("Sushi")},
		},
		SelectableOptionsCount: proto.Uint32(1),
	}}
	m, act := tsvc.convert("c", "p1", "s", "", false, time.Now(), wm)
	if act.kind != actInsert || m.Type != store.TPoll || m.Text != "Lunch?" {
		t.Fatalf("poll: %+v %+v", m, act)
	}
	if len(m.PollOptions) != 2 || m.PollOptions[0] != "Pizza" || m.PollMax != 1 {
		t.Fatalf("poll opts: %+v", m)
	}
}

func TestConvertPollVote(t *testing.T) {
	wm := &waE2E.Message{PollUpdateMessage: &waE2E.PollUpdateMessage{
		PollCreationMessageKey: &waCommon.MessageKey{ID: proto.String("poll1")},
	}}
	m, act := tsvc.convert("c", "v1", "voter@s.whatsapp.net", "", false, time.Now(), wm)
	if m != nil || act.kind != actPollVote || act.targetID != "poll1" || act.senderJID != "voter@s.whatsapp.net" {
		t.Fatalf("vote: %+v %+v", m, act)
	}
}

func TestConvertViewOnce(t *testing.T) {
	inner := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Mimetype: proto.String("image/jpeg"),
	}}
	wm := &waE2E.Message{ViewOnceMessage: &waE2E.FutureProofMessage{Message: inner}}
	m, _ := tsvc.convert("c", "vo1", "s", "", false, time.Now(), wm)
	if m == nil || !m.ViewOnce || m.Type != store.TImage {
		t.Fatalf("view-once: %+v", m)
	}
	// V2 and extension unwrap too.
	wm = &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: inner}}
	m, _ = tsvc.convert("c", "vo2", "s", "", false, time.Now(), wm)
	if m == nil || !m.ViewOnce || m.Type != store.TImage {
		t.Fatalf("view-once v2: %+v", m)
	}
}

func TestConvertEphemeral(t *testing.T) {
	inner := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String("secret"),
		ContextInfo: &waE2E.ContextInfo{
			Expiration: proto.Uint32(86400),
		},
	}}
	ts := time.Now()
	m, _ := tsvc.convert("c", "e1", "s", "", false, ts, &waE2E.Message{
		EphemeralMessage: &waE2E.FutureProofMessage{Message: inner},
	})
	if m == nil || m.ExpiresAt.IsZero() {
		t.Fatalf("ephemeral: %+v", m)
	}
	if d := m.ExpiresAt.Sub(ts); d != 24*time.Hour {
		t.Fatalf("expiry delta: %v", d)
	}
}

func TestConvertDisappSetting(t *testing.T) {
	wm := &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type:                waE2E.ProtocolMessage_EPHEMERAL_SETTING.Enum(),
		EphemeralExpiration: proto.Uint32(604800),
	}}
	m, act := tsvc.convert("c", "d1", "s", "", false, time.Now(), wm)
	if m != nil || act.kind != actDisapp || act.disappSec != 604800 {
		t.Fatalf("disapp: %+v %+v", m, act)
	}
}

func TestConvertReaction(t *testing.T) {
	wm := &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
		Key:  &waCommon.MessageKey{ID: proto.String("target")},
		Text: proto.String("👍"),
	}}
	_, act := tsvc.convert("c", "id4", "sender@lid", "", false, time.Now(), wm)
	if act.kind != actReact || act.targetID != "target" || act.emoji != "👍" || act.senderJID != "sender@lid" {
		t.Fatalf("react: %+v", act)
	}
}

func TestConvertMediaType(t *testing.T) {
	wm := &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
		PTT:     proto.Bool(true),
		Seconds: proto.Uint32(7),
	}}
	m, act := tsvc.convert("c", "id5", "s", "", false, time.Now(), wm)
	if act.kind != actInsert || m.Type != store.TPTT || m.Duration != 7 {
		t.Fatalf("ptt: %+v", m)
	}
}

func TestCallStubText(t *testing.T) {
	mk := func(outcome waE2E.CallLogMessage_CallOutcome, video bool, dur int64) *waE2E.CallLogMessage {
		return &waE2E.CallLogMessage{
			CallOutcome:  outcome.Enum(),
			IsVideo:      proto.Bool(video),
			DurationSecs: proto.Int64(dur),
		}
	}
	cases := []struct {
		cl       *waE2E.CallLogMessage
		fromMe   bool
		wantText string
		wantMiss bool
	}{
		{mk(waE2E.CallLogMessage_MISSED, false, 0), false, "Missed voice call", true},
		{mk(waE2E.CallLogMessage_MISSED, true, 0), false, "Missed video call", true},
		{mk(waE2E.CallLogMessage_CONNECTED, false, 75), false, "Voice call · 1:15", false},
		{mk(waE2E.CallLogMessage_CONNECTED, true, 60), true, "Video call · 1:00", false},
		{mk(waE2E.CallLogMessage_MISSED, false, 0), true, "No answer", false},
		{mk(waE2E.CallLogMessage_REJECTED, false, 0), false, "Declined voice call", false},
	}
	for _, tc := range cases {
		got, miss := callStubText(tc.cl, tc.fromMe)
		if got != tc.wantText || miss != tc.wantMiss {
			t.Fatalf("%v: got %q,%v want %q,%v", tc.cl.GetCallOutcome(), got, miss, tc.wantText, tc.wantMiss)
		}
	}
}

func TestIsCallLog(t *testing.T) {
	if IsCallLog(nil) || IsCallLog(&waE2E.Message{}) {
		t.Fatal("false positive")
	}
	wm := &waE2E.Message{CallLogMesssage: &waE2E.CallLogMessage{}}
	if !IsCallLog(wm) {
		t.Fatal("missed call log")
	}
}

func TestConvertLinkPreview(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir()) // thumb staging stays in the sandbox
	wm := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:          proto.String("look https://example.com/a"),
		MatchedText:   proto.String("https://example.com/a"),
		Title:         proto.String("An Article"),
		Description:   proto.String("About things."),
		JPEGThumbnail: []byte{0xFF, 0xD8, 0xFF, 0x01},
	}}
	m, act := tsvc.convert("c@s.whatsapp.net", "lp1", "s", "", false, time.Now(), wm)
	if act.kind != actInsert || m.Preview == nil {
		t.Fatalf("preview missing: %+v", m)
	}
	if m.Preview.URL != "https://example.com/a" || m.Preview.Title != "An Article" ||
		m.Preview.Desc != "About things." {
		t.Fatalf("preview fields: %+v", m.Preview)
	}
	if m.Preview.Thumb == "" {
		t.Fatal("inline thumbnail not staged")
	}
	data, err := os.ReadFile(m.Preview.Thumb)
	if err != nil || len(data) != 4 {
		t.Fatalf("thumb file: %v %d", err, len(data))
	}
}

func TestConvertVideoThumb(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir()) // thumb staging stays in the sandbox
	wm := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
		Mimetype:      proto.String("video/mp4"),
		GifPlayback:   proto.Bool(true),
		Width:         proto.Uint32(480),
		Height:        proto.Uint32(360),
		JPEGThumbnail: []byte{0xFF, 0xD8, 0xFF, 0x03},
	}}
	m, act := tsvc.convert("c@s.whatsapp.net", "v1", "s", "", false, time.Now(), wm)
	if act.kind != actInsert || m.Type != store.TVideo {
		t.Fatalf("video convert: %+v", m)
	}
	if m.MediaThumb == "" {
		t.Fatal("inline video thumbnail not staged")
	}
	data, err := os.ReadFile(m.MediaThumb)
	if err != nil || len(data) != 4 {
		t.Fatalf("thumb file: %v %d", err, len(data))
	}
}

func TestConvertVideoNoThumb(t *testing.T) {
	wm := &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
		Mimetype: proto.String("video/mp4"),
	}}
	m, _ := tsvc.convert("c", "v2", "s", "", false, time.Now(), wm)
	if m.MediaThumb != "" {
		t.Fatalf("phantom thumb: %q", m.MediaThumb)
	}
}

func TestConvertLinkPreviewRequiresURL(t *testing.T) {
	// A bare ExtendedTextMessage (quote/mention carries no card).
	wm := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:  proto.String("plain reply"),
		Title: proto.String("stray title"),
	}}
	m, _ := tsvc.convert("c", "lp2", "s", "", false, time.Now(), wm)
	if m.Preview != nil {
		t.Fatalf("preview without URL: %+v", m.Preview)
	}
}

func TestPreview(t *testing.T) {
	m := &store.Message{Type: store.TPTT}
	if p := preview(m); p != "Voice message" {
		t.Fatalf("preview: %q", p)
	}
	m2 := &store.Message{Type: store.TText, Text: "hey"}
	if p := preview(m2); p != "hey" {
		t.Fatalf("preview self: %q", p)
	}
}

func TestSeedReactions(t *testing.T) {
	m := &store.Message{}
	tsvc.seedReactions(m, []*waWeb.Reaction{
		{ // group reaction — participant carries the reactor
			Key:  &waCommon.MessageKey{Participant: proto.String("g1@s.whatsapp.net")},
			Text: proto.String("👍"),
		},
		{ // own DM reaction — fromMe, no participant → own JID
			Key:  &waCommon.MessageKey{FromMe: proto.Bool(true)},
			Text: proto.String("❤️"),
		},
		{ // peer DM reaction — no participant → remoteJID
			Key:  &waCommon.MessageKey{RemoteJID: proto.String("peer@s.whatsapp.net")},
			Text: proto.String("👍"),
		},
		{ // removal entry — skipped
			Key:  &waCommon.MessageKey{Participant: proto.String("g2@s.whatsapp.net")},
			Text: proto.String(""),
		},
		{ // no identifiable reactor — skipped
			Key:  &waCommon.MessageKey{},
			Text: proto.String("🔥"),
		},
	}, "wa:me@s.whatsapp.net")

	want := map[string][]string{
		"👍":  {"wa:g1@s.whatsapp.net", "wa:peer@s.whatsapp.net"},
		"❤️": {"wa:me@s.whatsapp.net"},
	}
	if len(m.Reactions) != len(want) {
		t.Fatalf("reactions: %v", m.Reactions)
	}
	for e, rs := range want {
		got := m.Reactions[e]
		if len(got) != len(rs) {
			t.Fatalf("%s: %v want %v", e, got, rs)
		}
		for i, r := range rs {
			if got[i].String() != r {
				t.Fatalf("%s[%d]: %s want %s", e, i, got[i], r)
			}
		}
	}
}

func TestJidBoundary(t *testing.T) {
	for _, in := range []string{
		"wa:123@s.whatsapp.net", "123@s.whatsapp.net",
		"wa:1234:40@s.whatsapp.net", "1234:40@s.whatsapp.net",
		"wa:555@g.us", "wa:111@lid", "123",
	} {
		if _, err := tsvc.jid(store.PeerID(in)); err != nil {
			t.Fatalf("jid(%q): %v", in, err)
		}
	}
	// A foreign account's id must be refused, not parsed as a user part.
	for _, in := range []string{"wa2:123@s.whatsapp.net", "wa9:12345", "wa2:1@g.us"} {
		if _, err := tsvc.jid(store.PeerID(in)); err == nil {
			t.Fatalf("jid(%q) accepted a foreign account id", in)
		}
	}
}

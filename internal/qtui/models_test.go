package qtui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/frontyard/whodat/internal/store"
)

func TestSortChatsPinnedFirst(t *testing.T) {
	now := time.Now()
	cs := []*store.Chat{
		{JID: "a", RowID: 3, LastMsgTS: now.Add(-time.Hour)},
		{JID: "pin", Pinned: true, RowID: 1},
		{JID: "b", RowID: 2, LastMsgTS: now}, // newest activity wins within unpinned
	}
	sortChats(cs)
	if cs[0].JID != "pin" {
		t.Fatalf("pinned chat must sort first, got %q", cs[0].JID)
	}
	if cs[1].JID != "b" || cs[2].JID != "a" {
		t.Fatalf("unpinned must sort by last-message activity: %v %v", cs[1].JID, cs[2].JID)
	}
}

func TestFixDayLabelsBoundaries(t *testing.T) {
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	m := &MessageListModel{}
	// Newest-first model order.
	for _, ts := range []time.Time{now, now, yesterday, yesterday} {
		m.items = append(m.items, &msgRow{m: &store.Message{TS: ts}})
	}
	m.fixDayLabels()
	// In BottomToTop layout the label rides on the *oldest* message of each
	// day block — drawn above it — so index 1 (last today msg) and index 3
	// (oldest) carry labels; 0 and 2 do not.
	if m.items[0].day != "" || m.items[2].day != "" {
		t.Fatalf("mid-block rows must have no label: %q %q", m.items[0].day, m.items[2].day)
	}
	if m.items[1].day == "" || m.items[3].day == "" {
		t.Fatalf("boundary rows must carry labels")
	}
}

func TestMatchFilter(t *testing.T) {
	m := &ChatListModel{filter: "ali"}
	if !m.matchFilter(&store.Chat{Name: "Alice"}) {
		t.Fatal("name match failed")
	}
	if !m.matchFilter(&store.Chat{LastMsgPreview: "hi alina"}) {
		t.Fatal("preview match failed")
	}
	if m.matchFilter(&store.Chat{Name: "Bob"}) {
		t.Fatal("non-match passed filter")
	}
}

func TestReactionsJSON(t *testing.T) {
	msg := &store.Message{Reactions: map[string][]store.PeerID{
		"👍":  {"a", "b"},
		"❤️": {"c"},
	}}
	var out []struct {
		Emoji string `json:"emoji"`
		N     int    `json:"n"`
	}
	if err := json.Unmarshal([]byte(reactionsJSON(msg, nil)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 2 || out[0].N != 2 || out[0].Emoji != "👍" {
		t.Fatalf("reactions not sorted by count: %+v", out)
	}
	if reactionsJSON(&store.Message{}, nil) != "" {
		t.Fatal("empty reactions must marshal as empty string")
	}
}

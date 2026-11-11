package core

import (
	"github.com/frontyard/whodat/internal/store"
	"testing"
	"time"
)

func TestJIDUser(t *testing.T) {
	cases := map[string]string{
		"1234:40@s.whatsapp.net": "1234",
		"1234@s.whatsapp.net":    "1234",
		"999@lid":                "999",
		"plain":                  "plain",
	}
	for in, want := range cases {
		if got := JIDUser(store.PeerID(in)); got != want {
			t.Fatalf("JIDUser(%q) = %q want %q", in, got, want)
		}
	}
}

func TestPhoneOf(t *testing.T) {
	if p := PhoneOf("1555:4@s.whatsapp.net"); p != "+1555" {
		t.Fatalf("PhoneOf device: %q", p)
	}
	if p := PhoneOf("1555@s.whatsapp.net"); p != "+1555" {
		t.Fatalf("PhoneOf: %q", p)
	}
	if p := PhoneOf("group-1@g.us"); p != "+group-1" {
		t.Fatalf("PhoneOf group: %q", p)
	}
}

func TestFmtDay(t *testing.T) {
	if d := FmtDay(time.Now()); d != "Today" {
		t.Fatalf("today: %q", d)
	}
	if d := FmtDay(time.Now().AddDate(0, 0, -1)); d != "Yesterday" {
		t.Fatalf("yesterday: %q", d)
	}
	old := time.Now().AddDate(-1, 0, 0)
	if d := FmtDay(old); d == "Today" || d == "Yesterday" {
		t.Fatalf("old: %q", d)
	}
}

func TestFmtDur(t *testing.T) {
	if d := FmtDur(75); d != "1:15" {
		t.Fatalf("FmtDur: %q", d)
	}
	if d := FmtDur(0); d != "0:00" {
		t.Fatalf("FmtDur 0: %q", d)
	}
}

func TestLinkify(t *testing.T) {
	out := Linkify("see https://example.com/a?b=1, ok")
	want := `see <a href="https://example.com/a?b=1"><u>https://example.com/a?b=1</u></a>, ok`
	if out != want {
		t.Fatalf("Linkify:\n got %q\nwant %q", out, want)
	}
	// escaping still applies around links
	out = Linkify("<b> www.x.io")
	want = `&lt;b&gt; <a href="https://www.x.io"><u>www.x.io</u></a>`
	if out != want {
		t.Fatalf("Linkify esc:\n got %q\nwant %q", out, want)
	}
}

func TestTruncate(t *testing.T) {
	if s := Truncate("héllo wörld", 5); s != "héllo…" {
		t.Fatalf("Truncate: %q", s)
	}
	if s := Truncate("hi", 5); s != "hi" {
		t.Fatalf("Truncate short: %q", s)
	}
}

func TestSameDay(t *testing.T) {
	a := time.Date(2025, 3, 1, 23, 59, 0, 0, time.Local)
	b := time.Date(2025, 3, 1, 0, 1, 0, 0, time.Local)
	c := time.Date(2025, 3, 2, 0, 1, 0, 0, time.Local)
	if !SameDay(a, b) {
		t.Fatal("same day missed")
	}
	if SameDay(a, c) {
		t.Fatal("day boundary missed")
	}
}

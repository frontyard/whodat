package core

import "testing"

func TestMentionSpans(t *testing.T) {
	cases := []struct {
		text string
		want [][2]int
	}{
		{"hi @12345", [][2]int{{3, 9}}},
		{"@12345 and @67890", [][2]int{{0, 6}, {11, 17}}},
		{"mail user@12345 x", nil}, // address — not a mention
		{"a@b @1234", nil},         // too short + stray @
		{"(@12345)", [][2]int{{1, 7}}},
	}
	for _, c := range cases {
		got := mentionSpans(c.text)
		if len(got) != len(c.want) {
			t.Fatalf("mentionSpans(%q) = %v, want %v", c.text, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("mentionSpans(%q) = %v, want %v", c.text, got, c.want)
			}
		}
	}
}

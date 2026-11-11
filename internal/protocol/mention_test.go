package protocol

import "testing"

func TestTokenSpans(t *testing.T) {
	cases := []struct {
		text, tok string
		want      [][2]int
	}{
		{"hi @Alina and @Alina", "@Alina", [][2]int{{3, 9}, {14, 20}}},
		{"@Alina!", "@Alina", [][2]int{{0, 6}}}, // punctuation is a boundary
		{"@AlinaB", "@Alina", nil},              // longer word — no match
		{"x@Alina y", "@Alina", nil},            // address-like — no match
		{"@Alina B met @Alina", "@Alina B", [][2]int{{0, 8}}},
		{"héllo @José", "@José", [][2]int{{7, 13}}}, // UTF-8 names: byte spans
		{"@Alina", "", nil},
	}
	for _, c := range cases {
		got := TokenSpans(c.text, c.tok)
		if len(got) != len(c.want) {
			t.Fatalf("TokenSpans(%q,%q) = %v, want %v", c.text, c.tok, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("TokenSpans(%q,%q) = %v, want %v", c.text, c.tok, got, c.want)
			}
		}
	}
}

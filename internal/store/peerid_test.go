package store

import "testing"

// ParsePeerID accepts account-prefixed ids and rejects every bare form —
// that's what keeps a raw wire id from silently entering the app layer.
func TestParsePeerID(t *testing.T) {
	for _, in := range []string{
		"wa:123@s.whatsapp.net", "wa2:555@g.us", "wa17:1@lid",
		"wa3:12345", "wa3:-100123", "wa:1234:40@s.whatsapp.net",
	} {
		if _, err := ParsePeerID(in); err != nil {
			t.Fatalf("ParsePeerID(%q): %v", in, err)
		}
	}
	for _, in := range []string{
		"123@s.whatsapp.net",     // bare wire JID — no account prefix
		"1234:40@s.whatsapp.net", // digit-led prefix is a device part, not an account
		"x@s.whatsapp.net",       // no colon before '@'
		"", ":x", "wa-x:1",       // missing/bad prefix shapes
	} {
		if _, err := ParsePeerID(in); err == nil {
			t.Fatalf("ParsePeerID(%q) accepted a non-namespaced id", in)
		}
	}
}

func TestPeerIDParts(t *testing.T) {
	id := NewID("wa2", "123@s.whatsapp.net")
	if id.Acct() != "wa2" || id.Wire() != "123@s.whatsapp.net" {
		t.Fatalf("acct/wire: %q %q", id.Acct(), id.Wire())
	}
	if id.String() != "wa2:123@s.whatsapp.net" {
		t.Fatalf("string: %q", id)
	}
	if NewID("wa", "") != "" {
		t.Fatal("empty wire should yield zero id")
	}
}

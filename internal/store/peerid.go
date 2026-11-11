package store

import (
	"database/sql/driver"
	"fmt"
	"strings"
)

// PeerID is an account-namespaced peer id — "wa:123@s.whatsapp.net",
// "wa2:555@g.us". It is the ONLY id form the store, bus and
// Service boundary accept: a PeerID cannot exist without a valid account
// prefix, so a bare JID can never silently enter the app layer.
//
// Construct one via NewID (service-side, account already known) or
// ParsePeerID (untrusted input — UI args, persisted strings). A direct
// PeerID("...") conversion defeats the guarantee; don't do it.
type PeerID string

// NewID namespaces a backend wire id under acct: NewID("wa", "123@…")
// → "wa:123@…". The acct part comes from NextAcct so it is always
// account-shaped by construction.
func NewID(acct, wire string) PeerID {
	if wire == "" {
		return ""
	}
	return PeerID(acct + ":" + wire)
}

// ParsePeerID validates untrusted input. The prefix must be account-shaped
// — starts with a letter, letters/digits only — which is what keeps a
// WhatsApp device JID ("1234:7@…") from masquerading as an account id.
func ParsePeerID(s string) (PeerID, error) {
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return "", fmt.Errorf("peer id %q has no account prefix", s)
	}
	for j := 0; j < i; j++ {
		c := s[j]
		isDigit := c >= '0' && c <= '9'
		isLetter := c >= 'a' && c <= 'z'
		if !isLetter && !(j > 0 && isDigit) {
			return "", fmt.Errorf("peer id %q: bad account prefix %q", s, s[:i])
		}
	}
	return PeerID(s), nil
}

// Acct returns the account prefix ("wa", "wa2") — "" for a zero id.
func (id PeerID) Acct() string {
	if i := strings.IndexByte(string(id), ':'); i > 0 {
		return string(id)[:i]
	}
	return ""
}

// Wire returns the backend's native form — everything after the account
// prefix ("wa:1234:40@s.whatsapp.net" → "1234:40@s.whatsapp.net").
func (id PeerID) Wire() string {
	if i := strings.IndexByte(string(id), ':'); i > 0 {
		return string(id)[i+1:]
	}
	return string(id)
}

// String returns the full namespaced form ("wa:123@…").
func (id PeerID) String() string { return string(id) }

// IsZero reports whether the id is unset.
func (id PeerID) IsZero() bool { return id == "" }

// Scan implements sql.Scanner — a peer-id column is TEXT.
func (id *PeerID) Scan(v any) error {
	switch s := v.(type) {
	case nil:
		*id = ""
	case string:
		*id = PeerID(s)
	case []byte:
		*id = PeerID(s)
	default:
		return fmt.Errorf("PeerID.Scan: unsupported type %T", v)
	}
	return nil
}

// Value implements driver.Valuer — a PeerID binds as its TEXT form.
func (id PeerID) Value() (driver.Value, error) { return string(id), nil }

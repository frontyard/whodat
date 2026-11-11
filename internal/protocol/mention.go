package protocol

import (
	"strings"

	"github.com/frontyard/whodat/internal/store"
)

// Mention binds a "@token" span in outgoing text to a peer. The service maps
// the span to its wire form — WhatsApp rewrites it to "@<digits>" and
// lists the JIDs in ContextInfo.
type Mention struct {
	Token string       // literal span in the text, "@"-prefixed
	ID    store.PeerID // participant id in the service's GroupInfo form
}

// TokenSpans returns the [start,end) byte spans at which tok appears in
// text at token boundaries — preceded by start or a non-word byte and
// followed by the same or end, so "@Al" inside "@Alina" doesn't match.
// Non-ASCII bytes count as boundaries, letting accented names match.
func TokenSpans(text, tok string) [][2]int {
	if tok == "" {
		return nil
	}
	var out [][2]int
	for i := 0; i+len(tok) <= len(text); {
		j := strings.Index(text[i:], tok)
		if j < 0 {
			break
		}
		i += j
		end := i + len(tok)
		if (i == 0 || !wordByte(text[i-1])) && (end == len(text) || !wordByte(text[end])) {
			out = append(out, [2]int{i, end})
		}
		i = end
	}
	return out
}

func wordByte(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

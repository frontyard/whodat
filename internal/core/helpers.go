package core

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/frontyard/whodat/internal/store"
)

var urlRe = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>"']+`)

// trimURLTail strips punctuation that conventionally ends a sentence —
// including ')' only when it has no matching '(' inside the URL (balanced
// parens are legal in paths, e.g. Wikipedia titles).
func trimURLTail(u string) string {
	for len(u) > 0 {
		c := u[len(u)-1]
		if strings.IndexByte(".,;:!?'\"", c) >= 0 {
			u = u[:len(u)-1]
			continue
		}
		if c == ')' && strings.Count(u, ")") > strings.Count(u, "(") {
			u = u[:len(u)-1]
			continue
		}
		break
	}
	return u
}

// Esc escapes text for HTML markup (QML StyledText uses
// &amp;/&lt;/&gt; entities).
func Esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

// linkURL renders one URL match as a clickable <a> link, moving any
// trailing sentence punctuation outside the anchor.
func linkURL(u string) string {
	trimmed := trimURLTail(u)
	href := trimmed
	if !strings.HasPrefix(strings.ToLower(href), "http") {
		href = "https://" + trimmed
	}
	return `<a href="` + Esc(href) + `"><u>` + Esc(trimmed) + `</u></a>` + Esc(u[len(trimmed):])
}

// Linkify returns markup with URLs wrapped in clickable <a> links.
func Linkify(s string) string {
	var b strings.Builder
	last := 0
	for _, loc := range urlRe.FindAllStringIndex(s, -1) {
		b.WriteString(Esc(s[last:loc[0]]))
		b.WriteString(linkURL(s[loc[0]:loc[1]]))
		last = loc[1]
	}
	b.WriteString(Esc(s[last:]))
	return b.String()
}

// Truncate shortens s to n runes, appending an ellipsis when cut.
func Truncate(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}

// JIDUser strips account prefix, server and device parts:
// "wa:1234:40@s.whatsapp.net" → "1234". A numeric prefix before ':' is a
// WhatsApp device part, not an account id — only non-digit prefixes strip.
// wireTail drops a valid account prefix — a digit-led "prefix" is a
// WhatsApp device part ("1234:40@…"), not an account, so the whole id
// is the wire form.
func wireTail(id store.PeerID) string {
	if a := id.Acct(); a != "" && a[0] >= 'a' && a[0] <= 'z' {
		return id.Wire()
	}
	return id.String()
}

func JIDUser(jid store.PeerID) string {
	user := wireTail(jid)
	if i := strings.IndexByte(user, '@'); i >= 0 {
		user = user[:i]
	}
	if i := strings.IndexByte(user, ':'); i >= 0 {
		user = user[:i]
	}
	return user
}

// PhoneOf renders a JID as +phone.
func PhoneOf(jid store.PeerID) string {
	user := wireTail(jid)
	if i := strings.IndexByte(user, '@'); i >= 0 {
		user = user[:i]
	}
	if i := strings.IndexByte(user, ':'); i >= 0 {
		user = user[:i]
	}
	if user == wireTail(jid) {
		return jid.String()
	}
	return "+" + user
}

// allDigits reports whether s is non-empty ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- fmt ----

// FmtTime renders a message timestamp.
func FmtTime(t time.Time) string { return t.Format("15:04") }

// FmtDay renders a day-separator label.
func FmtDay(t time.Time) string {
	now := time.Now()
	switch {
	case SameDay(t, now):
		return "Today"
	case SameDay(t, now.AddDate(0, 0, -1)):
		return "Yesterday"
	default:
		return t.Format("Mon, 2 Jan 2006")
	}
}

// FmtStamp renders sidebar timestamps (time today, weekday this week, else date).
func FmtStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	if time.Since(t) < 20*time.Hour && t.Day() == time.Now().Day() {
		return t.Format("15:04")
	}
	if time.Since(t) < 7*24*time.Hour {
		return t.Format("Mon")
	}
	return t.Format("02/01/06")
}

// FmtDur renders seconds as m:ss.
func FmtDur(secs int) string { return fmt.Sprintf("%d:%02d", secs/60, secs%60) }

// FmtSize renders a byte count.
func FmtSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// FileExists reports whether a file exists.
func FileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// SameDay reports whether two times fall on the same calendar day.
func SameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// ChatTitle returns the display name for a chat.
func ChatTitle(c *store.Chat) string {
	if c.Name != "" {
		return c.Name
	}
	return PhoneOf(c.JID)
}

// StatusGlyph renders outgoing delivery ticks.
func StatusGlyph(s store.MsgStatus) string {
	switch s {
	case store.SPending:
		return "🕓"
	case store.SSent:
		return "✓"
	case store.SDelivered:
		return "✓✓"
	case store.SRead, store.SPlayed:
		return "✓✓" // styled blue by the frontend
	case store.SFailed:
		return "⚠"
	default:
		return ""
	}
}

// DisappText renders a disappearing timer duration.
func DisappText(secs int) string {
	switch {
	case secs >= 90*24*3600:
		return "90 days"
	case secs >= 7*24*3600:
		return "7 days"
	case secs >= 24*3600:
		return "24 hours"
	case secs >= 3600:
		return "1 hour"
	default:
		return FmtDur(secs)
	}
}

// BadgeSVG overlays an unread dot onto the base icon (bottom-right).
func BadgeSVG(base []byte) []byte {
	return BadgeSVGAt(base, 106, 106, 19, 5)
}

// BadgeSVGAt overlays an unread dot at the given position — icons with a
// non-zero viewBox origin need coordinates inside their own space.
func BadgeSVGAt(base []byte, cx, cy, r, sw int) []byte {
	dot := fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d" fill="#e53935" stroke="#e53935" stroke-width="%d"/></svg>`,
		cx, cy, r, sw)
	s := string(base)
	if i := strings.LastIndex(s, "</svg>"); i >= 0 {
		return []byte(s[:i] + dot)
	}
	return base
}

// MimeForExt guesses a media mime type from a file extension.
func MimeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg":
		return "audio/ogg"
	case ".opus":
		return "audio/ogg; codecs=opus"
	case ".m4a":
		return "audio/mp4"
	case ".pdf":
		return "application/pdf"
	}
	return "application/octet-stream"
}

// previewText renders a media message's type label for the starred list.
func PreviewText(m *store.Message) string {
	switch m.Type {
	case store.TImage:
		return "📷 Photo"
	case store.TVideo:
		return "🎬 Video"
	case store.TAudio, store.TPTT:
		return "🎤 Voice message"
	case store.TDocument:
		if m.MediaName != "" {
			return "📄 " + m.MediaName
		}
		return "📄 Document"
	case store.TSticker:
		return "Sticker"
	case store.TPoll:
		return "📊 Poll"
	case store.TCall:
		return "📞 Call"
	case store.TLocation:
		return "📍 Location"
	case store.TRevoked:
		return "Message deleted"
	}
	return "Message"
}

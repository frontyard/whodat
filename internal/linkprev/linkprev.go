// Package linkprev resolves Open Graph cards for outgoing messages —
// WhatsApp-style link previews are generated sender-side, so the client
// fetches the page, reads og:title/og:description/og:image/og:site_name and
// shrinks the image into the small JPEG the wire format expects.
package linkprev

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	xhtml "golang.org/x/net/html"
)

// Preview is the resolved card content for one URL.
type Preview struct {
	URL   string // final URL after redirects
	Title string
	Desc  string
	Site  string
	Thumb []byte // small JPEG, nil when the page has no usable image
}

var urlRe = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>"']+`)

// trimTail strips punctuation that conventionally ends a sentence —
// including ')' only when it has no matching '(' inside the URL.
func trimTail(u string) string {
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

// FirstURL returns the first link in text (raw form as written, no scheme
// added), or "" when the message carries none.
func FirstURL(text string) string {
	return trimTail(urlRe.FindString(text))
}

var errPrivate = errors.New("private address")

// client dials only public addresses — preview targets come from chat
// content and must not reach loopback/link-local/internal hosts.
var client = &http.Client{
	Timeout: 12 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	},
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if ta, ok := conn.RemoteAddr().(*net.TCPAddr); ok && privateIP(ta.IP) {
				conn.Close()
				return nil, errPrivate
			}
			return conn, nil
		},
	},
}

func privateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

func normalize(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("bad url")
	}
	return u.String(), nil
}

func get(ctx context.Context, u string, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", accept)
	return client.Do(req)
}

// Fetch resolves rawurl into a card. The page is fetched with a browser UA,
// og:* meta tags win over <title>, and the og:image is re-encoded as a
// ≤320px JPEG. Returns nil when the page yields nothing worth showing.
func Fetch(ctx context.Context, raw string) (*Preview, error) {
	u, err := normalize(raw)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := get(ctx, u, "text/html,application/xhtml+xml")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "" && !strings.Contains(ct, "html") {
		return nil, errors.New("not html: " + ct)
	}
	doc, err := xhtml.Parse(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	meta, title := scan(doc)
	p := cardFromMeta(resp.Request.URL.String(), meta, title)
	if p == nil {
		return nil, errors.New("no metadata")
	}
	if img := first(meta["og:image"], meta["twitter:image"]); img != "" {
		if ref, err := resp.Request.URL.Parse(img); err == nil {
			p.Thumb = fetchThumb(ctx, ref.String())
		}
	}
	return p, nil
}

// cardFromMeta assembles the card from parsed head metadata — og:* wins
// over twitter:* over <title>; site falls back to the page host. Nil when
// the page yields nothing worth showing.
func cardFromMeta(finalURL string, meta map[string]string, title string) *Preview {
	p := &Preview{
		URL:   finalURL,
		Title: first(meta["og:title"], meta["twitter:title"], title),
		Desc:  first(meta["og:description"], meta["twitter:description"]),
		Site:  meta["og:site_name"],
	}
	if p.Site == "" {
		if u, err := url.Parse(finalURL); err == nil {
			p.Site = strings.TrimPrefix(u.Hostname(), "www.")
		}
	}
	if p.Title == "" && p.Desc == "" {
		return nil
	}
	return p
}

func first(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

// scan walks the document collecting meta key→content and the <title> text.
func scan(doc *xhtml.Node) (map[string]string, string) {
	meta := map[string]string{}
	var title string
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "meta":
				var key, content string
				for _, a := range n.Attr {
					switch a.Key {
					case "property", "name":
						key = strings.ToLower(a.Val)
					case "content":
						content = a.Val
					}
				}
				if _, seen := meta[key]; key != "" && content != "" && !seen {
					meta[key] = content
				}
			case "title":
				if title == "" && n.FirstChild != nil {
					title = n.FirstChild.Data
				}
			case "body":
				return // metadata lives in <head>; stop at the body
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return meta, title
}

// fetchThumb downloads and downscales a preview image to a ≤320px JPEG.
func fetchThumb(ctx context.Context, u string) []byte {
	resp, err := get(ctx, u, "image/*")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, 6<<20))
	if err != nil {
		return nil
	}
	const maxSide = 320
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxSide || h > maxSide {
		scale := float64(maxSide) / float64(max(w, h))
		nw, nh := int(float64(w)*scale), int(float64(h)*scale)
		if nw < 1 || nh < 1 {
			return nil
		}
		dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
		draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
		img = dst
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil
	}
	if buf.Len() > 100<<10 {
		return nil // absurdly heavy thumb — send the card without one
	}
	return buf.Bytes()
}

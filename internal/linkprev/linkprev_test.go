package linkprev

import (
	"context"
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestFirstURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"no links here", ""},
		{"see https://example.com/a?b=1 ok", "https://example.com/a?b=1"},
		{"www.example.com/path", "www.example.com/path"},
		{"check this (https://a.b/c)", "https://a.b/c"},
		{"visit https://x.io/p.", "https://x.io/p"},
		{"https://en.wikipedia.org/wiki/Go_(lang)", "https://en.wikipedia.org/wiki/Go_(lang)"},
		{"a https://first.io b https://second.io", "https://first.io"},
		{"mail a@b.co or https://x.io", "https://x.io"},
	}
	for _, c := range cases {
		if got := FirstURL(c.in); got != c.want {
			t.Fatalf("FirstURL(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	if got, _ := normalize("www.example.com"); got != "https://www.example.com" {
		t.Fatalf("bare host: %q", got)
	}
	if got, _ := normalize("http://x.io/a"); got != "http://x.io/a" {
		t.Fatalf("scheme kept: %q", got)
	}
	if _, err := normalize("http://"); err == nil {
		t.Fatal("hostless URL accepted")
	}
}

func TestPrivateIP(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1", "169.254.1.1", "::1", "0.0.0.0", "fe80::1"} {
		if !privateIP(net.ParseIP(s)) {
			t.Fatalf("%s must be blocked", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if privateIP(net.ParseIP(s)) {
			t.Fatalf("%s is public", s)
		}
	}
}

func TestFetchRefusesLocalTargets(t *testing.T) {
	srv := httptest.NewServer(nil)
	defer srv.Close()
	// The dialer blocks loopback — the SSRF guard is exercised end-to-end.
	if _, err := Fetch(context.Background(), srv.URL); err == nil {
		t.Fatal("fetch to loopback must be refused")
	}
}

const ogDoc = `<html><head>
<meta property="og:title" content="An Article">
<meta property="og:description" content="About things.">
<meta property="og:site_name" content="Example News">
<meta property="og:image" content="/img/cover.png">
<title>Fallback Title</title>
</head><body>content</body></html>`

func TestScanMeta(t *testing.T) {
	doc, err := xhtml.Parse(strings.NewReader(ogDoc))
	if err != nil {
		t.Fatal(err)
	}
	meta, title := scan(doc)
	if meta["og:title"] != "An Article" || meta["og:site_name"] != "Example News" {
		t.Fatalf("meta: %v", meta)
	}
	if title != "Fallback Title" {
		t.Fatalf("title: %q", title)
	}
	p := cardFromMeta("https://news.example.com/a", meta, title)
	if p == nil || p.Title != "An Article" || p.Desc != "About things." || p.Site != "Example News" {
		t.Fatalf("card: %+v", p)
	}
}

func TestCardFromMetaFallbacks(t *testing.T) {
	// No og:* at all → <title> and the URL host fill in.
	p := cardFromMeta("https://www.blog.io/post", map[string]string{}, "A Post")
	if p == nil || p.Title != "A Post" || p.Site != "blog.io" {
		t.Fatalf("fallback: %+v", p)
	}
	// twitter: variants when og: is absent.
	p = cardFromMeta("https://x.io", map[string]string{
		"twitter:title": "T", "twitter:description": "D",
	}, "")
	if p == nil || p.Title != "T" || p.Desc != "D" {
		t.Fatalf("twitter: %+v", p)
	}
	// Nothing usable → nil.
	if cardFromMeta("https://x.io", map[string]string{}, "") != nil {
		t.Fatal("empty page must not produce a card")
	}
}

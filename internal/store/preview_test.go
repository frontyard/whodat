package store

import (
	"context"
	"testing"
	"time"
)

func TestLinkPreviewRoundTrip(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	m := msg("wa2:1", "p1", time.Now().Truncate(time.Second))
	m.Preview = &LinkPreview{
		URL:   "https://example.com/a",
		Title: "An Article", Desc: "About things.", Site: "Example",
		Thumb: "/media/thumb.jpg", Large: true,
	}
	if err := d.InsertMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMessage(ctx, "wa2:1", "p1")
	if err != nil || got.Preview == nil {
		t.Fatalf("get: %v %+v", err, got)
	}
	pv := got.Preview
	if pv.URL != m.Preview.URL || pv.Title != "An Article" ||
		pv.Desc != "About things." || pv.Site != "Example" ||
		pv.Thumb != "/media/thumb.jpg" || !pv.Large || pv.Pending {
		t.Fatalf("preview: %+v", pv)
	}
}

func TestLinkPreviewUpsertPreserves(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	ts := time.Now().Truncate(time.Second)
	m := msg("wa2:1", "p2", ts)
	m.Preview = &LinkPreview{URL: "https://x.io", Title: "T", Thumb: "/t.jpg"}
	if err := d.InsertMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	// Re-ingest without a preview (history resync) — card survives.
	if err := d.InsertMessage(ctx, msg("wa2:1", "p2", ts)); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetMessage(ctx, "wa2:1", "p2")
	if got.Preview == nil || got.Preview.Title != "T" || got.Preview.Thumb != "/t.jpg" {
		t.Fatalf("resync dropped card: %+v", got.Preview)
	}
	// Re-ingest with fresh metadata but no local thumb — the downloaded
	// file path merges into the new card instead of being lost.
	fresh := msg("wa2:1", "p2", ts)
	fresh.Preview = &LinkPreview{URL: "https://x.io", Title: "T2"}
	if err := d.InsertMessage(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetMessage(ctx, "wa2:1", "p2")
	if got.Preview == nil || got.Preview.Title != "T2" || got.Preview.Thumb != "/t.jpg" {
		t.Fatalf("thumb merge: %+v", got.Preview)
	}
	// A newer download wins over the stale path.
	fresher := msg("wa2:1", "p2", ts)
	fresher.Preview = &LinkPreview{URL: "https://x.io", Title: "T3", Thumb: "/new.jpg"}
	if err := d.InsertMessage(ctx, fresher); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetMessage(ctx, "wa2:1", "p2")
	if got.Preview == nil || got.Preview.Thumb != "/new.jpg" {
		t.Fatalf("new thumb lost: %+v", got.Preview)
	}
}

func TestSetLinkPreviewAndThumb(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	m := msg("wa2:7", "p3", time.Now().Truncate(time.Second))
	m.Preview = &LinkPreview{URL: "https://p.io", Pending: true}
	if err := d.InsertMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := d.SetLinkPreview(ctx, "wa2:7", "p3", &LinkPreview{
		URL: "https://p.io", Title: "Resolved",
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetMessage(ctx, "wa2:7", "p3")
	if got.Preview == nil || got.Preview.Title != "Resolved" || got.Preview.Pending {
		t.Fatalf("set: %+v", got.Preview)
	}
	if err := d.SetPreviewThumb(ctx, "wa2:7", "p3", "/th.jpg"); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetMessage(ctx, "wa2:7", "p3")
	if got.Preview == nil || got.Preview.Thumb != "/th.jpg" {
		t.Fatalf("thumb: %+v", got.Preview)
	}
	// nil clears the card entirely.
	if err := d.SetLinkPreview(ctx, "wa2:7", "p3", nil); err != nil {
		t.Fatal(err)
	}
	got, _ = d.GetMessage(ctx, "wa2:7", "p3")
	if got.Preview != nil {
		t.Fatalf("clear: %+v", got.Preview)
	}
}

func TestMessagesByPreviewURL(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	ts := time.Now().Truncate(time.Second)
	for i, chat := range []PeerID{"wa2:1", "wa2:2", "wa:1"} {
		m := msg(chat, "x"+string(rune('a'+i)), ts)
		m.Preview = &LinkPreview{URL: "https://shared.io"}
		if err := d.InsertMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := d.MessagesByPreviewURL(ctx, "wa2", "https://shared.io")
	if err != nil || len(rows) != 2 {
		t.Fatalf("acct-scoped match: %d %v", len(rows), err)
	}
	rows, err = d.MessagesByPreviewURL(ctx, "wa", "https://shared.io")
	if err != nil || len(rows) != 1 {
		t.Fatalf("wa match: %d %v", len(rows), err)
	}
	rows, _ = d.MessagesByPreviewURL(ctx, "wa2", "https://absent.io")
	if len(rows) != 0 {
		t.Fatalf("phantom match: %d", len(rows))
	}
}

func TestRevokedClearsPreview(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	m := msg("wa2:9", "p4", time.Now().Truncate(time.Second))
	m.Preview = &LinkPreview{URL: "https://x.io", Title: "T"}
	if err := d.InsertMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	if err := d.SetRevoked(ctx, "wa2:9", "p4"); err != nil {
		t.Fatal(err)
	}
	got, _ := d.GetMessage(ctx, "wa2:9", "p4")
	if got.Preview != nil || !got.Revoked {
		t.Fatalf("revoke kept card: %+v", got.Preview)
	}
	rows, _ := d.MessagesByPreviewURL(ctx, "wa2", "https://x.io")
	if len(rows) != 0 {
		t.Fatal("revoked row still matches preview_url")
	}
}

func TestDeleteMessageCollectsPreviewThumb(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	m := msg("wa2:5", "p5", time.Now().Truncate(time.Second))
	m.Preview = &LinkPreview{URL: "https://x.io", Thumb: "/pt.jpg"}
	if err := d.InsertMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	paths, err := d.DeleteMessage(ctx, "wa2:5", "p5")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range paths {
		if p == "/pt.jpg" {
			found = true
		}
	}
	if !found {
		t.Fatalf("preview thumb not in cleanup list: %v", paths)
	}
}

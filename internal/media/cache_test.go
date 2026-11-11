package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtFor(t *testing.T) {
	cases := []struct{ mime, file, want string }{
		{"audio/ogg; codecs=opus", "", ".ogg"},
		{"image/webp", "", ".webp"},
		{"video/mp4", "", ".mp4"},
		{"image/jpeg", "", ".jpg"},
		{"application/pdf", "", ".pdf"},
		{"application/x-weird", "doc.xyz", ".xyz"},
		{"", "", ".bin"},
	}
	for _, c := range cases {
		if got := ExtFor(c.mime, c.file); got != c.want {
			t.Fatalf("ExtFor(%q,%q) = %q want %q", c.mime, c.file, got, c.want)
		}
	}
}

func TestStageBytes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	p, err := StageBytes("wa", []byte("payload"), ".ogg")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, filepath.Join(dir, "whodat", "media", "wa")) {
		t.Fatalf("staged outside media dir: %s", p)
	}
	data, err := os.ReadFile(p)
	if err != nil || string(data) != "payload" {
		t.Fatalf("read back: %v %q", err, data)
	}
	// Same payload, two calls → distinct names (no clobber).
	p2, err := StageBytes("wa", []byte("payload"), ".ogg")
	if err != nil {
		t.Fatal(err)
	}
	if p == p2 {
		t.Fatalf("colliding stage paths: %s", p)
	}
}

func TestStageFile(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jpg")
	if err := os.WriteFile(src, []byte("img"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	p, err := StageFile("wa", src, ".jpg")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p, ".jpg") {
		t.Fatalf("ext lost: %s", p)
	}
}

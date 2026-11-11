package qtui

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/frontyard/whodat/internal/config"
)

//go:embed qml
var qmlFS embed.FS

// prepareQML makes the bundled QML available as real files and returns the
// directory containing Main.qml. WHODAT_QML_DIR overrides the location for
// development (edit QML without rebuilding).
func (h *Host) prepareQML() string {
	if d := os.Getenv("WHODAT_QML_DIR"); d != "" {
		return d
	}

	// Extract to a versioned cache dir so upgrades replace stale files and
	// identical builds re-use the extraction.
	hash := sha256.New()
	entries := qmlFileList()
	for _, p := range entries {
		if b, err := qmlFS.ReadFile(p); err == nil {
			hash.Write(b)
		}
	}
	tag := hex.EncodeToString(hash.Sum(nil))[:12]
	dir := filepath.Join(config.CacheDir(), "qml", tag)
	if _, err := os.Stat(filepath.Join(dir, "Main.qml")); err == nil {
		return dir
	}
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("qtui: mkdir qml cache: %v", err)
	}
	for _, p := range entries {
		b, err := qmlFS.ReadFile(p)
		if err != nil {
			continue
		}
		// Preserve subdirs (qml/icons/…) so Qt.resolvedUrl paths still work.
		rel := strings.TrimPrefix(p, "qml/")
		out := filepath.Join(dir, rel)
		_ = os.MkdirAll(filepath.Dir(out), 0o755)
		if err := os.WriteFile(out, b, 0o644); err != nil {
			log.Printf("qtui: write %s: %v", out, err)
		}
	}
	return dir
}

func qmlFileList() []string {
	var out []string
	_ = fs.WalkDir(qmlFS, "qml", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		out = append(out, path)
		return nil
	})
	return out
}

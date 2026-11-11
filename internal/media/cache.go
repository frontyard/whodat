// Package media handles staging, path naming and (voice-note) recording.
// Downloads live behind protocol.Service.FetchMedia — each backend owns
// its attachment format.
package media

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frontyard/whodat/internal/config"
	"github.com/frontyard/whodat/internal/store"
)

// StageFile copies an outgoing attachment into the account's media dir and
// returns its new path — the DB must not record paths into the user's home
// (leaks the fs layout; breaks when the source moves).
func StageFile(acct, src, ext string) (string, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	return StageBytes(acct, data, ext)
}

// StageBytes writes media into the account's media dir and returns the path.
func StageBytes(acct string, data []byte, ext string) (string, error) {
	dir := config.MediaAcctDir(acct)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	path := filepath.Join(dir, hex.EncodeToString(sum[:8])+"-"+rand4()+ext)
	return path, os.WriteFile(path, data, 0o600)
}

func rand4() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// PathFor builds a stable content-addressed path for the attachment under
// the owning account's media dir.
func PathFor(m *store.Message, mimeType string) string {
	sum := sha256.Sum256([]byte(m.ChatJID.String() + ":" + m.ID))
	name := hex.EncodeToString(sum[:8]) + ExtFor(mimeType, m.MediaName)
	dir := config.MediaAcctDir(m.ChatJID.Acct())
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, name)
}

// ExtFor picks a file extension from the mime type (filename as tiebreak).
func ExtFor(mimeType, filename string) string {
	base, _, _ := strings.Cut(mimeType, ";")
	base = strings.TrimSpace(base)
	switch base {
	case "audio/ogg", "audio/opus", "application/ogg":
		return ".ogg"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	}
	if exts, _ := mime.ExtensionsByType(base); len(exts) > 0 {
		return exts[0]
	}
	if e := filepath.Ext(filename); e != "" {
		return e
	}
	return ".bin"
}

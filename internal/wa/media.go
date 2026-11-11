package wa

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/polymorfa/hypermeow/proto/waE2E"

	"github.com/frontyard/whodat/internal/media"
	appstore "github.com/frontyard/whodat/internal/store"
)

// FetchMedia resolves a message's attachment to a local file, downloading
// it from WhatsApp on first use. The stored proto payload carries the
// upload keys — decryption happens inside the client.
func (s *Service) FetchMedia(ctx context.Context, m *appstore.Message) (string, error) {
	if m.MediaPath != "" {
		if _, err := os.Stat(m.MediaPath); err == nil {
			return m.MediaPath, nil
		}
	}
	if len(m.Proto) == 0 {
		return "", fmt.Errorf("no proto payload for download")
	}
	wm := unmarshalProto(m.Proto)
	if wm == nil || downloadable(wm) == nil {
		return "", fmt.Errorf("message carries no downloadable media")
	}
	data, err := s.Client().DownloadAny(ctx, wm)
	if err != nil {
		return "", err
	}
	path := media.PathFor(m, m.MediaMIME)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	// Stage then rename — a crash mid-write must not leave a partial file
	// behind to be served as the real attachment forever.
	tmp := path + ".part"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	_ = s.db.SetMediaPath(ctx, m.ChatJID, m.ID, path)
	m.MediaPath = path
	return path, nil
}

// CanFetchMedia reports whether the row carries everything a fetch needs —
// the proto payload holds the upload keys, so without it nothing resolves.
func (s *Service) CanFetchMedia(m *appstore.Message) bool {
	if len(m.Proto) == 0 {
		return false
	}
	switch m.Type {
	case appstore.TImage, appstore.TVideo, appstore.TAudio, appstore.TPTT,
		appstore.TDocument, appstore.TSticker:
		return true
	}
	return false
}

// downloadable returns the media sub-message when present.
func downloadable(wm *waE2E.Message) any {
	switch {
	case wm.GetImageMessage() != nil:
		return wm.GetImageMessage()
	case wm.GetVideoMessage() != nil:
		return wm.GetVideoMessage()
	case wm.GetAudioMessage() != nil:
		return wm.GetAudioMessage()
	case wm.GetDocumentMessage() != nil:
		return wm.GetDocumentMessage()
	case wm.GetStickerMessage() != nil:
		return wm.GetStickerMessage()
	}
	return nil
}

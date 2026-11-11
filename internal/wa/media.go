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

// FetchMediaThumb resolves the sender-provided inline thumbnail (the
// video poster / GIF first frame) to a local file — the bytes ship inside
// the message itself, so no download is needed. Rows converted before
// thumb staging still carry them in the stored proto: extract on first view.
func (s *Service) FetchMediaThumb(ctx context.Context, m *appstore.Message) (string, error) {
	if m.MediaThumb != "" {
		if _, err := os.Stat(m.MediaThumb); err == nil {
			return m.MediaThumb, nil
		}
	}
	if len(m.Proto) == 0 {
		return "", fmt.Errorf("no proto payload for thumbnail")
	}
	wm := unmarshalProto(m.Proto)
	if wm == nil {
		return "", fmt.Errorf("message carries no proto")
	}
	var thumb []byte
	switch {
	case wm.GetVideoMessage() != nil:
		thumb = wm.GetVideoMessage().GetJPEGThumbnail()
	case wm.GetImageMessage() != nil:
		thumb = wm.GetImageMessage().GetJPEGThumbnail()
	case wm.GetDocumentMessage() != nil:
		thumb = wm.GetDocumentMessage().GetJPEGThumbnail()
	}
	if len(thumb) == 0 {
		return "", fmt.Errorf("message carries no inline thumbnail")
	}
	path, err := media.StageBytes(m.ChatJID.Acct(), thumb, ".jpg")
	if err != nil {
		return "", err
	}
	_ = s.db.SetMediaThumb(ctx, m.ChatJID, m.ID, path)
	m.MediaThumb = path
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

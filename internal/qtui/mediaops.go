package qtui

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	qt "github.com/mappu/miqt/qt6"

	"github.com/frontyard/whodat/internal/core"
	"github.com/frontyard/whodat/internal/media"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

// registerMediaOps adds attachment ops (download/open/save/send/record).
func (h *Host) registerMediaOps() {
	h.ops["mediaUrl"] = h.opMediaURL
	h.ops["mediaThumb"] = h.opMediaThumb
	h.ops["openMedia"] = h.opOpenMedia
	h.ops["saveMedia"] = h.opSaveMedia
	h.ops["sendFile"] = h.opSendFile
	h.ops["startRecord"] = h.opStartRecord
	h.ops["stopRecord"] = h.opStopRecord
}

// resolve returns the local file for a media message, downloading on demand.
func (h *Host) resolve(ctx context.Context, jid store.PeerID, msgID string) (string, *store.Message, error) {
	m, err := h.app.DB.GetMessage(ctx, jid, msgID)
	if err != nil || m == nil {
		return "", nil, err
	}
	p, err := h.app.MediaPath(ctx, m)
	if err != nil {
		return "", m, err
	}
	return p, m, nil
}

// opMediaURL fetches (downloads if needed) the attachment path.
// args: {jid, msgId} → {path}
func (h *Host) opMediaURL(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID   store.PeerID `json:"jid"`
		MsgID string       `json:"msgId"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	p, _, err := h.resolve(ctx, a.JID, a.MsgID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"path": p}, nil
}

// opMediaThumb resolves the sender-provided inline thumbnail (video
// poster / GIF first frame) without downloading the attachment — the
// bytes ship inside the stored proto and are staged on first view.
// args: {jid, msgId} → {path} ("" when the message carries none)
func (h *Host) opMediaThumb(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID   store.PeerID `json:"jid"`
		MsgID string       `json:"msgId"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	m, err := h.app.DB.GetMessage(ctx, a.JID, a.MsgID)
	if err != nil || m == nil {
		return nil, err
	}
	path := ""
	if svc, ok := h.app.SvcFor(a.JID).(protocol.MediaThumber); ok {
		if p, err := svc.FetchMediaThumb(ctx, m); err == nil {
			path = p
		}
	}
	return map[string]any{"path": path}, nil
}

// opOpenMedia opens the attachment with the desktop default handler.
// args: {jid, msgId}
func (h *Host) opOpenMedia(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID   store.PeerID `json:"jid"`
		MsgID string       `json:"msgId"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	p, _, err := h.resolve(ctx, a.JID, a.MsgID)
	if err != nil {
		return nil, err
	}
	mt(func() {
		if !qt.QDesktopServices_OpenUrl(qt.QUrl_FromLocalFile(p)) {
			h.Toast("No application can open this file")
		}
	})
	return map[string]any{}, nil
}

// opSaveMedia copies the attachment to a user-chosen path.
// args: {jid, msgId, dest}
func (h *Host) opSaveMedia(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID   store.PeerID `json:"jid"`
		MsgID string       `json:"msgId"`
		Dest  string       `json:"dest"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	p, _, err := h.resolve(ctx, a.JID, a.MsgID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(a.Dest, data, 0o644); err != nil {
		return nil, err
	}
	return map[string]any{"path": a.Dest}, nil
}

// opSendFile uploads a picked file, choosing message type by mime
// (webp → sticker, image dims via QImageReader).
// args: {jid, path}
func (h *Host) opSendFile(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID  store.PeerID `json:"jid"`
		Path string       `json:"path"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(a.Path)
	if err != nil || len(data) == 0 {
		return nil, err
	}
	mime := http.DetectContentType(first512(data))
	if ext := filepath.Ext(a.Path); mime == "application/octet-stream" && ext != "" {
		mime = core.MimeForExt(ext)
	}
	m := &store.Message{ChatJID: a.JID}
	if staged, err := media.StageFile(a.JID.Acct(), a.Path, filepath.Ext(a.Path)); err == nil {
		m.MediaPath = staged
	}
	switch {
	case strings.HasPrefix(mime, "image/") && mime != "image/webp":
		m.Type = store.TImage
		if r := qt.NewQImageReader3(a.Path); r != nil {
			if sz := r.Size(); sz.IsValid() {
				m.Width, m.Height = sz.Width(), sz.Height()
			}
		}
	case mime == "image/webp":
		m.Type = store.TSticker
	case strings.HasPrefix(mime, "video/"):
		m.Type = store.TVideo
	case strings.HasPrefix(mime, "audio/"):
		m.Type = store.TAudio
	default:
		m.Type = store.TDocument
	}
	m.MediaMIME = mime
	m.MediaName = filepath.Base(a.Path)
	m.MediaSize = int64(len(data))

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		svc, err := svcForChat(h.app, a.JID)
		if err != nil {
			h.Toast("Upload failed: " + err.Error())
			return
		}
		if _, err := svc.SendMedia(ctx, a.JID, m, data); err != nil {
			h.Toast("Upload failed: " + err.Error())
		}
	}()
	return map[string]any{}, nil
}

func first512(b []byte) []byte {
	if len(b) > 512 {
		return b[:512]
	}
	return b
}

// opStartRecord begins PTT capture. Errors when no mic or already recording.
func (h *Host) opStartRecord(_ *Host, _ json.RawMessage) (any, error) {
	h.recMu.Lock()
	defer h.recMu.Unlock()
	if h.recorder != nil {
		return nil, nil // already recording — ignore double tap
	}
	r, err := media.StartRecorder()
	if err != nil {
		return nil, err
	}
	h.recorder = r
	return map[string]any{}, nil
}

// opStopRecord ends capture and sends (or discards) the voice note.
// args: {jid, cancel}
func (h *Host) opStopRecord(_ *Host, args json.RawMessage) (any, error) {
	var a struct {
		JID    store.PeerID `json:"jid"`
		Cancel bool         `json:"cancel"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	h.recMu.Lock()
	r := h.recorder
	h.recorder = nil
	h.recMu.Unlock()
	if r == nil {
		return map[string]any{}, nil
	}
	go func() {
		vn, err := r.Stop()
		if a.Cancel || vn == nil {
			return
		}
		if err != nil {
			h.Toast("Couldn't encode voice message")
			return
		}
		if vn.Seconds == 0 {
			h.Toast("Recording too short")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		m := &store.Message{
			ChatJID:   a.JID,
			Type:      store.TPTT,
			MediaMIME: "audio/ogg; codecs=opus",
			Duration:  vn.Seconds,
			Waveform:  vn.Waveform,
		}
		if p, err := media.StageBytes(a.JID.Acct(), vn.Ogg, ".ogg"); err == nil {
			m.MediaPath = p
		}
		svc, err := svcForChat(h.app, a.JID)
		if err != nil {
			h.Toast("Voice message failed: " + err.Error())
			return
		}
		if _, err := svc.SendMedia(ctx, a.JID, m, vn.Ogg); err != nil {
			h.Toast("Voice message failed: " + err.Error())
		}
	}()
	return map[string]any{}, nil
}

package qtui

import (
	"encoding/json"

	qt "github.com/mappu/miqt/qt6"
)

// registerOps installs the command table. Ops run on background goroutines;
// UI-visible results travel back through Reply or State.
func (h *Host) registerOps() {
	h.ops = map[string]opFunc{
		"quit":   opQuit,
		"notify": opNotify,
	}
	h.registerChatOps()
	h.registerMsgOps()
	h.registerMediaOps()
	h.registerAuthOps()
}

// opNotify echoes a message into the toast area — lets QML surface its own
// transient errors through the same path Go toasts use.
func opNotify(h *Host, args json.RawMessage) (any, error) {
	var a struct {
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, err
	}
	h.Toast(a.Msg)
	return map[string]any{}, nil
}

// opQuit exits the app immediately.
func opQuit(h *Host, _ json.RawMessage) (any, error) {
	mt(func() { qt.QCoreApplication_Quit() })
	return map[string]any{}, nil
}

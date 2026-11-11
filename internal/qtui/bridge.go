package qtui

import (
	"encoding/json"
	"log"
)

// opFunc implements a bridge command. It runs on a background goroutine and
// returns data for the reply payload.
type opFunc func(h *Host, args json.RawMessage) (any, error)

type cmdPayload struct {
	ID   int64           `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args"`
}

type replyPayload struct {
	ID    int64  `json:"id"`
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// dispatch handles a Cmd.payload write from QML. It parses the envelope and
// runs the op off-thread; the reply lands via Reply.payload.
func (h *Host) dispatch(raw string) {
	var c cmdPayload
	if err := json.Unmarshal([]byte(raw), &c); err != nil || c.Op == "" {
		log.Printf("qtui: bad cmd payload: %q", raw)
		return
	}
	op, ok := h.ops[c.Op]
	if !ok {
		mt(func() { h.setReply(replyPayload{ID: c.ID, OK: false, Error: "unknown op: " + c.Op}) })
		return
	}
	go func() {
		data, err := op(h, c.Args)
		mt(func() {
			r := replyPayload{ID: c.ID, OK: err == nil, Data: data}
			if err != nil {
				r.Error = err.Error()
			}
			h.setReply(r)
		})
	}()
}

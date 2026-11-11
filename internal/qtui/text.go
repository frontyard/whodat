package qtui

import (
	"context"
	"encoding/json"

	"github.com/frontyard/whodat/internal/store"
)

// Text helpers the models use to produce display strings. All are called on
// the Qt thread — they hit SQLite only (fast) or cached data.

// chatTitle: stored name, else "+phone" (LID ids resolve to their PN first).
func (h *Host) chatTitle(c *store.Chat) string {
	if c.Name != "" {
		return c.Name
	}
	return h.app.NumberOf(context.Background(), c.JID)
}

// msgHTML renders the bubble text: protocol-pre-rendered HTML when the row
// carries it (TG entities), else escaping, linkify and mention markup.
func (h *Host) msgHTML(m *store.Message) string {
	if m.HTML != "" {
		return m.HTML
	}
	return h.app.MentionsHTML(context.Background(), m.ChatJID, m.Text)
}

// senderName resolves a group sender for the colored name line.
func (h *Host) senderName(m *store.Message) string {
	return h.app.SenderName(context.Background(), m)
}

// quotedSender resolves the quoted message's author.
func (h *Host) quotedSender(m *store.Message) string {
	if m.QuotedSender == "" {
		return ""
	}
	return h.app.ContactName(context.Background(), m.QuotedSender)
}

// pollJSON serializes a poll bubble for the delegate:
// {options:[{name,votes,mine}],max:n} — votes pulled live from the DB.
func (h *Host) pollJSON(m *store.Message) string {
	if m.Type != store.TPoll || len(m.PollOptions) == 0 {
		return ""
	}
	votes, err := h.app.DB.PollVotes(context.Background(), m.ChatJID, m.ID)
	if err != nil {
		votes = map[string][]string{}
	}
	var own store.PeerID
	if svc := h.app.SvcFor(m.ChatJID); svc != nil {
		own = svc.OwnID()
	}
	type opt struct {
		Name  string `json:"name"`
		Votes int    `json:"votes"`
		Mine  bool   `json:"mine"`
	}
	out := struct {
		Options []opt `json:"options"`
		Max     int   `json:"max"`
	}{Max: m.PollMax}
	for _, o := range m.PollOptions {
		vs := votes[o]
		mine := false
		for _, v := range vs {
			if v == own.String() {
				mine = true
			}
		}
		out.Options = append(out.Options, opt{Name: o, Votes: len(vs), Mine: mine})
	}
	b, _ := json.Marshal(out)
	return string(b)
}

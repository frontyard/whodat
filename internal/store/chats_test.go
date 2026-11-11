package store

import (
	"context"
	"testing"
	"time"
)

func sysMsg(chat PeerID, id string, ts time.Time) *Message {
	m := msg(chat, id, ts)
	m.Type = TSystem
	m.Text = "Security code changed for this chat"
	return m
}

func chatJIDs(cs []*Chat) map[PeerID]bool {
	out := map[PeerID]bool{}
	for _, c := range cs {
		out[c.JID] = true
	}
	return out
}

// A DM earns a sidebar slot by carrying a real message: a row built out of
// crypto notices alone — or no rows at all — stays hidden. Groups are
// memberships and always list; pinned/unread marks always surface.
func TestListChatsHidesPhantomDMs(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	ts := time.Now()

	_ = d.EnsureChat(ctx, "ghost@s.whatsapp.net", KindDM)
	_ = d.InsertMessage(ctx, sysMsg("ghost@s.whatsapp.net", "idchg-1", ts))

	_ = d.UpsertChat(ctx, &Chat{JID: "empty@s.whatsapp.net", Kind: KindDM})

	_ = d.EnsureChat(ctx, "real@s.whatsapp.net", KindDM)
	_ = d.InsertMessage(ctx, sysMsg("real@s.whatsapp.net", "idchg-1", ts))
	_ = d.InsertMessage(ctx, msg("real@s.whatsapp.net", "m1", ts))

	_ = d.EnsureChat(ctx, "pin@s.whatsapp.net", KindDM)
	on := true
	_ = d.SetChatFlags(ctx, "pin@s.whatsapp.net", &on, nil, nil)
	_ = d.InsertMessage(ctx, sysMsg("pin@s.whatsapp.net", "idchg-1", ts))

	_ = d.EnsureChat(ctx, "unread@s.whatsapp.net", KindDM)
	_ = d.InsertMessage(ctx, sysMsg("unread@s.whatsapp.net", "idchg-1", ts))
	_ = d.SetUnread(ctx, "unread@s.whatsapp.net", 1)

	_ = d.EnsureChat(ctx, "quiet@g.us", KindGroup)

	got, err := d.ListChats(ctx, true)
	if err != nil {
		t.Fatalf("ListChats: %v", err)
	}
	seen := chatJIDs(got)
	for _, jid := range []string{
		"ghost@s.whatsapp.net", "empty@s.whatsapp.net",
	} {
		if seen[PeerID(jid)] {
			t.Errorf("%s should be hidden", jid)
		}
	}
	for _, jid := range []string{
		"real@s.whatsapp.net", "pin@s.whatsapp.net", "unread@s.whatsapp.net", "quiet@g.us",
	} {
		if !seen[PeerID(jid)] {
			t.Errorf("%s should be listed", jid)
		}
	}
}

// A merged LID/PN pair keeps showing under the PN row when only the LID
// side holds system noise — the notice stays readable inside the transcript.
func TestListChatsPhantomMergedPair(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	ts := time.Now()

	_ = d.UpsertAltJID(ctx, "111@lid", "pn@s.whatsapp.net")
	_ = d.EnsureChat(ctx, "111@lid", KindDM)
	_ = d.InsertMessage(ctx, sysMsg("111@lid", "idchg-1", ts))
	_ = d.EnsureChat(ctx, "pn@s.whatsapp.net", KindDM)
	_ = d.InsertMessage(ctx, msg("pn@s.whatsapp.net", "m1", ts))

	got, err := d.ListChats(ctx, true)
	if err != nil {
		t.Fatalf("ListChats: %v", err)
	}
	if len(got) != 1 || got[0].JID != "pn@s.whatsapp.net" {
		t.Fatalf("merged pair should list once as PN chat: %+v", got)
	}
}

// HasRealMessages drives the IdentityChange gate — system-only and empty
// chats don't qualify; any other type does.
func TestHasRealMessages(t *testing.T) {
	d := openTemp(t)
	ctx := context.Background()
	ts := time.Now()

	if d.HasRealMessages(ctx, "none@s.whatsapp.net") {
		t.Error("no messages at all")
	}
	_ = d.InsertMessage(ctx, sysMsg("sys@s.whatsapp.net", "idchg-1", ts))
	if d.HasRealMessages(ctx, "sys@s.whatsapp.net") {
		t.Error("system-only should not count")
	}
	call := msg("call@s.whatsapp.net", "c1", ts)
	call.Type = TCall
	_ = d.InsertMessage(ctx, call)
	if !d.HasRealMessages(ctx, "call@s.whatsapp.net") {
		t.Error("call rows are real traffic")
	}
	_ = d.InsertMessage(ctx, sysMsg("mix@s.whatsapp.net", "idchg-1", ts))
	_ = d.InsertMessage(ctx, msg("mix@s.whatsapp.net", "m1", ts))
	if !d.HasRealMessages(ctx, "mix@s.whatsapp.net") {
		t.Error("system + text counts")
	}
}

package qtui

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	qt "github.com/mappu/miqt/qt6"

	"github.com/frontyard/whodat/internal/core"
	"github.com/frontyard/whodat/internal/protocol"
	"github.com/frontyard/whodat/internal/store"
)

const pageSize = 60

// Role IDs shared by the models — keep the same numbering in the QML
// delegates (referenced by name through roleNames).
const (
	roleJID        = int(qt.UserRole) + 1
	roleName       = int(qt.UserRole) + 2
	rolePreview    = int(qt.UserRole) + 3
	roleTime       = int(qt.UserRole) + 4
	roleUnread     = int(qt.UserRole) + 5
	rolePinned     = int(qt.UserRole) + 6
	roleMuted      = int(qt.UserRole) + 7
	roleAvatarPath = int(qt.UserRole) + 8
	roleIsGroup    = int(qt.UserRole) + 9

	// message roles
	roleMsgID        = int(qt.UserRole) + 20
	roleMsgType      = int(qt.UserRole) + 21
	roleText         = int(qt.UserRole) + 22
	roleHTML         = int(qt.UserRole) + 23
	roleFromMe       = int(qt.UserRole) + 24
	roleSenderName   = int(qt.UserRole) + 25
	roleStatus       = int(qt.UserRole) + 26
	roleStarred      = int(qt.UserRole) + 27
	roleEdited       = int(qt.UserRole) + 28
	roleRevoked      = int(qt.UserRole) + 29
	roleMediaPath    = int(qt.UserRole) + 30
	roleMediaMIME    = int(qt.UserRole) + 31
	roleMediaName    = int(qt.UserRole) + 32
	roleMediaSize    = int(qt.UserRole) + 33
	roleMediaW       = int(qt.UserRole) + 34
	roleMediaH       = int(qt.UserRole) + 35
	roleQuotedSender = int(qt.UserRole) + 36
	roleQuotedText   = int(qt.UserRole) + 37
	roleQuotedID     = int(qt.UserRole) + 38
	roleReactions    = int(qt.UserRole) + 39
	roleDayLabel     = int(qt.UserRole) + 40
	rolePollData     = int(qt.UserRole) + 41
	roleSenderJID    = int(qt.UserRole) + 42
	roleDuration     = int(qt.UserRole) + 43
	roleViewOnce     = int(qt.UserRole) + 44
	roleExpiresAt    = int(qt.UserRole) + 45
	roleWaveform     = int(qt.UserRole) + 46
	roleMediaBytes   = int(qt.UserRole) + 47
	roleForwarded    = int(qt.UserRole) + 48
	roleFwdFrom      = int(qt.UserRole) + 49
	roleLinkPreview  = int(qt.UserRole) + 50
	roleMediaThumb   = int(qt.UserRole) + 51
)

var chatRoleNames = map[int][]byte{
	roleJID:        []byte("jid"),
	roleName:       []byte("name"),
	rolePreview:    []byte("preview"),
	roleTime:       []byte("timeStr"),
	roleUnread:     []byte("unread"),
	rolePinned:     []byte("pinned"),
	roleMuted:      []byte("muted"),
	roleAvatarPath: []byte("avatarPath"),
	roleIsGroup:    []byte("isGroup"),
}

var msgRoleNames = map[int][]byte{
	roleMsgID:        []byte("msgId"),
	roleMsgType:      []byte("msgType"),
	roleText:         []byte("text"),
	roleHTML:         []byte("html"),
	roleFromMe:       []byte("fromMe"),
	roleSenderName:   []byte("senderName"),
	roleSenderJID:    []byte("senderJid"),
	roleTime:         []byte("timeStr"),
	roleStatus:       []byte("status"),
	roleStarred:      []byte("starred"),
	roleEdited:       []byte("edited"),
	roleRevoked:      []byte("revoked"),
	roleMediaPath:    []byte("mediaPath"),
	roleMediaMIME:    []byte("mediaMIME"),
	roleMediaName:    []byte("mediaName"),
	roleMediaSize:    []byte("mediaSize"),
	roleMediaW:       []byte("mediaW"),
	roleMediaH:       []byte("mediaH"),
	roleQuotedSender: []byte("quotedSender"),
	roleQuotedText:   []byte("quotedText"),
	roleQuotedID:     []byte("quotedId"),
	roleReactions:    []byte("reactions"),
	roleDayLabel:     []byte("dayLabel"),
	rolePollData:     []byte("pollData"),
	roleDuration:     []byte("duration"),
	roleViewOnce:     []byte("viewOnce"),
	roleExpiresAt:    []byte("expiresAt"),
	roleWaveform:     []byte("waveform"),
	roleMediaBytes:   []byte("mediaBytes"),
	roleForwarded:    []byte("forwarded"),
	roleFwdFrom:      []byte("fwdFrom"),
	roleLinkPreview:  []byte("linkPreview"),
	roleMediaThumb:   []byte("mediaThumb"),
}

func qvStr(s string) *qt.QVariant { return qt.NewQVariant14(s) }
func qvBool(b bool) *qt.QVariant  { return qt.NewQVariant8(b) }
func qvInt(i int) *qt.QVariant    { return qt.NewQVariant4(i) }
func qvEmpty() *qt.QVariant       { return qt.NewQVariant() }

// ---------------------------------------------------------------- ChatList

// ChatListModel backs the sidebar ListView. Index 0 is the top of the list:
// pinned chats first, then most-recent activity. Archived chats live behind
// the archived-mode toggle.
type ChatListModel struct {
	*qt.QAbstractListModel
	h        *Host
	chats    []*store.Chat
	byJID    map[store.PeerID]int
	archived []*store.Chat
	filter   string
	inArch   bool
	avAsked  map[store.PeerID]bool
}

func NewChatListModel(h *Host) *ChatListModel {
	m := &ChatListModel{
		QAbstractListModel: qt.NewQAbstractListModel(),
		h:                  h,
		byJID:              map[store.PeerID]int{},
		avAsked:            map[store.PeerID]bool{},
	}
	m.OnRowCount(func(parent *qt.QModelIndex) int { return len(m.chats) })
	m.OnRoleNames(func(super func() map[int][]byte) map[int][]byte { return chatRoleNames })
	m.OnData(func(idx *qt.QModelIndex, role int) *qt.QVariant {
		if !idx.IsValid() || idx.Row() >= len(m.chats) {
			return qvEmpty()
		}
		c := m.chats[idx.Row()]
		switch role {
		case roleJID:
			return qvStr(c.JID.String())
		case roleName:
			return qvStr(m.h.chatTitle(c))
		case rolePreview:
			return qvStr(m.h.app.ResolveMentions(context.Background(), c.JID, c.LastMsgPreview))
		case roleTime:
			return qvStr(core.FmtStamp(c.LastMsgTS))
		case roleUnread:
			return qvInt(c.Unread)
		case rolePinned:
			return qvBool(c.Pinned)
		case roleMuted:
			return qvBool(c.Muted)
		case roleAvatarPath:
			return qvStr(c.AvatarPath)
		case roleIsGroup:
			return qvBool(c.Kind == store.KindGroup)
		}
		return qvEmpty()
	})
	return m
}

// Reload refetches chats and resets the model — Qt thread.
func (m *ChatListModel) Reload() {
	go func() {
		all, err := m.h.app.DB.ListChats(context.Background(), true)
		if err != nil {
			return
		}
		mt(func() { m.applyAll(all) })
	}()
}

// applyAll splits and filters the full list — Qt thread.
func (m *ChatListModel) applyAll(all []*store.Chat) {
	m.archived = m.archived[:0]
	var act []*store.Chat
	for _, c := range all {
		if c.Archived {
			m.archived = append(m.archived, c)
		} else if m.matchFilter(c) {
			act = append(act, c)
		}
	}
	show := act
	if m.inArch {
		show = []*store.Chat{}
		for _, c := range m.archived {
			if m.matchFilter(c) {
				show = append(show, c)
			}
		}
	}
	sortChats(show)

	m.BeginResetModel()
	m.chats = show
	m.byJID = map[store.PeerID]int{}
	for i, c := range show {
		m.byJID[c.JID] = i
	}
	m.EndResetModel()

	m.h.setState("archivedCount", len(m.archived))
	if len(m.archived) == 0 && m.inArch {
		m.SetArchivedMode(false)
	}
	m.prefetchAvatars(all)
}

// matchFilter implements the sidebar search: name+jid+preview substring.
func (m *ChatListModel) matchFilter(c *store.Chat) bool {
	if m.filter == "" {
		return true
	}
	name := c.Name + " " + c.LastMsgPreview + " " + c.JID.String()
	return strings.Contains(strings.ToLower(name), m.filter)
}

func sortChats(cs []*store.Chat) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.Pinned != b.Pinned {
			return a.Pinned
		}
		if !a.LastMsgTS.Equal(b.LastMsgTS) {
			return a.LastMsgTS.After(b.LastMsgTS)
		}
		return a.RowID > b.RowID
	})
}

// SetFilter applies the sidebar search text — Qt thread.
func (m *ChatListModel) SetFilter(f string) {
	m.filter = strings.ToLower(strings.TrimSpace(f))
	m.Reload()
}

// SetArchivedMode toggles the archived-only view — Qt thread.
func (m *ChatListModel) SetArchivedMode(on bool) {
	m.inArch = on
	m.Reload()
}

// OnChatUpdated refreshes one row (or reloads when unknown) — Qt thread.
func (m *ChatListModel) OnChatUpdated(jid store.PeerID) {
	go func() {
		c, err := m.h.app.DB.GetChat(context.Background(), jid)
		if err != nil || c == nil {
			return
		}
		mt(func() {
			idx, ok := m.byJID[jid]
			if !ok {
				m.Reload()
				return
			}
			old := m.chats[idx]
			m.chats[idx] = c
			top := m.Index(idx, 0, qt.NewQModelIndex())
			m.DataChanged(top, top)
			// Resort when the ordering keys may have moved (pin, activity).
			if old.Pinned != c.Pinned || !old.LastMsgTS.Equal(c.LastMsgTS) || old.RowID != c.RowID {
				m.Reload()
			}
		})
	}()
}

// OnChatGone removes a row — Qt thread.
func (m *ChatListModel) OnChatGone(jid store.PeerID) {
	idx, ok := m.byJID[jid]
	if !ok {
		return
	}
	m.BeginRemoveRows(qt.NewQModelIndex(), idx, idx)
	m.chats = append(m.chats[:idx], m.chats[idx+1:]...)
	m.byJID = map[store.PeerID]int{}
	for i, c := range m.chats {
		m.byJID[c.JID] = i
	}
	m.EndRemoveRows()
}

// prefetchAvatars lazy-fetches profile pictures for chats without one —
// each JID is requested at most once.
func (m *ChatListModel) prefetchAvatars(chats []*store.Chat) {
	var missing []store.PeerID
	for _, c := range chats {
		if c.AvatarPath != "" || m.avAsked[c.JID] {
			continue
		}
		m.avAsked[c.JID] = true
		missing = append(missing, c.JID)
	}
	if len(missing) == 0 {
		return
	}
	go func() {
		ctx := context.Background()
		for _, jid := range missing {
			_ = m.h.app.RefreshAvatar(ctx, jid)
		}
	}()
}

// ---------------------------------------------------------------- Messages

// msgRow wraps a message plus its computed day-separator label.
type msgRow struct {
	m   *store.Message
	day string // non-empty: delegate draws a day separator above the bubble
}

// MessageListModel backs the chat transcript. Index 0 is the newest message
// (the QML ListView runs BottomToTop, so index 0 sits at the visual bottom —
// the view is naturally bottom-pinned and fetchMore pages older history at
// the model end).
type MessageListModel struct {
	*qt.QAbstractListModel
	h           *Host
	jid         store.PeerID
	isGroup     bool
	items       []*msgRow
	byID        map[string]int
	oldest      time.Time
	oldestRowID int64
	loading     atomic.Bool
	exhaust     bool
	pvAsked     map[string]bool // preview-thumb fetches already requested (Qt thread)
}

// canServerBackfill reports whether the chat's service can pull older
// history from the server (protocol.HistoryFetcher).
func (m *MessageListModel) canServerBackfill() bool {
	svc := m.h.app.SvcFor(m.jid)
	if svc == nil {
		return false
	}
	_, ok := svc.(protocol.HistoryFetcher)
	return ok
}

func NewMessageListModel(h *Host) *MessageListModel {
	m := &MessageListModel{
		QAbstractListModel: qt.NewQAbstractListModel(),
		h:                  h,
		byID:               map[string]int{},
		pvAsked:            map[string]bool{},
	}
	m.OnRowCount(func(parent *qt.QModelIndex) int { return len(m.items) })
	m.OnRoleNames(func(super func() map[int][]byte) map[int][]byte { return msgRoleNames })
	m.OnCanFetchMore(func(super func(parent *qt.QModelIndex) bool, parent *qt.QModelIndex) bool {
		return m.jid != "" && !m.exhaust && !m.loading.Load()
	})
	m.OnFetchMore(func(super func(parent *qt.QModelIndex), parent *qt.QModelIndex) {
		m.loadMore()
	})
	m.OnData(func(idx *qt.QModelIndex, role int) *qt.QVariant {
		if !idx.IsValid() || idx.Row() >= len(m.items) {
			return qvEmpty()
		}
		it := m.items[idx.Row()]
		msg := it.m
		switch role {
		case roleMsgID:
			return qvStr(msg.ID)
		case roleMsgType:
			return qvStr(string(msg.Type))
		case roleText:
			return qvStr(msg.Text)
		case roleHTML:
			return qvStr(m.h.msgHTML(msg))
		case roleFromMe:
			return qvBool(msg.FromMe)
		case roleSenderName:
			return qvStr(m.h.senderName(msg))
		case roleSenderJID:
			return qvStr(msg.SenderJID.String())
		case roleTime:
			return qvStr(core.FmtTime(msg.TS))
		case roleStatus:
			return qvStr(string(msg.Status))
		case roleStarred:
			return qvBool(msg.Starred)
		case roleEdited:
			return qvBool(msg.Edited)
		case roleRevoked:
			return qvBool(msg.Revoked)
		case roleMediaPath:
			return qvStr(msg.MediaPath)
		case roleMediaMIME:
			return qvStr(msg.MediaMIME)
		case roleMediaName:
			return qvStr(msg.MediaName)
		case roleMediaSize:
			return qvStr(core.FmtSize(msg.MediaSize))
		case roleMediaW:
			return qvInt(msg.Width)
		case roleMediaH:
			return qvInt(msg.Height)
		case roleQuotedSender:
			return qvStr(m.h.quotedSender(msg))
		case roleQuotedText:
			return qvStr(msg.QuotedText)
		case roleQuotedID:
			return qvStr(msg.QuotedID)
		case roleReactions:
			return qvStr(reactionsJSON(msg, m.h.reactorWho))
		case roleDayLabel:
			return qvStr(it.day)
		case rolePollData:
			return qvStr(m.h.pollJSON(msg))
		case roleDuration:
			return qvInt(msg.Duration)
		case roleViewOnce:
			return qvBool(msg.ViewOnce)
		case roleExpiresAt:
			if msg.ExpiresAt.IsZero() {
				return qvInt(0)
			}
			return qt.NewQVariant6(msg.ExpiresAt.Unix())
		case roleWaveform:
			return qvStr(waveformJSON(msg))
		case roleMediaBytes:
			return qt.NewQVariant6(msg.MediaSize)
		case roleForwarded:
			return qvBool(msg.Forwarded)
		case roleFwdFrom:
			return qvStr(msg.ForwardedFrom)
		case roleLinkPreview:
			return qvStr(linkPreviewJSON(msg))
		case roleMediaThumb:
			return qvStr(msg.MediaThumb)
		}
		return qvEmpty()
	})
	return m
}

// SetChat switches the model to a conversation — Qt thread. Pass "" to clear.
func (m *MessageListModel) SetChat(jid store.PeerID, isGroup bool) {
	m.BeginResetModel()
	m.jid = jid
	m.isGroup = isGroup
	m.items = nil
	m.byID = map[string]int{}
	m.oldest = time.Time{}
	m.oldestRowID = 0
	m.exhaust = jid == ""
	m.loading.Store(false)
	m.pvAsked = map[string]bool{}
	m.EndResetModel()
}

// seed fills the first page after SetChat — Qt thread.
func (m *MessageListModel) seed(msgs []*store.Message) {
	// DB order is (ts,rowid) DESC = newest first = model order.
	m.BeginResetModel()
	m.items = nil
	m.byID = map[string]int{}
	for _, msg := range msgs {
		m.items = append(m.items, &msgRow{m: msg})
		m.byID[msg.ID] = len(m.items) - 1
	}
	m.fixDayLabels()
	m.EndResetModel()
	if len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		m.oldest, m.oldestRowID = last.TS, last.RowID
	}
	if len(msgs) < pageSize && !m.canServerBackfill() {
		m.exhaust = true
	}
	m.ensurePreviewThumbs()
}

// loadMore pages older history at the model end — may be called from the
// fetchMore virtual (Qt thread) or from jumpTo.
func (m *MessageListModel) loadMore() {
	if m.loading.Load() || m.exhaust || m.jid == "" || m.oldest.IsZero() {
		return
	}
	m.loading.Store(true)
	jid, before, beforeID := m.jid, m.oldest, m.oldestRowID
	go func() {
		msgs, err := m.h.app.DB.MessagesBefore(context.Background(), jid, before, beforeID, pageSize)
		if err == nil && len(msgs) < pageSize {
			// Local history ran short — a server-backed service can pull the
			// next page from the network, then re-read the local window.
			if hf, ok := m.h.app.SvcFor(jid).(protocol.HistoryFetcher); ok {
				_, _ = hf.FetchEarlier(context.Background(), jid, before, pageSize)
				msgs, err = m.h.app.DB.MessagesBefore(context.Background(), jid, before, beforeID, pageSize)
			}
		}
		mt(func() {
			m.loading.Store(false)
			if err != nil || len(msgs) == 0 {
				m.exhaust = true
				return
			}
			if len(msgs) < pageSize {
				m.exhaust = true
			}
			last := msgs[len(msgs)-1]
			m.oldest, m.oldestRowID = last.TS, last.RowID
			first := len(m.items)
			m.BeginInsertRows(qt.NewQModelIndex(), first, first+len(msgs)-1)
			for _, msg := range msgs {
				m.items = append(m.items, &msgRow{m: msg})
				m.byID[msg.ID] = len(m.items) - 1
			}
			m.EndInsertRows()
			// Boundary day labels may change at the seam.
			m.fixDayLabels()
			m.emitRange(first-1, first+len(msgs)-1, roleDayLabel)
			m.ensurePreviewThumbs()
		})
	}()
}

// fixDayLabels recomputes day-separator labels across the list.
// In newest-first order a message gets a label when the next *older* item
// falls on a different day (or it's the oldest loaded).
func (m *MessageListModel) fixDayLabels() {
	for i, it := range m.items {
		day := ""
		if i+1 >= len(m.items) || !core.SameDay(m.items[i+1].m.TS, it.m.TS) {
			day = core.FmtDay(it.m.TS)
		}
		it.day = day
	}
}

// refreshReactor re-emits the reactions role on every row jid reacted to —
// called after their avatar finishes downloading so pills pick up the face.
// Qt thread.
func (m *MessageListModel) refreshReactor(jid store.PeerID) {
outer:
	for i, it := range m.items {
		if it.m == nil {
			continue
		}
		for _, jids := range it.m.Reactions {
			for _, j := range jids {
				if j == jid {
					m.emitRange(i, i, roleReactions)
					continue outer
				}
			}
		}
	}
}

// emitRange marks rows [a,b] changed for one role — Qt thread.
func (m *MessageListModel) emitRange(a, b int, role int) {
	if a < 0 {
		a = 0
	}
	if b >= len(m.items) || a > b {
		return
	}
	m.DataChanged2(m.Index(a, 0, qt.NewQModelIndex()), m.Index(b, 0, qt.NewQModelIndex()), []int{role})
}

// OnMessage inserts or updates a row — Qt thread.
func (m *MessageListModel) OnMessage(chatJID store.PeerID, msg *store.Message) {
	if m.jid == "" || msg == nil || !m.h.sameChat(chatJID, m.jid) {
		return
	}
	if i, ok := m.byID[msg.ID]; ok {
		m.items[i].m = msg
		m.fixDayLabels()
		m.emitRange(i-1, i+1, roleDayLabel)
		idx := m.Index(i, 0, qt.NewQModelIndex())
		m.DataChanged(idx, idx)
		return
	}
	m.BeginInsertRows(qt.NewQModelIndex(), 0, 0)
	m.items = append([]*msgRow{{m: msg}}, m.items...)
	m.byID = map[string]int{}
	for i, it := range m.items {
		m.byID[it.m.ID] = i
	}
	m.EndInsertRows()
	m.fixDayLabels()
	m.emitRange(0, 1, roleDayLabel)
	m.ensurePreviewThumbs()
}

// OnReceipt updates delivery status — Qt thread.
func (m *MessageListModel) OnReceipt(chatJID store.PeerID, msg *store.Message) {
	if m.jid == "" || msg == nil || !m.h.sameChat(chatJID, m.jid) {
		return
	}
	if i, ok := m.byID[msg.ID]; ok {
		m.items[i].m.Status = msg.Status
		m.emitRange(i, i, roleStatus)
	}
}

// Remove drops a row locally (delete-for-me) — Qt thread.
func (m *MessageListModel) Remove(id string) {
	i, ok := m.byID[id]
	if !ok {
		return
	}
	m.BeginRemoveRows(qt.NewQModelIndex(), i, i)
	m.items = append(m.items[:i], m.items[i+1:]...)
	delete(m.byID, id)
	for j := i; j < len(m.items); j++ {
		m.byID[m.items[j].m.ID] = j
	}
	m.EndRemoveRows()
	m.fixDayLabels()
	if i > 0 {
		m.emitRange(i-1, i-1, roleDayLabel)
	}
}

// IndexOf returns the row for a message id (or -1) — Qt thread.
func (m *MessageListModel) IndexOf(id string) int {
	if i, ok := m.byID[id]; ok {
		return i
	}
	return -1
}

// JumpTo pages history until id is loaded, then returns its row index.
// Reply: {index: n} or {index: -1}.
func (m *MessageListModel) JumpTo(id string, done func(int)) {
	go func() {
		for i := 0; i < 40; i++ {
			found, exhausted := false, false
			mainthreadWait(func() {
				found = m.IndexOf(id) >= 0
				exhausted = m.exhaust
				if !found && !exhausted {
					m.loadMore()
				}
			})
			if found || exhausted {
				break
			}
			time.Sleep(120 * time.Millisecond)
		}
		mt(func() { done(m.IndexOf(id)) })
	}()
}

// ensurePreviewThumbs lazy-fetches link-card images for rows that carry a
// protocol ref but no local file yet — the same lazy contract FetchMedia
// gives attachments. Qt thread; each message is requested at most once.
func (m *MessageListModel) ensurePreviewThumbs() {
	for _, it := range m.items {
		msg := it.m
		if msg == nil || msg.Preview == nil || msg.Preview.Thumb != "" || len(msg.Preview.Ref) == 0 {
			continue
		}
		key := msg.ChatJID.String() + "\x00" + msg.ID
		if m.pvAsked[key] {
			continue
		}
		m.pvAsked[key] = true
		svc, ok := m.h.app.SvcFor(msg.ChatJID).(protocol.PreviewThumber)
		if !ok {
			continue
		}
		go func(msg *store.Message) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, _ = svc.FetchPreviewThumb(ctx, msg)
		}(msg)
	}
}

// helpers -----------------------------------------------------------------

// reactorWho resolves a reactor JID for the who-reacted popup — display
// name ("You" for our own PN/LID JIDs) plus the cached avatar path. A
// cache miss kicks off a one-shot fetch (own account and group-only
// participants have no chat row, so the prefetch pass never reaches them);
// refreshReactor repaints the chips once the file lands.
func (h *Host) reactorWho(jid store.PeerID) whoEnt {
	ctx := context.Background()
	w := whoEnt{
		Name:   h.app.ReactorName(ctx, jid),
		Avatar: h.app.ReactorAvatar(ctx, jid),
	}
	if w.Avatar == "" && !h.rvAsked[jid] {
		h.rvAsked[jid] = true
		go func() {
			// Reactions may key the peer by an alt id (LID vs PN) that
			// the profile-pic endpoint won't accept — try every form.
			forms := append([]store.PeerID{jid}, h.app.DB.AltJIDs(ctx, jid)...)
			for _, j := range forms {
				if err := h.app.RefreshAvatar(ctx, j); err == nil {
					break
				}
			}
			mt(func() { h.msgs.refreshReactor(jid) })
		}()
	}
	return w
}

// whoEnt is one reactor in the who-reacted popup payload.
type whoEnt struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

// reactionsJSON flattens the reaction map into a JSON array for the
// delegate: [{emoji:"👍",n:2,who:[{name:"Jane",avatar:"/path"}]}]. whoFor
// resolves a reactor JID to name + avatar; nil keeps the raw JID.
func reactionsJSON(m *store.Message, whoFor func(jid store.PeerID) whoEnt) string {
	if len(m.Reactions) == 0 {
		return ""
	}
	type rct struct {
		Emoji string   `json:"emoji"`
		N     int      `json:"n"`
		Who   []whoEnt `json:"who"`
	}
	var out []rct
	for e, jids := range m.Reactions {
		who := make([]whoEnt, len(jids))
		for i, j := range jids {
			who[i] = whoEnt{Name: j.String()}
			if whoFor != nil {
				who[i] = whoFor(j)
			}
		}
		out = append(out, rct{Emoji: e, N: len(jids), Who: who})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N > out[j].N })
	b, _ := json.Marshal(out)
	return string(b)
}

// ---------------------------------------------------------------- bridge --

// linkPreviewJSON serializes the link card for the delegate — the stored
// LinkPreview shape {url,title,desc,site,thumb,large,pending}; "" when the
// row carries none.
func linkPreviewJSON(m *store.Message) string {
	if m.Preview == nil {
		return ""
	}
	b, err := json.Marshal(m.Preview)
	if err != nil {
		return ""
	}
	return string(b)
}

// waveformJSON flattens voice-note amplitudes to a JSON array.
func waveformJSON(m *store.Message) string {
	if len(m.Waveform) == 0 {
		return ""
	}
	b, _ := json.Marshal(m.Waveform)
	return string(b)
}

// mainthreadWait runs fn on the Qt thread and blocks until done — for
// goroutines that need a model read before deciding.
func mainthreadWait(f func()) {
	done := make(chan struct{})
	mt(func() {
		f()
		close(done)
	})
	<-done
}

package wa

import (
	"time"

	"github.com/polymorfa/hypermeow/proto/waE2E"
	"github.com/polymorfa/hypermeow/types"
	"github.com/polymorfa/hypermeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/frontyard/whodat/internal/media"
	"github.com/frontyard/whodat/internal/store"
)

// actionKind tells the service what to do with a converted message.
type actionKind int

const (
	actInsert   actionKind = iota
	actReact               // apply/remove a reaction on target
	actRevoke              // mark target revoked
	actEdit                // patch target text
	actPollVote            // apply a poll ballot on target
	actDisapp              // disappearing-message timer changed
	actSkip                // nothing to persist
)

type msgAction struct {
	kind      actionKind
	targetID  string             // for react/revoke/edit/vote
	emoji     string             // react ("" = removal)
	senderJID store.PeerID       // react / vote caster
	newText   string             // edit
	pv        *store.LinkPreview // edit: card carried by the new body (nil clears)
	voteEvt   *events.Message    // poll vote: real event for secret decrypt
	disappSec int64              // disappearing timer seconds
}

// unwrap strips FutureProof wrappers (view-once, ephemeral, doc-with-caption,
// edited) down to the real payload and reports what it found.
func unwrap(wm *waE2E.Message) (inner *waE2E.Message, viewOnce bool) {
	for i := 0; i < 4 && wm != nil; i++ {
		switch {
		case wm.GetViewOnceMessage() != nil,
			wm.GetViewOnceMessageV2() != nil,
			wm.GetViewOnceMessageV2Extension() != nil:
			viewOnce = true
		case wm.GetEphemeralMessage() != nil,
			wm.GetDocumentWithCaptionMessage() != nil,
			wm.GetEditedMessage() != nil:
		default:
			return wm, viewOnce
		}
		switch {
		case wm.GetViewOnceMessage() != nil:
			wm = wm.GetViewOnceMessage().GetMessage()
		case wm.GetViewOnceMessageV2() != nil:
			wm = wm.GetViewOnceMessageV2().GetMessage()
		case wm.GetViewOnceMessageV2Extension() != nil:
			wm = wm.GetViewOnceMessageV2Extension().GetMessage()
		case wm.GetEphemeralMessage() != nil:
			wm = wm.GetEphemeralMessage().GetMessage()
		case wm.GetDocumentWithCaptionMessage() != nil:
			wm = wm.GetDocumentWithCaptionMessage().GetMessage()
		default:
			wm = wm.GetEditedMessage().GetMessage()
		}
	}
	return wm, viewOnce
}

// ephemeralCtx returns the ContextInfo of the innermost message — where
// EphemeralExpiration lives for disappearing-message wrappers.
func ephemeralCtx(m *waE2E.Message) *waE2E.ContextInfo {
	switch {
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetContextInfo()
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetContextInfo()
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetContextInfo()
	case m.GetAudioMessage() != nil:
		return m.GetAudioMessage().GetContextInfo()
	case m.GetDocumentMessage() != nil:
		return m.GetDocumentMessage().GetContextInfo()
	case m.GetStickerMessage() != nil:
		return m.GetStickerMessage().GetContextInfo()
	}
	return nil
}

func jidKind(j types.JID) store.ChatKind {
	switch j.Server {
	case types.GroupServer:
		return store.KindGroup
	case "broadcast":
		return store.KindBroadcast
	case "newsletter":
		return store.KindNewsletter
	default:
		return store.KindDM
	}
}

// textOf extracts display text from any message body type.
func textOf(m *waE2E.Message) (string, store.MsgType) {
	if m == nil {
		return "", store.TText
	}
	switch {
	case m.GetConversation() != "":
		return m.GetConversation(), store.TText
	case m.GetExtendedTextMessage() != nil:
		return m.GetExtendedTextMessage().GetText(), store.TText
	case m.GetImageMessage() != nil:
		return m.GetImageMessage().GetCaption(), store.TImage
	case m.GetVideoMessage() != nil:
		return m.GetVideoMessage().GetCaption(), store.TVideo
	case m.GetAudioMessage() != nil:
		if m.GetAudioMessage().GetPTT() {
			return "", store.TPTT
		}
		return "", store.TAudio
	case m.GetDocumentMessage() != nil:
		d := m.GetDocumentMessage()
		if d.GetCaption() != "" {
			return d.GetCaption(), store.TDocument
		}
		return d.GetFileName(), store.TDocument
	case m.GetStickerMessage() != nil:
		return "", store.TSticker
	case m.GetLocationMessage() != nil, m.GetLiveLocationMessage() != nil:
		return "", store.TLocation
	case len(m.GetContactsArrayMessage().GetContacts()) > 0,
		m.GetContactMessage() != nil:
		return "", store.TContact
	case m.GetPollCreationMessage() != nil:
		return m.GetPollCreationMessage().GetName(), store.TPoll
	case m.GetPollCreationMessageV2() != nil:
		return m.GetPollCreationMessageV2().GetName(), store.TPoll
	case m.GetPollCreationMessageV3() != nil:
		return m.GetPollCreationMessageV3().GetName(), store.TPoll
	case m.GetProtocolMessage() != nil:
		return "", store.TSystem
	}
	return "", store.TSystem
}

// preview renders the one-line sidebar summary.
func preview(m *store.Message) string {
	body := m.Text
	if body == "" {
		switch m.Type {
		case store.TImage:
			body = "Photo"
		case store.TVideo:
			body = "Video"
		case store.TAudio:
			body = "Audio"
		case store.TPTT:
			body = "Voice message"
		case store.TDocument:
			body = m.MediaName
			if body == "" {
				body = "Document"
			}
		case store.TSticker:
			body = "Sticker"
		case store.TLocation:
			body = "Location"
		case store.TContact:
			body = "Contact"
		case store.TCall:
			body = "Call"
		case store.TPoll:
			body = "Poll"
		case store.TRevoked:
			body = "Message deleted"
		default:
			body = "Message"
		}
	}
	return body
}

func quotedOf(m *waE2E.Message) (stanza, participant, text string) {
	var ctx *waE2E.ContextInfo
	switch {
	case m.GetExtendedTextMessage() != nil:
		ctx = m.GetExtendedTextMessage().GetContextInfo()
	case m.GetImageMessage() != nil:
		ctx = m.GetImageMessage().GetContextInfo()
	case m.GetVideoMessage() != nil:
		ctx = m.GetVideoMessage().GetContextInfo()
	case m.GetAudioMessage() != nil:
		ctx = m.GetAudioMessage().GetContextInfo()
	case m.GetDocumentMessage() != nil:
		ctx = m.GetDocumentMessage().GetContextInfo()
	case m.GetStickerMessage() != nil:
		ctx = m.GetStickerMessage().GetContextInfo()
	case m.GetContactsArrayMessage() != nil:
		ctx = m.GetContactsArrayMessage().GetContextInfo()
	}
	if ctx == nil || ctx.GetStanzaID() == "" {
		return "", "", ""
	}
	qt, _ := textOf(ctx.GetQuotedMessage())
	return ctx.GetStanzaID(), ctx.GetParticipant(), qt
}

// convert builds a store.Message + action from the shared fields of a live
// event or a history-sync WebMessageInfo. chatJID/senderJID arrive already
// namespaced; the quoted sender comes off the wire bare and is prefixed here.
func (s *Service) convert(chatJID store.PeerID, id string, senderJID store.PeerID, senderName string, fromMe bool, ts time.Time, wm *waE2E.Message) (*store.Message, *msgAction) {
	out := &store.Message{
		ChatJID:    chatJID,
		ID:         id,
		SenderJID:  senderJID,
		SenderName: senderName,
		TS:         ts,
		FromMe:     fromMe,
		Status:     store.SNone,
	}
	if fromMe {
		out.Status = store.SSent
	}
	if wm == nil {
		// Protocol-only events with no WebMessageInfo have nothing to show —
		// inserting an empty system row just leaves a gap at the list bottom.
		return nil, &msgAction{kind: actSkip}
	}
	// View-once/ephemeral/doc-with-caption wrappers hide the real payload —
	// unwrap first, then store the inner message: FetchMedia's downloadable
	// gate reads the stored proto and needs to see the media type, not the
	// wrapper.
	wm, viewOnce := unwrap(wm)
	out.ViewOnce = viewOnce
	if blob, err := proto.Marshal(wm); err == nil {
		out.Proto = blob
	}
	// Disappearing messages carry the deadline in the inner ContextInfo;
	// IsForwarded rides the same slot.
	if ctx := ephemeralCtx(wm); ctx != nil {
		if ctx.GetExpiration() > 0 {
			out.ExpiresAt = ts.Add(time.Duration(ctx.GetExpiration()) * time.Second)
		}
		out.Forwarded = ctx.GetIsForwarded()
	}

	text, typ := textOf(wm)
	out.Text = text

	// Non-message control payloads first.
	if pm := wm.GetProtocolMessage(); pm != nil {
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			return nil, &msgAction{kind: actRevoke, targetID: pm.GetKey().GetID()}
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			em := pm.GetEditedMessage()
			t, _ := textOf(em)
			// The edited body re-carries (or drops) the link card.
			var pv *store.LinkPreview
			if ewm, _ := unwrap(em); ewm != nil {
				pv = previewFromExt(s.acct, ewm.GetExtendedTextMessage())
			}
			return nil, &msgAction{kind: actEdit, targetID: pm.GetKey().GetID(), newText: t, pv: pv}
		case waE2E.ProtocolMessage_EPHEMERAL_SETTING:
			return nil, &msgAction{kind: actDisapp, disappSec: int64(pm.GetEphemeralExpiration())}
		default:
			out.Type = store.TSystem
			out.Proto = nil
			return out, &msgAction{kind: actSkip}
		}
	}
	if pu := wm.GetPollUpdateMessage(); pu != nil {
		return nil, &msgAction{
			kind:      actPollVote,
			targetID:  pu.GetPollCreationMessageKey().GetID(),
			senderJID: senderJID,
		}
	}
	if rm := wm.GetReactionMessage(); rm != nil {
		return nil, &msgAction{
			kind:      actReact,
			targetID:  rm.GetKey().GetID(),
			emoji:     rm.GetText(),
			senderJID: senderJID,
		}
	}
	if wm.GetSenderKeyDistributionMessage() != nil {
		return nil, &msgAction{kind: actSkip}
	}

	out.Type = typ
	var qs string
	out.QuotedID, qs, out.QuotedText = quotedOf(wm)
	out.QuotedSender = s.cid(qs)
	if et := wm.GetExtendedTextMessage(); et != nil {
		out.Preview = previewFromExt(s.acct, et)
	}

	switch typ {
	case store.TImage:
		im := wm.GetImageMessage()
		out.MediaMIME = im.GetMimetype()
		out.MediaSize = int64(im.GetFileLength())
		out.Width = int(im.GetWidth())
		out.Height = int(im.GetHeight())
	case store.TVideo:
		vm := wm.GetVideoMessage()
		out.MediaMIME = vm.GetMimetype()
		out.MediaSize = int64(vm.GetFileLength())
		out.Width = int(vm.GetWidth())
		out.Height = int(vm.GetHeight())
		out.Duration = int(vm.GetSeconds())
	case store.TAudio, store.TPTT:
		am := wm.GetAudioMessage()
		out.MediaMIME = am.GetMimetype()
		out.MediaSize = int64(am.GetFileLength())
		out.Duration = int(am.GetSeconds())
	case store.TDocument:
		dm := wm.GetDocumentMessage()
		out.MediaMIME = dm.GetMimetype()
		out.MediaSize = int64(dm.GetFileLength())
		out.MediaName = dm.GetFileName()
	case store.TSticker:
		sm := wm.GetStickerMessage()
		out.MediaMIME = sm.GetMimetype()
		out.MediaSize = int64(sm.GetFileLength())
		out.Width = int(sm.GetWidth())
		out.Height = int(sm.GetHeight())
	case store.TLocation:
		lm := wm.GetLocationMessage()
		if lm != nil {
			out.Text = locationText(lm.GetDegreesLatitude(), lm.GetDegreesLongitude(), lm.GetName())
		} else if llm := wm.GetLiveLocationMessage(); llm != nil {
			out.Text = locationText(llm.GetDegreesLatitude(), llm.GetDegreesLongitude(), "Live location")
		}
	case store.TContact:
		out.Text = contactText(wm)
	case store.TPoll:
		pc := wm.GetPollCreationMessage()
		if pc == nil {
			pc = wm.GetPollCreationMessageV2()
		}
		if pc == nil {
			pc = wm.GetPollCreationMessageV3()
		}
		if pc != nil {
			for _, o := range pc.GetOptions() {
				out.PollOptions = append(out.PollOptions, o.GetOptionName())
			}
			out.PollMax = int(pc.GetSelectableOptionsCount())
		}
	case store.TSystem:
		// Crypto noise, key distribution, unknown types — keep only when it has text.
		out.Proto = nil
		if out.Text == "" {
			return nil, &msgAction{kind: actSkip}
		}
	}
	return out, &msgAction{kind: actInsert}
}

// previewFromExt reads the link card WhatsApp attaches to an
// ExtendedTextMessage — the matched URL plus sender-resolved title,
// description and an inline JPEG thumbnail. No URL means no card.
func previewFromExt(acct string, et *waE2E.ExtendedTextMessage) *store.LinkPreview {
	u := et.GetMatchedText()
	if u == "" {
		return nil
	}
	pv := &store.LinkPreview{
		URL:   u,
		Title: et.GetTitle(),
		Desc:  et.GetDescription(),
	}
	if b := et.GetJPEGThumbnail(); len(b) > 0 {
		if path, err := media.StageBytes(acct, b, ".jpg"); err == nil {
			pv.Thumb = path
		}
	}
	return pv
}

func locationText(lat, lon float64, name string) string {
	if name != "" {
		return name
	}
	return "Location"
}

func contactText(wm *waE2E.Message) string {
	if c := wm.GetContactMessage(); c != nil {
		return c.GetDisplayName()
	}
	if arr := wm.GetContactsArrayMessage(); arr != nil && len(arr.GetContacts()) > 0 {
		return arr.GetContacts()[0].GetDisplayName()
	}
	return "Contact"
}

// IsCallLog identifies call log stubs we render as "call" rows.
func IsCallLog(wm *waE2E.Message) bool {
	if wm == nil {
		return false
	}
	return wm.GetCallLogMesssage() != nil
}

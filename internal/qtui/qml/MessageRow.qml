import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material
import QtQuick.Effects

// MessageRow is the transcript delegate for one message. Roles come from
// MessageModel; the ListView runs BottomToTop (index 0 = newest = visual
// bottom).
Item {
    id: root

    required property string msgId
    required property string msgType
    required property string text
    required property string html
    required property bool fromMe
    required property string senderName
    required property string senderJid
    required property string timeStr
    required property string status
    required property bool starred
    required property bool edited
    required property bool revoked
    required property string mediaPath
    required property string mediaMIME
    required property string mediaName
    required property string mediaSize
    required property int mediaW
    required property int mediaH
    required property string quotedSender
    required property string quotedText
    required property string quotedId
    required property string reactions
    required property string dayLabel
    required property string pollData
    required property int duration
    required property bool viewOnce
    required property bool forwarded
    required property string fwdFrom
    required property string linkPreview
    required property var waveform
    required property int mediaBytes
    required property int index

    signal replyRequested(var id, var sender, var text)
    signal editRequested(var id, var text)
    signal forwardRequested(var id)
    signal jumpTo(var id)
    signal openViewer(string path)
    signal playVideo(string path)

    width: ListView.view ? ListView.view.width : 400
    implicitHeight: content.implicitHeight + content.y + 4

    // rowHover drives the floating react button's visibility.
    HoverHandler { id: rowHover }

    ColumnLayout {
        id: content
        width: parent.width
        y: dayLabel !== "" ? 6 : 2
        spacing: 2

        // ---- day separator ------------------------------------------
        Rectangle {
            visible: root.dayLabel !== ""
            implicitHeight: 26
            implicitWidth: dayLbl.implicitWidth + 24
            radius: 13
            color: Theme.surfaceContainerHigh
            Layout.alignment: Qt.AlignHCenter

            Label {
                id: dayLbl
                anchors.centerIn: parent
                text: root.dayLabel
                color: Theme.onSurfaceVariant
                font.pixelSize: 12
            }
        }

        // ---- system message ------------------------------------------
        Label {
            visible: root.msgType === "system" || root.msgType === "revoked"
            text: root.msgType === "revoked" ? "🚫 Message deleted" : root.text
            font.italic: true
            color: Theme.onSurfaceVariant
            font.pixelSize: 13
            Layout.alignment: Qt.AlignHCenter
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.Wrap
            Layout.maximumWidth: parent.width - 80
        }

        // ---- bubble --------------------------------------------------
        Rectangle {
            id: bubble
            visible: root.msgType !== "system" && root.msgType !== "revoked"
            implicitWidth: bubbleCol.implicitWidth + 20
            implicitHeight: bubbleCol.implicitHeight + 12
            radius: Theme.radiusSmall + 4
            color: root.fromMe ? Theme.bubbleOut : Theme.bubbleIn
            Layout.alignment: root.fromMe ? Qt.AlignRight : Qt.AlignLeft
            Layout.leftMargin: 12
            Layout.rightMargin: 12
            Layout.maximumWidth: parent.width * 0.72

            MouseArea {
                anchors.fill: parent
                acceptedButtons: Qt.RightButton
                onClicked: (m) => bubbleMenu.popup(m.x, m.y)
            }

            Menu {
                id: bubbleMenu
                WdMenuItem { text: "Reply"; onTriggered: root.replyRequested(root.msgId, root.senderName, root.text) }
                WdMenuItem {
                    text: "React…"
                    visible: root.canReact
                    height: visible ? implicitHeight : 0
                    onTriggered: Qt.callLater(reactPop.open)
                }
                WdMenuItem { text: "Copy"; enabled: root.text !== ""; onTriggered: copyHelper.copyText() }
                WdMenuItem { text: "Forward"; onTriggered: root.forwardRequested(root.msgId) }
                WdMenuItem {
                    visible: App.chatCapsStar !== false
                    height: visible ? implicitHeight : 0
                    text: root.starred ? "Unstar" : "Star"
                    onTriggered: Bridge.cmd("sendStar", {jid: App.chatJID, msgId: root.msgId, on: !root.starred}, function(ok, d, e) {
                        if (!ok) Bridge.cmd("notify", {msg: "Star failed: " + e});
                    })
                }
                WdMenuItem {
                    text: "Edit"
                    // invisible MenuItems still take a row in Menu's internal
                    // ListView — height must be collapsed too.
                    visible: root.fromMe && !root.revoked && root.msgType === "text"
                    height: visible ? implicitHeight : 0
                    onTriggered: root.editRequested(root.msgId, root.text)
                }
                WdMenuItem {
                    text: "Delete for everyone"
                    visible: root.fromMe && !root.revoked
                    height: visible ? implicitHeight : 0
                    onTriggered: Bridge.cmd("sendRevoke", {jid: App.chatJID, msgId: root.msgId}, function(ok, d, e) {
                        if (!ok) Bridge.cmd("notify", {msg: "Delete failed: " + e});
                    })
                }
                WdMenuItem {
                    text: "Delete for me"
                    onTriggered: Bridge.cmd("deleteForMe", {jid: App.chatJID, msgId: root.msgId})
                }
                WdMenuItem {
                    text: "Jump to quoted"
                    visible: root.quotedId !== ""
                    height: visible ? implicitHeight : 0
                    onTriggered: root.jumpTo(root.quotedId)
                }
            }

            // quick-react popup. Opened via
            // callLater so the context-menu's closing click can't dismiss it.
            Popup {
                id: reactPop
                padding: 4
                y: -implicitHeight - 6
                // Outgoing bubbles sit at the right window edge — anchor the
                // popup's right edge to the bubble's so it can't clip off.
                x: root.fromMe ? parent.width - implicitWidth : 0
                Row {
                    spacing: 2
                    Repeater {
                        model: ["👍", "❤️", "😂", "😮", "😢", "🙏"]
                        WdItemDelegate {
                            required property string modelData
                            width: 38; height: 38
                            contentItem: Label {
                                text: modelData
                                font.pixelSize: 22
                                horizontalAlignment: Text.AlignHCenter
                                verticalAlignment: Text.AlignVCenter
                            }
                            onClicked: {
                                reactPop.close();
                                root.reactBurst(modelData);
                                Bridge.cmd("sendReaction", {jid: App.chatJID, msgId: root.msgId, emoji: modelData}, function(ok, d, e) {
                                    if (!ok) Bridge.cmd("notify", {msg: "Reaction failed: " + e});
                                });
                            }
                        }
                    }
                }
            }

            TextEdit {
                id: copyHelper
                visible: false
                function copyText() { text = root.text; selectAll(); copy(); }
            }

            ColumnLayout {
                id: bubbleCol
                anchors.centerIn: parent
                spacing: 4

                // forwarded marker
                Label {
                    visible: root.forwarded
                    text: root.fwdFrom !== "" ? "Forwarded from " + root.fwdFrom : "Forwarded"
                    font.italic: true
                    color: root.fromMe ? Theme.onBubbleOut : Theme.onBubbleIn
                    opacity: 0.7
                    font.pixelSize: 12
                }

                // sender name (groups, incoming only)
                Label {
                    visible: !root.fromMe && root.senderName !== "" && root.isGroupMsg
                    text: root.senderName
                    color: Theme.secondary
                    font.bold: true
                    font.pixelSize: 13
                }

                // quoted block
                Rectangle {
                    visible: root.quotedText !== "" || root.quotedSender !== ""
                    Layout.fillWidth: true
                    // The quote must widen the bubble to fit its text (up to
                    // maxTextWidth), not just stretch to the body text width.
                    // TextMetrics is used because elided labels report their
                    // *clamped* implicitWidth, collapsing the sizing chain.
                    implicitWidth: Math.min(Math.max(quoteSenderMetrics.advanceWidth, quoteTextMetrics.advanceWidth) + 16, root.maxTextWidth())
                    implicitHeight: quoteCol.implicitHeight + 8
                    radius: 6
                    color: Qt.rgba(0, 0, 0, 0.15)

                    TextMetrics {
                        id: quoteSenderMetrics
                        font: quoteSenderLbl.font
                        text: root.quotedSender
                    }
                    TextMetrics {
                        id: quoteTextMetrics
                        font: quoteTextLbl.font
                        text: quoteTextLbl.text
                    }

                    Rectangle {
                        width: 3
                        height: parent.height
                        color: Theme.primary
                    }

                    Column {
                        id: quoteCol
                        anchors.left: parent.left
                        anchors.leftMargin: 10
                        anchors.top: parent.top
                        anchors.topMargin: 4
                        width: parent.width - 16
                        spacing: 1

                        Label {
                            id: quoteSenderLbl
                            text: root.quotedSender
                            color: Theme.primary
                            font.bold: true
                            font.pixelSize: 12
                            visible: text !== ""
                            width: parent.width
                            elide: Text.ElideRight
                        }
                        Label {
                            id: quoteTextLbl
                            text: root.quotedText.length > 90 ? root.quotedText.slice(0, 90) + "…" : root.quotedText
                            color: root.fromMe ? Theme.onBubbleOut : Theme.onBubbleIn
                            opacity: 0.8
                            font.pixelSize: 12
                            elide: Text.ElideRight
                            width: parent.width
                        }
                    }

                    MouseArea {
                        anchors.fill: parent
                        cursorShape: Qt.PointingHandCursor
                        onClicked: root.jumpTo(root.quotedId)
                    }
                }

                // attachment area (image/video/audio/ptt/document)
                MediaBlock {
                    msgId: root.msgId
                    msgType: root.msgType
                    mediaPath: root.mediaPath
                    mediaMIME: root.mediaMIME
                    mediaName: root.mediaName
                    mediaSize: root.mediaSize
                    mediaBytes: root.mediaBytes
                    mediaW: root.mediaW
                    mediaH: root.mediaH
                    duration: root.duration
                    waveform: root.waveform
                    viewOnce: root.viewOnce
                    fg: root.fromMe ? Theme.onBubbleOut : Theme.onBubbleIn
                    Layout.fillWidth: true
                    onOpenViewer: (p) => root.openViewer(p)
                    onPlayVideo: (p) => root.playVideo(p)
                }

                // non-media fallback types (location/contact/call/…)
                Label {
                    visible: root.msgType !== "text" && root.msgType !== "poll" && !root.isMediaType() && root.text === ""
                    text: root.typeFallback()
                    color: root.fromMe ? Theme.onBubbleOut : Theme.onBubbleIn
                    font.pixelSize: 14
                }

                // body text
                Label {
                    visible: text !== ""
                    text: root.html
                    textFormat: Text.StyledText
                    wrapMode: Text.Wrap
                    linkColor: root.fromMe ? Theme.onBubbleOut : Theme.primary
                    color: root.fromMe ? Theme.onBubbleOut : Theme.onBubbleIn
                    font.pixelSize: 14
                    Layout.maximumWidth: root.maxTextWidth()
                    onLinkActivated: (link) => Qt.openUrlExternally(link)
                    MouseArea {
                        anchors.fill: parent
                        acceptedButtons: Qt.NoButton
                        cursorShape: parent.hoveredLink ? Qt.PointingHandCursor : Qt.ArrowCursor
                    }
                }

                // link-preview card — title/desc/site under the body text,
                // optional thumb; large pages get a banner image on top.
                Rectangle {
                    id: linkCard
                    visible: linkCard.pv !== null
                    Layout.fillWidth: true
                    Layout.minimumWidth: Math.min(root.maxTextWidth(), 260)
                    implicitHeight: lcCol.implicitHeight
                    radius: 6
                    color: Qt.rgba(0, 0, 0, 0.15)

                    property var pv: root.linkCard()

                    ColumnLayout {
                        id: lcCol
                        anchors.left: parent.left
                        anchors.right: parent.right
                        spacing: 0

                        Image {
                            visible: linkCard.pv !== null && linkCard.pv.large === true && linkCard.pv.thumb
                            source: visible ? Bridge.fileUrl(linkCard.pv.thumb) : ""
                            fillMode: Image.PreserveAspectCrop
                            asynchronous: true
                            Layout.fillWidth: true
                            Layout.preferredHeight: 140
                        }

                        RowLayout {
                            spacing: 8
                            Layout.fillWidth: true
                            Layout.topMargin: 6
                            Layout.bottomMargin: 6
                            Layout.leftMargin: 0
                            Layout.rightMargin: 8

                            Rectangle {
                                Layout.preferredWidth: 3
                                Layout.fillHeight: true
                                color: Theme.primary
                            }

                            ColumnLayout {
                                spacing: 1
                                Layout.fillWidth: true
                                Layout.leftMargin: 6

                                Label {
                                    visible: text !== ""
                                    text: linkCard.pv ? (linkCard.pv.site || root.linkHost()) : ""
                                    color: Theme.primary
                                    font.pixelSize: 12
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                                Label {
                                    visible: text !== ""
                                    text: linkCard.pv ? (linkCard.pv.title || "") : ""
                                    color: root.fromMe ? Theme.onBubbleOut : Theme.onBubbleIn
                                    font.bold: true
                                    font.pixelSize: 13
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                                Label {
                                    visible: text !== ""
                                    text: linkCard.pv ? (linkCard.pv.desc || "") : ""
                                    color: root.fromMe ? Theme.onBubbleOut : Theme.onBubbleIn
                                    opacity: 0.75
                                    font.pixelSize: 12
                                    wrapMode: Text.Wrap
                                    maximumLineCount: 2
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                                Label {
                                    // URL row — the only text a pending card shows.
                                    visible: linkCard.pv !== null &&
                                             (linkCard.pv.title || "") === "" &&
                                             (linkCard.pv.desc || "") === "" &&
                                             (linkCard.pv.site || "") === ""
                                    text: root.linkHost()
                                    color: Theme.primary
                                    font.pixelSize: 12
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                            }

                            Image {
                                visible: linkCard.pv !== null && linkCard.pv.large !== true && linkCard.pv.thumb
                                source: visible ? Bridge.fileUrl(linkCard.pv.thumb) : ""
                                fillMode: Image.PreserveAspectCrop
                                asynchronous: true
                                Layout.preferredWidth: 48
                                Layout.preferredHeight: 48
                            }
                        }
                    }

                    MouseArea {
                        anchors.fill: parent
                        cursorShape: Qt.PointingHandCursor
                        onClicked: root.openLink()
                    }
                }

                // poll — option rows are clickable ballots (votePoll toggles)
                Column {
                    visible: root.msgType === "poll" && root.pollData !== ""
                    Layout.fillWidth: true
                    spacing: 3
                    Repeater {
                        model: root.pollOptions()
                        delegate: Rectangle {
                            required property var modelData
                            width: pollCol2.implicitWidth + 16
                            implicitWidth: pollCol2.implicitWidth + 16
                            implicitHeight: 26
                            radius: 6
                            color: modelData.mine ? Theme.primary : Qt.rgba(0,0,0,0.15)
                            Label {
                                id: pollCol2
                                anchors.centerIn: parent
                                text: (modelData.mine ? "● " : "○ ") + modelData.name + "  " + modelData.votes
                                color: modelData.mine ? Theme.onPrimary : (root.fromMe ? Theme.onBubbleOut : Theme.onBubbleIn)
                                font.pixelSize: 13
                            }
                            MouseArea {
                                anchors.fill: parent
                                cursorShape: Qt.PointingHandCursor
                                onClicked: Bridge.cmd("votePoll", {jid: App.chatJID, msgId: root.msgId, option: modelData.name}, function(ok, d, e) {
                                    if (!ok) Bridge.cmd("notify", {msg: "Vote failed: " + e});
                                })
                            }
                        }
                    }
                }

                // footer: star/edited/time/ticks
                RowLayout {
                    spacing: 4
                    Layout.alignment: Qt.AlignRight

                    WdIcon {
                        visible: root.starred
                        source: Qt.resolvedUrl("icons/star.svg")
                        color: Theme.primary
                        iconSize: 12
                        Layout.alignment: Qt.AlignVCenter
                    }
                    Label {
                        visible: root.edited
                        text: "edited"
                        font.italic: true
                        color: footerColor()
                        font.pixelSize: 10
                    }
                    Label {
                        text: root.timeStr
                        color: footerColor()
                        font.pixelSize: 10
                    }
                    WdIcon {
                        visible: root.fromMe && root.tickIcon() !== ""
                        source: root.tickIcon() !== "" ? Qt.resolvedUrl(root.tickIcon()) : ""
                        color: Theme.onBubbleOut
                        iconSize: 15
                        Layout.alignment: Qt.AlignVCenter
                    }
                }
            }

            // hover react button — fades in centered on the bubble's outer
            // side edge and opens the quick-react popup.
            Rectangle {
                id: reactFab
                width: 30
                height: 30
                radius: 15
                z: 3
                color: Theme.surfaceContainerHigh
                border.color: Theme.outlineVariant
                border.width: 1
                anchors.verticalCenter: parent.verticalCenter
                anchors.left: root.fromMe ? undefined : parent.right
                anchors.leftMargin: 6
                anchors.right: root.fromMe ? parent.left : undefined
                anchors.rightMargin: 6
                visible: opacity > 0
                opacity: (root.canReact && (rowHover.hovered || reactPop.opened)) ? 1 : 0
                scale: opacity > 0 ? 1 : 0.6
                Behavior on opacity { NumberAnimation { duration: 130 } }
                Behavior on scale { NumberAnimation { duration: 140; easing.type: Easing.OutBack; easing.overshoot: 3 } }

                WdIcon {
                    anchors.centerIn: parent
                    source: Qt.resolvedUrl("icons/sentiment_satisfied.svg")
                    color: Theme.onSurfaceVariant
                    iconSize: 17
                }
                TapHandler { onTapped: reactPop.open() }
                HoverHandler { cursorShape: Qt.PointingHandCursor }
            }

            // reaction burst — the picked emoji springs out of the bubble's
            // bottom edge, hangs, then shrinks into the chips row.
            Label {
                id: burstEmoji
                property real baseY: parent.height - 20
                visible: opacity > 0
                opacity: 0
                z: 6
                font.pixelSize: 28
                x: root.fromMe ? parent.width - width - 10 : 10
                y: baseY
            }

            SequentialAnimation {
                id: burstAnim
                ParallelAnimation {
                    NumberAnimation { target: burstEmoji; property: "scale"; to: 1.9; duration: 300; easing.type: Easing.OutBack; easing.overshoot: 2.2 }
                    NumberAnimation { target: burstEmoji; property: "opacity"; to: 1; duration: 90 }
                    NumberAnimation { target: burstEmoji; property: "y"; to: burstEmoji.baseY - 46; duration: 300; easing.type: Easing.OutCubic }
                }
                PauseAnimation { duration: 260 }
                ParallelAnimation {
                    NumberAnimation { target: burstEmoji; property: "scale"; to: 0.45; duration: 190; easing.type: Easing.InQuad }
                    NumberAnimation { target: burstEmoji; property: "opacity"; to: 0; duration: 190 }
                    NumberAnimation { target: burstEmoji; property: "y"; to: burstEmoji.baseY; duration: 190; easing.type: Easing.InQuad }
                }
            }
        }

        // ---- reactions: per-emoji pills below the bubble ------------------
        Flow {
            id: chipsFlow
            visible: reactionChips.length > 0
            spacing: 4
            Layout.fillWidth: true
            Layout.leftMargin: 20
            Layout.rightMargin: 20
            Layout.topMargin: 2
            layoutDirection: root.fromMe ? Qt.RightToLeft : Qt.LeftToRight

            property var reactionChips: root.reactionChips()

            Repeater {
                model: chipsFlow.reactionChips
                delegate: Rectangle {
                    id: chipPill
                    required property var modelData
                    implicitWidth: chipRow.implicitWidth + 16
                    implicitHeight: 30
                    radius: 15
                    color: root.fromMe
                        ? (Theme.dark ? Qt.lighter(Theme.bubbleOut, 1.25) : Qt.darker(Theme.bubbleOut, 1.06))
                        : Theme.surfaceContainerHigh
                    border.color: Qt.rgba(0, 0, 0, 0.22)
                    border.width: 1
                    layer.enabled: true
                    layer.effect: MultiEffect {
                        shadowEnabled: true
                        shadowBlur: 0.5
                        shadowColor: Qt.rgba(0, 0, 0, 0.4)
                        shadowVerticalOffset: 2
                        shadowHorizontalOffset: 0
                    }

                    Row {
                        id: chipRow
                        anchors.centerIn: parent
                        spacing: 5

                        // reactor faces — first 3, overlapping
                        Item {
                            id: avStack
                            anchors.verticalCenter: parent.verticalCenter
                            height: 22
                            property int shown: Math.min(chipPill.modelData.who ? chipPill.modelData.who.length : 0, 3)
                            width: shown > 0 ? shown * 14 + 8 : 0

                            Repeater {
                                model: (chipPill.modelData.who || []).slice(0, 3)
                                delegate: Avatar {
                                    required property var modelData
                                    required property int index
                                    x: index * 14
                                    size: 22
                                    source: modelData.avatar
                                    name: modelData.name
                                }
                            }
                        }
                        Label {
                            text: chipPill.modelData.emoji
                            font.pixelSize: 16
                            width: 22
                            height: 22
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                            // Emoji glyphs sit low in the line box — nudge
                            // up so the ink centers on the avatar row.
                            anchors.verticalCenter: parent.verticalCenter
                            anchors.verticalCenterOffset: -2
                        }
                        Label {
                            text: chipPill.modelData.n
                            visible: chipPill.modelData.n > 1
                            color: Theme.onSurfaceVariant
                            font.pixelSize: 12
                            font.bold: true
                            anchors.verticalCenter: parent.verticalCenter
                        }
                    }

                    TapHandler { onTapped: whoPop.open() }
                    HoverHandler { cursorShape: Qt.PointingHandCursor }

                    // who-reacted popup — tap a chip for the avatar + name +
                    // emoji row of everyone who used it.
                    Popup {
                        id: whoPop
                        padding: 8
                        y: -implicitHeight - 8
                        x: root.fromMe ? parent.width - implicitWidth : 0

                        Column {
                            spacing: 6
                            Repeater {
                                model: chipPill.modelData.who || []
                                delegate: Row {
                                    required property var modelData
                                    spacing: 8

                                    Avatar {
                                        size: 24
                                        source: modelData.avatar
                                        name: modelData.name
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                    Label {
                                        text: modelData.name
                                        color: Theme.onSurface
                                        font.pixelSize: 13
                                        width: Math.min(implicitWidth, 180)
                                        elide: Text.ElideRight
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                    Label {
                                        text: chipPill.modelData.emoji
                                        font.pixelSize: 14
                                        anchors.verticalCenter: parent.verticalCenter
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    // ---- helpers ------------------------------------------------------
    function footerColor() {
        return root.fromMe ? Qt.darker(Theme.onBubbleOut, 1.35) : Theme.onSurfaceVariant;
    }
    function tickIcon() {
        switch (root.status) {
        case "pending":   return "icons/schedule.svg";
        case "sent":      return "icons/check.svg";
        case "delivered": return "icons/done_all.svg";
        case "read":      return "icons/done_all.svg";
        case "played":    return "icons/done_all.svg";
        case "failed":    return "icons/error.svg";
        default:          return "";
        }
    }
    function typeFallback() {
        switch (root.msgType) {
        case "image":    return "Photo";
        case "video":    return "Video";
        case "audio":    return "Audio";
        case "ptt":      return "Voice message";
        case "document": return root.mediaName || "Document";
        case "sticker":  return "Sticker";
        case "location": return "📍 Location";
        case "contact":  return "👤 Contact";
        case "call":     return "📞 Call";
        default:         return root.msgType;
        }
    }
    function pollOptions() {
        if (root.pollData === "") return [];
        try { return JSON.parse(root.pollData).options || []; } catch (e) { return []; }
    }
    function reactionChips() {
        if (root.reactions === "") return [];
        try { return JSON.parse(root.reactions) || []; } catch (e) { return []; }
    }
    function linkCard() {
        if (root.linkPreview === "") return null;
        try {
            var pv = JSON.parse(root.linkPreview);
            return (pv && pv.url) ? pv : null;
        } catch (e) { return null; }
    }
    function linkHost() {
        var pv = linkCard();
        if (!pv) return "";
        var m = /^(?:[a-z][a-z0-9+.-]*:\/\/)?([^\s\/:]+)/i.exec(pv.url);
        return m ? m[1].replace(/^www\./, "") : pv.url;
    }
    function openLink() {
        var pv = linkCard();
        if (!pv || pv.url === "") return;
        var u = pv.url;
        if (!/^[a-z][a-z0-9+.-]*:/i.test(u)) u = "https://" + u;
        Qt.openUrlExternally(u);
    }
    function maxTextWidth() {
        return ListView.view ? ListView.view.width * 0.68 : 400;
    }
    function isMediaType() {
        return ["image", "sticker", "video", "audio", "ptt", "document"].indexOf(root.msgType) >= 0;
    }

    // whether the enclosing chat is a group (drives sender-name display)
    property bool isGroupMsg: App.chatIsGroup === true
    // whether the chat's protocol accepts reactions (gates the react UI)
    readonly property bool canReact: App.chatCapsReact !== false

    // reaction burst — pops a big copy of the picked emoji off the bubble.
    function reactBurst(emoji) {
        burstEmoji.text = emoji;
        burstEmoji.scale = 0;
        burstEmoji.opacity = 0;
        burstAnim.restart();
    }
}

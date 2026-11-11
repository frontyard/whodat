import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// MessageRowFlat is the bubble-less transcript delegate (chat_style=flat):
// one full-width row per message — a colored sender/meta header line above
// the content, a subtle hover tint, and the timestamp cluster on the right.
// Shares MessageRow's roles, signals and ListView contract.
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
    required property string mediaThumb
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

    // rowHover drives both the row tint and the floating react button.
    HoverHandler { id: rowHover }

    ColumnLayout {
        id: content
        width: parent.width
        y: dayLabel !== "" ? 6 : 2
        spacing: 2

        // ---- day separator ------------------------------------------
        RowLayout {
            visible: root.dayLabel !== ""
            spacing: 10
            Layout.fillWidth: true
            Layout.leftMargin: 18
            Layout.rightMargin: 18
            Layout.topMargin: 4
            Layout.bottomMargin: 2

            Label {
                text: root.dayLabel
                color: Theme.onSurfaceVariant
                font.bold: true
                font.pixelSize: 12
            }
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: 1
                color: Theme.outlineVariant
                Layout.alignment: Qt.AlignVCenter
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

        // ---- flat row ------------------------------------------------
        Rectangle {
            id: row
            visible: root.msgType !== "system" && root.msgType !== "revoked"
            Layout.fillWidth: true
            Layout.leftMargin: 6
            Layout.rightMargin: 6
            implicitHeight: rowLayout.implicitHeight + 14
            radius: Theme.radiusSmall
            color: rowHover.hovered
                   ? Qt.rgba(Theme.onSurface.r, Theme.onSurface.g, Theme.onSurface.b, 0.06)
                   : "transparent"

            MouseArea {
                anchors.fill: parent
                acceptedButtons: Qt.RightButton
                onClicked: (m) => rowMenu.popup(m.x, m.y)
            }

            Menu {
                id: rowMenu
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

            // quick-react popup — right edge so full-width rows can't push
            // it past the window edge.
            Popup {
                id: reactPop
                padding: 4
                y: -implicitHeight - 6
                x: parent.width - implicitWidth
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

            // Gutter + content: the timestamp sits in a fixed-width left
            // column so every row's content starts at the same x.
            RowLayout {
                id: rowLayout
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.verticalCenter: parent.verticalCenter
                anchors.leftMargin: 12
                anchors.rightMargin: 12
                spacing: 8

                Label {
                    text: root.timeStr
                    color: Theme.onSurfaceVariant
                    font.pixelSize: 11
                    horizontalAlignment: Text.AlignRight
                    Layout.preferredWidth: 40
                    // Baseline with the sender-name line below the top.
                    Layout.alignment: Qt.AlignTop
                    Layout.topMargin: 2
                }

                ColumnLayout {
                    id: rowCol
                    Layout.fillWidth: true
                    spacing: 3

                // header line — sender name left, meta cluster right
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 6

                    Label {
                        text: root.fromMe ? "You" : root.senderName
                        visible: text !== ""
                        color: root.fromMe ? Theme.primary : Theme.secondary
                        font.bold: true
                        font.pixelSize: 13
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
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
                        color: Theme.onSurfaceVariant
                        font.pixelSize: 11
                    }
                    WdIcon {
                        visible: root.fromMe && root.tickIcon() !== ""
                        source: root.tickIcon() !== "" ? Qt.resolvedUrl(root.tickIcon()) : ""
                        color: root.status === "read" || root.status === "played" ? Theme.primary : Theme.onSurfaceVariant
                        iconSize: 15
                        Layout.alignment: Qt.AlignVCenter
                    }
                }

                // forwarded marker
                Label {
                    visible: root.forwarded
                    text: root.fwdFrom !== "" ? "Forwarded from " + root.fwdFrom : "Forwarded"
                    font.italic: true
                    color: Theme.onSurfaceVariant
                    font.pixelSize: 12
                }

                // quoted block — accent bar + sender/text
                Rectangle {
                    visible: root.quotedText !== "" || root.quotedSender !== ""
                    Layout.fillWidth: true
                    implicitWidth: Math.min(Math.max(quoteSenderMetrics.advanceWidth, quoteTextMetrics.advanceWidth) + 16, root.maxTextWidth())
                    implicitHeight: quoteCol.implicitHeight + 8
                    radius: 6
                    color: Qt.rgba(Theme.onSurface.r, Theme.onSurface.g, Theme.onSurface.b, 0.08)

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
                            color: Theme.onSurface
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
                    mediaThumb: root.mediaThumb
                    mediaMIME: root.mediaMIME
                    mediaName: root.mediaName
                    mediaSize: root.mediaSize
                    mediaBytes: root.mediaBytes
                    mediaW: root.mediaW
                    mediaH: root.mediaH
                    duration: root.duration
                    waveform: root.waveform
                    viewOnce: root.viewOnce
                    fg: Theme.onSurface
                    Layout.fillWidth: true
                    onOpenViewer: (p) => root.openViewer(p)
                    onPlayVideo: (p) => root.playVideo(p)
                }

                // non-media fallback types (location/contact/call/…)
                Label {
                    visible: root.msgType !== "text" && root.msgType !== "poll" && !root.isMediaType() && root.text === ""
                    text: root.typeFallback()
                    color: Theme.onSurface
                    font.pixelSize: 14
                }

                // body text
                Label {
                    visible: text !== ""
                    text: root.html
                    textFormat: Text.StyledText
                    wrapMode: Text.Wrap
                    linkColor: Theme.primary
                    color: Theme.onSurface
                    font.pixelSize: 14
                    Layout.maximumWidth: root.maxTextWidth()
                    onLinkActivated: (link) => Qt.openUrlExternally(link)
                    MouseArea {
                        anchors.fill: parent
                        acceptedButtons: Qt.NoButton
                        cursorShape: parent.hoveredLink ? Qt.PointingHandCursor : Qt.ArrowCursor
                    }
                }

                // link-preview card — same structure as the bubble renderer,
                // tinted for a flat row.
                Rectangle {
                    id: linkCard
                    visible: linkCard.pv !== null
                    Layout.fillWidth: true
                    Layout.minimumWidth: Math.min(root.maxTextWidth(), 260)
                    Layout.maximumWidth: root.maxTextWidth()
                    implicitHeight: lcCol.implicitHeight
                    radius: 6
                    color: Qt.rgba(Theme.onSurface.r, Theme.onSurface.g, Theme.onSurface.b, 0.08)

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
                                    color: Theme.onSurface
                                    font.bold: true
                                    font.pixelSize: 13
                                    elide: Text.ElideRight
                                    Layout.fillWidth: true
                                }
                                Label {
                                    visible: text !== ""
                                    text: linkCard.pv ? (linkCard.pv.desc || "") : ""
                                    color: Theme.onSurface
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
                            color: modelData.mine ? Theme.primary : Qt.rgba(Theme.onSurface.r, Theme.onSurface.g, Theme.onSurface.b, 0.10)
                            Label {
                                id: pollCol2
                                anchors.centerIn: parent
                                text: (modelData.mine ? "● " : "○ ") + modelData.name + "  " + modelData.votes
                                color: modelData.mine ? Theme.onPrimary : Theme.onSurface
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

                // reactions — inline emoji+count tokens under the content;
                // flat mode skips pills and avatar stacks entirely.
                Row {
                    visible: root.reactionChips().length > 0
                    spacing: 12
                    Layout.topMargin: 2

                    Repeater {
                        model: root.reactionChips()
                        delegate: Item {
                            id: chipToken
                            required property var modelData
                            implicitWidth: chipLbl.implicitWidth + 2
                            implicitHeight: 22

                            Label {
                                id: chipLbl
                                anchors.centerIn: parent
                                text: chipToken.modelData.emoji +
                                      (chipToken.modelData.n > 1 ? " " + chipToken.modelData.n : "")
                                color: Theme.onSurfaceVariant
                                font.pixelSize: 14
                                anchors.verticalCenterOffset: -1
                            }
                            TapHandler { onTapped: whoPop.open() }
                            HoverHandler { cursorShape: Qt.PointingHandCursor }

                            // who-reacted popup — tap a token for the
                            // avatar + name + emoji of everyone who used it.
                            Popup {
                                id: whoPop
                                padding: 8
                                y: -implicitHeight - 6

                                Column {
                                    spacing: 6
                                    Repeater {
                                        model: chipToken.modelData.who || []
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
                                                text: chipToken.modelData.emoji
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
            }

            // hover react button — fades in on the row's right edge,
            // vertically centered on the content.
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
                anchors.right: parent.right
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
        }

    }

    // ---- helpers ------------------------------------------------------
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
        // view width minus row margins (12), gutter (40+8) and padding (12+12).
        return ListView.view ? ListView.view.width - 84 : 400;
    }
    function isMediaType() {
        return ["image", "sticker", "video", "audio", "ptt", "document"].indexOf(root.msgType) >= 0;
    }

    // whether the enclosing chat is a group (drives sender-name display)
    property bool isGroupMsg: App.chatIsGroup === true
    // whether the chat's protocol accepts reactions (gates the react UI)
    readonly property bool canReact: App.chatCapsReact !== false
}

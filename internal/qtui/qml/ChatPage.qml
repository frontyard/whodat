import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// ChatPage is the conversation pane: header, transcript (BottomToTop — index
// 0 is the newest message, sitting at the visual bottom), typing bar,
// composer.
Item {
    id: page
    signal dialogReq(string name, string jid, int extra)
    signal chatActionReq(string jid, string id)

    // ---- empty state ------------------------------------------------
    Item {
        anchors.fill: parent
        visible: App.chatJID === ""

        Column {
            anchors.centerIn: parent
            spacing: 8
            Label {
                text: "WhoDat"
                font.pixelSize: 26
                font.bold: true
                color: Theme.onSurface
                anchors.horizontalCenter: parent.horizontalCenter
            }
            Label {
                text: "Select a chat to start messaging"
                color: Theme.onSurfaceVariant
                anchors.horizontalCenter: parent.horizontalCenter
            }
        }
    }

    ColumnLayout {
        anchors.fill: parent
        visible: App.chatJID !== ""
        spacing: 0

        // ---- header --------------------------------------------------
        Rectangle {
            Layout.fillWidth: true
            implicitHeight: 64
            color: Theme.surfaceContainer

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: 10
                anchors.rightMargin: 6
                spacing: 10

                Avatar {
                    source: App.chatAvatarPath
                    name: App.chatTitle
                    size: 44
                }

                Item {
                    Layout.fillWidth: true
                    Layout.fillHeight: true

                    ColumnLayout {
                        id: titleCol
                        anchors.left: parent.left
                        anchors.right: parent.right
                        anchors.verticalCenter: parent.verticalCenter
                        spacing: 2

                        RowLayout {
                            Layout.fillWidth: true
                            spacing: 5

                            // Measured width caps the label at its natural
                            // text width so the lock sits beside the title —
                            // TextMetrics because an elided Label's own
                            // implicitWidth shrinks, collapsing the cap.
                            TextMetrics { id: titleMetrics; font: titleLbl.font; text: App.chatTitle }
                            Label {
                                id: titleLbl
                                text: App.chatTitle
                                font.pixelSize: 15
                                color: Theme.onSurface
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                                Layout.maximumWidth: titleMetrics.advanceWidth
                            }
                            // E2E status lock — "yes" solid accent, "no"
                            // dimmed with a strike; hidden when unknown.
                            Item {
                                visible: App.chatE2EE === "yes" || App.chatE2EE === "no"
                                implicitWidth: 15
                                implicitHeight: 15
                                Layout.alignment: Qt.AlignVCenter

                                WdIcon {
                                    anchors.fill: parent
                                    source: Qt.resolvedUrl("icons/lock.svg")
                                    color: App.chatE2EE === "yes" ? Theme.primary : Theme.onSurfaceVariant
                                    iconSize: 15
                                    opacity: App.chatE2EE === "yes" ? 1.0 : 0.45
                                }
                                Rectangle {
                                    visible: App.chatE2EE === "no"
                                    width: parent.width * 1.45
                                    height: 1.6
                                    radius: 0.8
                                    rotation: -45
                                    anchors.centerIn: parent
                                    color: Theme.error
                                }
                                MouseArea {
                                    id: encHover
                                    anchors.fill: parent
                                    hoverEnabled: true
                                }
                                ToolTip.text: App.chatE2EE === "yes" ? "End-to-end encrypted" : "Not end-to-end encrypted"
                                ToolTip.visible: encHover.containsMouse
                                ToolTip.delay: 400
                            }
                        }
                        Label {
                            text: App.chatTyping !== "" ? App.chatTyping : App.chatSub
                            color: App.chatTyping !== "" ? Theme.primary : Theme.onSurfaceVariant
                            font.pixelSize: 13
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                        }
                    }
                    MouseArea {
                        anchors.fill: parent
                        cursorShape: Qt.PointingHandCursor
                        onClicked: page.chatActionReq(App.chatJID, "info")
                    }
                }

                RowLayout {
                    spacing: 0

                    WdIconButton {
                        icon.source: Qt.resolvedUrl("icons/call.svg")
                        icon.color: Theme.onSurfaceVariant
                        visible: (App.chatCapsCalls !== false) && (!App.chatIsGroup || (App.chatCapsGroupCalls === true && App.showGroupCalls === true))
                        onClicked: Bridge.cmd("startCall", {jid: App.chatJID, video: false, group: App.chatIsGroup})
                    }
                    WdIconButton {
                        icon.source: Qt.resolvedUrl("icons/videocam.svg")
                        icon.color: Theme.onSurfaceVariant
                        visible: (App.chatCapsVideo !== false) && (!App.chatIsGroup || (App.chatCapsGroupCalls === true && App.showGroupCalls === true))
                        onClicked: Bridge.cmd("startCall", {jid: App.chatJID, video: true, group: App.chatIsGroup})
                    }
                    WdIconButton {
                        id: chatMenuBtn
                        icon.source: Qt.resolvedUrl("icons/more_vert.svg")
                        icon.color: Theme.onSurfaceVariant
                        onClicked: {
                            if (App.chatJID !== "")
                                chatMenuLoader.open(App.chatJID, chatMenuBtn);
                        }
                    }
                }
            }
        }

        // ---- transcript -----------------------------------------------
        ListView {
            id: transcript
            Layout.fillWidth: true
            Layout.fillHeight: true
            model: MessageModel
            clip: true
            verticalLayoutDirection: ListView.BottomToTop
            spacing: 0
            boundsBehavior: Flickable.StopAtBounds

            // history paging is fetchMore-driven — reaching the visual top
            // (model end) pages older rows in automatically.
            // App.chatStyle picks the renderer; swapping delegate reloads
            // the view, so the change applies live.
            delegate: App.chatStyle === "flat" ? flatRow : bubbleRow

            Component {
                id: bubbleRow
                MessageRow {
                    onReplyRequested: (id, sender, text) => composer.quote(id, sender, text)
                    onEditRequested: (id, text) => composer.edit(id, text)
                    onForwardRequested: (id) => fwdDlg.openForward(App.chatJID, id)
                    onJumpTo: (id) => page.jumpToMessage(id)
                    onOpenViewer: (p) => viewer.show(p, msgId)
                    onPlayVideo: (p) => videoDlg.play(p)
                }
            }
            Component {
                id: flatRow
                MessageRowFlat {
                    onReplyRequested: (id, sender, text) => composer.quote(id, sender, text)
                    onEditRequested: (id, text) => composer.edit(id, text)
                    onForwardRequested: (id) => fwdDlg.openForward(App.chatJID, id)
                    onJumpTo: (id) => page.jumpToMessage(id)
                    onOpenViewer: (p) => viewer.show(p, msgId)
                    onPlayVideo: (p) => videoDlg.play(p)
                }
            }

            // new-message scroll assist: when the user is near the bottom,
            // incoming rows snap the view; otherwise we keep position.
            // BottomToTop quirk: atYEnd is true while index 0 sits at the
            // visual bottom, and positionViewAtBeginning() is what returns
            // there (positionViewAtEnd() scrolls to the oldest rows).
            property bool atBottom: atYEnd
            onCountChanged: if (atBottom) positionViewAtBeginning()

            ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }
        }

        // ---- typing indicator -------------------------------------------
        Rectangle {
            Layout.fillWidth: true
            implicitHeight: visible ? 24 : 0
            visible: App.chatTyping !== ""
            color: "transparent"
            clip: true
            Label {
                anchors.left: parent.left
                anchors.leftMargin: 14
                anchors.verticalCenter: parent.verticalCenter
                text: App.chatTyping
                color: Theme.primary
                font.pixelSize: 12
            }
        }

        // ---- composer --------------------------------------------------
        Composer {
            id: composer
            Layout.fillWidth: true
        }
    }

    // jump-to-latest FAB — floats over the transcript's bottom-right corner
    // whenever the newest row isn't in view.
    WdRoundButton {
        id: jumpEnd
        z: 5
        visible: !transcript.atBottom && transcript.count > 0
        anchors.right: parent.right
        anchors.rightMargin: 18
        y: transcript.mapToItem(page, 0, transcript.height).y - height - 14
        implicitWidth: 40
        implicitHeight: 40
        padding: 0
        icon.source: Qt.resolvedUrl("icons/expand_more.svg")
        icon.color: Theme.onSurface
        icon.width: 20
        icon.height: 20
        onClicked: transcript.positionViewAtBeginning()
    }

    ForwardDialog { id: fwdDlg }
    MediaViewer   { id: viewer }
    VideoPlayer   { id: videoDlg }

    // chat context menu (header ⋮) — reuses the row-menu machinery
    ListModel { id: chatMenuModel }
    Menu {
        id: chatMenu
        Instantiator {
            model: chatMenuModel
            WdMenuItem {
                required property string act
                required property string label
                text: label
                onTriggered: page.chatActionReq(App.chatJID, act)
            }
            onObjectAdded: (i, o) => chatMenu.insertItem(i, o)
            onObjectRemoved: (i, o) => chatMenu.removeItem(o)
        }
    }
    QtObject {
        id: chatMenuLoader
        function open(jid, btn) {
            chatMenuModel.clear();
            Bridge.cmd("chatMenu", {jid: jid}, function(ok, data) {
                if (ok && data.items) {
                    for (var i = 0; i < data.items.length; i++)
                        chatMenuModel.append(data.items[i]);
                    chatMenu.parent = btn;
                    chatMenu.popup(0, btn.height);
                }
            });
        }
    }

    // Receipts re-fire when the window regains focus while a chat is open.
    Connections {
        target: Window.window
        function onActiveChanged() {
            if (Window.window.active && App.chatJID !== "")
                Bridge.cmd("markRead", {});
        }
    }

    // jump-to-message: Go pages history, then App.jumpTo (JSON {n,index})
    // triggers positioning. Binding-watched: the map's valueChanged signal
    // does not reach QML Connections.
    property var jumpJson: App.jumpTo
    onJumpJsonChanged: {
        if (!jumpJson) return;
        var j;
        try { j = JSON.parse(jumpJson); } catch (e) { return; }
        if (j.index >= 0) {
            transcript.positionViewAtIndex(j.index, ListView.Center);
            var it = transcript.itemAtIndex(j.index);
            if (it) flash.target = it, flash.restart();
        }
    }
    SequentialAnimation {
        id: flash
        property Item target: null
        NumberAnimation { target: flash.target; property: "opacity"; to: 0.3; duration: 120 }
        NumberAnimation { target: flash.target; property: "opacity"; to: 1.0; duration: 400 }
    }

    function jumpToMessage(id) {
        Bridge.cmd("jumpTo", {id: id});
    }
}

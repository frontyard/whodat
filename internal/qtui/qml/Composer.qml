import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material
import QtQuick.Controls.Basic as Basic
import QtQuick.Dialogs

// Composer is the message input strip: emoji, attach/poll menu, text area,
// send. It carries two message-level modes: quoting (reply banner) and
// editing (edit banner) — both set by transcript signals.
Item {
    id: comp
    implicitHeight: col.implicitHeight

    property string quotedId: ""
    property string quoteLabel: ""
    property string editId: ""
    property bool   recording: false
    property int    recSeconds: 0

    // mention autocomplete — group members loaded via chatMembers, each
    // pick records {token,pid} so send() can ship the mentions to Go.
    property var    _members: []
    property string _membersJID: ""
    property var    _mentions: []

    // per-chat composer drafts (in-memory): switching chats stashes the
    // in-progress text + quote/edit/mention context under the old jid and
    // restores whatever was left in the new one.
    property string _prevJID: ""
    property var    _drafts: ({})
    property bool   _silenceTyping: false

    // re-fetches the member list whenever another chat is opened
    property string watchJID: App.chatJID
    onWatchJIDChanged: {
        comp.saveDraft(comp._prevJID);
        comp._prevJID = comp.watchJID;
        mentionPop.close();
        comp.restoreDraft(comp.watchJID);
        comp._members = [];
        comp._membersJID = "";
        comp.loadMembers();
    }

    // snapshot only — a bound property would track App.chatJID and lose the
    // "previous chat" key that saveDraft needs.
    Component.onCompleted: comp._prevJID = App.chatJID

    function saveDraft(jid) {
        var d = comp._drafts;
        if (input.text !== "" || quotedId !== "" || editId !== "")
            d[jid] = {text: input.text, cursor: input.cursorPosition,
                      quotedId: quotedId, quoteLabel: quoteLabel,
                      editId: editId, mentions: comp._mentions.slice()};
        else
            delete d[jid];
    }

    function restoreDraft(jid) {
        var d = comp._drafts[jid];
        comp._silenceTyping = true;
        input.text = d ? d.text : "";
        input.cursorPosition = d ? d.cursor : 0;
        comp._silenceTyping = false;
        comp._mentions = d ? d.mentions : [];
        quotedId = d ? d.quotedId : "";
        quoteLabel = d ? d.quoteLabel : "";
        editId = d ? d.editId : "";
    }

    function loadMembers() {
        var jid = App.chatJID;
        if (jid === "" || jid === comp._membersJID || App.chatIsGroup !== true)
            return;
        comp._membersJID = jid; // marks in-flight — one fetch per chat
        Bridge.cmd("chatMembers", {jid: jid}, function(ok, data) {
            if (ok && App.chatJID === jid) {
                comp._members = data.members || [];
                comp.updateMention(); // a "@…" may be waiting on the list
            }
        });
    }

    // the "@query" span ending at the cursor, or null
    function mentionQuery() {
        var t = input.text.slice(0, input.cursorPosition);
        var m = /(^|\s)@([^\s@]{0,30})$/.exec(t);
        if (!m) return null;
        return {start: input.cursorPosition - m[2].length - 1, query: m[2]};
    }

    function updateMention() {
        var q = mentionQuery();
        if (q === null || App.chatIsGroup !== true) {
            mentionPop.close();
            return;
        }
        comp.loadMembers();
        var query = q.query.toLowerCase();
        mentionModel.clear();
        for (var i = 0; i < comp._members.length; i++) {
            var m = comp._members[i];
            if (query === "" || m.name.toLowerCase().indexOf(query) >= 0
                || (m.sub && m.sub.toLowerCase().indexOf(query) >= 0))
                mentionModel.append(m);
        }
        if (mentionModel.count === 0) {
            mentionPop.close();
            return;
        }
        mentionList.currentIndex = 0;
        mentionPop.x = inputScroll.mapToItem(col, 0, 0).x;
        if (!mentionPop.visible) mentionPop.open();
    }

    function pickMember(m) {
        var q = mentionQuery();
        if (q === null || !m) return;
        var pre = input.text.slice(0, q.start);
        var post = input.text.slice(input.cursorPosition);
        input.text = pre + m.token + " " + post;
        input.cursorPosition = pre.length + m.token.length + 1;
        var dup = false;
        for (var i = 0; i < comp._mentions.length; i++)
            if (comp._mentions[i].token === m.token) dup = true;
        if (!dup) comp._mentions.push({token: m.token, id: m.pid});
        mentionPop.close();
        input.forceActiveFocus();
    }

    function quote(id, sender, text) {
        clearState();
        quotedId = id;
        var t = (text || "").replace(/\n/g, " ");
        if (t.length > 80) t = t.slice(0, 80) + "…";
        quoteLabel = (sender || "Message") + ": " + t;
        input.forceActiveFocus();
    }

    function edit(id, text) {
        clearState();
        editId = id;
        input.text = text || "";
        input.forceActiveFocus();
        input.cursorPosition = input.text.length;
    }

    function clearState() {
        quotedId = "";
        quoteLabel = "";
        editId = "";
    }

    ColumnLayout {
        id: col
        width: parent.width
        spacing: 0

        // recording banner
        Rectangle {
            visible: comp.recording
            Layout.fillWidth: true
            implicitHeight: 34
            color: Theme.errorContainer

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: 12
                anchors.rightMargin: 4

                Label {
                    text: "Recording " + fmtTime(comp.recSeconds) + " — stop sends, cancel discards"
                    color: Theme.onErrorContainer
                    font.pixelSize: 12
                    Layout.fillWidth: true
                }
            }
        }

        // quote / edit banner
        Rectangle {
            visible: comp.quotedId !== "" || comp.editId !== ""
            Layout.fillWidth: true
            implicitHeight: 34
            color: Theme.surfaceContainerHigh

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: 12
                anchors.rightMargin: 4
                spacing: 8

                Rectangle { width: 3; Layout.fillHeight: true; Layout.topMargin: 6; Layout.bottomMargin: 6; color: Theme.primary }
                Label {
                    text: comp.editId !== ""
                          ? "Editing message"
                          : "Replying to " + comp.quoteLabel
                    elide: Text.ElideRight
                    color: Theme.onSurfaceVariant
                    font.pixelSize: 12
                    Layout.fillWidth: true
                }
                WdIconButton {
                    icon.source: Qt.resolvedUrl("icons/close.svg")
                    icon.color: Theme.onSurfaceVariant
                    // 44px button would push the row's implicit height past
                    // the 34px banner and sink every item below center.
                    Layout.maximumHeight: 34
                    onClicked: comp.clearState()
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            implicitHeight: inputRow.implicitHeight + 12
            color: Theme.surfaceContainerLow

            RowLayout {
                id: inputRow
                anchors.fill: parent
                anchors.margins: 6
                spacing: 2

                WdIconButton {
                    id: emojiBtn
                    icon.source: Qt.resolvedUrl("icons/sentiment_satisfied.svg")
                    icon.color: Theme.onSurfaceVariant
                    onClicked: emojiPop.open()
                    EmojiPicker {
                        id: emojiPop
                        parent: emojiBtn
                        y: -emojiPop.height - 8
                        onPicked: (e) => {
                            input.insert(input.cursorPosition, e);
                            input.forceActiveFocus();
                        }
                    }
                }

                WdIconButton {
                    icon.source: Qt.resolvedUrl("icons/attach_file.svg")
                    icon.color: Theme.onSurfaceVariant
                    onClicked: attachMenu.popup(0, -attachMenu.implicitHeight)
                    Menu {
                        id: attachMenu
                        WdMenuItem {
                            text: "Attach file…"
                            onTriggered: fileDlg.open()
                        }
                        WdMenuItem {
                            text: "Create poll…"
                            visible: App.chatCapsPolls !== false
                            onTriggered: pollDlg.openFor()
                        }
                    }
                }

                ScrollView {
                    id: inputScroll
                    Layout.fillWidth: true
                    // Grow with content up to ~5 lines, then scroll.
                    Layout.preferredHeight: Math.max(40, Math.min(110, input.implicitHeight))
                    Layout.maximumHeight: 110
                    Layout.minimumHeight: 40
                    background: Rectangle {
                        radius: 18
                        color: Theme.surfaceContainerHigh
                    }

                    // Basic style: its PlaceholderText is a plain centered
                    // Text honoring verticalAlignment — Material's is a
                    // floating label reparented outside the control, which
                    // reads as a misaligned placeholder in a 40px pill.
                    Basic.TextArea {
                        id: input
                        placeholderText: "Message"
                        wrapMode: TextArea.Wrap
                        color: Theme.onSurface
                        placeholderTextColor: Theme.onSurfaceVariant
                        // Fill the pill so 1-line text centers; placeholder
                        // shares this alignment → no split-position bug.
                        height: Math.max(inputScroll.availableHeight, implicitHeight)
                        verticalAlignment: Text.AlignVCenter
                        background: Item {}
                        leftPadding: 14
                        rightPadding: 14
                        topPadding: 0
                        bottomPadding: 0

                        Keys.onReturnPressed: (e) => {
                            if (mentionPop.visible) {
                                e.accepted = true;
                                comp.pickMember(mentionModel.get(mentionList.currentIndex));
                            } else if (e.modifiers & Qt.ShiftModifier) {
                                e.accepted = false; // newline
                            } else {
                                e.accepted = true;
                                comp.send();
                            }
                        }
                        Keys.onEnterPressed: (e) => {
                            if (mentionPop.visible) {
                                e.accepted = true;
                                comp.pickMember(mentionModel.get(mentionList.currentIndex));
                            } else if (e.modifiers & Qt.ShiftModifier) {
                                e.accepted = false;
                            } else {
                                e.accepted = true;
                                comp.send();
                            }
                        }
                        Keys.onEscapePressed: (e) => {
                            e.accepted = true;
                            if (mentionPop.visible) mentionPop.close();
                            else comp.clearState();
                        }
                        Keys.onUpPressed: (e) => {
                            if (mentionPop.visible) {
                                e.accepted = true;
                                mentionList.decrementCurrentIndex();
                            } else e.accepted = false;
                        }
                        Keys.onDownPressed: (e) => {
                            if (mentionPop.visible) {
                                e.accepted = true;
                                mentionList.incrementCurrentIndex();
                            } else e.accepted = false;
                        }
                        Keys.onTabPressed: (e) => {
                            if (mentionPop.visible) {
                                e.accepted = true;
                                comp.pickMember(mentionModel.get(mentionList.currentIndex));
                            } else e.accepted = false;
                        }

                        onTextChanged: {
                            comp.pulseTyping();
                            comp.updateMention();
                        }
                        onCursorPositionChanged: comp.updateMention()
                    }
                }

                // mic / stop-send / cancel while recording
                WdIconButton {
                    icon.source: Qt.resolvedUrl("icons/mic.svg")
                    icon.color: Theme.onSurfaceVariant
                    visible: !comp.recording
                    onClicked: comp.startRecord()
                }
                WdIconButton {
                    icon.source: Qt.resolvedUrl("icons/send.svg")
                    icon.color: Theme.primary
                    visible: comp.recording
                    onClicked: comp.stopRecord(false)
                }
                WdIconButton {
                    icon.source: Qt.resolvedUrl("icons/close.svg")
                    icon.color: Theme.onSurfaceVariant
                    visible: comp.recording
                    onClicked: comp.stopRecord(true)
                }

                WdIconButton {
                    icon.source: Qt.resolvedUrl(comp.editId !== "" ? "icons/check.svg" : "icons/send.svg")
                    icon.color: Theme.primary
                    visible: !comp.recording
                    enabled: input.text.trim() !== ""
                    onClicked: comp.send()
                }
            }
        }
    }

    // "@" mention autocomplete — floats above the input, keyboard-driven.
    Popup {
        id: mentionPop
        parent: col
        y: -implicitHeight - 6
        width: Math.min(col.width - 24, 380)
        implicitHeight: Math.min(mentionModel.count, 6) * 46 + 8
        padding: 4
        focus: false
        modal: false

        ListView {
            id: mentionList
            anchors.fill: parent
            model: mentionModel
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            keyNavigationEnabled: false

            delegate: WdItemDelegate {
                id: mRow
                required property string name
                required property string sub
                required property string token
                required property string pid
                required property int index
                width: mentionList.width
                height: 46
                highlighted: mentionList.currentIndex === index

                contentItem: Column {
                    spacing: 0
                    Label {
                        text: mRow.name
                        color: Theme.onSurface
                        font.pixelSize: 14
                        elide: Text.ElideRight
                        width: parent.width
                    }
                    Label {
                        visible: mRow.sub !== ""
                        text: mRow.sub
                        color: Theme.onSurfaceVariant
                        font.pixelSize: 12
                        elide: Text.ElideRight
                        width: parent.width
                    }
                }
                onClicked: comp.pickMember({token: mRow.token, pid: mRow.pid})
            }
        }
    }

    ListModel { id: mentionModel }

    PollDialog { id: pollDlg }

    FileDialog {
        id: fileDlg
        onAccepted: {
            var p = String(fileDlg.selectedFile).replace(/^file:\/\//, "");
            Bridge.cmd("sendFile", {jid: App.chatJID, path: p});
        }
    }

    Timer {
        interval: 1000
        running: comp.recording
        repeat: true
        onTriggered: comp.recSeconds++
    }

    function startRecord() {
        Bridge.cmd("startRecord", {}, function(ok, d, e) {
            if (!ok) { Bridge.cmd("notify", {msg: "Microphone unavailable"}); return; }
            comp.recSeconds = 0;
            comp.recording = true;
        });
    }

    function stopRecord(cancel) {
        comp.recording = false;
        Bridge.cmd("stopRecord", {jid: App.chatJID, cancel: cancel});
    }

    function fmtTime(s) {
        var m = Math.floor(s / 60), r = s % 60;
        return m + ":" + (r < 10 ? "0" : "") + r;
    }

    property double _lastTypingPulse: 0
    function pulseTyping() {
        var now = Date.now();
        if (comp._silenceTyping || now - _lastTypingPulse < 4000 || App.chatJID === "") return;
        _lastTypingPulse = now;
        Bridge.cmd("sendTyping", {jid: App.chatJID, composing: input.text !== ""});
    }

    function send() {
        var t = input.text.trim();
        if (t === "" || App.chatJID === "") return;
        if (comp.editId !== "") {
            var id = comp.editId;
            Bridge.cmd("sendEdit", {jid: App.chatJID, msgId: id, text: t}, function(ok, d, e) {
                if (!ok) Bridge.cmd("notify", {msg: "Edit failed: " + e});
            });
        } else {
            var ms = [];
            for (var i = 0; i < comp._mentions.length; i++)
                if (t.indexOf(comp._mentions[i].token) >= 0) ms.push(comp._mentions[i]);
            Bridge.cmd("sendMessage", {jid: App.chatJID, text: t, quotedId: comp.quotedId, mentions: ms}, function(ok, d, e) {
                if (!ok) Bridge.cmd("notify", {msg: "Send failed: " + e});
            });
        }
        input.text = "";
        comp._mentions = [];
        comp.clearState();
        Bridge.cmd("sendTyping", {jid: App.chatJID, composing: false});
    }
}

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// ForwardDialog picks a target chat for a forwarded message. open(jid,msgId)
// loads the candidate list (all chats except the source) and shows the dialog.
Dialog {
    id: dlg
    title: "Forward to"
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 380
    height: 480
    padding: 0

    property string fromJid: ""
    property string msgId: ""

    function openForward(fromJid, msgId) {
        dlg.fromJid = fromJid;
        dlg.msgId = msgId;
        chatList.model = [];
        search.text = "";
        dlg.open();
        Bridge.cmd("forwardTargets", {exceptJid: fromJid}, function(ok, data) {
            if (ok) dlg._all = data.chats || [], dlg.applyFilter();
        });
    }

    property var _all: []
    function applyFilter() {
        var q = search.text.trim().toLowerCase();
        chatList.model = dlg._all.filter(function(c) {
            return q === "" || c.name.toLowerCase().indexOf(q) >= 0 || c.jid.indexOf(q) >= 0;
        });
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        TextField {
            id: search
            placeholderText: "Search chats"
            Layout.fillWidth: true
            Layout.margins: 8
            rightPadding: clearSearch.visible ? clearSearch.width + 8 : Material.textFieldHorizontalPadding
            onTextChanged: dlg.applyFilter()
            WdIconButton {
                id: clearSearch
                visible: search.text !== ""
                anchors.right: parent.right
                anchors.rightMargin: 2
                anchors.verticalCenter: parent.verticalCenter
                implicitWidth: 34
                implicitHeight: 34
                focusPolicy: Qt.NoFocus
                icon.source: Qt.resolvedUrl("icons/close.svg")
                icon.color: Theme.onSurfaceVariant
                icon.width: 20
                icon.height: 20
                onClicked: search.clear()
            }
        }

        ListView {
            id: chatList
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true

            delegate: WdItemDelegate {
                required property var modelData
                width: chatList.width
                implicitHeight: 56

                contentItem: RowLayout {
                    spacing: 10
                    Avatar {
                        source: modelData.avatarPath
                        name: modelData.name
                        size: 38
                        Layout.leftMargin: 10
                    }
                    Label {
                        text: modelData.name
                        elide: Text.ElideRight
                        color: Theme.onSurface
                        Layout.fillWidth: true
                    }
                    Label {
                        text: modelData.isGroup ? "Group" : ""
                        color: Theme.onSurfaceVariant
                        font.pixelSize: 11
                        Layout.rightMargin: 10
                    }
                }

                onClicked: {
                    var to = modelData.jid;
                    dlg.close();
                    Bridge.cmd("forward", {fromJid: dlg.fromJid, msgId: dlg.msgId, toJid: to}, function(ok, data, err) {
                        if (!ok) Bridge.cmd("notify", {msg: "Forward failed: " + err});
                    });
                }
            }
        }
    }
}

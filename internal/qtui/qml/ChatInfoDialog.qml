import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// ChatInfoDialog: header, about, disapp timer,
// starred, verify/block (DM), participants + admin actions (group).
Dialog {
    id: dlg
    title: "Chat info"
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 440
    height: 600
    padding: 0
    standardButtons: Dialog.Close

    signal openDisapp(string jid, int secs)
    signal openVerify(string jid)
    signal openStarred(string jid)

    property string jid: ""
    property var info: ({})
    property bool weAdmin: false

    function openFor(jid) {
        dlg.jid = jid;
        dlg.info = {};
        partModel.clear();
        dlg.open();
        Bridge.cmd("chatInfo", {jid: jid}, function(ok, data, err) {
            if (ok) {
                dlg.info = data;
                if (data.isGroup) dlg.loadParticipants();
            } else {
                Bridge.cmd("notify", {msg: "Info failed: " + err});
            }
        });
    }

    function loadParticipants() {
        Bridge.cmd("groupInfo", {jid: dlg.jid}, function(ok, data) {
            if (!ok) return;
            dlg.weAdmin = data.weAdmin === true;
            partModel.clear();
            var ps = data.participants || [];
            for (var i = 0; i < ps.length; i++) partModel.append(ps[i]);
        });
    }

    ListModel { id: partModel }

    ScrollView {
        anchors.fill: parent
        clip: true
        contentWidth: availableWidth

        ColumnLayout {
            width: parent.width
            spacing: 0

            // ---- header -------------------------------------------------
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: 100
                color: Theme.surfaceContainer

                RowLayout {
                    anchors.fill: parent
                    anchors.margins: 14
                    spacing: 14

                    Avatar { source: dlg.info.avatarPath || ""; name: dlg.info.name || ""; size: 64 }
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 2
                        Label { text: dlg.info.name || ""; font.pixelSize: 19; font.bold: true; color: Theme.onSurface; elide: Text.ElideRight; Layout.fillWidth: true }
                        Label { text: dlg.info.phone || ""; color: Theme.onSurfaceVariant; font.pixelSize: 13 }
                        Label {
                            visible: (dlg.info.about || "") !== ""
                            text: dlg.info.about || ""
                            color: Theme.onSurfaceVariant
                            font.pixelSize: 12
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                        }
                    }
                    WdRoundButton {
                        visible: dlg.info.isGroup === true && dlg.weAdmin
                        icon.source: Qt.resolvedUrl("icons/edit.svg")
                        icon.color: Theme.onSurfaceVariant
                        flat: true
                        onClicked: renameDlg.open()
                    }
                }
            }

            // ---- rows ----------------------------------------------------
            WdItemDelegate {
                Layout.fillWidth: true
                contentItem: RowLayout {
                    ColumnLayout {
                        Layout.fillWidth: true
                        Label { text: "Disappearing messages"; color: Theme.onSurface }
                        Label { text: dlg.info.disappText || "Off"; color: Theme.onSurfaceVariant; font.pixelSize: 12 }
                    }
                }
                onClicked: dlg.openDisapp(dlg.jid, dlg.info.disappSecs || 0)
            }
            WdItemDelegate {
                Layout.fillWidth: true
                contentItem: RowLayout {
                    ColumnLayout {
                        Layout.fillWidth: true
                        Label { text: "Starred messages"; color: Theme.onSurface }
                        Label { text: (dlg.info.starredCount || 0) + " items"; color: Theme.onSurfaceVariant; font.pixelSize: 12 }
                    }
                }
                onClicked: dlg.openStarred(dlg.jid)
            }
            WdItemDelegate {
                visible: dlg.info.isGroup === false
                Layout.fillWidth: true
                text: "Verify security code"
                onClicked: dlg.openVerify(dlg.jid)
            }
            WdItemDelegate {
                visible: dlg.info.isGroup === true
                Layout.fillWidth: true
                text: "Copy invite link"
                onClicked: Bridge.cmd("groupInvite", {jid: dlg.jid}, function(ok, data, err) {
                    if (ok) { linkField.text = data.link; linkField.selectAll(); linkField.copy(); linkField.text = ""; Bridge.cmd("notify", {msg: "Invite link copied"}); }
                    else Bridge.cmd("notify", {msg: "Invite failed: " + err});
                })
            }

            // ---- participants ---------------------------------------------
            Label {
                visible: dlg.info.isGroup === true
                text: "Participants"
                font.bold: true
                color: Theme.onSurface
                Layout.leftMargin: 14
                Layout.topMargin: 10
            }
            Repeater {
                model: partModel
                WdItemDelegate {
                    required property string jid
                    required property string name
                    required property string phone
                    required property bool isAdmin
                    required property bool isMe
                    width: parent ? parent.width : 0
                    contentItem: RowLayout {
                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 0
                            Label {
                                text: name + (isMe ? " (you)" : "") + (isAdmin ? " · admin" : "")
                                color: Theme.onSurface
                            }
                            Label { text: phone; color: Theme.onSurfaceVariant; font.pixelSize: 11 }
                        }
                    }
                    onClicked: {
                        if (dlg.weAdmin && !isMe) { partMenu.jid = jid; partMenu.isAdmin = isAdmin; partMenu.parent = this; partMenu.popup(); }
                    }
                }
            }
            Menu {
                id: partMenu
                property string jid: ""
                property bool isAdmin: false
                WdMenuItem {
                    text: partMenu.isAdmin ? "Demote" : "Make admin"
                    onTriggered: Bridge.cmd("groupUpdate", {jid: dlg.jid, participant: partMenu.jid,
                        action: partMenu.isAdmin ? "demote" : "promote"}, function(ok) { if (ok) dlg.loadParticipants(); })
                }
                WdMenuItem {
                    text: "Remove"
                    onTriggered: Bridge.cmd("groupUpdate", {jid: dlg.jid, participant: partMenu.jid,
                        action: "remove"}, function(ok) { if (ok) dlg.loadParticipants(); })
                }
            }

            Rectangle { Layout.fillWidth: true; implicitHeight: 1; color: Theme.outlineVariant; Layout.topMargin: 8 }

            // ---- danger zone ---------------------------------------------
            WdItemDelegate {
                visible: dlg.info.isGroup === false
                Layout.fillWidth: true
                contentItem: Label {
                    text: dlg.info.blocked ? "Unblock" : "Block"
                    color: Theme.error
                }
                onClicked: {
                    var on = !(dlg.info.blocked === true);
                    Bridge.cmd("setBlocked", {jid: dlg.jid, on: on}, function(ok) {
                        if (ok) { dlg.info.blocked = on; dlg.infoChanged(); }
                    });
                }
            }
            WdItemDelegate {
                visible: dlg.info.isGroup === true
                Layout.fillWidth: true
                contentItem: Label { text: "Leave group"; color: Theme.error }
                onClicked: {
                    Bridge.cmd("chatAction", {jid: dlg.jid, id: "leave"});
                    dlg.close();
                }
            }
        }
    }

    TextField { id: linkField; visible: false }

    Dialog {
        id: renameDlg
        title: "Rename group"
        modal: true
        Overlay.modal: Scrim {}
        anchors.centerIn: Overlay.overlay
        width: 340
        standardButtons: Dialog.Ok | Dialog.Cancel
        onOpened: nameField.text = dlg.info.name || ""
        onAccepted: Bridge.cmd("groupRename", {jid: dlg.jid, name: nameField.text}, function(ok, d, e) {
            if (ok) { dlg.info.name = nameField.text; dlg.infoChanged(); }
            else Bridge.cmd("notify", {msg: "Rename failed: " + e});
        })
        TextField { id: nameField; anchors.fill: parent; placeholderText: "Group name" }
    }
}

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// NewGroupDialog creates a group from a name + picked members. Contacts are
// found through the same searchContacts lookup as the new-chat dialog,
// scoped to the chosen account; a raw number/id can be added
// verbatim when it isn't in the store (WhatsApp digits).
Dialog {
    id: dlg
    title: "New group"
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 400
    height: 560
    topPadding: 8
    bottomPadding: 8

    property var accounts: []
    property string acct: ""
    property var picked: []

    onOpened: {
        gname.text = ""; search.text = ""; dlg.picked = []; results.model = [];
        dlg.load("");
        gname.forceActiveFocus();
        Bridge.cmd("listAccounts", {}, function(ok, data) {
            if (!ok) return;
            var live = (data.accounts || []).filter(function(a) { return a.paired; });
            dlg.accounts = live;
            if (live.length > 0) {
                var keep = live.findIndex(function(a) { return a.acct === dlg.acct; });
                acctPicker.currentIndex = keep >= 0 ? keep : 0;
            }
        });
    }

    function load(q) {
        Bridge.cmd("searchContacts", {q: q, acct: dlg.acct}, function(ok, data) {
            if (!ok) return;
            results.model = (data.items || []).filter(function(it) {
                return !dlg.isPicked(it.jid);
            });
            results.currentIndex = -1;
        });
    }

    function isPicked(id) {
        for (var i = 0; i < dlg.picked.length; i++)
            if (dlg.picked[i].id === id) return true;
        return false;
    }

    function addMember(id, name) {
        id = (id || "").trim();
        if (id === "" || dlg.isPicked(id)) return;
        var p = dlg.picked.slice();
        p.push({id: id, name: name});
        dlg.picked = p;
        search.text = "";
        search.forceActiveFocus();
        dlg.load("");
    }

    function removeMember(id) {
        dlg.picked = dlg.picked.filter(function(m) { return m.id !== id; });
        dlg.load(search.text.trim());
    }

    footer: DialogButtonBox {
        WdButton {
            text: "Cancel"
            DialogButtonBox.buttonRole: DialogButtonBox.RejectRole
            onClicked: dlg.close()
        }
        WdButton {
            text: "Create"
            enabled: gname.text.trim() !== "" && dlg.picked.length > 0
            DialogButtonBox.buttonRole: DialogButtonBox.AcceptRole
            onClicked: {
                var ids = [];
                for (var i = 0; i < dlg.picked.length; i++) ids.push(dlg.picked[i].id);
                Bridge.cmd("createGroup", {name: gname.text, numbers: ids.join(","), acct: dlg.acct},
                    function(ok, data, err) {
                        if (ok && data.jid) {
                            Bridge.cmd("openChat", {jid: data.jid});
                        } else {
                            Bridge.cmd("notify", {msg: "Group failed: " + err});
                        }
                    });
                dlg.close();
            }
        }
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 8

        WdComboBox {
            id: acctPicker
            Layout.fillWidth: true
            visible: dlg.accounts.length > 1
            Layout.preferredHeight: 44
            model: dlg.accounts
            textRole: "name"
            onCurrentIndexChanged: {
                if (currentIndex >= 0 && currentIndex < dlg.accounts.length) {
                    dlg.acct = dlg.accounts[currentIndex].acct;
                    dlg.picked = []; // picked ids belong to the old account
                    dlg.load(search.text.trim());
                }
            }
        }
        TextField {
            id: gname
            placeholderText: "Group name"
            Layout.fillWidth: true
            Layout.preferredHeight: 50
            // the floated label rises ~11px above the border — the margin
            // keeps that strip clear of the picker/chips above the field.
            Layout.topMargin: 12
        }

        // picked members — click a chip to remove
        ScrollView {
            id: chipsView
            Layout.fillWidth: true
            Layout.preferredHeight: Math.min(chips.implicitHeight, 90)
            visible: dlg.picked.length > 0
            clip: true

            Flow {
                id: chips
                width: chipsView.availableWidth
                spacing: 6

                Repeater {
                    model: dlg.picked
                    delegate: Rectangle {
                        id: chip
                        required property var modelData
                        width: Math.min(chipRow.implicitWidth + 22, chips.width)
                        height: 26
                        radius: 13
                        color: Theme.secondaryContainer

                        Row {
                            id: chipRow
                            anchors.centerIn: parent
                            spacing: 6

                            Label {
                                text: chip.modelData.name
                                color: Theme.onSecondaryContainer
                                font.pixelSize: 12
                                elide: Text.ElideRight
                                width: Math.min(implicitWidth, chips.width - 40)
                                anchors.verticalCenter: parent.verticalCenter
                            }
                            WdIcon {
                                source: Qt.resolvedUrl("icons/close.svg")
                                color: Theme.onSecondaryContainer
                                iconSize: 12
                                anchors.verticalCenter: parent.verticalCenter
                            }
                        }
                        MouseArea {
                            anchors.fill: parent
                            cursorShape: Qt.PointingHandCursor
                            onClicked: dlg.removeMember(chip.modelData.id)
                        }
                    }
                }
            }
        }

        TextField {
            id: search
            placeholderText: "Add members — name, number or @username"
            Layout.fillWidth: true
            Layout.preferredHeight: 50
            Layout.topMargin: 12
            onTextChanged: debounce.restart()
            onAccepted: {
                var idx = results.currentIndex >= 0 ? results.currentIndex : 0;
                if (idx < results.model.length) {
                    var it = results.model[idx];
                    dlg.addMember(it.jid, it.name);
                } else {
                    dlg.addMember(search.text.trim(), search.text.trim());
                }
            }
            Keys.onDownPressed: results.incrementCurrentIndex()
            Keys.onUpPressed: results.decrementCurrentIndex()
            Timer { id: debounce; interval: 250; onTriggered: dlg.load(search.text.trim()) }
        }

        ListView {
            id: results
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            model: []
            boundsBehavior: Flickable.StopAtBounds

            header: WdItemDelegate {
                width: results.width
                visible: search.text.trim() !== ""
                height: visible ? implicitHeight : 0
                text: "Add \u201C" + search.text.trim() + "\u201D"
                onClicked: dlg.addMember(search.text.trim(), search.text.trim())
            }

            delegate: WdItemDelegate {
                id: hit
                required property var modelData
                width: results.width
                highlighted: ListView.isCurrentItem

                contentItem: Column {
                    spacing: 0
                    Label {
                        text: hit.modelData.name
                        color: Theme.onSurface
                        font.pixelSize: 14
                        elide: Text.ElideRight
                        width: parent.width
                    }
                    Label {
                        visible: hit.modelData.sub !== undefined && hit.modelData.sub !== ""
                                 && hit.modelData.sub !== hit.modelData.name
                        text: hit.modelData.sub || ""
                        color: Theme.onSurfaceVariant
                        font.pixelSize: 12
                        elide: Text.ElideRight
                        width: parent.width
                    }
                }
                onClicked: dlg.addMember(hit.modelData.jid, hit.modelData.name)
            }
        }
    }
}

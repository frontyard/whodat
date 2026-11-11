import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// NewChatDialog finds a contact by name or phone number, or creates a group.
Dialog {
    id: dlg
    title: "New chat"
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 380
    height: 560
    padding: 0
    standardButtons: Dialog.Close

    signal openGroup()

    property var accounts: []
    property string acct: ""

    onOpened: {
        search.text = ""; phone.text = ""; results.model = []; load("");
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
            if (ok) results.model = data.items || [];
        });
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        WdComboBox {
            id: acctPicker
            Layout.fillWidth: true
            Layout.margins: 8
            visible: dlg.accounts.length > 1
            Layout.preferredHeight: 44
            model: dlg.accounts
            textRole: "name"
            onCurrentIndexChanged: {
                if (currentIndex >= 0 && currentIndex < dlg.accounts.length) {
                    dlg.acct = dlg.accounts[currentIndex].acct;
                    load(search.text.trim());
                }
            }
        }

        TextField {
            id: search
            placeholderText: "Search contacts"
            Layout.fillWidth: true
            Layout.margins: 8
            Layout.preferredHeight: 50
            Layout.topMargin: 12 // the floated label rises ~11px above the border
            onTextChanged: debounce.restart()
            Timer { id: debounce; interval: 250; onTriggered: dlg.load(search.text.trim()) }
        }

        WdItemDelegate {
            Layout.fillWidth: true
            text: "+ New group"
            onClicked: { dlg.close(); dlg.openGroup(); }
        }

        Rectangle { Layout.fillWidth: true; implicitHeight: 1; color: Theme.outlineVariant }

        Label {
            text: "By phone, number or @username"
            color: Theme.onSurfaceVariant
            font.pixelSize: 12
            Layout.leftMargin: 12
            Layout.topMargin: 6
        }
        RowLayout {
            Layout.fillWidth: true
            Layout.margins: 8
            Layout.topMargin: 12 // room for the field's floated label
            TextField {
                id: phone
                placeholderText: "+1 555 123 4567 or @name"
                Layout.fillWidth: true
                Layout.preferredHeight: 50
            }
            WdButton {
                text: "Start"
                onClicked: startPhone()
            }
        }

        Rectangle { Layout.fillWidth: true; implicitHeight: 1; color: Theme.outlineVariant }

        ListView {
            id: results
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            delegate: WdItemDelegate {
                width: results.width
                text: modelData.name
                onClicked: {
                    Bridge.cmd("openOrCreateChat", {jid: modelData.jid});
                    Bridge.cmd("openChat", {jid: modelData.jid});
                    dlg.close();
                }
            }
        }
    }

    function startPhone() {
        var num = phone.text.trim();
        if (num === "") return;
        var acct = dlg.acct;
        var info = dlg.accounts.find(function(a) { return a.acct === acct; });
        if (!info || !info.dirSearch) return;
        Bridge.cmd("resolvePeer", {acct: acct, query: num}, function(ok, data) {
            if (ok && data.ok) {
                Bridge.cmd("openOrCreateChat", {jid: data.jid});
                Bridge.cmd("openChat", {jid: data.jid});
                dlg.close();
            } else {
                Bridge.cmd("notify", {msg: (data && data.err) ? data.err : "Not found: " + num});
            }
        });
    }
}

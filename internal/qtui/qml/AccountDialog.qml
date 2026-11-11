import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// AccountDialog: linked devices, privacy
// settings, personal QR code.
Dialog {
    id: dlg
    title: "Account"
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 460
    height: 560
    standardButtons: Dialog.Close

    readonly property var privacyNames: ["all", "contacts", "contact_blacklist", "match_last_seen", "known", "none"]
    readonly property var privacyLabels: ["Everyone", "My contacts", "My contacts except…", "Same as last seen", "Contacts and saved", "Nobody"]

    property string acct: ""
    // Driven by the account's Caps.Privacy — the privacy section renders
    // for any backend that advertises it, not by protocol name.
    property bool hasPrivacy: false

    function load() {
        Bridge.cmd("linkedDevices", {acct: dlg.acct}, function(ok, data) {
            if (ok) devList.model = data.devices || [];
        });
        if (dlg.hasPrivacy) {
            Bridge.cmd("privacyGet", {acct: dlg.acct}, function(ok, data) {
                if (!ok) return;
                setIdx(lastSeenRow.combo, data.last);
                setIdx(profileRow.combo, data.profile);
                setIdx(aboutRow.combo, data.status);
                setIdx(receiptsRow.combo, data.readreceipts);
                setIdx(groupAddRow.combo, data.groupadd);
            });
        }
        Bridge.cmd("contactQR", {acct: dlg.acct}, function(ok, data) {
            if (ok && data.path) {
                qrImg.source = Bridge.fileUrl(data.path);
                qrLoading.visible = false;
            } else {
                qrLoading.text = "No profile link";
            }
        });
    }
    onOpened: { qrImg.source = ""; devList.model = []; load(); }

    function setIdx(combo, v) {
        var i = privacyNames.indexOf(v);
        combo.currentIndex = i >= 0 ? i : 0;
    }
    component PrivacyRow: RowLayout {
        property string caption: ""
        property string pname: ""
        property alias combo: c
        Layout.fillWidth: true
        Label { text: caption; Layout.fillWidth: true; color: Theme.onSurface }
        WdComboBox {
            id: c
            model: dlg.privacyLabels
            Layout.preferredWidth: 200
            onActivated: Bridge.cmd("privacySet", {acct: dlg.acct, name: pname, value: dlg.privacyNames[currentIndex]}, function(ok, d, e) {
                if (!ok) Bridge.cmd("notify", {msg: "Privacy change failed: " + e});
            })
        }
    }

    ScrollView {
        anchors.fill: parent
        clip: true
        contentWidth: availableWidth

        ColumnLayout {
            width: parent.width
            spacing: 6

            Label { text: "Linked devices"; font.bold: true; color: Theme.onSurface }
            Rectangle {
                Layout.fillWidth: true
                implicitHeight: Math.min(140, devList.contentHeight + 8)
                color: Theme.surfaceContainer
                radius: Theme.radiusSmall
                ListView {
                    id: devList
                    anchors.fill: parent
                    anchors.margins: 4
                    clip: true
                    delegate: Label {
                        text: modelData
                        color: Theme.onSurfaceVariant
                        font.pixelSize: 12
                        padding: 4
                        width: devList.width - 8
                        elide: Text.ElideRight
                    }
                }
            }

            Label { text: "Privacy"; font.bold: true; color: Theme.onSurface; Layout.topMargin: 8; visible: dlg.hasPrivacy }
            PrivacyRow { id: lastSeenRow; caption: "Last seen"; pname: "lastseen"; visible: dlg.hasPrivacy }
            PrivacyRow { id: profileRow; caption: "Profile photo"; pname: "profile"; visible: dlg.hasPrivacy }
            PrivacyRow { id: aboutRow; caption: "About"; pname: "status"; visible: dlg.hasPrivacy }
            PrivacyRow { id: receiptsRow; caption: "Read receipts"; pname: "readreceipts"; visible: dlg.hasPrivacy }
            PrivacyRow { id: groupAddRow; caption: "Group invites"; pname: "groupadd"; visible: dlg.hasPrivacy }

            Label { text: "My QR code"; font.bold: true; color: Theme.onSurface; Layout.topMargin: 8 }
            Image {
                id: qrImg
                Layout.alignment: Qt.AlignHCenter
                Layout.preferredWidth: 200
                Layout.preferredHeight: 200
                fillMode: Image.PreserveAspectFit
                visible: source != ""
            }
            Label {
                id: qrLoading
                visible: qrImg.source == ""
                text: "Loading…"
                color: Theme.onSurfaceVariant
                Layout.alignment: Qt.AlignHCenter
            }
        }
    }
}

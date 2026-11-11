import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// VerifyDialog shows the 60-digit E2E safety number for a DM.
Dialog {
    id: dlg
    title: "Verify security code"
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 420
    standardButtons: Dialog.Close

    property string jid: ""

    function openFor(jid) {
        dlg.jid = jid;
        codeLbl.text = "Loading…";
        dlg.open();
        Bridge.cmd("verifyCodes", {jid: jid}, function(ok, data, err) {
            codeLbl.text = ok ? data.code : ("Could not load codes: " + err);
        });
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 10

        Label {
            text: "Compare this 60-digit code with the one on your contact's phone."
            wrapMode: Text.WordWrap
            Layout.fillWidth: true
            color: Theme.onSurfaceVariant
            font.pixelSize: 12
        }
        Rectangle {
            Layout.fillWidth: true
            color: Theme.surfaceContainer
            radius: Theme.radiusSmall
            implicitHeight: codeLbl.implicitHeight + 20
            Label {
                id: codeLbl
                anchors.fill: parent
                anchors.margins: 10
                font.family: "monospace"
                wrapMode: Text.WrapAnywhere
                color: Theme.onSurface
            }
        }
    }
}

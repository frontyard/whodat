import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material
import QtQuick.Dialogs

// MediaViewer shows a full-size image with a save-as action.
Dialog {
    id: dlg
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: Math.min(parent ? parent.width * 0.9 : 900, 900)
    height: Math.min(parent ? parent.height * 0.9 : 700, 700)
    padding: 0

    property string path: ""
    property string msgId: ""

    function show(path, msgId) {
        dlg.path = path;
        dlg.msgId = msgId || "";
        dlg.open();
    }

    contentItem: Item {
        Image {
            anchors.fill: parent
            source: Bridge.fileUrl(dlg.path)
            fillMode: Image.PreserveAspectFit
            asynchronous: true
        }

        Row {
            anchors.top: parent.top
            anchors.right: parent.right
            anchors.margins: 8

            WdRoundButton {
                icon.source: Qt.resolvedUrl("icons/download.svg")
                icon.color: "white"
                Material.background: "#88000000"
                onClicked: saveDlg.open()
            }
            WdRoundButton {
                icon.source: Qt.resolvedUrl("icons/close.svg")
                icon.color: "white"
                Material.background: "#88000000"
                onClicked: dlg.close()
            }
        }
    }

    FileDialog {
        id: saveDlg
        fileMode: FileDialog.SaveFile
        onAccepted: {
            Bridge.cmd("saveMedia", {jid: App.chatJID, msgId: dlg.msgId,
                dest: String(saveDlg.selectedFile).replace(/^file:\/\//, "")}, function(ok, d, e) {
                Bridge.cmd("notify", {msg: ok ? "Saved to " + d.path : "Save failed: " + e});
            });
        }
    }
}

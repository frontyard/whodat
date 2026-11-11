import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// StarredDialog lists a chat's starred messages; tapping jumps to the row.
Dialog {
    id: dlg
    title: "Starred messages"
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 440
    height: 480
    padding: 0
    standardButtons: Dialog.Close

    signal jumpToMessage(string id)

    property string jid: ""

    function openFor(jid) {
        dlg.jid = jid;
        list.model = [];
        dlg.open();
        Bridge.cmd("starredMessages", {jid: jid}, function(ok, data) {
            if (ok) list.model = data.items || [];
        });
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        Label {
            visible: list.count === 0
            text: "No starred messages"
            color: Theme.onSurfaceVariant
            Layout.alignment: Qt.AlignHCenter
            Layout.topMargin: 20
        }

        ListView {
            id: list
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            delegate: WdItemDelegate {
                width: list.width
                implicitHeight: starCol.implicitHeight + 16
                onClicked: { dlg.jumpToMessage(modelData.id); dlg.close(); }
                contentItem: ColumnLayout {
                    id: starCol
                    spacing: 2
                    RowLayout {
                        Layout.fillWidth: true
                        Label {
                            text: modelData.who
                            color: Theme.primary
                            font.pixelSize: 12
                            font.bold: true
                        }
                        Item { Layout.fillWidth: true }
                        Label {
                            text: modelData.day
                            color: Theme.onSurfaceVariant
                            font.pixelSize: 11
                        }
                    }
                    Label {
                        text: modelData.text
                        color: Theme.onSurface
                        font.pixelSize: 13
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                        maximumLineCount: 2
                        wrapMode: Text.WrapAnywhere
                    }
                }
            }
        }
    }
}

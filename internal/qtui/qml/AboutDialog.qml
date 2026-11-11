import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// AboutDialog: icon, name, version, credits,
// website and license.
Dialog {
    id: dlg
    header: null
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 360
    standardButtons: Dialog.Close

    ColumnLayout {
        anchors.fill: parent
        spacing: 4

        Image {
            source: Bridge.fileUrl(App.appIconPath || "")
            sourceSize: Qt.size(72, 72)
            fillMode: Image.PreserveAspectFit
            Layout.preferredWidth: 72
            Layout.preferredHeight: 72
            Layout.alignment: Qt.AlignHCenter
            Layout.bottomMargin: 4
        }

        Label {
            text: "WhoDat"
            font.pixelSize: 22
            font.bold: true
            color: Theme.onSurface
            Layout.alignment: Qt.AlignHCenter
        }

        Label {
            text: "Version " + (App.appVersion || "")
            color: Theme.onSurfaceVariant
            font.pixelSize: 12
            Layout.alignment: Qt.AlignHCenter
        }

        Label {
            text: "Unofficial WhatsApp messaging app for Linux.\nNot affiliated with WhatsApp or Meta."
            wrapMode: Text.WordWrap
            horizontalAlignment: Text.AlignHCenter
            color: Theme.onSurface
            font.pixelSize: 13
            Layout.fillWidth: true
            Layout.topMargin: 12
        }

        Label {
            text: "WhoDat contributors"
            color: Theme.onSurfaceVariant
            font.pixelSize: 12
            Layout.alignment: Qt.AlignHCenter
            Layout.topMargin: 12
        }

        Label {
            text: "GNU General Public License v3.0"
            color: Theme.onSurfaceVariant
            font.pixelSize: 12
            Layout.alignment: Qt.AlignHCenter
        }

        Label {
            text: '<a href="https://github.com/frontyard/whodat">github.com/frontyard/whodat</a>'
            linkColor: Theme.primary
            font.pixelSize: 12
            Layout.alignment: Qt.AlignHCenter
            Layout.topMargin: 8
            onLinkActivated: (link) => Qt.openUrlExternally(link)
            MouseArea {
                anchors.fill: parent
                acceptedButtons: Qt.NoButton
                cursorShape: parent.hoveredLink ? Qt.PointingHandCursor : Qt.ArrowCursor
            }
        }
    }
}

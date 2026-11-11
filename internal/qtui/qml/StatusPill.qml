import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// StatusPill is a floating status chip (connection/sync state). Unlike the
// old banners it overlays the layout instead of occupying a slot in it, so
// content never shifts when the state toggles. The pill keeps its geometry
// while fading so sibling pills never reposition either.
Rectangle {
    id: pill

    property bool active: false
    property string label: ""
    property url iconSource: ""
    property color container: Theme.errorContainer
    property color onContainer: Theme.onErrorContainer

    implicitHeight: 28
    implicitWidth: row.implicitWidth + 28
    radius: height / 2
    color: container
    opacity: active ? 1 : 0
    visible: opacity > 0
    Behavior on opacity { NumberAnimation { duration: 180 } }

    RowLayout {
        id: row
        anchors.centerIn: parent
        spacing: 6

        WdIcon {
            source: pill.iconSource
            color: pill.onContainer
            iconSize: 16
        }
        Label {
            text: pill.label
            color: pill.onContainer
            font.pixelSize: 12
        }
    }
}

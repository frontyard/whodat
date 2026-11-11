import QtQuick
import QtQuick.Effects

// WdIcon renders a monochrome SVG tinted to a theme color — public-API
// equivalent of the Controls-internal IconImage (not exported in Qt 6.8).
Item {
    id: ic
    property url source: ""
    property color color: "black"
    property int iconSize: 16
    implicitWidth: iconSize
    implicitHeight: iconSize

    Image {
        id: img
        anchors.fill: parent
        source: ic.source
        sourceSize: Qt.size(ic.iconSize * 2, ic.iconSize * 2)
        fillMode: Image.PreserveAspectFit
        asynchronous: true
        visible: false
    }
    MultiEffect {
        anchors.fill: img
        source: img
        // Black-filled SVGs: colorization preserves source luminance, so the
        // source must be pushed to white first or the tint never applies.
        brightness: 1.0
        colorization: 1.0
        colorizationColor: ic.color
        visible: ic.source.toString() !== ""
    }
}

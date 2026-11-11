import QtQuick
import QtQuick.Controls

// See WdButton — stock control + pointing-hand cursor on hover.
RadioButton {
    HoverHandler {
        enabled: parent.enabled
        cursorShape: Qt.PointingHandCursor
    }
}

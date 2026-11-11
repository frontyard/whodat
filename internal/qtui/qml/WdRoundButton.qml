import QtQuick
import QtQuick.Controls

// See WdButton — stock control + pointing-hand cursor on hover.
RoundButton {
    HoverHandler {
        enabled: parent.enabled
        cursorShape: Qt.PointingHandCursor
    }
}

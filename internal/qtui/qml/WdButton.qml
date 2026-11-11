import QtQuick
import QtQuick.Controls
import QtQuick.Controls.Material

// Wd* controls are stock Qt Quick Controls with a pointing-hand cursor on
// hover — Control has no cursorShape property (Qt 6.8), so a HoverHandler
// does it. Use WdButton/WdRoundButton/WdItemDelegate/etc for anything
// clickable.
Button {
    Material.roundedScale: Material.SmallScale
    HoverHandler {
        enabled: parent.enabled
        cursorShape: Qt.PointingHandCursor
    }
}

import QtQuick
import QtQuick.Controls
import QtQuick.Controls.Material

// See WdButton — stock control + pointing-hand cursor on hover.
ComboBox {
    Material.roundedScale: Material.SmallScale
    HoverHandler {
        enabled: parent.enabled
        cursorShape: Qt.PointingHandCursor
    }
}

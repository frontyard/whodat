import QtQuick
import QtQuick.Controls
import QtQuick.Controls.Material

// See WdButton — stock control + pointing-hand cursor on hover.
ComboBox {
    Material.roundedScale: Material.SmallScale
    // dropdown list styled like the rest of the app's popups (see WdPopup).
    popup.background: Rectangle {
        radius: Theme.radiusSmall
        color: Theme.surfaceContainerHigh
        border.color: Theme.outlineVariant
        border.width: 1
    }
    HoverHandler {
        enabled: parent.enabled
        cursorShape: Qt.PointingHandCursor
    }
}

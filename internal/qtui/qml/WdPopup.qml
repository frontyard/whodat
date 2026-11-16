import QtQuick
import QtQuick.Controls

// WdPopup: stock Popup with the MsgHoverBar look — elevated-surface fill
// plus a hairline outline, replacing the default flat popup color.
Popup {
    background: Rectangle {
        radius: Theme.radiusSmall
        color: Theme.surfaceContainerHigh
        border.color: Theme.outlineVariant
        border.width: 1
    }
}

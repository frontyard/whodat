import QtQuick
import QtQuick.Controls

// WdMenu: stock Menu reskinned like MsgHoverBar — elevated-surface fill
// with a hairline outline in place of the default flat popup color.
// implicitWidth keeps the Material default's 200px minimum.
Menu {
    background: Rectangle {
        implicitWidth: 200
        radius: Theme.radiusSmall
        color: Theme.surfaceContainerHigh
        border.color: Theme.outlineVariant
        border.width: 1
    }
}

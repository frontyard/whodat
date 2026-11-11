import QtQuick

// Scrim dims the window behind modal popups (Overlay.modal). Qt Material's
// default dim is translucent white in dark theme (backgroundDimColor),
// which looks washed out — use the theme's scrim token instead.
Rectangle {
    color: Qt.alpha(Theme.scrim, 0.6)
    Behavior on opacity { NumberAnimation { duration: 150 } }
}

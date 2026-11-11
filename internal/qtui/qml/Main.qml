import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

ApplicationWindow {
    id: win
    visible: true
    width: 1080
    height: 720
    minimumWidth: 720
    minimumHeight: 480
    title: "WhoDat"
    color: Theme.surface

    Material.theme: Theme.dark ? Material.Dark : Material.Light
    Material.primary: Theme.primary
    Material.accent: Theme.primary
    Material.background: Theme.surface
    Material.foreground: Theme.onSurface

    // Close-to-tray: reject the close and hide instead — only with a real
    // tray icon, or the window would hide with no way to bring it back.
    onClosing: (close) => {
        if (App.closeToTray === true && App.hasTray === true) {
            close.accepted = false;
            win.hide();
        }
    }

    // Report focus so notifications can suppress for the active chat.
    onActiveChanged: Cmd.payload = JSON.stringify({id: -1, op: "windowActive", args: {active: win.active}})

    // Tray Show raises the window — showSeq increments per request. Watch
    // it via a binding; the map's valueChanged signal never reaches QML.
    property int showSeqW: App.showSeq
    onShowSeqWChanged: {
        if (showSeqW > 0) {
            win.show();
            win.raise();
            win.requestActivate();
        }
    }

    StackLayout {
        anchors.fill: parent
        currentIndex: App.paired ? 1 : 0

        LoginPage {}
        MainPage {}
    }

    // Floating status pills — connection/sync state overlays the layout
    // instead of shifting it: fixed slots, fade-only transitions.
    Column {
        anchors.top: parent.top
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.topMargin: 8
        spacing: 8
        z: 50

        StatusPill {
            label: "Connecting…"
            iconSource: Qt.resolvedUrl("icons/cloud_off.svg")
            active: App.paired && App.connState !== "online" && App.connState !== "boot"
        }
        StatusPill {
            label: "Syncing messages…"
            iconSource: Qt.resolvedUrl("icons/sync.svg")
            container: Theme.secondaryContainer
            onContainer: Theme.onSecondaryContainer
            active: App.paired && App.syncing === true
        }
    }

    ToastArea {
        anchors.fill: parent
    }

    CallDialog {}
}

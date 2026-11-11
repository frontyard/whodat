import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// SettingsDialog: theme, notifications,
// close-to-tray, launch-at-login, experimental group calls.
Dialog {
    id: dlg
    title: "Settings"
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 420
    standardButtons: Dialog.Close

    function load() {
        Bridge.cmd("getConfig", {}, function(ok, data) {
            if (!ok) return;
            themeCombo.currentIndex = ["system","light","dark"].indexOf(data.theme || "system");
            chatStyleCombo.currentIndex = Math.max(0, ["bubbles","flat"].indexOf(data.chatStyle || "bubbles"));
            notifSw.checked = data.notifications === true;
            traySw.checked = data.closeToTray === true;
            autoSw.checked = data.autostart === true;
            groupCallsSw.checked = data.showGroupCalls === true;
            dlg._ready = true;
        });
    }

    property bool _ready: false
    onOpened: { _ready = false; load(); }

    function set(key, value) {
        if (!_ready) return;
        Bridge.cmd("setConfig", {key: key, value: value}, function(ok, data, err) {
            if (!ok) Bridge.cmd("notify", {msg: "Setting failed: " + err});
        });
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 4

        RowLayout {
            Layout.fillWidth: true
            Label { text: "Theme"; Layout.fillWidth: true; color: Theme.onSurface }
            WdComboBox {
                id: themeCombo
                model: ["System", "Light", "Dark"]
                onActivated: dlg.set("theme", ["system","light","dark"][currentIndex])
            }
        }

        RowLayout {
            Layout.fillWidth: true
            Label { text: "Chat style"; Layout.fillWidth: true; color: Theme.onSurface }
            WdComboBox {
                id: chatStyleCombo
                model: ["Bubbles", "Flat"]
                onActivated: dlg.set("chatStyle", ["bubbles","flat"][currentIndex])
            }
        }

        WdSwitchDelegate {
            id: notifSw
            Layout.fillWidth: true
            leftPadding: 0
            rightPadding: 0
            text: "Notifications"
            onToggled: dlg.set("notifications", checked)
            contentItem: ColumnLayout {
                Label { text: notifSw.text; color: Theme.onSurface }
                Label { text: "Show message notifications"; color: Theme.onSurfaceVariant; font.pixelSize: 12 }
            }
        }

        WdSwitchDelegate {
            id: traySw
            Layout.fillWidth: true
            leftPadding: 0
            rightPadding: 0
            text: "Close to tray"
            onToggled: dlg.set("closeToTray", checked)
            contentItem: ColumnLayout {
                Label { text: traySw.text; color: Theme.onSurface }
                Label { text: "Keep running when the window closes"; color: Theme.onSurfaceVariant; font.pixelSize: 12 }
            }
        }

        WdSwitchDelegate {
            id: autoSw
            Layout.fillWidth: true
            leftPadding: 0
            rightPadding: 0
            text: "Launch at login"
            onToggled: dlg.set("autostart", checked)
            contentItem: ColumnLayout {
                Label { text: autoSw.text; color: Theme.onSurface }
                Label { text: "Start WhoDat with your session"; color: Theme.onSurfaceVariant; font.pixelSize: 12 }
            }
        }

        WdSwitchDelegate {
            id: groupCallsSw
            Layout.fillWidth: true
            leftPadding: 0
            rightPadding: 0
            text: "Group calls (experimental)"
            onToggled: dlg.set("showGroupCalls", checked)
            contentItem: ColumnLayout {
                Label { text: groupCallsSw.text; color: Theme.onSurface }
                Label { text: "Allow calling from group chats"; color: Theme.onSurfaceVariant; font.pixelSize: 12 }
            }
        }

    }
}

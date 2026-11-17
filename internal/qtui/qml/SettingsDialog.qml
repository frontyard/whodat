import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// SettingsDialog: theme, notifications,
// close-to-tray, launch-at-login, experimental group calls.
Dialog {
    id: dlg
    title: qsTr("Settings")
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
            var langs = data.languages || [];
            langRow.visible = langs.length > 0;
            langCombo._codes = [""].concat(langs);
            langCombo.model = [qsTr("System default")].concat(langs.map(function(c) {
                var name = Qt.locale(c).nativeLanguageName;
                return name.charAt(0).toUpperCase() + name.slice(1);
            }));
            langCombo.currentIndex = Math.max(0, langCombo._codes.indexOf(data.language || ""));
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
            if (!ok) Bridge.cmd("notify", {msg: qsTr("Setting failed: %1").arg(err)});
        });
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 4

        RowLayout {
            Layout.fillWidth: true
            Label { text: qsTr("Theme"); Layout.fillWidth: true; color: Theme.onSurface }
            WdComboBox {
                id: themeCombo
                model: [qsTr("System"), qsTr("Light"), qsTr("Dark")]
                onActivated: dlg.set("theme", ["system","light","dark"][currentIndex])
            }
        }

        RowLayout {
            Layout.fillWidth: true
            Label { text: qsTr("Chat style"); Layout.fillWidth: true; color: Theme.onSurface }
            WdComboBox {
                id: chatStyleCombo
                model: [qsTr("Bubbles"), qsTr("Flat")]
                onActivated: dlg.set("chatStyle", ["bubbles","flat"][currentIndex])
            }
        }

        RowLayout {
            id: langRow
            Layout.fillWidth: true
            Label { text: qsTr("Language"); Layout.fillWidth: true; color: Theme.onSurface }
            ColumnLayout {
                WdComboBox {
                    id: langCombo
                    property var _codes: [""]
                    model: [qsTr("System default")]
                    onActivated: dlg.set("language", _codes[currentIndex] || "")
                }
                Label { text: qsTr("Applies after restart"); color: Theme.onSurfaceVariant; font.pixelSize: 12 }
            }
        }

        WdSwitchDelegate {
            id: notifSw
            Layout.fillWidth: true
            leftPadding: 0
            rightPadding: 0
            text: qsTr("Notifications")
            onToggled: dlg.set("notifications", checked)
            contentItem: ColumnLayout {
                Label { text: notifSw.text; color: Theme.onSurface }
                Label { text: qsTr("Show message notifications"); color: Theme.onSurfaceVariant; font.pixelSize: 12 }
            }
        }

        WdSwitchDelegate {
            id: traySw
            Layout.fillWidth: true
            leftPadding: 0
            rightPadding: 0
            text: qsTr("Close to tray")
            onToggled: dlg.set("closeToTray", checked)
            contentItem: ColumnLayout {
                Label { text: traySw.text; color: Theme.onSurface }
                Label { text: qsTr("Keep running when the window closes"); color: Theme.onSurfaceVariant; font.pixelSize: 12 }
            }
        }

        WdSwitchDelegate {
            id: autoSw
            Layout.fillWidth: true
            leftPadding: 0
            rightPadding: 0
            text: qsTr("Launch at login")
            onToggled: dlg.set("autostart", checked)
            contentItem: ColumnLayout {
                Label { text: autoSw.text; color: Theme.onSurface }
                Label { text: qsTr("Start WhoDat with your session"); color: Theme.onSurfaceVariant; font.pixelSize: 12 }
            }
        }

        WdSwitchDelegate {
            id: groupCallsSw
            Layout.fillWidth: true
            leftPadding: 0
            rightPadding: 0
            text: qsTr("Group calls (experimental)")
            onToggled: dlg.set("showGroupCalls", checked)
            contentItem: ColumnLayout {
                Label { text: groupCallsSw.text; color: Theme.onSurface }
                Label { text: qsTr("Allow calling from group chats"); color: Theme.onSurfaceVariant; font.pixelSize: 12 }
            }
        }

    }
}

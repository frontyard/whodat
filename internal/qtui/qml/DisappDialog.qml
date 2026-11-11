import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// DisappDialog picks a disappearing-message timer for the chat.
Dialog {
    id: dlg
    title: "Disappearing messages"
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 360
    standardButtons: Dialog.Ok | Dialog.Cancel

    property string jid: ""
    property int current: 0
    readonly property var choices: [
        {label: "Off", secs: 0},
        {label: "24 hours", secs: 86400},
        {label: "7 days", secs: 604800},
        {label: "90 days", secs: 7776000}
    ]

    function openFor(jid, secs) {
        dlg.jid = jid;
        dlg.current = secs || 0;
        for (var i = 0; i < radioGroup.buttons.length; i++)
            radioGroup.buttons[i].checked = (choices[i].secs === dlg.current);
        dlg.open();
    }

    onAccepted: {
        var secs = 0;
        for (var i = 0; i < radioGroup.buttons.length; i++)
            if (radioGroup.buttons[i].checked) secs = choices[i].secs;
        Bridge.cmd("setDisapp", {jid: dlg.jid, secs: secs}, function(ok, d, e) {
            if (!ok) Bridge.cmd("notify", {msg: "Timer failed: " + e});
        });
    }

    ColumnLayout {
        anchors.fill: parent
        Label {
            text: "New messages disappear for everyone after the selected time."
            wrapMode: Text.WordWrap
            Layout.fillWidth: true
            color: Theme.onSurfaceVariant
            font.pixelSize: 12
        }
        ButtonGroup { id: radioGroup }
        Repeater {
            model: dlg.choices
            WdRadioButton {
                required property int index
                required property var modelData
                ButtonGroup.group: radioGroup
                text: modelData.label
            }
        }
    }
}

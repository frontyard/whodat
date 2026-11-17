import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// PollDialog collects a question + options and sends a single-select poll.
Dialog {
    id: dlg
    title: qsTr("Create poll")
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 380

    ListModel { id: optModel }

    function openFor() {
        optModel.clear();
        optModel.append({text: ""});
        optModel.append({text: ""});
        question.text = "";
        dlg.open();
    }

    footer: DialogButtonBox {
        WdButton {
            text: qsTr("Cancel")
            DialogButtonBox.buttonRole: DialogButtonBox.RejectRole
            onClicked: dlg.close()
        }
        WdButton {
            text: qsTr("Send poll")
            enabled: question.text.trim() !== "" && dlg.optionCount() >= 2
            DialogButtonBox.buttonRole: DialogButtonBox.AcceptRole
            onClicked: {
                var opts = dlg.optionValues();
                Bridge.cmd("sendPoll", {jid: App.chatJID, question: question.text.trim(), options: opts}, function(ok, data, err) {
                    if (!ok) Bridge.cmd("notify", {msg: qsTr("Poll failed: %1").arg(err)});
                });
                dlg.close();
            }
        }
    }

    function optionValues() {
        var out = [];
        for (var i = 0; i < optModel.count; i++) {
            var t = optModel.get(i).text.trim();
            if (t !== "") out.push(t);
        }
        return out;
    }
    function optionCount() { return optionValues().length; }

    contentItem: ColumnLayout {
        spacing: 8
        width: 380

        TextField {
            id: question
            placeholderText: qsTr("Question")
            Layout.fillWidth: true
            Layout.margins: 8
        }

        Label {
            text: qsTr("Options")
            color: Theme.onSurfaceVariant
            font.pixelSize: 12
            Layout.leftMargin: 8
        }

        ListView {
            Layout.fillWidth: true
            Layout.preferredHeight: Math.min(contentHeight, 260)
            model: optModel
            clip: true
            spacing: 4

            delegate: RowLayout {
                required property int index
                required property string text
                width: ListView.view.width
                spacing: 4

                TextField {
                    placeholderText: qsTr("Option %1").arg(index + 1)
                    text: model.text
                    Layout.fillWidth: true
                    Layout.leftMargin: 8
                    onTextChanged: optModel.setProperty(index, "text", text)
                }
                WdRoundButton {
                    icon.source: Qt.resolvedUrl("icons/close.svg")
                    icon.color: Theme.onSurfaceVariant
                    flat: true
                    enabled: optModel.count > 2
                    onClicked: optModel.remove(index)
                }
            }
        }

        WdItemDelegate {
            text: qsTr("Add option")
            enabled: optModel.count < 12
            Layout.fillWidth: true
            onClicked: optModel.append({text: ""})
        }
    }
}

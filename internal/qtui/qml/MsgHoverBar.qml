import QtQuick
import QtQuick.Controls
import QtQuick.Controls.Material

// MsgHoverBar is the floating quick-action pill that fades in over a hovered
// transcript row — shortcuts for the row-menu's common actions plus a ⋮
// overflow that opens the full context menu. Hosts drive visibility via the
// row's HoverHandler (see MessageRow/MessageRowFlat).
Rectangle {
    id: bar

    signal reactClicked()
    signal replyClicked()
    signal forwardClicked()
    signal editClicked()
    signal moreClicked()

    // per-chat gates — reactions depend on the protocol's capabilities,
    // edit only applies to the user's own text messages.
    property bool canReact: true
    property bool canEdit: false

    implicitWidth: pillRow.implicitWidth + 10
    implicitHeight: 34
    radius: 17
    color: Theme.surfaceContainerHigh
    border.color: Theme.outlineVariant
    border.width: 1

    Row {
        id: pillRow
        anchors.centerIn: parent
        spacing: 2

        Repeater {
            model: [
                { act: "react",   icon: "sentiment_satisfied", tip: qsTr("React") },
                { act: "reply",   icon: "reply",               tip: qsTr("Reply") },
                { act: "forward", icon: "forward",             tip: qsTr("Forward") },
                { act: "edit",    icon: "edit",                tip: qsTr("Edit") },
                { act: "more",    icon: "more_vert",           tip: qsTr("More") }
            ]
            delegate: Item {
                required property var modelData
                visible: bar.gate(modelData.act)
                width: visible ? 30 : 0
                height: 30

                Rectangle {
                    anchors.fill: parent
                    radius: 15
                    color: Qt.rgba(Theme.onSurface.r, Theme.onSurface.g, Theme.onSurface.b,
                                   btnHover.hovered ? 0.10 : 0)
                }
                WdIcon {
                    anchors.centerIn: parent
                    source: Qt.resolvedUrl("icons/" + modelData.icon + ".svg")
                    color: Theme.onSurfaceVariant
                    iconSize: 17
                }
                TapHandler { onTapped: bar.dispatch(modelData.act) }
                HoverHandler { id: btnHover; cursorShape: Qt.PointingHandCursor }

                ToolTip.text: modelData.tip
                ToolTip.visible: btnHover.hovered
            }
        }
    }

    function gate(act) {
        if (act === "react") return canReact;
        if (act === "edit")  return canEdit;
        return true;
    }
    function dispatch(act) {
        if (act === "react")        reactClicked();
        else if (act === "reply")   replyClicked();
        else if (act === "forward") forwardClicked();
        else if (act === "edit")    editClicked();
        else                        moreClicked();
    }
}

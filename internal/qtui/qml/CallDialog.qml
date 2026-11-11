import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material
import QtMultimedia

// CallDialog is the in-call window: remote video fills the area, the local
// preview floats in a corner (click to hop), controls bar below. Driven by
// App.call* state keys; opens when callActive flips true.
Window {
    id: callWin
    width: 640
    height: 480
    minimumWidth: 360
    minimumHeight: 300
    title: "WhoDat — " + App.callName
    color: Theme.surface
    flags: Qt.Window | Qt.WindowTitleHint | Qt.WindowCloseButtonHint

    property int localCorner: 0 // 0=BR 1=TR 2=TL 3=BL

    function placeLocal() {
        var x = 12, y = 12;
        if (localCorner === 0 || localCorner === 1)
            x = Math.max(videoArea.width - localFrame.width - 12, 0);
        if (localCorner === 0 || localCorner === 3)
            y = Math.max(videoArea.height - localFrame.height - 12, 0);
        localFrame.x = x;
        localFrame.y = y;
    }

    Component.onCompleted: {
        RemoteVideoSession.videoOutput = remoteVO;
        LocalVideoSession.videoOutput = localVO;
    }

    // App.callActive/callEnded are map keys — watch via bindings (the map's
    // valueChanged signal does not reach QML Connections).
    property bool callActiveW: App.callActive
    onCallActiveWChanged:
        if (callActiveW) { callWin.show(); callWin.raise(); callWin.requestActivate(); }
        else { callWin.hide(); }
    property bool callEndedW: App.callEnded
    onCallEndedWChanged:
        if (callEndedW) endTimer.restart();
    Timer { id: endTimer; interval: 1200; onTriggered: callWin.close() }
    onClosing: Bridge.cmd("hangupCall", {})

    // Reshape to the remote video's real aspect: the window follows
    // the frame dims so the video fills
    // the area — fixed presets would pillarbox anything but exact 16:9/9:16.
    property string remSize: App.callRemoteSize
    onRemSizeChanged: {
        var p = remSize.split("x");
        var rw = parseInt(p[0]), rh = parseInt(p[1]);
        if (!(rw > 0) || !(rh > 0)) return;
        var aspect = rw / rh;
        var chrome = 120; // header (48) + controls bar (72)
        // Keep the video area ~constant at the old landscape preset's size.
        var ah = Math.round(Math.sqrt(640 * 360 / aspect));
        var aw = Math.round(ah * aspect);
        var maxW = Screen.desktopAvailableWidth - 60;
        var maxH = Screen.desktopAvailableHeight - 60;
        callWin.width = Math.min(Math.max(aw, callWin.minimumWidth), maxW);
        callWin.height = Math.min(Math.max(ah + chrome, callWin.minimumHeight), maxH);
        callWin.placeLocal();
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // ---- header --------------------------------------------------
        Rectangle {
            Layout.fillWidth: true
            implicitHeight: 48
            color: Theme.surfaceContainer
            Label {
                anchors.centerIn: parent
                text: App.callName + (App.callPhase !== "" ? " — " + App.callPhase : "")
                font.pixelSize: 15
                font.bold: true
                color: Theme.onSurface
            }
        }

        // ---- video area ----------------------------------------------
        Item {
            id: videoArea
            Layout.fillWidth: true
            Layout.fillHeight: true
            Layout.minimumHeight: 200
            onWidthChanged: callWin.placeLocal()
            onHeightChanged: callWin.placeLocal()

            // remote frames
            VideoOutput {
                id: remoteVO
                anchors.fill: parent
                fillMode: VideoOutput.PreserveAspectFit
                visible: App.callRemoteVideo === true
            }
            // placeholder for audio calls
            Item {
                anchors.fill: parent
                visible: App.callRemoteVideo !== true
                Column {
                    anchors.centerIn: parent
                    spacing: 12
                    Avatar { source: ""; name: App.callName; size: 96; anchors.horizontalCenter: parent.horizontalCenter }
                    Label {
                        text: App.callPhase
                        color: Theme.onSurfaceVariant
                        anchors.horizontalCenter: parent.horizontalCenter
                    }
                }
            }

            // local preview PiP — click to hop corners
            Rectangle {
                id: localFrame
                visible: App.callVideo === true
                width: 160
                height: 120
                radius: Theme.radiusSmall
                color: Theme.surfaceContainerHighest
                clip: true
                border.color: Theme.outlineVariant

                VideoOutput {
                    id: localVO
                    anchors.fill: parent
                    fillMode: VideoOutput.PreserveAspectCrop
                }
                MouseArea {
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: {
                        callWin.localCorner = (callWin.localCorner + 1) % 4;
                        callWin.placeLocal();
                    }
                    ToolTip.visible: containsMouse
                    ToolTip.text: "Click to move preview"
                }
            }

            // E2E key fingerprint — same four emoji the peer's client shows
            // (visible only while a backend reports verification emoji).
            Rectangle {
                anchors.top: parent.top
                anchors.right: parent.right
                anchors.margins: 12
                visible: emoLbl.text !== ""
                radius: Theme.radiusSmall
                color: Qt.rgba(0, 0, 0, 0.45)
                implicitWidth: emoLbl.implicitWidth + 20
                implicitHeight: emoLbl.implicitHeight + 12
                Label {
                    id: emoLbl
                    anchors.centerIn: parent
                    text: App.callEmojis
                    font.pixelSize: 22
                }
                MouseArea {
                    anchors.fill: parent
                    hoverEnabled: true
                    ToolTip.visible: containsMouse
                    ToolTip.text: "Encryption verification — these emojis should match on the other end"
                }
            }
        }

        // ---- bars -----------------------------------------------------
        Rectangle {
            Layout.fillWidth: true
            implicitHeight: 72
            color: "transparent"

            // accept/reject — incoming only
            RowLayout {
                anchors.centerIn: parent
                spacing: 32
                visible: App.callIncoming === true

                WdRoundButton {
                    icon.source: Qt.resolvedUrl("icons/call_end.svg")
                    icon.color: Theme.onErrorContainer
                    icon.width: 24
                    icon.height: 24
                    Material.background: Theme.errorContainer
                    Material.foreground: Theme.onErrorContainer
                    implicitWidth: 56
                    implicitHeight: 56
                    onClicked: Bridge.cmd("rejectCall", {})
                    ToolTip.visible: hovered
                    ToolTip.text: "Decline"
                }
                WdRoundButton {
                    icon.source: Qt.resolvedUrl("icons/call.svg")
                    icon.color: Theme.onPrimary
                    icon.width: 24
                    icon.height: 24
                    Material.background: Theme.primary
                    Material.foreground: Theme.onPrimary
                    implicitWidth: 56
                    implicitHeight: 56
                    onClicked: Bridge.cmd("answerCall", {})
                    ToolTip.visible: hovered
                    ToolTip.text: "Accept"
                }
            }

            // in-call controls
            RowLayout {
                anchors.centerIn: parent
                spacing: 14
                visible: App.callIncoming !== true && App.callEnded !== true

                WdRoundButton {
                    icon.source: Qt.resolvedUrl(App.callMuted ? "icons/mic_off.svg" : "icons/mic.svg")
                    icon.color: App.callMuted ? Theme.onErrorContainer : Theme.onSurface
                    icon.width: 22
                    icon.height: 22
                    Material.background: App.callMuted ? Theme.errorContainer : Theme.surfaceContainerHigh
                    implicitWidth: 52
                    implicitHeight: 52
                    onClicked: Bridge.cmd("muteCall", {on: !App.callMuted})
                    ToolTip.visible: hovered
                    ToolTip.text: App.callMuted ? "Unmute" : "Mute"
                }
                WdRoundButton {
                    icon.source: Qt.resolvedUrl(App.callVideo ? "icons/videocam.svg" : "icons/videocam_off.svg")
                    icon.color: App.callVideo ? Theme.onPrimary : Theme.onSurface
                    icon.width: 22
                    icon.height: 22
                    Material.background: App.callVideo ? Theme.primary : Theme.surfaceContainerHigh
                    implicitWidth: 52
                    implicitHeight: 52
                    onClicked: Bridge.cmd("videoCall", {on: !App.callVideo})
                    ToolTip.visible: hovered
                    ToolTip.text: "Toggle video"
                }
                WdRoundButton {
                    icon.source: Qt.resolvedUrl("icons/rotate_left.svg")
                    icon.color: Theme.onSurface
                    icon.width: 22
                    icon.height: 22
                    Material.background: Theme.surfaceContainerHigh
                    implicitWidth: 52
                    implicitHeight: 52
                    onClicked: Bridge.cmd("rotateVideo", {turns: 3})
                    ToolTip.visible: hovered
                    ToolTip.text: "Rotate incoming video left"
                }
                WdRoundButton {
                    icon.source: Qt.resolvedUrl("icons/rotate_right.svg")
                    icon.color: Theme.onSurface
                    icon.width: 22
                    icon.height: 22
                    Material.background: Theme.surfaceContainerHigh
                    implicitWidth: 52
                    implicitHeight: 52
                    onClicked: Bridge.cmd("rotateVideo", {turns: 1})
                    ToolTip.visible: hovered
                    ToolTip.text: "Rotate incoming video right"
                }
                WdRoundButton {
                    icon.source: Qt.resolvedUrl("icons/call_end.svg")
                    icon.color: Theme.onErrorContainer
                    icon.width: 24
                    icon.height: 24
                    Material.background: Theme.errorContainer
                    Material.foreground: Theme.onErrorContainer
                    implicitWidth: 52
                    implicitHeight: 52
                    onClicked: Bridge.cmd("hangupCall", {})
                    ToolTip.visible: hovered
                    ToolTip.text: "Hang up"
                }
            }

            // ended state
            Label {
                anchors.centerIn: parent
                visible: App.callEnded === true
                text: "Call ended" + (App.callEndedReason !== "" ? " — " + App.callEndedReason : "")
                color: Theme.onSurfaceVariant
                font.pixelSize: 14
            }
        }
    }
}

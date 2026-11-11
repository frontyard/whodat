import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material
import QtMultimedia

// VideoPlayer plays a video file in a dialog with basic controls.
Dialog {
    id: dlg
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: Math.min(parent ? parent.width * 0.9 : 900, 900)
    height: Math.min(parent ? parent.height * 0.9 : 700, 700)
    padding: 0

    property string path: ""

    function play(path) {
        dlg.path = path;
        dlg.open();
        player.source = Bridge.fileUrl(path);
        player.play();
    }

    onClosed: player.stop()

    contentItem: Item {
        VideoOutput {
            id: vout
            anchors.fill: parent
            fillMode: VideoOutput.PreserveAspectFit
        }

        MediaPlayer {
            id: player
            videoOutput: vout
            audioOutput: AudioOutput {}
            onErrorOccurred: (e, s) => Bridge.cmd("notify", {msg: "Playback failed: " + s})
        }

        Rectangle {
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.bottom: parent.bottom
            height: 48
            color: Qt.rgba(0, 0, 0, 0.55)

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: 10
                anchors.rightMargin: 10
                spacing: 8

                WdRoundButton {
                    icon.source: Qt.resolvedUrl(player.playbackState === MediaPlayer.PlayingState ? "icons/pause.svg" : "icons/play_arrow.svg")
                    icon.color: "white"
                    onClicked: player.playbackState === MediaPlayer.PlayingState ? player.pause() : player.play()
                }
                Slider {
                    Layout.fillWidth: true
                    from: 0
                    to: Math.max(1, player.duration)
                    value: player.position
                    onMoved: player.position = value
                }
                Label {
                    text: fmt(player.position) + " / " + fmt(player.duration)
                    color: "white"
                    font.pixelSize: 11
                }
                WdRoundButton {
                    icon.source: Qt.resolvedUrl("icons/close.svg")
                    icon.color: "white"
                    onClicked: dlg.close()
                }
            }
        }
    }

    function fmt(ms) {
        var s = Math.floor(ms / 1000);
        var m = Math.floor(s / 60), r = s % 60;
        return m + ":" + (r < 10 ? "0" : "") + r;
    }
}

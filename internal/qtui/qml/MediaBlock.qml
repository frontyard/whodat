import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material
import QtMultimedia

// MediaBlock renders the attachment area of a message bubble by type:
// image/sticker thumbnail, video card, audio/PTT player, document row.
// Files resolve lazily via mediaUrl (auto-download under App.autoDownloadMax).
Item {
    id: blk

    property string msgId: ""
    property string msgType: ""
    property string mediaPath: ""
    property string mediaThumb: ""
    property string mediaMIME: ""
    property string mediaName: ""
    property string mediaSize: ""
    property int    mediaBytes: 0
    property int    mediaW: 0
    property int    mediaH: 0
    property int    duration: 0
    property var    waveform: null
    property bool   viewOnce: false
    property color  fg: Theme.onBubbleIn

    signal openViewer(string path)
    signal playVideo(string path)

    implicitWidth: 260
    implicitHeight: content.implicitHeight
    visible: isMedia

    property bool isMedia: ["image", "sticker", "video", "audio", "ptt", "document"].indexOf(msgType) >= 0
    property string localPath: mediaPath
    property string thumbPath: mediaThumb
    property bool fetching: false

    function fetch() {
        if (blk.fetching) return;
        blk.fetching = true;
        Bridge.cmd("mediaUrl", {jid: App.chatJID, msgId: blk.msgId}, function(ok, d, e) {
            blk.fetching = false;
            if (ok && d.path) blk.localPath = d.path;
        });
    }

    function fetchThumb() {
        if (blk.thumbPath !== "") return;
        Bridge.cmd("mediaThumb", {jid: App.chatJID, msgId: blk.msgId}, function(ok, d, e) {
            if (ok && d.path) blk.thumbPath = d.path;
        });
    }

    function fmtDur(s) {
        var m = Math.floor(s / 60), r = s % 60;
        return m + ":" + (r < 10 ? "0" : "") + r;
    }

    Component.onCompleted: {
        // auto-download small attachments on first render
        if (blk.localPath === "" && blk.isMedia
            && (blk.mediaBytes === 0 || blk.mediaBytes <= App.autoDownloadMax))
            blk.fetch();
        // older rows carry the poster inside their proto — extract on view
        if (blk.msgType === "video" && blk.thumbPath === "" && !blk.viewOnce)
            blk.fetchThumb();
    }

    ColumnLayout {
        id: content
        width: parent.width
        spacing: 4

        // ---- image / sticker -------------------------------------------
        Rectangle {
            visible: blk.msgType === "image" || blk.msgType === "sticker"
            Layout.fillWidth: true
            // Cap the card width — in the flat transcript the row stretches
            // full-window, and a full-bleed media card looks wrong.
            Layout.maximumWidth: 320
            implicitHeight: blk.msgType === "sticker" ? 140 : Math.min(320, blk.mediaH > 0 ? blk.mediaH * width / Math.max(1, blk.mediaW) : 200)
            radius: 8
            color: Qt.rgba(0, 0, 0, 0.12)
            clip: true

            Image {
                anchors.fill: parent
                // Source must be gated on msgType: this binding still
                // evaluates while the parent is hidden, and audio/doc
                // paths aren't decodable images.
                source: (blk.msgType === "image" || blk.msgType === "sticker")
                        ? Bridge.fileUrl(blk.localPath) : ""
                fillMode: blk.msgType === "sticker" ? Image.PreserveAspectFit : Image.PreserveAspectCrop
                asynchronous: true
                cache: false
                visible: status === Image.Ready
            }

            BusyIndicator {
                anchors.centerIn: parent
                visible: blk.localPath === "" && blk.fetching
                running: visible
            }

            WdButton {
                anchors.centerIn: parent
                visible: blk.localPath === "" && !blk.fetching
                text: "Download " + blk.mediaSize
                onClicked: blk.fetch()
            }

            Row {
                anchors.centerIn: parent
                visible: blk.viewOnce && blk.localPath === ""
                spacing: 4
                WdIcon {
                    source: Qt.resolvedUrl("icons/local_fire_department.svg")
                    color: blk.fg
                    iconSize: 16
                }
                Label { text: "View once"; color: blk.fg }
            }

            MouseArea {
                anchors.fill: parent
                enabled: blk.localPath !== ""
                cursorShape: Qt.PointingHandCursor
                onClicked: blk.openViewer(blk.localPath)
            }
        }

        // ---- video -----------------------------------------------------
        Rectangle {
            visible: blk.msgType === "video"
            Layout.fillWidth: true
            Layout.maximumWidth: 320
            // Aspect-correct height from the wire dims — GIFs are often
            // square-ish and a fixed strip crops them badly.
            implicitHeight: blk.mediaW > 0 && blk.mediaH > 0
                ? Math.min(400, Math.max(120, width * blk.mediaH / blk.mediaW))
                : 160
            radius: 8
            color: Qt.rgba(0, 0, 0, 0.25)
            clip: true

            Image {
                anchors.fill: parent
                // sender-provided first frame (GIFs are videos on the wire)
                source: blk.thumbPath !== "" && !blk.viewOnce
                        ? Bridge.fileUrl(blk.thumbPath) : ""
                fillMode: Image.PreserveAspectCrop
                asynchronous: true
                cache: false
                visible: status === Image.Ready
            }

            WdIcon {
                anchors.centerIn: parent
                source: Qt.resolvedUrl("icons/play_arrow.svg")
                color: "white"
                iconSize: 36
                visible: blk.localPath !== "" || blk.mediaBytes <= App.autoDownloadMax
            }
            BusyIndicator {
                anchors.centerIn: parent
                visible: blk.fetching
                running: visible
            }
            Label {
                anchors.bottom: parent.bottom
                anchors.left: parent.left
                anchors.margins: 8
                text: (blk.mediaName !== "" ? blk.mediaName : "Video") + "  " + blk.mediaSize
                color: "white"
                font.pixelSize: 12
                style: Text.Outline
                styleColor: "black"
            }
            MouseArea {
                anchors.fill: parent
                cursorShape: Qt.PointingHandCursor
                onClicked: {
                    if (blk.localPath !== "") blk.playVideo(blk.localPath);
                    else blk.fetch();
                }
            }
        }

        // ---- audio / ptt -------------------------------------------------
        RowLayout {
            visible: blk.msgType === "audio" || blk.msgType === "ptt"
            Layout.fillWidth: true
            spacing: 8

                WdRoundButton {
                icon.source: Qt.resolvedUrl(player.playbackState === MediaPlayer.PlayingState ? "icons/pause.svg" : "icons/play_arrow.svg")
                icon.color: blk.fg
                flat: true
                onClicked: {
                    if (blk.localPath === "") { blk.fetch(); return; }
                    if (player.playbackState === MediaPlayer.PlayingState) player.pause();
                    else {
                        player.source = Bridge.fileUrl(blk.localPath);
                        player.play();
                    }
                }
            }

            ColumnLayout {
                Layout.fillWidth: true
                spacing: 1

                // PTT waveform bars (downsampled to ~40)
                Row {
                    visible: blk.msgType === "ptt" && wfBars.length > 0
                    height: 22
                    spacing: 1
                    property var wfBars: blk.waveData()
                    Repeater {
                        model: parent.wfBars
                        Rectangle {
                            required property var modelData
                            width: 2
                            height: 3 + 19 * modelData
                            radius: 1
                            color: blk.fg
                            anchors.bottom: parent.bottom
                            opacity: 0.75
                        }
                    }
                }

                RowLayout {
                    spacing: 4
                    Layout.fillWidth: true
                    WdIcon {
                        source: Qt.resolvedUrl(blk.msgType === "ptt" ? "icons/mic.svg" : "icons/description.svg")
                        color: blk.fg
                        iconSize: 12
                        Layout.alignment: Qt.AlignVCenter
                    }
                    Label {
                        text: blk.fmtDur(blk.duration) + (blk.mediaName !== "" ? "  " + blk.mediaName : "")
                        color: blk.fg
                        font.pixelSize: 12
                        elide: Text.ElideRight
                        Layout.fillWidth: true
                    }
                }
            }

            MediaPlayer {
                id: player
                audioOutput: AudioOutput {}
                onPlaybackStateChanged: {
                    if (playbackState === MediaPlayer.StoppedState) source = "";
                }
                onErrorOccurred: (e, s) => Bridge.cmd("notify", {msg: "Playback failed"})
            }
        }

        // ---- document ---------------------------------------------------
        RowLayout {
            visible: blk.msgType === "document"
            Layout.fillWidth: true
            spacing: 8

            WdIcon {
                source: Qt.resolvedUrl("icons/description.svg")
                color: blk.fg
                iconSize: 26
                Layout.alignment: Qt.AlignVCenter
            }

            ColumnLayout {
                Layout.fillWidth: true
                spacing: 0
                Label {
                    text: blk.mediaName !== "" ? blk.mediaName : "Document"
                    elide: Text.ElideMiddle
                    color: blk.fg
                    font.pixelSize: 13
                    Layout.fillWidth: true
                }
                Label {
                    text: blk.mediaSize + (blk.fetching ? " — downloading…" : "")
                    color: blk.fg
                    opacity: 0.7
                    font.pixelSize: 11
                }
            }

            WdRoundButton {
                icon.source: Qt.resolvedUrl("icons/download.svg")
                icon.color: blk.fg
                flat: true
                enabled: !blk.fetching
                onClicked: {
                    if (blk.localPath !== "")
                        Bridge.cmd("openMedia", {jid: App.chatJID, msgId: blk.msgId});
                    else
                        blk.fetch();
                }
            }
        }
    }

    // waveData flattens the waveform role ([]byte → JSON array → 0..1 floats).
    function waveData() {
        var w = blk.waveform;
        if (!w) return [];
        var arr = w;
        if (typeof w === "string") {
            try { arr = JSON.parse(w); } catch (e) { return []; }
        }
        if (!arr || arr.length === 0) return [];
        var max = 1;
        for (var i = 0; i < arr.length; i++) if (arr[i] > max) max = arr[i];
        var step = Math.max(1, Math.floor(arr.length / 40));
        var out = [];
        for (var j = 0; j < arr.length; j += step) out.push(arr[j] / max);
        return out;
    }
}

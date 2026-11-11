import QtQuick
import QtQuick.Controls
import QtQuick.Controls.Material

// ToastArea renders transient messages written to App.toast
// (JSON {n,msg}); n dedupes identical texts.
Item {
    id: root
    z: 100

    ListModel { id: toasts }

    // App.toast carries JSON {n,msg}; watch it via a property binding — the
    // map's valueChanged signal does not reach QML Connections.
    property var toastJson: App.toast
    onToastJsonChanged: {
        if (!toastJson) return;
        var t;
        try { t = JSON.parse(toastJson); } catch (e) { return; }
        if (!t.msg) return;
        toasts.append({n: t.n, msg: t.msg});
        if (toasts.count > 4) toasts.remove(0);
    }

    Column {
        id: toastCol
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: 24
        spacing: 8
        width: Math.min(420, parent.width - 48)

        Repeater {
            model: toasts
            delegate: Rectangle {
                id: delegateRoot
                required property int n
                required property string msg
                required property int index
                width: toastCol ? toastCol.width : 400
                implicitWidth: toastLabel.implicitWidth + 32
                implicitHeight: toastLabel.implicitHeight + 16
                radius: 8
                color: Theme.inverseSurface
                anchors.horizontalCenter: parent.horizontalCenter
                opacity: 0
                Component.onCompleted: showAnim.start()

                NumberAnimation { id: showAnim; target: delegateRoot; property: "opacity"; to: 1; duration: 150 }

                Timer {
                    interval: 4000
                    running: true
                    onTriggered: {
                        for (var i = 0; i < toasts.count; i++) {
                            if (toasts.get(i).n === n) { toasts.remove(i); break; }
                        }
                    }
                }

                Label {
                    id: toastLabel
                    anchors.centerIn: parent
                    width: Math.min(implicitWidth, 360)
                    wrapMode: Text.Wrap
                    horizontalAlignment: Text.AlignHCenter
                    color: Theme.inverseOnSurface
                    text: msg
                }
            }
        }
    }
}

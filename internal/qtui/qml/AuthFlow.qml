import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// AuthFlow is the generic add-account flow: a protocol picker driven by the
// registered backends, then a stage renderer driven by App.authStage — the
// service advances it (qr → phone → code → password) via EvAuthStage; this
// component never branches on protocol.
Item {
    id: flow
    // stages the active account's service offers (the authStart reply) —
    // decides whether the QR view offers the phone-code alternative.
    property var stages: []
    // showBack: first-run renders Back under the stages; the Accounts
    // dialog moves Back into the dialog footer instead.
    property bool showBack: true
    // showHeading: first-run keeps the picker heading/subtitle; the
    // Accounts dialog drops them — its own title says "Add account".
    property bool showHeading: true
    // Emitted when the user backs out — after the cancel/back op cleared
    // App.authAcct, so the host can return to whatever preceded the flow.
    signal back()

    function submit(stage, value) {
        Bridge.cmd("authSubmit", {acct: App.authAcct, stage: stage, value: value},
                   function(ok, data, err) {
                       if (!ok) Bridge.cmd("notify", {msg: err});
                   });
    }

    function cancel() {
        // Every flow creates its account — backing out deletes it; an
        // unlinked registration is never kept "for resume".
        Bridge.cmd("authCancel", {acct: App.authAcct}, function(ok) {
            flow.back();
        });
    }

    StackLayout {
        anchors.fill: parent
        currentIndex: App.authAcct === "" ? 0 : 1

        // ── Page 0: protocol picker ────────────────────────────────────
        ColumnLayout {
            spacing: 12

            // Top+bottom spacers center the block vertically — without
            // the top one the picker hugs the upper edge of its host.
            Item { Layout.fillHeight: true }
            Label {
                visible: flow.showHeading
                text: "Add an account"
                font.pixelSize: 22
                font.bold: true
                color: Theme.onSurface
                Layout.alignment: Qt.AlignHCenter
            }
            Label {
                visible: flow.showHeading
                text: "Link your WhatsApp account — you can add more accounts later from the Accounts menu."
                color: Theme.onSurfaceVariant
                wrapMode: Text.Wrap
                horizontalAlignment: Text.AlignHCenter
                Layout.fillWidth: true
            }

            Repeater {
                id: protoList
                model: []
                delegate: WdButton {
                    required property var modelData
                    text: modelData.canAdd === true
                          ? modelData.name
                          : modelData.name + " — already linked"
                    enabled: modelData.canAdd === true
                    Layout.fillWidth: true
                    Layout.leftMargin: 24
                    Layout.rightMargin: 24
                    Material.background: Theme.primary
                    Material.foreground: Theme.onPrimary
                    onClicked: {
                        Bridge.cmd("authStart", {proto: modelData.proto},
                                   function(ok, data, err) {
                                       if (ok) {
                                           flow.stages = data.stages || [];
                                       } else {
                                           Bridge.cmd("notify", {msg: err});
                                       }
                                   });
                    }
                }
            }
            Label {
                visible: protoList.count === 0
                text: "Loading…"
                color: Theme.onSurfaceVariant
                Layout.alignment: Qt.AlignHCenter
            }
            Item { Layout.fillHeight: true }
        }

        // ── Page 1: auth stages ────────────────────────────────────────
        ColumnLayout {
            id: stageCol
            spacing: 16

            // Nested layouts can't rely on Layout.fillWidth (ignored by
            // Qt 6.8 on layout children) — preferredWidth binds the stage
            // columns to the stage page's real width instead.
            // QR — WhatsApp companion pairing.
            ColumnLayout {
                spacing: 16
                visible: App.authStage === "qr" || App.authStage === ""
                Layout.fillWidth: true
                Layout.preferredWidth: stageCol.width

                Label {
                    text: App.loginStatus || "Waiting for a QR code…"
                    color: Theme.onSurfaceVariant
                    wrapMode: Text.Wrap
                    horizontalAlignment: Text.AlignHCenter
                    Layout.fillWidth: true
                }
                Rectangle {
                    width: 272; height: 272
                    radius: Theme.radius
                    color: "#FFFFFF"
                    Layout.alignment: Qt.AlignHCenter
                    border.color: Theme.outlineVariant
                    border.width: 1
                    Image {
                        anchors.fill: parent
                        anchors.margins: 8
                        source: Bridge.fileUrl(App.qrPath)
                        fillMode: Image.PreserveAspectFit
                        cache: false
                        visible: App.qrPath !== ""
                    }
                    BusyIndicator {
                        anchors.centerIn: parent
                        running: App.qrPath === ""
                        visible: App.qrPath === ""
                    }
                }
            }

            // phone — enter a number (TG login, or WA pair-code request).
            ColumnLayout {
                spacing: 12
                visible: App.authStage === "phone"
                Layout.fillWidth: true
                Layout.preferredWidth: stageCol.width
                Label {
                    text: App.authHint || "Enter your phone number."
                    color: Theme.onSurfaceVariant
                    wrapMode: Text.Wrap
                    horizontalAlignment: Text.AlignHCenter
                    Layout.fillWidth: true
                }
                TextField {
                    id: phoneField
                    placeholderText: "Phone number (e.g. +15551234567)"
                    inputMethodHints: Qt.ImhDigitsOnly | Qt.ImhNoPredictiveText
                    Layout.fillWidth: true
                    onAccepted: flow.submit("phone", text)
                }
                WdButton {
                    text: "Continue"
                    enabled: phoneField.text.length >= 6
                    Layout.fillWidth: true
                    Material.background: Theme.primary
                    Material.foreground: Theme.onPrimary
                    onClicked: flow.submit("phone", phoneField.text)
                }
            }

            // code — OTP/login code.
            ColumnLayout {
                spacing: 12
                visible: App.authStage === "code"
                Layout.fillWidth: true
                Layout.preferredWidth: stageCol.width
                Label {
                    text: App.authHint || "Enter the code you received."
                    color: Theme.onSurfaceVariant
                    wrapMode: Text.Wrap
                    horizontalAlignment: Text.AlignHCenter
                    Layout.fillWidth: true
                }
                TextField {
                    id: codeField
                    placeholderText: "Code"
                    inputMethodHints: Qt.ImhDigitsOnly | Qt.ImhNoPredictiveText
                    Layout.fillWidth: true
                    onAccepted: flow.submit("code", text)
                }
                WdButton {
                    text: "Continue"
                    enabled: codeField.text.length >= 4
                    Layout.fillWidth: true
                    Material.background: Theme.primary
                    Material.foreground: Theme.onPrimary
                    onClicked: flow.submit("code", codeField.text)
                }
            }

            // password — 2FA.
            ColumnLayout {
                spacing: 12
                visible: App.authStage === "password"
                Layout.fillWidth: true
                Layout.preferredWidth: stageCol.width
                Label {
                    text: App.authHint || "Enter your two-step verification password."
                    color: Theme.onSurfaceVariant
                    wrapMode: Text.Wrap
                    horizontalAlignment: Text.AlignHCenter
                    Layout.fillWidth: true
                }
                TextField {
                    id: passField
                    placeholderText: "Password"
                    echoMode: TextInput.Password
                    Layout.fillWidth: true
                    onAccepted: flow.submit("password", text)
                }
                WdButton {
                    text: "Continue"
                    enabled: passField.text.length > 0
                    Layout.fillWidth: true
                    Material.background: Theme.primary
                    Material.foreground: Theme.onPrimary
                    onClicked: flow.submit("password", passField.text)
                }
            }

            // paircode — display the 8-digit code to enter on the phone.
            ColumnLayout {
                spacing: 12
                visible: App.authStage === "paircode"
                Layout.fillWidth: true
                Layout.preferredWidth: stageCol.width
                Label {
                    text: "Enter this code on your phone:"
                    color: Theme.onSurfaceVariant
                    horizontalAlignment: Text.AlignHCenter
                    Layout.fillWidth: true
                }
                Label {
                    text: App.authHint
                    font.pixelSize: 30
                    font.bold: true
                    color: Theme.primary
                    Layout.alignment: Qt.AlignHCenter
                }
            }

            // failed — AuthHint carries the error.
            ColumnLayout {
                spacing: 12
                visible: App.authStage === "failed"
                Layout.fillWidth: true
                Layout.preferredWidth: stageCol.width
                Label {
                    text: App.authHint || "Linking failed."
                    color: Theme.error
                    wrapMode: Text.Wrap
                    horizontalAlignment: Text.AlignHCenter
                    Layout.fillWidth: true
                }
            }

            // phone-code alternative on the QR stage (WA offers "phone") —
            // shown alongside the QR, not hidden behind a reveal button.
            Label {
                visible: pairAlt.visible
                text: "Or pair with code"
                color: Theme.onSurfaceVariant
                Layout.alignment: Qt.AlignHCenter
            }
            ColumnLayout {
                id: pairAlt
                visible: App.authStage === "qr" && flow.stages.indexOf("phone") >= 0
                spacing: 8
                Layout.fillWidth: true
                Layout.preferredWidth: stageCol.width
                TextField {
                    id: altPhoneField
                    placeholderText: "Phone number (e.g. +15551234567)"
                    inputMethodHints: Qt.ImhDigitsOnly | Qt.ImhNoPredictiveText
                    Layout.fillWidth: true
                    onAccepted: flow.submit("phone", text)
                }
                WdButton {
                    text: "Request code"
                    enabled: altPhoneField.text.length >= 6
                    Layout.fillWidth: true
                    Material.background: Theme.primary
                    Material.foreground: Theme.onPrimary
                    onClicked: flow.submit("phone", altPhoneField.text)
                }
            }

            Item { Layout.fillHeight: true }

            WdButton {
                visible: flow.showBack
                text: "Back"
                flat: true
                Material.foreground: Material.accent
                Layout.alignment: Qt.AlignHCenter
                onClicked: flow.cancel()
            }
        }
    }

    Component.onCompleted: {
        Bridge.cmd("listBackends", {}, function(ok, data) {
            if (ok) protoList.model = data.backends || [];
        });
    }
}

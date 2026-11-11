import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// AccountsPane is the Accounts-dialog surface: the list of linked
// accounts — log out, open per-account settings — plus the picker and
// stage-driven auth flow for adding another (App.authAcct / wantPicker).
// The first-run login path hosts AuthFlow directly and never lists here.
Item {
    id: pane

    // wantPicker flips the stack to the picker page while App.authAcct is
    // still empty; cleared when a flow starts or the user backs out.
    property bool wantPicker: false

    // Rows pass their privacy capability so the settings dialog can
    // show/hide the privacy section without knowing protocols.
    signal accountSelected(string acct, bool privacy)

    // inFlow: the picker or a mid-auth stage page is showing — the
    // dialog renders its footer Back while this holds.
    readonly property bool inFlow: App.authAcct !== "" || pane.wantPicker

    // The list page hugs its content so the dialog can size itself to the
    // account count instead of leaving dead space below the rows.
    implicitHeight: listPage.implicitHeight

    // Protocol glyphs for account rows — unknown protos fall back to a
    // generic person icon.
    readonly property var protoIcons: ({
        "whatsapp": "icons/whatsapp.svg"
    })

    // goBack is the dialog footer Back: cancels mid-auth, otherwise
    // leaves the picker for the account list.
    function goBack() {
        if (App.authAcct !== "") flow.cancel();
        else pane.wantPicker = false;
    }

    function loadAccounts() {
        Bridge.cmd("listAccounts", {}, function(ok, data) {
            if (ok) acctList.model = data.accounts || [];
        });
    }

    // Live-refresh rows on conn-state changes, and return to the list
    // whenever the auth flow clears its acct (paired/cancelled/backed out).
    property var _connW: App.connStates
    on_ConnWChanged: loadAccounts()
    // string (not var): a var binding fires on_AcctWChanged once at
    // startup (undefined → ""), which would stomp wantPicker right after
    // the dialog's Add account sets it.
    property string _acctW: App.authAcct
    on_AcctWChanged: {
        if (App.authAcct === "") {
            wantPicker = false;
            loadAccounts();
        }
    }

    StackLayout {
        anchors.fill: parent
        currentIndex: App.authAcct === "" && !pane.wantPicker ? 0 : 1

        // ── Page 0: account list ───────────────────────────────────────
        ColumnLayout {
            id: listPage
            spacing: 0

            Label {
                visible: acctList.count === 0
                text: "No accounts yet — link one to get started."
                color: Theme.onSurfaceVariant
                wrapMode: Text.Wrap
                horizontalAlignment: Text.AlignHCenter
                Layout.fillWidth: true
                Layout.margins: 16
            }

            ListView {
                id: acctList
                model: []
                clip: true
                implicitHeight: contentHeight
                Layout.fillWidth: true
                Layout.fillHeight: true
                Layout.topMargin: 4

                delegate: WdItemDelegate {
                    required property var modelData
                    width: acctList.width
                    // Rows are always paired — an unlinked registration
                    // can't exist. Taps open the account settings dialog.
                    onClicked: pane.accountSelected(modelData.acct, modelData.privacy === true)
                    contentItem: RowLayout {
                        spacing: 12
                        Rectangle {
                            Layout.preferredWidth: 40
                            Layout.preferredHeight: 40
                            radius: 20
                            color: Theme.surfaceContainerHigh
                            WdIcon {
                                anchors.centerIn: parent
                                source: Qt.resolvedUrl(pane.protoIcons[modelData.proto] || "icons/person.svg")
                                color: Theme.onSurfaceVariant
                                iconSize: 20
                            }
                        }
                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 0
                            Label {
                                text: modelData.name + " — " + (modelData.ownId || modelData.acct)
                                color: Theme.onSurface
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                            Label {
                                text: modelData.connState || ""
                                color: Theme.onSurfaceVariant
                                font.pixelSize: 12
                                elide: Text.ElideRight
                                Layout.fillWidth: true
                            }
                        }
                        WdButton {
                            text: "Log out"
                            flat: true
                            Material.foreground: Theme.error
                            onClicked: Bridge.cmd("logoutAccount", {acct: modelData.acct},
                                                  function(ok, d, e) {
                                                      if (ok) pane.loadAccounts();
                                                      else Bridge.cmd("notify", {msg: e});
                                                  })
                        }
                        WdIcon {
                            source: Qt.resolvedUrl("icons/chevron_right.svg")
                            color: Theme.onSurfaceVariant
                            iconSize: 18
                        }
                    }
                }
            }
        }

        // ── Page 1: picker (authAcct empty) or stages (mid-auth) ───────
        ColumnLayout {
            spacing: 0
            AuthFlow {
                id: flow
                Layout.fillWidth: true
                Layout.fillHeight: true
                // The dialog keeps its own inner margin.
                Layout.margins: 16
                // Its title/footer already provide "Add account" and Back.
                showBack: false
                showHeading: false
                onBack: pane.wantPicker = false
            }
        }
    }

    Component.onCompleted: loadAccounts()
}

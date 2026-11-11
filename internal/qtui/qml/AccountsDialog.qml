import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// AccountsDialog is the Accounts-menu surface: the shared AccountsPane
// in manage mode — log out, add accounts — and rows open the
// per-account settings dialog (devices, privacy, QR).
Dialog {
    id: dlg
    // The picker/auth stages get their own title — the dialog is the
    // account list only while it shows one.
    title: pane.inFlow ? "Add account" : "Accounts"
    modal: true
    Overlay.modal: Scrim {}
    anchors.centerIn: Overlay.overlay
    width: 440
    padding: 0
    // Hug the account list (the pane reports its content height) but give
    // the picker/auth stages the full 560 — and never exceed it, so a long
    // account list scrolls instead of stretching the dialog off-screen.
    height: pane.inFlow ? 560 : Math.min(560, implicitHeight)

    signal openAccount(string acct, bool privacy)

    // Always land on the account list — a stale wantPicker from a
    // previous open would otherwise reshow the picker.
    onOpened: {
        pane.wantPicker = false;
        pane.loadAccounts();
    }

    AccountsPane {
        id: pane
        anchors.fill: parent
        onAccountSelected: (acct, privacy) => dlg.openAccount(acct, privacy)
    }

    footer: Item {
        implicitHeight: 56
        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: 8
            anchors.rightMargin: 8
            WdButton {
                // Footer add-account action — hidden while the picker or
                // an auth stage occupies the dialog (Back owns it then).
                visible: !pane.inFlow
                text: "Add account"
                flat: true
                Material.foreground: Material.accent
                onClicked: pane.wantPicker = true
            }
            Item { Layout.fillWidth: true }
            WdButton {
                visible: pane.inFlow
                text: "Back"
                flat: true
                Material.foreground: Material.accent
                onClicked: pane.goBack()
            }
            WdButton {
                text: "Close"
                flat: true
                Material.foreground: Material.accent
                onClicked: dlg.close()
            }
        }
    }
}

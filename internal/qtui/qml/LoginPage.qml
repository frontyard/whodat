import QtQuick

// LoginPage hosts the add-account flow on first run: the protocol picker,
// then the stage-driven auth for the chosen account (see AuthFlow.qml).
// Pairing flips App.paired and the window stack swaps to MainPage; extra
// accounts are managed from then on in the Accounts dialog.
Item {
    id: page

    AuthFlow {
        anchors.centerIn: parent
        width: Math.min(380, parent.width - 48)
        height: Math.min(560, parent.height - 48)
    }
}

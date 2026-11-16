import QtQuick
import QtQuick.Layouts

// MainPage is the split sidebar/chat layout and hosts the app-level dialogs.
Item {
    id: page

    // chatAction runs a menu action; "info"/"disapp" come back as dialogs.
    function chatAction(jid, id) {
        Bridge.cmd("chatAction", {jid: jid, id: id}, function(ok, data) {
            if (ok && data && data.dialog)
                page.openDialog(data.dialog, jid, data.disappSecs || 0);
        });
    }

    function openDialog(name, jid, extra) {
        switch (name) {
        case "info":     infoDlg.openFor(jid); break;
        case "disapp":   disappDlg.openFor(jid, extra || 0); break;
        case "verify":   verifyDlg.openFor(jid); break;
        case "starred":  starredDlg.openFor(jid); break;
        case "settings": settingsDlg.open(); break;
        case "accounts": accountsDlg.open(); break;
        case "account":  accountDlg.open(); break;
        case "newchat":  newChatDlg.open(); break;
        case "newgroup": newGroupDlg.open(); break;
        case "about":    aboutDlg.open(); break;
        }
    }

    // Sidebar collapse: manual via toggleSidebar(), automatic when the window
    // narrows past 700. _autoCollapsed marks a collapse we caused so widening
    // restores it; a manual collapse survives resizes either way.
    property bool sidebarCollapsed: false
    property bool _autoCollapsed: false
    readonly property bool narrowWindow: width < 700

    onNarrowWindowChanged: {
        if (narrowWindow) {
            if (!sidebarCollapsed) { sidebarCollapsed = true; _autoCollapsed = true; }
        } else if (_autoCollapsed) {
            sidebarCollapsed = false; _autoCollapsed = false;
        }
    }

    function toggleSidebar() {
        sidebarCollapsed = !sidebarCollapsed;
        _autoCollapsed = false;
    }

    Component.onCompleted: if (narrowWindow) { sidebarCollapsed = true; _autoCollapsed = true; }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        Rectangle {
            id: sidebarBox
            implicitWidth: page.sidebarCollapsed ? 0 : Theme.sidebarWidth
            Layout.fillHeight: true
            color: Theme.surfaceContainerLow
            clip: true

            Behavior on implicitWidth {
                NumberAnimation { duration: 150; easing.type: Easing.InOutQuad }
            }

            Sidebar {
                anchors.fill: parent
                onChatSelected: (jid) => Bridge.cmd("openChat", {jid: jid})
                onDialogReq: (name, jid, extra) => page.openDialog(name, jid, extra)
                onChatActionReq: (jid, id) => page.chatAction(jid, id)
                onCollapseRequested: page.toggleSidebar()
            }
        }

        Rectangle {
            Layout.preferredWidth: 1
            Layout.fillHeight: true
            color: Theme.outlineVariant
            visible: sidebarBox.width > 0
        }

        ChatPage {
            Layout.fillWidth: true
            Layout.fillHeight: true
            sidebarCollapsed: page.sidebarCollapsed
            onSidebarToggleRequested: page.toggleSidebar()
            onDialogReq: (name, jid, extra) => page.openDialog(name, jid, extra)
            onChatActionReq: (jid, id) => page.chatAction(jid, id)
        }
    }

    SettingsDialog { id: settingsDlg }
    AccountsDialog { id: accountsDlg; onOpenAccount: (acct, privacy) => { accountDlg.acct = acct; accountDlg.hasPrivacy = privacy; page.openDialog("account", "", 0) } }
    AccountDialog  { id: accountDlg }
    NewChatDialog  { id: newChatDlg; onOpenGroup: page.openDialog("newgroup", "", 0) }
    NewGroupDialog { id: newGroupDlg }
    ChatInfoDialog {
        id: infoDlg
        onOpenDisapp: (jid, secs) => page.openDialog("disapp", jid, secs)
        onOpenVerify: (jid) => page.openDialog("verify", jid, 0)
        onOpenStarred: (jid) => page.openDialog("starred", jid, 0)
    }
    AboutDialog   { id: aboutDlg }
    DisappDialog  { id: disappDlg }
    VerifyDialog  { id: verifyDlg }
    StarredDialog { id: starredDlg; onJumpToMessage: (id) => Bridge.cmd("jumpTo", {id: id}) }
}

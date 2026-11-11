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

    RowLayout {
        anchors.fill: parent
        spacing: 0

        Rectangle {
            Layout.preferredWidth: Theme.sidebarWidth
            Layout.minimumWidth: 280
            Layout.maximumWidth: 420
            Layout.fillHeight: true
            color: Theme.surfaceContainerLow

            Sidebar {
                anchors.fill: parent
                onChatSelected: (jid) => Bridge.cmd("openChat", {jid: jid})
                onDialogReq: (name, jid, extra) => page.openDialog(name, jid, extra)
                onChatActionReq: (jid, id) => page.chatAction(jid, id)
            }
        }

        Rectangle {
            Layout.preferredWidth: 1
            Layout.fillHeight: true
            color: Theme.outlineVariant
        }

        ChatPage {
            Layout.fillWidth: true
            Layout.fillHeight: true
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

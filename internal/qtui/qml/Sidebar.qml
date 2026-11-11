import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// Sidebar is the chat list column: search, archived toggle, chat rows with
// context menu, app menu. Talks to ChatModel + the chat ops.
Item {
    id: side
    signal chatSelected(string jid)
    signal dialogReq(string name, string jid, int extra)
    signal chatActionReq(string jid, string id)

    // context-menu item model shared by row menus
    ListModel { id: menuModel }

    Menu {
        id: rowMenu
        property string jid: ""
        Instantiator {
            model: menuModel
            WdMenuItem {
                required property string act
                required property string label
                text: label
                onTriggered: side.chatActionReq(rowMenu.jid, act)
            }
            onObjectAdded: (i, o) => rowMenu.insertItem(i, o)
            onObjectRemoved: (i, o) => rowMenu.removeItem(o)
        }
    }

    function openRowMenu(jid, item, x, y) {
        menuModel.clear();
        Bridge.cmd("chatMenu", {jid: jid}, function(ok, data) {
            if (ok && data.items) {
                for (var i = 0; i < data.items.length; i++)
                    menuModel.append(data.items[i]);
                rowMenu.jid = jid;
                rowMenu.parent = item;
                rowMenu.popup(x, y);
            }
        });
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // ---- header ---------------------------------------------------
        Rectangle {
            Layout.fillWidth: true
            implicitHeight: 56
            color: Theme.surfaceContainer

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: 16
                anchors.rightMargin: 6
                spacing: 0

                Label {
                    text: "Chats"
                    font.pixelSize: 20
                    font.bold: true
                    color: Theme.onSurface
                    Layout.fillWidth: true
                }

                // + new chat/group
                WdIconButton {
                    icon.source: Qt.resolvedUrl("icons/add.svg")
                    icon.color: Theme.onSurfaceVariant
                    onClicked: newMenu.popup()
                    Menu {
                        id: newMenu
                        WdMenuItem { text: "New chat"; onTriggered: side.dialogReq("newchat", "", 0) }
                        WdMenuItem { text: "New group"; onTriggered: side.dialogReq("newgroup", "", 0) }
                    }
                }

                // ⋮ app menu
                WdIconButton {
                    icon.source: Qt.resolvedUrl("icons/more_vert.svg")
                    icon.color: Theme.onSurfaceVariant
                    onClicked: appMenu.popup()
                    Menu {
                        id: appMenu
                        WdMenuItem { text: "New chat"; onTriggered: side.dialogReq("newchat", "", 0) }
                        WdMenuItem { text: "Accounts"; onTriggered: side.dialogReq("accounts", "", 0) }
                        WdMenuItem { text: "Settings"; onTriggered: side.dialogReq("settings", "", 0) }
                        WdMenuItem { text: "About WhoDat"; onTriggered: side.dialogReq("about", "", 0) }
                    }
                }
            }
        }

        // ---- search ---------------------------------------------------
        TextField {
            id: search
            placeholderText: "Search chats"
            Layout.fillWidth: true
            Layout.leftMargin: 8
            Layout.rightMargin: 8
            Layout.bottomMargin: 6
            rightPadding: clearSearch.visible ? clearSearch.width + 8 : Material.textFieldHorizontalPadding
            onTextChanged: filterDebounce.restart()
            Timer {
                id: filterDebounce
                interval: 250
                onTriggered: Bridge.cmd("setFilter", {text: search.text})
            }
            WdIconButton {
                id: clearSearch
                visible: search.text !== ""
                anchors.right: parent.right
                anchors.rightMargin: 2
                anchors.verticalCenter: parent.verticalCenter
                implicitWidth: 34
                implicitHeight: 34
                focusPolicy: Qt.NoFocus
                icon.source: Qt.resolvedUrl("icons/close.svg")
                icon.color: Theme.onSurfaceVariant
                icon.width: 20
                icon.height: 20
                onClicked: {
                    search.clear();
                    filterDebounce.stop();
                    Bridge.cmd("setFilter", {text: ""});
                }
            }
        }

        // ---- archived toggle ------------------------------------------
        WdItemDelegate {
            visible: App.archivedCount > 0 || side._inArchived
            text: side._inArchived ? "← Back to chats" : "Archived (" + App.archivedCount + ")"
            Layout.fillWidth: true
            onClicked: {
                side._inArchived = !side._inArchived;
                Bridge.cmd("setArchivedMode", {on: side._inArchived});
            }
        }

        // ---- chat list ------------------------------------------------
        ListView {
            id: chatList
            Layout.fillWidth: true
            Layout.fillHeight: true
            model: ChatModel
            clip: true
            boundsBehavior: Flickable.StopAtBounds

            delegate: WdItemDelegate {
                id: row
                required property string jid
                required property string name
                required property string preview
                required property string timeStr
                required property int unread
                required property bool pinned
                required property bool muted
                required property string avatarPath
                required property bool isGroup
                required property int index

                width: chatList.width
                implicitHeight: 64
                highlighted: App.chatJID === jid

                contentItem: RowLayout {
                    spacing: 10

                    Avatar {
                        source: row.avatarPath
                        name: row.name
                        size: 44
                        Layout.leftMargin: 10
                    }

                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 2

                        RowLayout {
                            Label {
                                text: row.name
                                elide: Text.ElideRight
                                color: Theme.onSurface
                                font.pixelSize: 15
                                Layout.fillWidth: true
                            }
                            WdIcon {
                                visible: row.pinned
                                source: Qt.resolvedUrl("icons/keep.svg")
                                color: Theme.onSurface
                                iconSize: 14
                            }
                            Label {
                                text: row.timeStr
                                color: row.unread > 0 ? Theme.primary : Theme.onSurfaceVariant
                                font.pixelSize: 12
                                Layout.rightMargin: 4
                            }
                        }

                        RowLayout {
                            Label {
                                text: row.preview
                                elide: Text.ElideRight
                                maximumLineCount: 1
                                color: Theme.onSurfaceVariant
                                font.pixelSize: 13
                                Layout.fillWidth: true
                            }
                            WdIcon {
                                visible: row.muted
                                source: Qt.resolvedUrl("icons/volume_off.svg")
                                color: Theme.onSurfaceVariant
                                iconSize: 14
                            }
                            Rectangle {
                                visible: row.unread > 0
                                implicitWidth: Math.max(18, unreadLbl.implicitWidth + 10)
                                implicitHeight: 18
                                radius: 9
                                color: Theme.primary
                                Layout.rightMargin: 4

                                Label {
                                    id: unreadLbl
                                    anchors.centerIn: parent
                                    text: row.unread
                                    color: Theme.onPrimary
                                    font.pixelSize: 11
                                    font.bold: true
                                }
                            }
                        }
                    }
                }

                onClicked: side.chatSelected(row.jid)
                MouseArea {
                    anchors.fill: parent
                    acceptedButtons: Qt.RightButton
                    onClicked: (m) => side.openRowMenu(row.jid, row, m.x, m.y)
                }
            }

            ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }
        }
    }

    property bool _inArchived: false
}

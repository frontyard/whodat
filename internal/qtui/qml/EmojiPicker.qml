import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Controls.Material

// EmojiPicker is the composer's emoji popover: search + category grids +
// "recently used" row. Data comes from the emojiData op (core.EmojiCats +
// EmojiNames); picks are noted via noteEmoji and insert into the composer.
Popup {
    id: pop
    width: 430
    height: 360
    padding: 0

    signal picked(string emoji)

    property bool _loaded: false
    property var _cats: []
    property var _names: ({})
    property var _recents: []

    onOpened: {
        if (!pop._loaded) {
            pop._loaded = true;
            Bridge.cmd("emojiData", {}, function(ok, data) {
                if (!ok) return;
                pop._cats = data.cats || [];
                pop._names = data.names || {};
                pop._recents = data.recents || [];
                rebuild();
            });
        } else {
            // refresh recents on reopen
            Bridge.cmd("emojiData", {}, function(ok, data) {
                if (ok) { pop._recents = data.recents || []; rebuild(); }
            });
        }
        searchField.forceActiveFocus();
    }

    function matches(e, catName) {
        var q = searchField.text.trim().toLowerCase();
        if (q === "") return true;
        var k = (pop._names[e] || "") + " " + catName.toLowerCase();
        return k.indexOf(q) >= 0;
    }

    function rebuild() {
        var out = [];
        var q = searchField.text.trim().toLowerCase();
        if (q === "" && pop._recents.length > 0) {
            out.push({name: "Recently used", emojis: pop._recents});
        }
        for (var i = 0; i < pop._cats.length; i++) {
            var c = pop._cats[i];
            var emojis = c.set.split(/\s+/).filter(function(e) { return pop.matches(e, c.name); });
            if (emojis.length > 0)
                out.push({name: c.name, emojis: emojis});
        }
        pop.sections = out;
    }

    // Plain JS array — ListModel.append({emojis:[...]}) silently converts the
    // array into a nested sub-model and emoji cells come out blank.
    property var sections: []

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        Rectangle {
            Layout.fillWidth: true
            implicitHeight: 48
            color: Theme.surfaceContainer

            TextField {
                id: searchField
                anchors.fill: parent
                anchors.margins: 6
                placeholderText: "Search emoji"
                onTextChanged: pop.rebuild()
            }
        }

        ListView {
            id: sectionList
            Layout.fillWidth: true
            Layout.fillHeight: true
            model: pop.sections
            clip: true
            spacing: 4

            delegate: ColumnLayout {
                required property var modelData
                required property int index
                width: sectionList.width
                spacing: 2

                Label {
                    text: modelData.name
                    color: Theme.onSurfaceVariant
                    font.pixelSize: 12
                    Layout.leftMargin: 12
                    Layout.topMargin: 4
                }

                Flow {
                    Layout.fillWidth: true
                    Layout.leftMargin: 10
                    Layout.rightMargin: 10
                    spacing: 0

                    Repeater {
                        model: modelData.emojis
                        delegate: WdItemDelegate {
                            required property string modelData
                            width: 40
                            height: 40
                            contentItem: Label {
                                text: modelData
                                font.pixelSize: 22
                                horizontalAlignment: Text.AlignHCenter
                                verticalAlignment: Text.AlignVCenter
                            }
                            onClicked: {
                                pop.picked(modelData);
                                Bridge.cmd("noteEmoji", {emoji: modelData});
                                pop.close();
                            }
                        }
                    }
                }
            }

            ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }
        }
    }
}

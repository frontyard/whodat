import QtQuick
import QtQuick.Controls
import QtQuick.Effects

// Avatar renders a circular picture (filesystem path) or initials fallback.
// Rectangle.clip only clips to the bounding box, so the circle mask is a
// MultiEffect whose maskSource is a layer-enabled item — the layer forces an
// offscreen texture even though the item is invisible (same pattern DMS uses).
Item {
    id: av
    property string source: ""   // filesystem path ("" → initials)
    property string name: "?"
    property int size: 44

    width: size
    height: size

    // A code point counts as an initial unless it's emoji, symbol,
    // punctuation or a format char. Qt's JS has no \p{...} classes, so
    // letters are detected by exclusion — this also keeps uncased scripts
    // (Hebrew, Arabic, CJK, …) working.
    function isInitial(cp) {
        return !(
            cp <= 0x2F ||                           // controls, space, !"-/
            (cp >= 0x3A && cp <= 0x40) ||           // :;<=>?@
            (cp >= 0x5B && cp <= 0x60) ||           // [\]^_`
            (cp >= 0x7B && cp <= 0xBF) ||           // {|}~ DEL C1 ¡-¿
            (cp >= 0x2B0 && cp <= 0x36F) ||         // modifier/combining marks
            (cp >= 0xD800 && cp <= 0xDFFF) ||       // lone surrogates
            (cp >= 0x2000 && cp <= 0x2BFF) ||       // punct, currency, arrows, symbols
            (cp >= 0x2E00 && cp <= 0x2E7F) ||       // supplemental punctuation
            (cp >= 0x3000 && cp <= 0x303F) ||       // CJK punctuation
            (cp >= 0xFE00 && cp <= 0xFE0F) ||       // variation selectors
            (cp >= 0xFE10 && cp <= 0xFE6F) ||       // vertical/small forms
            (cp >= 0xFF00 && cp <= 0xFF0F) ||       // fullwidth punctuation
            (cp >= 0xFF1A && cp <= 0xFF20) ||       // fullwidth punctuation
            (cp >= 0xFF3B && cp <= 0xFF40) ||       // fullwidth punctuation
            (cp >= 0xFF5B && cp <= 0xFF65) ||       // fullwidth punctuation
            (cp >= 0xFFE0 && cp <= 0xFFFF) ||       // specials/interlinear
            (cp >= 0xE000 && cp <= 0xF8FF) ||       // private use
            (cp >= 0x1D000 && cp <= 0x1D2FF) ||     // musical symbols
            (cp >= 0x1F000 && cp <= 0x1FFFF) ||     // emoji & pictographs
            cp === 0xFEFF ||                        // BOM
            (cp >= 0xE0000)                         // tags, VS supplement, PU planes
        );
    }

    function initials() {
        var t = name.trim();
        if (t === "") return "?";
        var parts = t.split(/\s+/);
        var letters = [];
        for (var i = 0; i < parts.length; i++) {
            // Iterate code points, not UTF-16 units — indexing [0] of an
            // astral char (emoji) leaks half a surrogate pair and renders
            // as "?" (names like "Cholich M33 🏡"). Spread uses the string
            // iterator; Array.from splits code units in this engine.
            var chars = [...parts[i]];
            for (var j = 0; j < chars.length; j++) {
                if (isInitial(chars[j].codePointAt(0))) {
                    letters.push(chars[j]);
                    break;
                }
            }
        }
        if (letters.length === 0) return "?";
        var s = letters[0];
        if (letters.length > 1) s += letters[letters.length - 1];
        return s.toUpperCase();
    }

    Rectangle {
        anchors.fill: parent
        radius: av.size / 2
        color: Theme.secondaryContainer
        visible: img.status !== Image.Ready

        Label {
            anchors.centerIn: parent
            text: av.initials()
            color: Theme.onSecondaryContainer
            font.pixelSize: av.size * 0.4
            font.bold: true
        }
    }

    Image {
        id: img
        anchors.fill: parent
        source: Bridge.fileUrl(av.source)
        sourceSize: Qt.size(av.size * 2, av.size * 2)
        fillMode: Image.PreserveAspectCrop
        asynchronous: true
        cache: false
        visible: false
    }

    MultiEffect {
        anchors.fill: img
        source: img
        visible: img.status === Image.Ready
        maskEnabled: true
        maskSource: maskLoader.item
        maskThresholdMin: 0.5
        maskSpreadAtMin: 1.0
    }

    Loader {
        id: maskLoader
        anchors.fill: parent
        active: true
        visible: false
        sourceComponent: Item {
            layer.enabled: true
            Rectangle {
                anchors.fill: parent
                radius: av.size / 2
                color: "black"
            }
        }
    }
}

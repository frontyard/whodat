// Compact flat icon button — a stock RoundButton is ~48px wide, which pushes
// header/composer icons far apart; 44px keeps comfortable padding around the
// glyph while still bringing adjacent icons closer together.
// Based on WdRoundButton for the pointing-hand hover cursor.
WdRoundButton {
    flat: true
    icon.width: 24
    icon.height: 24
    implicitWidth: 44
    implicitHeight: 44
}

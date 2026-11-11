package core

import _ "embed"

//go:embed icon.svg
var IconSVG []byte

// Monochrome tray glyphs, picked by desktop mode: white on dark panels,
// black on light ones.

//go:embed tray-dark.svg
var TrayIconDarkSVG []byte

//go:embed tray-light.svg
var TrayIconLightSVG []byte

package qtui

import (
	"log"

	qt "github.com/mappu/miqt/qt6"
)

// themeDefaults is the built-in Material 3 palette (dark, WhatsApp-green
// seed). Phase 7 replaces values at runtime from the desktop's palette
// (DMS matugen → portal color-scheme → Qt palette → this fallback).
var themeDefaults = map[string]any{
	"dark": true,

	// M3 color roles
	"primary":                 "#A8D5A2",
	"onPrimary":               "#0A3819",
	"primaryContainer":        "#1C4F2C",
	"onPrimaryContainer":      "#C4F1BC",
	"secondary":               "#BBCBB8",
	"onSecondary":             "#26342A",
	"secondaryContainer":      "#3C4B3F",
	"onSecondaryContainer":    "#D7E7D4",
	"tertiary":                "#A0CFD0",
	"onTertiary":              "#003637",
	"tertiaryContainer":       "#1E4E4F",
	"onTertiaryContainer":     "#BCECEE",
	"error":                   "#FFB4AB",
	"onError":                 "#690005",
	"errorContainer":          "#93000A",
	"onErrorContainer":        "#FFDAD6",
	"background":              "#101510",
	"onBackground":            "#DFE4DC",
	"surface":                 "#101510",
	"onSurface":               "#DFE4DC",
	"surfaceVariant":          "#414942",
	"onSurfaceVariant":        "#C1C9BE",
	"outline":                 "#8B9385",
	"outlineVariant":          "#414942",
	"shadow":                  "#000000",
	"surfaceTint":             "#A8D5A2",
	"inverseSurface":          "#DFE4DC",
	"inverseOnSurface":        "#2D322B",
	"inversePrimary":          "#3C6943",
	"surfaceDim":              "#101510",
	"surfaceBright":           "#363A34",
	"surfaceContainerLowest":  "#0B0F0B",
	"surfaceContainerLow":     "#181D18",
	"surfaceContainer":        "#1C211C",
	"surfaceContainerHigh":    "#262B25",
	"surfaceContainerHighest": "#31362F",
	"scrim":                   "#000000",

	// WhatsApp-ish functional roles the UI consults directly
	"bubbleOut":   "#1C4F2C", // outgoing bubble = primaryContainer
	"onBubbleOut": "#C4F1BC",
	"bubbleIn":    "#1C211C", // incoming bubble = surfaceContainer
	"onBubbleIn":  "#DFE4DC",
	"tickRead":    "#53BDEB", // read receipt blue

	// design tokens
	"radius":       16,
	"radiusSmall":  8,
	"spacing":      12,
	"sidebarWidth": 340,
}

// themeLight is the light variant of the built-in palette (same green seed).
var themeLight = map[string]any{
	"dark": false,

	"primary":                 "#106C2E",
	"onPrimary":               "#FFFFFF",
	"primaryContainer":        "#A6F2AC",
	"onPrimaryContainer":      "#002107",
	"secondary":               "#516350",
	"onSecondary":             "#FFFFFF",
	"secondaryContainer":      "#D4E8D0",
	"onSecondaryContainer":    "#0E1F10",
	"tertiary":                "#39656B",
	"onTertiary":              "#FFFFFF",
	"tertiaryContainer":       "#BCEBF2",
	"onTertiaryContainer":     "#001F23",
	"error":                   "#BA1A1A",
	"onError":                 "#FFFFFF",
	"errorContainer":          "#FFDAD6",
	"onErrorContainer":        "#410002",
	"background":              "#F6FBF2",
	"onBackground":            "#181D17",
	"surface":                 "#F6FBF2",
	"onSurface":               "#181D17",
	"surfaceVariant":          "#DDE5DB",
	"onSurfaceVariant":        "#414942",
	"outline":                 "#717970",
	"outlineVariant":          "#C1C9BE",
	"shadow":                  "#000000",
	"surfaceTint":             "#106C2E",
	"inverseSurface":          "#2D322B",
	"inverseOnSurface":        "#EDF2E7",
	"inversePrimary":          "#8BD591",
	"surfaceDim":              "#D6DBD3",
	"surfaceBright":           "#F6FBF2",
	"surfaceContainerLowest":  "#FFFFFF",
	"surfaceContainerLow":     "#F0F5EC",
	"surfaceContainer":        "#EAEFE6",
	"surfaceContainerHigh":    "#E4EAE1",
	"surfaceContainerHighest": "#DFE4DB",
	"scrim":                   "#000000",

	"bubbleOut":   "#A6F2AC", // outgoing = primaryContainer
	"onBubbleOut": "#002107",
	"bubbleIn":    "#EAEFE6", // incoming = surfaceContainer
	"onBubbleIn":  "#181D17",
	"tickRead":    "#53BDEB",

	"radius":       16,
	"radiusSmall":  8,
	"spacing":      12,
	"sidebarWidth": 340,
}

// applyTheme re-seeds the map for the current cfg.Theme — Qt thread.
func (h *Host) applyTheme() {
	dark := true
	switch h.app.Cfg.Snapshot().Theme {
	case "light":
		dark = false
	case "system":
		dark = h.wantDark()
	}
	src := themeDefaults
	if !dark {
		src = themeLight
	}
	for k, v := range src {
		var qv *qt.QVariant
		switch t := v.(type) {
		case string:
			qv = qt.NewQVariant14(t)
		case bool:
			qv = qt.NewQVariant8(t)
		case int:
			qv = qt.NewQVariant4(t)
		default:
			qv = qt.NewQVariant14("")
		}
		h.theme.Insert(k, qv)
	}
	// Desktop accent (GNOME 47+/KDE): re-derive the accent roles over the
	// just-seeded base so missing roles keep the built-in values.
	if roles := accentRoles(src); roles != nil {
		for k, v := range roles {
			h.theme.Insert(k, qt.NewQVariant14(v))
		}
		log.Println("qtui: theme accent from portal")
	}
}

// setTheme applies a palette map (Phase 7's DMS/desktop ingest uses this).
// Qt thread only.
func (h *Host) setTheme(colors map[string]string) {
	for k, v := range colors {
		h.theme.Insert(k, qt.NewQVariant14(v))
	}
}

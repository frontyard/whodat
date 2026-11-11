package qtui

// dms.go integrates with DankMaterialShell (Dux): it reads the resolved
// Material-You palette that DMS already computes (dms-colors.json — a full
// M3 role dump per mode), applies it to the Theme map, and re-applies live
// when the wallpaper/scheme changes. Fallbacks keep every other desktop
// working: portal color-scheme → Qt palette → built-in defaults.
//
// A status.json in the XDG state dir exposes unread state for shell plugins.

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/fsnotify/fsnotify"

	"github.com/frontyard/whodat/internal/config"
)

// dmsColorsPath is DMS's resolved palette dump (per-mode M3 roles).
func dmsColorsPath() string {
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		return filepath.Join(x, "DankMaterialShell", "dms-colors.json")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".cache", "DankMaterialShell", "dms-colors.json")
}

// dmsSessionPath holds the live wallpaper/mode DMS wrote.
func dmsSessionPath() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "DankMaterialShell", "session.json")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "state", "DankMaterialShell", "session.json")
}

// dmsPalette is the subset of dms-colors.json we consume.
type dmsPalette struct {
	Mode   string                       `json:"mode"`
	Colors map[string]map[string]string `json:"colors"`
}

// m3Keys maps theme-map keys (camel) → dms-colors.json keys (snake).
var m3Keys = map[string]string{
	"primary":                 "primary",
	"onPrimary":               "on_primary",
	"primaryContainer":        "primary_container",
	"onPrimaryContainer":      "on_primary_container",
	"secondary":               "secondary",
	"onSecondary":             "on_secondary",
	"secondaryContainer":      "secondary_container",
	"onSecondaryContainer":    "on_secondary_container",
	"tertiary":                "tertiary",
	"onTertiary":              "on_tertiary",
	"tertiaryContainer":       "tertiary_container",
	"onTertiaryContainer":     "on_tertiary_container",
	"error":                   "error",
	"onError":                 "on_error",
	"errorContainer":          "error_container",
	"onErrorContainer":        "on_error_container",
	"background":              "background",
	"onBackground":            "on_background",
	"surface":                 "surface",
	"onSurface":               "on_surface",
	"surfaceVariant":          "surface_variant",
	"onSurfaceVariant":        "on_surface_variant",
	"outline":                 "outline",
	"outlineVariant":          "outline_variant",
	"shadow":                  "shadow",
	"surfaceTint":             "surface_tint",
	"inverseSurface":          "inverse_surface",
	"inverseOnSurface":        "inverse_on_surface",
	"inversePrimary":          "inverse_primary",
	"surfaceDim":              "surface_dim",
	"surfaceBright":           "surface_bright",
	"surfaceContainerLowest":  "surface_container_lowest",
	"surfaceContainerLow":     "surface_container_low",
	"surfaceContainer":        "surface_container",
	"surfaceContainerHigh":    "surface_container_high",
	"surfaceContainerHighest": "surface_container_highest",
	"scrim":                   "scrim",
}

// readDMSPalette loads dms-colors.json and returns the palette for `dark`,
// or nil when DMS isn't installed/hasn't themed yet.
func readDMSPalette(dark bool) map[string]string {
	b, err := os.ReadFile(dmsColorsPath())
	if err != nil {
		return nil
	}
	var p dmsPalette
	if err := json.Unmarshal(b, &p); err != nil || p.Colors == nil {
		return nil
	}
	mode := "dark"
	if !dark {
		mode = "light"
	}
	src := p.Colors[mode]
	if src == nil {
		src = p.Colors["dark"]
	}
	out := make(map[string]string, len(m3Keys)+4)
	for camel, snake := range m3Keys {
		if v, ok := src[snake]; ok && v != "" {
			out[camel] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	// Functional roles derived from the palette.
	out["bubbleOut"] = src["primary_container"]
	out["onBubbleOut"] = src["on_primary_container"]
	out["bubbleIn"] = src["surface_container"]
	out["onBubbleIn"] = src["on_surface"]
	return out
}

// dmsIsDark reports DMS's own dark-mode flag for cfg.Theme=="system".
func dmsIsDark() bool {
	b, err := os.ReadFile(dmsColorsPath())
	if err != nil {
		return true // dark default
	}
	var p struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return true
	}
	return p.Mode != "light"
}

// wantDark resolves cfg.Theme + desktop mode to a boolean.
func (h *Host) wantDark() bool {
	switch h.app.Cfg.Snapshot().Theme {
	case "light":
		return false
	case "dark":
		return true
	default: // "system"
		return desktopDark()
	}
}

// desktopDark reports the desktop's dark-mode preference — DMS palette →
// portal color-scheme → Qt palette fallback. Unlike wantDark it ignores
// the app's Theme pref, which is what the SNI panel actually follows.
func desktopDark() bool {
	if readDMSPaletteFile() != nil {
		return dmsIsDark()
	}
	switch portalColorScheme() {
	case 1:
		return true
	case 2:
		return false
	}
	if pal := qt.QGuiApplication_Palette(); pal != nil {
		if c := pal.ColorWithCr(qt.QPalette__Window); c != nil {
			return c.Lightness() <= 140
		}
	}
	return true
}

// readDMSPaletteFile is a cheap existence probe for the DMS palette.
func readDMSPaletteFile() []byte {
	b, err := os.ReadFile(dmsColorsPath())
	if err != nil {
		return nil
	}
	return b
}

// applyDesktopTheme picks the palette source in priority order and re-seeds
// the Theme map. Qt thread only.
func (h *Host) applyDesktopTheme() {
	dark := h.wantDark()
	if pal := readDMSPalette(dark); pal != nil {
		// Re-seed the full base palette first so roles the DMS dump lacks
		// (bubbleIn/onBubbleIn, tickRead, design tokens) stay defined.
		base := themeDefaults
		if !dark {
			base = themeLight
		}
		for k, v := range base {
			switch t := v.(type) {
			case string:
				h.theme.Insert(k, qt.NewQVariant14(t))
			case bool:
				h.theme.Insert(k, qt.NewQVariant8(t))
			case int:
				h.theme.Insert(k, qt.NewQVariant4(t))
			}
		}
		h.theme.Insert("dark", qt.NewQVariant8(dark))
		for k, v := range pal {
			h.theme.Insert(k, qt.NewQVariant14(v))
		}
		log.Printf("qtui: theme from DMS palette (mode=%s)", map[bool]string{true: "dark", false: "light"}[dark])
	} else {
		// Fallback: built-in defaults (already applied via applyTheme).
		h.applyTheme()
	}
	// The tray glyph tracks the panel mode — keep it in step.
	h.refreshTrayIcon()
}

// watchDesktopTheme re-applies the theme when DMS rewrites its palette or
// session file (wallpaper/scheme change). fsnotify on the parent dirs —
// the files themselves are recreated rather than appended.
func (h *Host) watchDesktopTheme() {
	h.watchPortalScheme()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("qtui: theme watcher: %v", err)
		return
	}
	for _, f := range []string{dmsColorsPath(), dmsSessionPath()} {
		dir := filepath.Dir(f)
		if _, err := os.Stat(dir); err == nil {
			_ = w.Add(dir)
		}
	}
	go func() {
		defer w.Close()
		var debounce *time.Timer
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				base := filepath.Base(ev.Name)
				if base != "dms-colors.json" && base != "session.json" {
					continue
				}
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(300*time.Millisecond, func() {
					mainthread.Start(func() { h.applyDesktopTheme() })
				})
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
}

// watchStatus rewrites status.json when the keys it exports change, plus a
// periodic unread refresh piggybacked on the tray watcher cadence.
func (h *Host) watchStatus() {
	go h.writeStatus()
	d := &debouncer{}
	h.state.OnValueChanged(func(key string, _ *qt.QVariant) {
		switch key {
		case "chatTitle", "chatJID", "connState", "windowActive", "trayUnread":
			d.fire(500*time.Millisecond, func() {
				go h.writeStatus()
			})
		}
	})
}

// debouncer collapses rapid-fire state updates into one write.
type debouncer struct {
	mu    sync.Mutex
	timer *time.Timer
}

func (d *debouncer) fire(delay time.Duration, f func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(delay, f)
}

// writeStatus publishes a tiny JSON blob for shell plugins (DMS bar widget,
// waybar, polybar): unread total, focused window flag, and the open chat.
// Runs on background goroutines — state keys are read on the Qt thread.
func (h *Host) writeStatus() {
	var title, jid, conn, active string
	mainthreadWait(func() {
		title = h.stateValue("chatTitle")
		jid = h.stateValue("chatJID")
		conn = h.stateValue("connState")
		active = h.stateValue("windowActive")
	})
	unread, _ := h.app.DB.TotalUnread(context.Background())
	st := map[string]any{
		"unread":    unread,
		"chat":      title,
		"chatJID":   jid,
		"connected": conn == "online",
		"focused":   active == "true",
	}
	dir := config.StateDir()
	_ = os.MkdirAll(dir, 0o755)
	b, _ := json.Marshal(st)
	tmp := filepath.Join(dir, ".status.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, filepath.Join(dir, "status.json"))
}

// stateValue is a small helper to read a state key as a string.
func (h *Host) stateValue(key string) string {
	if v := h.state.Value(key); v != nil {
		return strings.TrimSpace(v.ToString())
	}
	return ""
}

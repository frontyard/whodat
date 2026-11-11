package qtui

// portal.go reads the desktop's dark-mode preference from the
// xdg-desktop-portal Settings service (org.freedesktop.appearance
// color-scheme) — the same source GTK4/libadwaita apps and KDE use.
//
// Why not the Qt palette: the gtk3 platformtheme builds its palette from
// GTK3 widgets, which don't honour color-scheme (GTK3 has no portal
// support), so it stays light under prefer-dark; the fallback Gnome theme
// supplies no palette at all. styleHints()->colorScheme() would be the Qt
// API, but miqt doesn't bind it — and Qt 6.8's built-in Gnome theme drops
// the portal's color-scheme signal anyway. Querying the portal directly
// is authoritative and works regardless of which platformtheme loaded.

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/mappu/miqt/qt6/mainthread"
)

var (
	portalOnce sync.Once
	portalConn *dbus.Conn
	portalErr  error
)

// portalBus returns the shared session-bus connection, or an error when
// no session bus exists (non-desktop environments).
func portalBus() (*dbus.Conn, error) {
	portalOnce.Do(func() {
		portalConn, portalErr = dbus.ConnectSessionBus()
	})
	return portalConn, portalErr
}

// portalColorScheme reads org.freedesktop.appearance color-scheme:
// 0 = unknown/no preference, 1 = dark, 2 = light.
func portalColorScheme() int {
	conn, err := portalBus()
	if err != nil {
		return 0
	}
	obj := conn.Object("org.freedesktop.portal.Desktop",
		dbus.ObjectPath("/org/freedesktop/portal/desktop"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var v dbus.Variant
	if err := obj.CallWithContext(ctx, "org.freedesktop.portal.Settings.ReadOne", 0,
		"org.freedesktop.appearance", "color-scheme").Store(&v); err != nil {
		return 0
	}
	n, _ := v.Value().(uint32)
	return int(n)
}

// portalAccent reads org.freedesktop.appearance accent-color (GNOME 47+,
// KDE Plasma 6): a variant holding either an (r,g,b) struct of doubles in
// [0,1] or a uint32 0 for "no preference". ok=false when unset/absent.
func portalAccent() (r, g, b float64, ok bool) {
	conn, err := portalBus()
	if err != nil {
		return 0, 0, 0, false
	}
	obj := conn.Object("org.freedesktop.portal.Desktop",
		dbus.ObjectPath("/org/freedesktop/portal/desktop"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var v dbus.Variant
	if err := obj.CallWithContext(ctx, "org.freedesktop.portal.Settings.ReadOne", 0,
		"org.freedesktop.appearance", "accent-color").Store(&v); err != nil {
		return 0, 0, 0, false
	}
	rgb, is := v.Value().([]interface{})
	if !is || len(rgb) != 3 {
		return 0, 0, 0, false
	}
	r, ok1 := rgb[0].(float64)
	g, ok2 := rgb[1].(float64)
	b, ok3 := rgb[2].(float64)
	return r, g, b, ok1 && ok2 && ok3
}

// accentRoles re-derives theme roles from the portal accent color: each
// role keeps the lightness of the built-in palette entry and only swaps in
// the accent hue at a role-scaled chroma — same idea as Material You.
// Neutrals get a whisper of chroma so surfaces track the accent instead of
// the built-in green cast. Returns nil when the desktop exposes no accent
// preference; base is the just-seeded palette the lightness is read from.
func accentRoles(base map[string]any) map[string]string {
	r, g, b, ok := portalAccent()
	if !ok {
		return nil
	}
	_, C, H := srgbToOKLCH(r, g, b)
	groups := map[float64][]string{
		C: {"primary", "onPrimary", "primaryContainer", "onPrimaryContainer",
			"inversePrimary", "surfaceTint", "bubbleOut", "onBubbleOut"},
		C * 0.35: {"secondary", "onSecondary", "secondaryContainer", "onSecondaryContainer"},
		min(C*0.08, 0.02): {"background", "onBackground", "surface", "onSurface",
			"surfaceDim", "surfaceBright", "inverseSurface", "inverseOnSurface",
			"surfaceContainerLowest", "surfaceContainerLow", "surfaceContainer",
			"surfaceContainerHigh", "surfaceContainerHighest", "bubbleIn", "onBubbleIn"},
		min(C*0.30, 0.06): {"surfaceVariant", "onSurfaceVariant", "outline", "outlineVariant"},
	}
	m := make(map[string]string, len(base))
	for c, roles := range groups {
		for _, role := range roles {
			hex, is := base[role].(string)
			if !is {
				continue
			}
			if l, ok := hexLightness(hex); ok {
				m[role] = oklchTone(l, c, H)
			}
		}
	}
	return m
}

// hexLightness parses "#RRGGBB" and returns its OKLCH lightness × 100.
func hexLightness(hex string) (float64, bool) {
	var r, g, b int
	if n, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil || n != 3 {
		return 0, false
	}
	l, _, _ := srgbToOKLCH(float64(r)/255, float64(g)/255, float64(b)/255)
	return l * 100, true
}

// srgbToOKLCH converts sRGB [0,1] to OKLCH (Björn Ottosson's matrices).
func srgbToOKLCH(r, g, b float64) (l, c, h float64) {
	lr, lg, lb := srgbLinear(r), srgbLinear(g), srgbLinear(b)
	l3 := math.Cbrt(0.4122214708*lr + 0.5363325363*lg + 0.0514459929*lb)
	m3 := math.Cbrt(0.2119034982*lr + 0.6806995451*lg + 0.1073969566*lb)
	s3 := math.Cbrt(0.0883024619*lr + 0.2817188376*lg + 0.6299787005*lb)
	l = 0.2104542553*l3 + 0.7936177850*m3 - 0.0040720468*s3
	a := 1.9779984951*l3 - 2.4285922050*m3 + 0.4505937099*s3
	bb := 0.0259040371*l3 + 0.7827717662*m3 - 0.8086757660*s3
	return l, math.Hypot(a, bb), math.Atan2(bb, a)
}

// oklchToSrgb converts OKLCH back to sRGB [0,1] (possibly out of gamut).
func oklchToSrgb(l, c, h float64) (r, g, b float64) {
	a, bb := c*math.Cos(h), c*math.Sin(h)
	l3 := l + 0.3963377774*a + 0.2158037573*bb
	m3 := l - 0.1055613458*a - 0.0638541728*bb
	s3 := l - 0.0894841775*a - 1.2914855480*bb
	lr := +4.0767416621*l3*l3*l3 - 3.3077115913*m3*m3*m3 + 0.2309699292*s3*s3*s3
	lg := -1.2684380046*l3*l3*l3 + 2.6097574011*m3*m3*m3 - 0.3413193965*s3*s3*s3
	lb := -0.0041960863*l3*l3*l3 - 0.7034186147*m3*m3*m3 + 1.7076147010*s3*s3*s3
	return srgbGamma(lr), srgbGamma(lg), srgbGamma(lb)
}

// oklchTone renders L tone t (0–100) at chroma c/hue h, clamped into the
// sRGB gamut by reducing chroma, and formats it as #RRGGBB.
func oklchTone(t, c, h float64) string {
	lo, hi := 0.0, c
	for i := 0; i < 16; i++ {
		mid := (lo + hi) / 2
		if r, g, b := oklchToSrgb(t/100, mid, h); inGamut(r, g, b) {
			lo = mid
		} else {
			hi = mid
		}
	}
	r, g, b := oklchToSrgb(t/100, lo, h)
	clamp := func(v float64) int {
		return int(math.Round(math.Max(0, math.Min(1, v)) * 255))
	}
	return fmt.Sprintf("#%02X%02X%02X", clamp(r), clamp(g), clamp(b))
}

func inGamut(r, g, b float64) bool {
	for _, v := range []float64{r, g, b} {
		if v < -0.001 || v > 1.001 {
			return false
		}
	}
	return true
}

func srgbLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func srgbGamma(c float64) float64 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1.0/2.4) - 0.055
}

// watchPortalScheme re-applies the theme when the desktop toggles dark
// mode. Runs once; the bus connection is shared with portalColorScheme.
func (h *Host) watchPortalScheme() {
	conn, err := portalBus()
	if err != nil {
		return
	}
	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath("/org/freedesktop/portal/desktop"),
		dbus.WithMatchInterface("org.freedesktop.portal.Settings"),
		dbus.WithMatchMember("SettingChanged"),
	); err != nil {
		log.Printf("qtui: portal theme watch: %v", err)
		return
	}
	ch := make(chan *dbus.Signal, 8)
	conn.Signal(ch)
	go func() {
		var debounce *time.Timer
		for s := range ch {
			// SettingChanged(namespace string, key string, value variant)
			if len(s.Body) != 3 || s.Body[0] != "org.freedesktop.appearance" {
				continue
			}
			if k := s.Body[1]; k != "color-scheme" && k != "accent-color" {
				continue
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(300*time.Millisecond, func() {
				mainthread.Start(func() { h.applyDesktopTheme() })
			})
		}
	}()
}

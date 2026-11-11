package qtui

import (
	"encoding/json"
	"os"
	"os/exec"
	"time"
)

// niriFocusWindow focuses the WhoDat window through the compositor's IPC.
// Wayland clients can't force activation — requestActivate() is only a hint,
// and niri grants it solely with an xdg-activation token a tray click can't
// provide. The IPC socket is authoritative: it switches to the window's
// workspace and focuses it. No-op outside niri.
func (h *Host) niriFocusWindow() {
	if os.Getenv("NIRI_SOCKET") == "" {
		return
	}
	niri, err := exec.LookPath("niri")
	if err != nil {
		return
	}
	go func() {
		type niriWin struct {
			ID      int    `json:"id"`
			PID     int    `json:"pid"`
			AppID   string `json:"app_id"`
			Focused bool   `json:"is_focused"`
		}
		self := os.Getpid()
		// A hidden window is unmapped: it only re-enters `niri msg windows`
		// once the showSeq bump reaches QML and the surface re-maps. Poll
		// briefly so both the mapped and re-mapping cases are covered.
		for range 20 {
			out, err := exec.Command(niri, "msg", "-j", "windows").Output()
			if err == nil {
				var wins []niriWin
				if json.Unmarshal(out, &wins) == nil {
					for _, w := range wins {
						if w.PID != self && w.AppID != appID {
							continue
						}
						if !w.Focused {
							_ = exec.Command(niri, "msg", "action", "focus-window", "--id", itoa(w.ID)).Run()
						}
						return
					}
				}
			}
			time.Sleep(150 * time.Millisecond)
		}
	}()
}

package core

import (
	"os"
	"path/filepath"
)

// AutostartDesktop is the XDG autostart entry template.
const AutostartDesktop = `[Desktop Entry]
Type=Application
Name=WhoDat
Comment=Unofficial WhatsApp messaging app for Linux
Exec=whodat
Icon=io.github.frontyard.whodat
Terminal=false
X-GNOME-Autostart-enabled=true
`

// SetAutostart writes/removes the XDG autostart desktop file.
func SetAutostart(on bool) error {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(home, ".config")
	}
	auto := filepath.Join(dir, "autostart")
	path := filepath.Join(auto, "whodat.desktop")
	if !on {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(auto, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(AutostartDesktop), 0o644)
}

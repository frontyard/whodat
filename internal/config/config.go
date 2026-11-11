package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/frontyard/whodat/internal/store"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const appDirName = "whodat"

// Prefs is the user-facing preference set persisted as JSON — separated
// from Config so Snapshot can hand out a lock-free copy (a mutex can't be
// copied, so the fields can't travel inside Config itself).
type Prefs struct {
	Theme           string `json:"theme"` // system|light|dark
	Notifications   bool   `json:"notifications"`
	CloseToTray     bool   `json:"close_to_tray"`
	Autostart       bool   `json:"autostart"`
	AutoDownloadMax int64  `json:"auto_download_max_bytes"` // auto-download media below this size
	ShowGroupCalls  bool   `json:"show_group_calls"`        // experimental
	ChatStyle       string `json:"chat_style"`              // bubbles|flat
}

// Config holds user-facing preferences persisted as JSON. Field access is
// promoted from Prefs for reads, but writes must go through Update —
// mutating fields directly races the marshaled copy inside Save.
type Config struct {
	Prefs
	mu   sync.Mutex `json:"-"`
	path string     `json:"-"`
}

// Snapshot returns a consistent copy of the preferences — safe to read on
// any goroutine.
func (c *Config) Snapshot() Prefs {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Prefs
}

// Update mutates preferences under the same mutex Save marshals with, then
// persists — a concurrent Save can no longer capture a half-applied set.
func (c *Config) Update(fn func(*Prefs)) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(&c.Prefs)
	return c.saveLocked()
}

func xdgBase(env, fallback string) string {
	if v := os.Getenv(env); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback)
}

func xdgDir(env, fallback string) string {
	return filepath.Join(xdgBase(env, fallback), appDirName)
}

// DataDir returns ~/.local/share/whodat (databases, media originals).
func DataDir() string { return xdgDir("XDG_DATA_HOME", ".local/share") }

// CacheDir returns ~/.cache/whodat (thumbnails, downloads-in-flight).
func CacheDir() string { return xdgDir("XDG_CACHE_HOME", ".cache") }

// ConfigDir returns ~/.config/whodat.
func ConfigDir() string { return xdgDir("XDG_CONFIG_HOME", ".config") }

// StateDir returns ~/.local/state/whodat (status.json, logs).
func StateDir() string { return xdgDir("XDG_STATE_HOME", ".local/state") }

// MediaDir returns DataDir()/media — the root under which per-account
// media subdirs live (MediaAcctDir).
func MediaDir() string { return filepath.Join(DataDir(), "media") }

// AvatarDir returns DataDir()/avatars — the root under which per-account
// avatar subdirs live (AvatarAcctDir).
func AvatarDir() string { return filepath.Join(DataDir(), "avatars") }

// MediaAcctDir returns the account's media subdir — media/<acct>. Files
// from different accounts never share a directory, so an account wipe is
// RemoveAll on this one path.
func MediaAcctDir(acct string) string {
	if acct == "" {
		acct = "_bare"
	}
	return filepath.Join(MediaDir(), acct)
}

// AvatarAcctDir returns the account's avatar subdir — avatars/<acct>.
func AvatarAcctDir(acct string) string {
	if acct == "" {
		acct = "_bare"
	}
	return filepath.Join(AvatarDir(), acct)
}

// MediaIDDir returns the media subdir owning a peer id — same rule as
// MediaAcctDir but derived from the id itself ("wa2:x@…" → media/wa2).
func MediaIDDir(id store.PeerID) string { return MediaAcctDir(id.Acct()) }

// AvatarIDDir is the avatar equivalent of MediaIDDir.
func AvatarIDDir(id store.PeerID) string { return AvatarAcctDir(id.Acct()) }

// AvatarPath is the cache file for id's profile picture (keyed by id hash,
// so account-prefixed ids stay safe filenames).
func AvatarPath(id store.PeerID) string {
	sum := sha256.Sum256([]byte(id))
	return filepath.Join(AvatarIDDir(id), hex.EncodeToString(sum[:8])+".jpg")
}

// AvatarPathVersioned is a fresh cache slot for id: AvatarPath plus a
// unique suffix. A re-downloaded picture must land on a new path or the
// UI's path-keyed image caches keep showing the old bytes.
func AvatarPathVersioned(id store.PeerID) string {
	sum := sha256.Sum256([]byte(id))
	name := hex.EncodeToString(sum[:8]) + "-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".jpg"
	return filepath.Join(AvatarIDDir(id), name)
}

// Load reads the config file, returning defaults when missing/corrupt.
func Load() *Config {
	c := &Config{Prefs: Prefs{
		Theme:           "system",
		Notifications:   true,
		AutoDownloadMax: 32 << 20, // 32 MiB
		ShowGroupCalls:  false,
		ChatStyle:       "bubbles",
	}}
	c.path = filepath.Join(ConfigDir(), "config.json")
	if data, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(data, c)
	}
	c.mu = sync.Mutex{}
	return c
}

// Save persists the config atomically.
func (c *Config) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

// saveLocked writes under mu — shared by Save and Update.
func (c *Config) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// EnsureDirs creates the app's data/cache/config directories — 0700:
// DataDir holds session keys and message history; none of it is shareable.
func EnsureDirs() error {
	for _, d := range []string{DataDir(), CacheDir(), ConfigDir(), MediaDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

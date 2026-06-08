// Package mjconfig handles mj's on-disk config and standard paths. No secrets
// are stored here — the Firebase refresh token lives only in hasp / the browser
// profile (spec §14).
package mjconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the non-secret persisted state. Stored as JSON (we avoid a TOML
// dependency) at <configDir>/mj/config.json.
type Config struct {
	UserID       string `json:"user_id,omitempty"`
	TOSAck       bool   `json:"tos_ack"`
	DefaultMode  string `json:"default_mode,omitempty"`  // fast|relax|turbo
	ThrottleSecs int    `json:"throttle_secs,omitempty"` // min gap between submits
	ProfileDir   string `json:"profile_dir,omitempty"`   // override; default DataDir/profile

	// DaemonAutostart, when true, makes headless commands transparently spawn a
	// background daemon (warm browser) if one isn't already running. Overridable
	// per-invocation with MJ_DAEMON_AUTOSTART=1/0.
	DaemonAutostart bool `json:"daemon_autostart,omitempty"`
}

// Dir returns <os.UserConfigDir>/mj, creating it.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, "mj")
	return d, os.MkdirAll(d, 0o700)
}

// DataDir returns the data dir (<os.UserConfigDir>/mj/data) for the browser
// profile and caches, creating it.
func DataDir() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	dd := filepath.Join(d, "data")
	return dd, os.MkdirAll(dd, 0o700)
}

func path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.json"), nil
}

// Load reads the config, returning a zero Config if none exists.
func Load() (Config, error) {
	p, err := path()
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Save writes the config atomically with 0600 perms.
func (c Config) Save() error {
	p, err := path()
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ProfilePath returns the persistent Camoufox profile dir (config override or
// DataDir/profile), creating its parent.
func (c Config) ProfilePath() (string, error) {
	if c.ProfileDir != "" {
		return c.ProfileDir, nil
	}
	dd, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dd, "profile"), nil
}

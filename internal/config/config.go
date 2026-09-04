// Package config loads the tuichk configuration file and resolves
// secrets (the CheckMK password is obtained by running a user-supplied
// command, so it never has to live in the config file itself).
package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk configuration for tuichk.
type Config struct {
	// URL is the base URL of the CheckMK site, including the site name,
	// e.g. "https://monitoring.example.com/mysite".
	URL string `toml:"url"`
	// Username is the CheckMK (automation) user to authenticate as.
	Username string `toml:"username"`
	// PasswordCmd is a shell command whose stdout is the password,
	// e.g. "pass show checkmk/automation".
	PasswordCmd string `toml:"password_cmd"`
	// Password may be set directly instead of PasswordCmd. PasswordCmd
	// wins if both are present.
	Password string `toml:"password"`
	// RefreshSeconds is the auto-refresh interval (default 120, 0 disables).
	RefreshSeconds *int `toml:"refresh_seconds"`
	// InsecureTLS skips TLS certificate verification.
	InsecureTLS bool `toml:"insecure_tls"`
	// Mouse enables mouse capture at startup (wheel, click to select,
	// click tabs). Off by default because capturing the mouse takes
	// over the terminal's own text selection. :mouse toggles at runtime.
	Mouse bool `toml:"mouse"`
	// BrowserCmd opens a URL for :browser; {url} is replaced by the
	// shell-quoted link. Default: "open {url}" on macOS, "xdg-open {url}"
	// elsewhere.
	BrowserCmd string `toml:"browser_cmd"`
	// SSHCmd is the command :ssh runs; {host} is replaced by the
	// shell-quoted host name. Default "ssh {host}".
	SSHCmd string `toml:"ssh_cmd"`
	// SSHInline stops :ssh from opening a multiplexer/terminal pane; the
	// command then always runs in place of the TUI until it exits.
	SSHInline bool `toml:"ssh_inline"`
	// WikiURL is the page :wiki opens for a host; {host} is replaced by
	// the host name, {short} by its first DNS label, both URL-escaped.
	WikiURL string `toml:"wiki_url"`
	// HotMin/HotMax bound the "hot window": crit-level problems aged
	// between them get extra visual weight. Go duration strings
	// ("15m", "4h", "90m"); defaults 15m and 4h.
	HotMin string `toml:"hot_min"`
	HotMax string `toml:"hot_max"`
}

// DefaultPath returns the default config file location,
// honoring XDG_CONFIG_HOME.
func DefaultPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "tuichk.toml"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "tuichk", "config.toml")
}

// Load reads and validates the config file at path.
func Load(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	cfg.URL = strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	if cfg.URL == "" {
		return nil, fmt.Errorf("config %s: 'url' is required (e.g. https://monitoring.example.com/mysite)", path)
	}
	if cfg.Username == "" {
		return nil, fmt.Errorf("config %s: 'username' is required", path)
	}
	if cfg.PasswordCmd == "" && cfg.Password == "" {
		return nil, fmt.Errorf("config %s: set 'password_cmd' (preferred) or 'password'", path)
	}
	return &cfg, nil
}

// Refresh returns the auto-refresh interval in seconds.
func (c *Config) Refresh() int {
	if c.RefreshSeconds == nil {
		return 120
	}
	if *c.RefreshSeconds < 0 {
		return 0
	}
	return *c.RefreshSeconds
}

// HotWindow returns the configured hot-window bounds (defaults 15m–4h).
func (c *Config) HotWindow() (min, max time.Duration, err error) {
	min, max = 15*time.Minute, 4*time.Hour
	if c.HotMin != "" {
		if min, err = time.ParseDuration(c.HotMin); err != nil {
			return 0, 0, fmt.Errorf("hot_min %q: %w", c.HotMin, err)
		}
	}
	if c.HotMax != "" {
		if max, err = time.ParseDuration(c.HotMax); err != nil {
			return 0, 0, fmt.Errorf("hot_max %q: %w", c.HotMax, err)
		}
	}
	if min < 0 || max <= min {
		return 0, 0, fmt.Errorf("hot window %s–%s is not a valid range", min, max)
	}
	return min, max, nil
}

// ResolvePassword returns the password, running PasswordCmd if configured.
func (c *Config) ResolvePassword() (string, error) {
	if c.PasswordCmd == "" {
		return c.Password, nil
	}
	cmd := exec.Command("sh", "-c", c.PasswordCmd)
	cmd.Stderr = os.Stderr // let e.g. pinentry prompts through
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("password_cmd %q failed: %w", c.PasswordCmd, err)
	}
	pw := strings.TrimRight(string(out), "\r\n")
	if pw == "" {
		return "", fmt.Errorf("password_cmd %q produced no output", c.PasswordCmd)
	}
	return pw, nil
}

// SiteName derives a short display name from the site URL.
func (c *Config) SiteName() string {
	if i := strings.LastIndex(c.URL, "/"); i >= 0 && i < len(c.URL)-1 {
		return c.URL[i+1:]
	}
	return c.URL
}

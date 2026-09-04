package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHotWindowDefaults(t *testing.T) {
	cfg, err := Load(write(t, `
url = "https://example.com/site"
username = "u"
password = "p"
`))
	if err != nil {
		t.Fatal(err)
	}
	min, max, err := cfg.HotWindow()
	if err != nil {
		t.Fatal(err)
	}
	if min != 15*time.Minute || max != 4*time.Hour {
		t.Errorf("defaults should be 15m–4h, got %s–%s", min, max)
	}
}

func TestHotWindowConfigured(t *testing.T) {
	cfg, err := Load(write(t, `
url = "https://example.com/site"
username = "u"
password = "p"
hot_min = "30m"
hot_max = "8h"
`))
	if err != nil {
		t.Fatal(err)
	}
	min, max, err := cfg.HotWindow()
	if err != nil {
		t.Fatal(err)
	}
	if min != 30*time.Minute || max != 8*time.Hour {
		t.Errorf("want 30m–8h, got %s–%s", min, max)
	}
}

func TestHotWindowInvalid(t *testing.T) {
	for _, extra := range []string{
		`hot_min = "banana"`,
		"hot_min = \"4h\"\nhot_max = \"15m\"", // inverted range
	} {
		cfg, err := Load(write(t, `
url = "https://example.com/site"
username = "u"
password = "p"
`+extra))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := cfg.HotWindow(); err == nil {
			t.Errorf("config %q should be rejected", extra)
		}
	}
}

func TestMouseOption(t *testing.T) {
	cfg, err := Load(write(t, "url = \"https://x/site\"\nusername = \"u\"\npassword = \"p\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mouse {
		t.Error("mouse should default to off")
	}
	cfg, err = Load(write(t, "url = \"https://x/site\"\nusername = \"u\"\npassword = \"p\"\nmouse = true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Mouse {
		t.Error("mouse = true not honored")
	}
}

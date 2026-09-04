// tuichk is a terminal UI for CheckMK, talking to the same
// web interface (REST API) as the browser dashboard.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"tuichk/internal/checkmk"
	"tuichk/internal/config"
	"tuichk/internal/ui"
)

const exampleConfig = `url = "https://monitoring.example.com/mysite"
username = "automation"
password_cmd = "pass show checkmk/automation"
# refresh_seconds = 30
# insecure_tls = false
# mouse = false
# ssh_cmd = "ssh {host}"
`

func main() {
	configPath := flag.String("config", "", "path to config file (default: ~/.config/tuichk/config.toml)")
	flag.Parse()

	path := *configPath
	if path == "" {
		if env := os.Getenv("TUICHK_CONFIG"); env != "" {
			path = env
		} else {
			path = config.DefaultPath()
		}
	}

	cfg, err := config.Load(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tuichk: %v\n", err)
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "\nCreate %s with:\n\n%s", path, exampleConfig)
		}
		os.Exit(1)
	}

	password, err := cfg.ResolvePassword()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tuichk: %v\n", err)
		os.Exit(1)
	}

	hotMin, hotMax, err := cfg.HotWindow()
	if err != nil {
		fmt.Fprintf(os.Stderr, "tuichk: %v\n", err)
		os.Exit(1)
	}
	ui.SetHotWindow(hotMin, hotMax)

	client := checkmk.New(cfg.URL, cfg.Username, password, cfg.InsecureTLS)
	model := ui.New(client, cfg.SiteName(), cfg.Refresh())
	model = model.WithExternal(ui.External{
		BaseURL:    cfg.URL,
		BrowserCmd: cfg.BrowserCmd,
		SSHCmd:     cfg.SSHCmd,
		SSHInline:  cfg.SSHInline,
		WikiURL:    cfg.WikiURL,
	})

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if cfg.Mouse {
		model = model.EnableMouse()
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(model, opts...)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tuichk: %v\n", err)
		os.Exit(1)
	}
}

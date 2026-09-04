package ui

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// External configures the two commands that leave the TUI: :browser
// opens the selected host or service in the CheckMK web GUI, :ssh opens
// a shell on the selected host.
type External struct {
	BaseURL    string // site URL, for building GUI links
	BrowserCmd string // template; {url} becomes the shell-quoted link
	SSHCmd     string // template; {host} becomes the shell-quoted host name
	SSHInline  bool   // never open a pane; run ssh in place of the TUI
	WikiURL    string // template; {host} becomes the URL-escaped host name
}

// WithExternal sets the external command configuration.
func (m Model) WithExternal(e External) Model {
	m.ext = e
	return m
}

// noticeMsg is a one-shot footer message from a background action.
type noticeMsg string

const (
	openTimeout = 15 * time.Second
	defaultSSH  = "ssh {host}"
)

// selected returns the row an action applies to: the open detail, else
// the cursor row.
func (m Model) selected() (row, bool) {
	if m.detail != nil {
		return *m.detail, true
	}
	rows := m.rows()
	if m.cursor < 0 || m.cursor >= len(rows) {
		return row{}, false
	}
	return rows[m.cursor], true
}

// guiURL is the web GUI page showing r.
func guiURL(base string, r row) string {
	q := url.Values{"host": {r.host}}
	if r.kind == rowService {
		q.Set("view_name", "service")
		q.Set("service", r.desc)
	} else {
		q.Set("view_name", "host")
	}
	return base + "/check_mk/view.py?" + q.Encode()
}

// shellQuote wraps s in single quotes so sh takes it literally.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// expand substitutes a {placeholder} in tmpl with the shell-quoted value.
// Quotes the user put around the placeholder are dropped first, so both
// `open {url}` and `open "{url}"` work. A template without the
// placeholder gets the value appended as a final argument.
func expand(tmpl, placeholder, value string) string {
	q := shellQuote(value)
	if !strings.Contains(tmpl, placeholder) {
		return tmpl + " " + q
	}
	for _, wrapped := range []string{`"` + placeholder + `"`, `'` + placeholder + `'`} {
		tmpl = strings.ReplaceAll(tmpl, wrapped, placeholder)
	}
	return strings.ReplaceAll(tmpl, placeholder, q)
}

func defaultBrowser() string {
	if runtime.GOOS == "darwin" {
		return "open {url}"
	}
	return "xdg-open {url}"
}

// openBrowser opens the selected row's GUI page in the browser.
func (m Model) openBrowser() (Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		m.cmdErr = "nothing selected"
		return m, nil
	}
	if m.ext.BaseURL == "" {
		m.cmdErr = "no site url configured"
		return m, nil
	}
	return m.openURL(guiURL(m.ext.BaseURL, r), r.host)
}

// openWiki opens the selected host's page in the configured wiki.
func (m Model) openWiki() (Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		m.cmdErr = "nothing selected"
		return m, nil
	}
	if m.ext.WikiURL == "" {
		m.cmdErr = "set wiki_url in the config, e.g. https://wiki.example.com/{host}"
		return m, nil
	}
	return m.openURL(wikiURL(m.ext.WikiURL, r.host), r.host)
}

// wikiURL fills {host} in tmpl with the host name, {short} with its
// first DNS label. Escaping follows where the placeholder sits: a path
// segment before any "?", a query value after it.
func wikiURL(tmpl, host string) string {
	short := host
	if i := strings.IndexByte(host, '.'); i > 0 {
		short = host[:i]
	}
	path, query, hasQuery := strings.Cut(tmpl, "?")
	path = strings.NewReplacer(
		"{host}", url.PathEscape(host),
		"{short}", url.PathEscape(short),
	).Replace(path)
	if !hasQuery {
		return path
	}
	query = strings.NewReplacer(
		"{host}", url.QueryEscape(host),
		"{short}", url.QueryEscape(short),
	).Replace(query)
	return path + "?" + query
}

// openURL runs the browser command on link in the background and reports
// the outcome in the footer.
func (m Model) openURL(link, what string) (Model, tea.Cmd) {
	tmpl := m.ext.BrowserCmd
	if tmpl == "" {
		tmpl = defaultBrowser()
	}
	script := expand(tmpl, "{url}", link)
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "sh", "-c", script).CombinedOutput()
		if err != nil {
			return noticeMsg(fmt.Sprintf("browser: %v %s", err, firstLine(out)))
		}
		return noticeMsg("opened " + what + " in browser")
	}
}

// paneArgv returns the command that opens script in a new pane of the
// multiplexer or terminal we are running in, or nil when there is none
// (plain terminal, an ssh session), in which case the caller runs it in
// place of the TUI. Multiplexers are checked before terminals because
// the innermost one is what the user sees.
func paneArgv(getenv func(string) string, goos, script string) []string {
	switch {
	case getenv("ZELLIJ") != "":
		// Without --close-on-exit zellij keeps the pane after ssh exits and
		// offers to rerun it on Enter.
		return []string{"zellij", "action", "new-pane", "--close-on-exit", "--", "sh", "-c", script}
	case getenv("TMUX") != "":
		return []string{"tmux", "split-window", "-h", script}
	case getenv("WEZTERM_PANE") != "":
		return []string{"wezterm", "cli", "split-pane", "--", "sh", "-c", script}
	case getenv("KITTY_WINDOW_ID") != "":
		// Needs allow_remote_control in kitty.conf; falls back inline if not.
		return []string{"kitten", "@", "launch", "--type=window", "sh", "-c", script}
	case getenv("TERM_PROGRAM") == "ghostty" && goos == "darwin":
		// Ghostty 1.3+ is scriptable on macOS; older versions reject the
		// script and we fall back inline. The surface closes when ssh exits.
		return []string{"osascript", "-e", ghosttySplit(script)}
	case getenv("TERM_PROGRAM") == "ghostty":
		// No split CLI on Linux; a new window is the closest thing.
		return []string{"ghostty", "+new-window", "-e", "sh", "-c", script}
	}
	return nil
}

// ghosttySplit is the AppleScript that splits the focused Ghostty
// terminal to the right and types script into the new shell. A surface
// created with its own command would stay open on "Process exited.
// Press any key" (Ghostty forces wait-after-command there, as for -e);
// a shell that runs the command and then exits closes on its own.
func ghosttySplit(script string) string {
	cmd := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(script + "; exit")
	return `tell application "Ghostty"
	set t to focused terminal of selected tab of front window
	set t2 to split t direction right
	input text "` + cmd + `" to t2
	send key "enter" to t2
end tell`
}

// openSSH opens a shell on the selected host: in a new pane when the
// terminal offers one, else by suspending the TUI and running ssh in its
// place until it exits.
func (m Model) openSSH() (Model, tea.Cmd) {
	r, ok := m.selected()
	if !ok {
		m.cmdErr = "nothing selected"
		return m, nil
	}
	tmpl := m.ext.SSHCmd
	if tmpl == "" {
		tmpl = defaultSSH
	}
	script := expand(tmpl, "{host}", r.host)
	inline := tea.ExecProcess(exec.Command("sh", "-c", script), func(err error) tea.Msg {
		if err != nil {
			return noticeMsg("ssh " + r.host + ": " + err.Error())
		}
		return noticeMsg("")
	})
	var argv []string
	if !m.ext.SSHInline {
		argv = paneArgv(os.Getenv, runtime.GOOS, script)
	}
	if argv == nil {
		return m, inline
	}
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
		defer cancel()
		if err := exec.CommandContext(ctx, argv[0], argv[1:]...).Run(); err != nil {
			// The pane could not be opened (remote control off, binary
			// missing): run inline instead. ExecProcess's message is what
			// the program acts on, so returning it from here is enough.
			return inline()
		}
		return noticeMsg("ssh " + r.host + " opened in a new pane")
	}
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

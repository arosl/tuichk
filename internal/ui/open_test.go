package ui

import (
	"strings"
	"testing"
)

func TestGuiURL(t *testing.T) {
	base := "https://mon.example.com/mysite"
	h := guiURL(base, row{kind: rowHost, host: "web01"})
	if h != base+"/check_mk/view.py?host=web01&view_name=host" {
		t.Errorf("host url = %q", h)
	}
	s := guiURL(base, row{kind: rowService, host: "web01", desc: "Filesystem /var & more"})
	want := base + "/check_mk/view.py?host=web01&service=Filesystem+%2Fvar+%26+more&view_name=service"
	if s != want {
		t.Errorf("service url = %q, want %q", s, want)
	}
}

func TestExpandQuotes(t *testing.T) {
	cases := []struct{ tmpl, want string }{
		{"ssh {host}", "ssh 'web01'"},
		{`ssh "{host}"`, "ssh 'web01'"},
		{"ssh '{host}'", "ssh 'web01'"},
		{"ssh -J bastion {host} -l root", "ssh -J bastion 'web01' -l root"},
		{"ssh", "ssh 'web01'"},
	}
	for _, c := range cases {
		if got := expand(c.tmpl, "{host}", "web01"); got != c.want {
			t.Errorf("expand(%q) = %q, want %q", c.tmpl, got, c.want)
		}
	}
	// A hostile name can't break out of the quotes.
	got := expand("ssh {host}", "{host}", "a'; rm -rf / #")
	if got != `ssh 'a'\''; rm -rf / #'` {
		t.Errorf("hostile expand = %q", got)
	}
}

func TestPaneArgv(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(k string) string { return vars[k] }
	}
	if argv := paneArgv(env(nil), "darwin", "ssh x"); argv != nil {
		t.Errorf("plain terminal should run inline, got %v", argv)
	}
	argv := paneArgv(env(map[string]string{"TERM_PROGRAM": "ghostty"}), "darwin", `ssh 'x'`)
	if argv[0] != "osascript" || !strings.Contains(argv[2], `input text "ssh 'x'; exit" to t2`) ||
		!strings.Contains(argv[2], "split t direction right") {
		t.Errorf("ghostty on macOS should split via AppleScript: %v", argv)
	}
	argv = paneArgv(env(map[string]string{"TERM_PROGRAM": "ghostty"}), "linux", "ssh x")
	if strings.Join(argv, " ") != "ghostty +new-window -e sh -c ssh x" {
		t.Errorf("ghostty on linux argv = %v", argv)
	}
	if got := ghosttySplit(`echo "a\b"`); !strings.Contains(got, `"echo \"a\\b\"; exit"`) {
		t.Errorf("applescript escaping wrong: %s", got)
	}
	argv = paneArgv(env(map[string]string{"ZELLIJ": "0", "TERM_PROGRAM": "ghostty"}), "darwin", "ssh 'x'")
	if strings.Join(argv, " ") != "zellij action new-pane --close-on-exit -- sh -c ssh 'x'" {
		t.Errorf("zellij inside ghostty should win: %v", argv)
	}
	argv = paneArgv(env(map[string]string{"TMUX": "/tmp/tmux-1/default,1,0"}), "linux", "ssh 'x'")
	if strings.Join(argv, " ") != "tmux split-window -h ssh 'x'" {
		t.Errorf("tmux argv = %v", argv)
	}
	argv = paneArgv(env(map[string]string{"WEZTERM_PANE": "3"}), "linux", "s")
	if argv[0] != "wezterm" {
		t.Errorf("wezterm argv = %v", argv)
	}
	argv = paneArgv(env(map[string]string{"KITTY_WINDOW_ID": "3"}), "linux", "s")
	if argv[0] != "kitten" {
		t.Errorf("kitty argv = %v", argv)
	}
}

func TestSelectedPrefersDetail(t *testing.T) {
	m := testModel(t)
	r, ok := m.selected()
	if !ok || r.host != "db02" {
		t.Fatalf("cursor row = %+v", r)
	}
	m = key(m, "enter") // open db02
	m = key(m, "j")     // list cursor doesn't move under the detail, but be explicit
	r, ok = m.selected()
	if !ok || r.host != "db02" || m.detail == nil {
		t.Fatalf("detail row = %+v", r)
	}
}

func TestBrowserCommand(t *testing.T) {
	m := testModel(t)
	m = key(m, ":", "browser", "enter")
	if m.cmdErr != "no site url configured" {
		t.Errorf("cmdErr = %q", m.cmdErr)
	}
	m = m.WithExternal(External{BaseURL: "https://mon/x", BrowserCmd: "true {url}"})
	upd, cmd := m.runCommand("browser")
	if cmd == nil {
		t.Fatal("expected a browser command")
	}
	msg, ok := cmd().(noticeMsg)
	if !ok || !strings.HasPrefix(string(msg), "opened db02") {
		t.Errorf("notice = %v", msg)
	}
	upd, _ = upd.Update(msg)
	if got := upd.(Model).cmdErr; got != string(msg) {
		t.Errorf("notice not shown in footer: %q", got)
	}
	// Works from an open detail too.
	m = key(m, "enter", ":", "browser", "enter")
	if m.detail == nil || m.cmdErr != "" {
		t.Errorf("detail=%v cmdErr=%q", m.detail != nil, m.cmdErr)
	}
	// A failing command reports instead of silently doing nothing.
	m = m.WithExternal(External{BaseURL: "https://mon/x", BrowserCmd: "false {url}"})
	_, cmd = m.runCommand("browser")
	if msg, _ := cmd().(noticeMsg); !strings.HasPrefix(string(msg), "browser: exit status 1") {
		t.Errorf("failure notice = %q", msg)
	}
}

func TestWikiURL(t *testing.T) {
	got := wikiURL("https://w/{short}?full={host}&x=a&b", "web 01.example.com")
	if got != "https://w/web%2001?full=web+01.example.com&x=a&b" {
		t.Errorf("wikiURL = %q", got)
	}
	got = wikiURL("https://wiki.example.com/search?go=1&q={host}", "a&b=c")
	if got != "https://wiki.example.com/search?go=1&q=a%26b%3Dc" {
		t.Errorf("query escaping = %q", got)
	}
	if got := wikiURL("https://w/{short}", "plain"); got != "https://w/plain" {
		t.Errorf("wikiURL short of undotted = %q", got)
	}
	m := testModel(t)
	m = key(m, ":", "wiki", "enter")
	if !strings.HasPrefix(m.cmdErr, "set wiki_url") {
		t.Errorf("unconfigured wiki should explain: %q", m.cmdErr)
	}
	m = m.WithExternal(External{WikiURL: "https://w/{host}", BrowserCmd: "true {url}"})
	_, cmd := m.runCommand("wiki")
	if cmd == nil {
		t.Fatal("expected a browser command")
	}
	if msg, _ := cmd().(noticeMsg); msg != "opened db02 in browser" {
		t.Errorf("notice = %q", msg)
	}
}

func TestSSHInlineReturnsExec(t *testing.T) {
	m := testModel(t).WithExternal(External{SSHInline: true, SSHCmd: "true {host}"})
	_, cmd := m.runCommand("ssh")
	if cmd == nil {
		t.Fatal("expected an exec command")
	}
	// ExecProcess yields bubbletea's private exec message; all we can
	// check without a Program is that it is not our own notice.
	if _, isNotice := cmd().(noticeMsg); isNotice {
		t.Error("inline ssh should hand the process to the program, not post a notice")
	}
}

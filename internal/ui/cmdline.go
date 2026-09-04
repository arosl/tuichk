package ui

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// commandNames are the ":" commands tab completion offers. Long forms
// only: the one-letter aliases are quicker typed than completed.
var commandNames = []string{
	"all", "browser", "crit", "down", "handled", "help", "hosts", "mouse",
	"numbers", "ok", "problems", "quit", "refresh", "refresh!", "services",
	"ssh", "unknown", "warn", "wiki",
}

// cmdKey handles a key while the ":" line is open: enter runs, esc
// cancels, up/down (ctrl+p/ctrl+n) walk the session history, tab cycles
// through the commands that start with what has been typed.
func (m Model) cmdKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeCmd()
		return m, nil
	case "enter":
		line := strings.TrimSpace(m.cmd.Value())
		m.closeCmd()
		if line != "" && (len(m.cmdHist) == 0 || m.cmdHist[len(m.cmdHist)-1] != line) {
			m.cmdHist = append(m.cmdHist, line)
		}
		return m.runCommand(line)
	case "up", "ctrl+p":
		m.cmdTabs = nil
		if m.cmdHistPos == 0 {
			return m, nil
		}
		if m.cmdHistPos == len(m.cmdHist) {
			m.cmdDraft = m.cmd.Value()
		}
		m.cmdHistPos--
		m.setCmd(m.cmdHist[m.cmdHistPos])
		return m, nil
	case "down", "ctrl+n":
		m.cmdTabs = nil
		if m.cmdHistPos >= len(m.cmdHist) {
			return m, nil
		}
		m.cmdHistPos++
		if m.cmdHistPos == len(m.cmdHist) {
			m.setCmd(m.cmdDraft)
		} else {
			m.setCmd(m.cmdHist[m.cmdHistPos])
		}
		return m, nil
	case "tab":
		if m.cmdTabs == nil {
			m.cmdTabs = completions(m.cmd.Value())
			m.cmdTabIdx = 0
		}
		if len(m.cmdTabs) == 0 {
			return m, nil
		}
		m.setCmd(m.cmdTabs[m.cmdTabIdx])
		m.cmdTabIdx = (m.cmdTabIdx + 1) % len(m.cmdTabs)
		return m, nil
	}
	m.cmdTabs = nil
	var cmd tea.Cmd
	m.cmd, cmd = m.cmd.Update(msg)
	return m, cmd
}

// completions returns the commands starting with prefix, sorted.
func completions(prefix string) []string {
	prefix = strings.TrimSpace(prefix)
	var out []string
	for _, c := range commandNames {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

func (m *Model) setCmd(s string) {
	m.cmd.SetValue(s)
	m.cmd.CursorEnd()
}

func (m *Model) openCmd() tea.Cmd {
	m.cmdMode = true
	m.cmdErr = ""
	m.cmdHistPos = len(m.cmdHist)
	m.cmdDraft = ""
	m.cmdTabs = nil
	m.cmd.Focus()
	return m.cmd.Cursor.BlinkCmd()
}

func (m *Model) closeCmd() {
	m.cmdMode = false
	m.cmdTabs = nil
	m.cmd.Blur()
	m.cmd.SetValue("")
}

// cmdLineView is the footer while the ":" line is open: the input, and
// the remaining completions when tab has more than one to offer.
func (m Model) cmdLineView() string {
	s := " " + m.cmd.View()
	if len(m.cmdTabs) > 1 {
		s += "  " + styleDim.Render(strings.Join(m.cmdTabs, " "))
	}
	return s
}

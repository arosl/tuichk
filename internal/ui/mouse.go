package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Mouse support is opt-in (config "mouse = true" or the :mouse command)
// because capturing the mouse takes over the terminal's own text
// selection; copying plugin output then needs shift- or alt-drag.

// Screen rows of the fixed chrome above the table, see View.
const (
	rowTabs        = 2 // "1 Problems  2 Down ..." tab bar
	rowFirstData   = 4 // title, summary, tabs, column header, then data
	wheelScrollBy  = 3
	mouseOnNotice  = "mouse on — shift-drag (or alt-drag) to select text"
	mouseOffNotice = "mouse off"
)

// EnableMouse marks the model as mouse-enabled. The caller must also
// start the program with tea.WithMouseCellMotion(); this only keeps the
// :mouse toggle and hints in sync with that.
func (m Model) EnableMouse() Model {
	m.mouse = true
	return m
}

// toggleMouse flips capture at runtime via the :mouse command.
func (m Model) toggleMouse() (Model, tea.Cmd) {
	m.mouse = !m.mouse
	if m.mouse {
		m.cmdErr = mouseOnNotice
		return m, tea.EnableMouseCellMotion
	}
	m.cmdErr = mouseOffNotice
	return m, tea.DisableMouse
}

// handleMouse maps clicks and wheel ticks onto the same actions the keys
// perform: wheel scrolls, a click selects a row, a click on the selected
// row opens it, a click on a tab switches view.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	wheel := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown
	click := msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft
	if !wheel && !click {
		return m, nil
	}
	m.quitArmed = false
	m.cmdErr = ""
	m.count = 0

	if m.helpOpen {
		if click {
			m.helpOpen = false
			return m, nil
		}
		var cmd tea.Cmd
		m.helpVP, cmd = m.helpVP.Update(msg)
		return m, cmd
	}
	if m.detail != nil {
		// The viewport scrolls itself on wheel events; clicks are ignored
		// so a stray click can't close a detail the user is reading.
		var cmd tea.Cmd
		m.detailVP, cmd = m.detailVP.Update(msg)
		return m, cmd
	}

	rows := m.rows()
	if wheel {
		delta := wheelScrollBy
		if msg.Button == tea.MouseButtonWheelUp {
			delta = -delta
		}
		m.scrollBy(delta, len(rows))
		return m, nil
	}

	switch {
	case msg.Y == rowTabs:
		if v, ok := m.tabAt(msg.X); ok && v != m.view {
			return m.switchView(v)
		}
	case msg.Y >= rowFirstData && msg.Y < rowFirstData+m.tableHeight():
		idx := m.scroll + msg.Y - rowFirstData
		if idx >= len(rows) {
			return m, nil
		}
		if idx == m.cursor {
			return m.openDetail(rows[idx])
		}
		m.cursor = idx
		m.clampCursor(len(rows))
	}
	return m, nil
}

// scrollBy moves the visible window by delta rows and drags the cursor
// along so it stays on screen, the way a wheel is expected to behave.
func (m *Model) scrollBy(delta, n int) {
	vis := m.tableHeight()
	if vis < 1 {
		vis = 1
	}
	m.scroll += delta
	if m.scroll > n-vis {
		m.scroll = n - vis
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	if m.cursor < m.scroll {
		m.cursor = m.scroll
	}
	if m.cursor >= m.scroll+vis {
		m.cursor = m.scroll + vis - 1
	}
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// tabAt returns the view whose tab label covers screen column x. The
// geometry mirrors viewTabs: one leading space, labels joined by one.
func (m Model) tabAt(x int) (view, bool) {
	pos := 1
	for i, name := range viewNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		w := lipgloss.Width(styleTabInactive.Render(label))
		if view(i) == m.view {
			w = lipgloss.Width(styleTabActive.Render(label))
		}
		if x >= pos && x < pos+w {
			return view(i), true
		}
		pos += w + 1
	}
	return 0, false
}

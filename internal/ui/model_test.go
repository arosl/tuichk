package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"tuicheck/internal/checkmk"
)

func testModel(t *testing.T) Model {
	t.Helper()
	m := New(nil, "mysite", 0)
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	upd, _ = upd.Update(problemsMsg{
		at: time.Now(),
		hosts: []checkmk.Host{
			{Name: "web01", State: checkmk.HostUp, LastStateChange: time.Now().Add(-time.Hour),
				NumServices: 3, NumOK: 1, NumWarn: 1, NumCrit: 1},
			{Name: "db02", State: checkmk.HostDown, PluginOutput: "Ping timed out",
				LastStateChange: time.Now().Add(-5 * time.Minute)},
		},
		problems: []checkmk.Service{
			{HostName: "web01", Description: "HTTPS certificate", State: checkmk.SvcCrit,
				PluginOutput: "cert expires in 3 days", LastStateChange: time.Now().Add(-2 * time.Hour)},
			{HostName: "web01", Description: "Filesystem /var", State: checkmk.SvcWarn,
				Acknowledged: true, LastStateChange: time.Now().Add(-3 * 24 * time.Hour)},
		},
	})
	// Full list (no output columns), as delivered by the lazy fetch.
	upd, _ = upd.Update(allSvcsMsg{
		at: time.Now(),
		services: []checkmk.Service{
			{HostName: "web01", Description: "HTTPS certificate", State: checkmk.SvcCrit,
				LastStateChange: time.Now().Add(-2 * time.Hour)},
			{HostName: "web01", Description: "Filesystem /var", State: checkmk.SvcWarn,
				Acknowledged: true, LastStateChange: time.Now().Add(-3 * 24 * time.Hour)},
			{HostName: "web01", Description: "CPU load", State: checkmk.SvcOK,
				LastStateChange: time.Now().Add(-time.Hour)},
		},
	})
	return upd.(Model)
}

func key(m tea.Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m, _ = m.Update(msg)
	}
	return m.(Model)
}

func TestProblemsViewSortsAndHidesHandled(t *testing.T) {
	m := testModel(t)
	rows := m.rows()
	if len(rows) != 2 {
		t.Fatalf("want 2 unhandled problems, got %d: %+v", len(rows), rows)
	}
	if rows[0].host != "db02" || rows[0].kind != rowHost {
		t.Errorf("host DOWN should sort first, got %+v", rows[0])
	}
	if rows[1].desc != "HTTPS certificate" {
		t.Errorf("CRIT service should be second, got %+v", rows[1])
	}

	m = key(m, "h") // include handled
	if got := len(m.rows()); got != 3 {
		t.Errorf("want 3 problems incl. acknowledged, got %d", got)
	}
}

func TestFuzzySearchFilters(t *testing.T) {
	m := key(testModel(t), "2") // services view (full list already cached)
	if got := len(m.rows()); got != 3 {
		t.Fatalf("want 3 services, got %d", got)
	}
	m = key(m, "/", "f", "s", "v", "a", "r") // fuzzy: "fsvar" → Filesystem /var
	rows := m.rows()
	if len(rows) != 1 || rows[0].desc != "Filesystem /var" {
		t.Errorf("fuzzy filter failed, got %+v", rows)
	}
	m = key(m, "esc") // clear search
	if got := len(m.rows()); got != 3 {
		t.Errorf("esc should clear filter, got %d rows", got)
	}
}

func TestSearchMatchesStateName(t *testing.T) {
	m := key(testModel(t), "h") // show handled too: CRIT + WARN + DOWN visible
	m = key(m, "/", "c", "r", "i", "t", " ", "c", "e", "r", "t")
	rows := m.rows()
	if len(rows) < 1 || rows[0].desc != "HTTPS certificate" || rows[0].state != checkmk.SvcCrit {
		t.Errorf("'crit cert' should top-rank the CRIT certificate service, got %+v", rows)
	}
	m = key(m, "esc")
	m = key(m, "/", "d", "o", "w", "n")
	rows = m.rows()
	if len(rows) < 1 || rows[0].kind != rowHost || rows[0].host != "db02" {
		t.Errorf("'down' should top-rank the DOWN host, got %+v", rows)
	}
}

func TestServicesViewEnrichesOutputFromProblems(t *testing.T) {
	m := key(testModel(t), "2")
	for _, r := range m.rows() {
		if r.desc == "HTTPS certificate" && !strings.Contains(r.output, "cert expires") {
			t.Errorf("problem output not merged into cached list row: %+v", r)
		}
	}
}

func TestServicesViewLazyFetchTriggered(t *testing.T) {
	m := New(nil, "mysite", 0)
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	mm, cmd := upd.(Model).switchView(viewServices)
	if !mm.allLoading || cmd == nil {
		t.Error("first visit to Services view should start the full fetch")
	}
	mm2, cmd2 := mm.switchView(viewServices)
	_ = mm2
	if cmd2 != nil {
		t.Error("re-entering Services view must not refetch")
	}
}

func TestPagingKeys(t *testing.T) {
	m := key(testModel(t), "2") // services view, 3 rows
	upd, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = upd.(Model)
	if want := len(m.rows()) - 1; m.cursor != want {
		t.Errorf("ctrl+f should page down (clamped to last row %d), cursor=%d", want, m.cursor)
	}
	upd, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	m = upd.(Model)
	if m.cursor != 0 {
		t.Errorf("ctrl+b should page back to top, cursor=%d", m.cursor)
	}
}

func TestVimMotions(t *testing.T) {
	m := key(testModel(t), "2") // services view, 3 rows
	m = key(m, "f")             // plain-key full page (zellij-safe)
	if want := len(m.rows()) - 1; m.cursor != want {
		t.Errorf("f should page down, cursor=%d want %d", m.cursor, want)
	}
	m = key(m, "b")
	if m.cursor != 0 {
		t.Errorf("b should page back up, cursor=%d", m.cursor)
	}
	m = key(m, "L")
	if want := len(m.rows()) - 1; m.cursor != want {
		t.Errorf("L should jump to bottom of screen, cursor=%d want %d", m.cursor, want)
	}
	m = key(m, "H")
	if m.cursor != 0 {
		t.Errorf("H should jump to top of screen, cursor=%d", m.cursor)
	}
	m = key(m, "z")
	if !m.pendingZ {
		t.Error("z should await the chord's second key")
	}
	m = key(m, "z") // zz on a short list keeps everything visible
	if m.pendingZ || m.scroll != 0 {
		t.Errorf("zz should complete the chord, scroll=%d", m.scroll)
	}
}

func TestSearchModeSelectionKeys(t *testing.T) {
	m := key(testModel(t), "2", "/")
	upd, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = upd.(Model)
	if m.cursor != 1 {
		t.Errorf("ctrl+n while searching should move selection, cursor=%d", m.cursor)
	}
	upd, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = upd.(Model)
	if m.cursor != 0 {
		t.Errorf("ctrl+p while searching should move selection up, cursor=%d", m.cursor)
	}
	if !m.searching {
		t.Error("selection keys must not leave search mode")
	}
}

func TestDetailOverlay(t *testing.T) {
	m := key(testModel(t), "enter")
	if m.detail == nil {
		t.Fatal("enter should open detail")
	}
	if !strings.Contains(m.View(), "Ping timed out") {
		t.Error("detail view missing plugin output")
	}
	m = key(m, "esc")
	if m.detail != nil {
		t.Error("esc should close detail")
	}
}

func TestDetailFetchesMissingOutput(t *testing.T) {
	m := key(testModel(t), "2", "/", "c", "p", "u", "enter") // CPU load: OK, no output cached
	m, cmd := m.openDetail(m.rows()[0])
	if !m.detailLoading || cmd == nil {
		t.Fatal("detail on output-less service should fetch live output")
	}
	upd, _ := m.Update(detailMsg{
		host: "web01", desc: "CPU load",
		svc: &checkmk.Service{HostName: "web01", Description: "CPU load",
			State: checkmk.SvcOK, PluginOutput: "15 min load 0.42"},
	})
	m = upd.(Model)
	if m.detailLoading || !strings.Contains(m.View(), "15 min load 0.42") {
		t.Error("detailMsg should fill in live output")
	}
}

func TestViewSwitchAndSummary(t *testing.T) {
	m := key(testModel(t), "3")
	if got := len(m.rows()); got != 2 {
		t.Errorf("want 2 hosts, got %d", got)
	}
	v := m.View()
	// Service counts derive from host num_services_* aggregates.
	for _, want := range []string{"1 DOWN", "1 CRIT", "1 WARN", "1 OK"} {
		if !strings.Contains(v, want) {
			t.Errorf("summary missing %q", want)
		}
	}
}

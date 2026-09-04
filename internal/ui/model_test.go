package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tuichk/internal/checkmk"
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

	m = key(m, ":", "handled", "enter") // include handled
	if got := len(m.rows()); got != 3 {
		t.Errorf("want 3 problems incl. acknowledged, got %d", got)
	}
}

func TestFuzzySearchFilters(t *testing.T) {
	m := key(testModel(t), "3") // services view (full list already cached)
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

func TestNegativeSearch(t *testing.T) {
	m := key(testModel(t), "3") // services view: HTTPS certificate, Filesystem /var, CPU load
	// exclude a term
	m = key(m, "/", "!", "c", "p", "u")
	for _, r := range m.rows() {
		if strings.Contains(r.search, "cpu") {
			t.Errorf("!cpu should exclude CPU load, got %+v", r)
		}
	}
	if got := len(m.rows()); got != 2 {
		t.Errorf("!cpu should leave 2 of 3 services, got %d", got)
	}
	// combine positive fuzzy + negation
	m = key(m, "esc")
	m = key(m, "/", "f", "i", "l", "e", " ", "!", "v", "a", "r")
	if got := len(m.rows()); got != 0 {
		t.Errorf("'file !var' should drop Filesystem /var, got %d rows", got)
	}
	m = key(m, "esc")
	m = key(m, "/", "c", "e", "r", "t", " ", "!", "c", "p", "u")
	rows := m.rows()
	if len(rows) != 1 || rows[0].desc != "HTTPS certificate" {
		t.Errorf("'cert !cpu' should keep only the certificate, got %+v", rows)
	}
}

func TestSearchMatchesStateName(t *testing.T) {
	m := key(testModel(t), ":", "handled", "enter") // show handled too: CRIT + WARN + DOWN visible
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
	m := key(testModel(t), "3")
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
	m := key(testModel(t), "3") // services view, 3 rows
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
	m := key(testModel(t), "3") // services view, 3 rows
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
	m := key(testModel(t), "3", "/")
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

func colon(t *testing.T, m Model, cmd string) Model {
	t.Helper()
	m = key(m, ":")
	for _, ch := range cmd {
		m = key(m, string(ch))
	}
	upd, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return upd.(Model)
}

func TestColonCommands(t *testing.T) {
	m := colon(t, testModel(t), "services")
	if m.view != viewServices {
		t.Errorf(":services should switch view, got %v", m.view)
	}
	if m = colon(t, m, "2"); m.view != viewDown {
		t.Errorf(":2 should switch to the Down view, got %v", m.view)
	}
	if m = colon(t, m, "3"); m.view != viewServices {
		t.Errorf(":3 is the services alias, got %v", m.view)
	}
	if m = colon(t, m, "hosts"); m.view != viewHosts || len(m.rows()) != 2 {
		t.Errorf(":hosts should switch to hosts, view=%v", m.view)
	}

	// :N jumps to a row (view aliases win for 1-4)
	m = colon(t, m, "99")
	if want := len(m.rows()) - 1; m.cursor != want {
		t.Errorf(":99 should jump (clamped) to last row, cursor=%d", m.cursor)
	}

	// :help opens the reference; any key closes it
	m = colon(t, m, "help")
	if !m.helpOpen || !strings.Contains(m.View(), "keys & commands") {
		t.Error(":help should open the reference overlay")
	}
	m = key(m, "j")
	if m.helpOpen {
		t.Error("any key should close help")
	}

	// :r! refetches everything
	m = key(m, ":", "r", "!")
	upd, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = upd.(Model)
	if !m.loading || !m.allLoading || cmd == nil {
		t.Error(":r! should start both the cheap and the full refresh")
	}
}

func TestScrollChordsAndCursorVisibility(t *testing.T) {
	m := New(nil, "mysite", 0)
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	svcs := make([]checkmk.Service, 100)
	for i := range svcs {
		svcs[i] = checkmk.Service{HostName: fmt.Sprintf("h%02d", i),
			Description: "svc", State: checkmk.SvcOK, LastStateChange: time.Now()}
	}
	upd, _ = upd.Update(allSvcsMsg{at: time.Now(), services: svcs})
	m = key(upd.(Model), "3") // services view, 100 rows
	vis := m.tableHeight()

	visible := func(ctx string) {
		t.Helper()
		if m.cursor < m.scroll || m.cursor >= m.scroll+vis {
			t.Errorf("%s: cursor %d outside visible window [%d,%d)", ctx, m.cursor, m.scroll, m.scroll+vis)
		}
	}

	m = key(m, "G") // bottom: the selected row must actually be drawn
	if m.cursor != 99 {
		t.Fatalf("G should select last row, cursor=%d", m.cursor)
	}
	visible("after G")
	if !strings.Contains(m.View(), "h99") {
		t.Error("after G the selected row h99 must be rendered on screen")
	}

	m = colon(t, m, "50") // row 50 → cursor 49, mid-list, far from both ends
	if m.cursor != 49 {
		t.Fatalf(":50 should select row index 49, cursor=%d", m.cursor)
	}
	m = key(m, "z", "t")
	if m.scroll != m.cursor {
		t.Errorf("zt should scroll cursor line to top, scroll=%d cursor=%d", m.scroll, m.cursor)
	}
	m = key(m, "z", "b")
	if want := m.cursor - vis + 1; m.scroll != want {
		t.Errorf("zb should scroll cursor line to bottom, scroll=%d want %d", m.scroll, want)
	}
	visible("after zb")
	m = key(m, "z", "z")
	if want := m.cursor - vis/2; m.scroll != want {
		t.Errorf("zz should center cursor line, scroll=%d want %d", m.scroll, want)
	}
	visible("after zz")
}

func TestDownViewShowsHandledHosts(t *testing.T) {
	m := New(nil, "mysite", 0)
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	upd, _ = upd.Update(problemsMsg{
		at: time.Now(),
		hosts: []checkmk.Host{
			{Name: "up01", State: checkmk.HostUp},
			{Name: "down-acked", State: checkmk.HostDown, Acknowledged: true},
			{Name: "down-fresh", State: checkmk.HostDown},
			{Name: "unreach-dt", State: checkmk.HostUnreachable, InDowntime: true},
		},
	})
	m = key(upd.(Model), "2")
	rows := m.rows()
	if m.view != viewDown || len(rows) != 3 {
		t.Fatalf("Down view should list all 3 non-UP hosts incl. handled, got %d", len(rows))
	}
	// Problems view hides the handled ones.
	m = key(m, "1")
	if got := len(m.rows()); got != 1 {
		t.Errorf("Problems view should show only the unhandled DOWN host, got %d", got)
	}
}

func TestStateFilterIsHard(t *testing.T) {
	m := key(testModel(t), ":", "handled", "enter") // show handled: DOWN host + CRIT + WARN(acked)
	if got := len(m.rows()); got != 3 {
		t.Fatalf("expected 3 problems, got %d", got)
	}
	m = colon(t, m, "crit")
	rows := m.rows()
	if len(rows) != 2 {
		t.Fatalf(":crit should keep CRIT service + DOWN host, got %+v", rows)
	}
	for _, r := range rows {
		if !r.matchesClass(checkmk.SvcCrit) {
			t.Errorf("non-crit row leaked through :crit: %+v", r)
		}
	}
	// Fuzzy search stays inside the hard filter.
	m = key(m, "/", "w", "a", "r")
	for _, r := range m.rows() {
		if r.state == checkmk.SvcWarn && r.kind == rowService {
			t.Errorf("WARN row visible under :crit filter: %+v", r)
		}
	}
	m = key(m, "esc")
	if m = colon(t, m, "warn"); len(m.rows()) != 1 {
		t.Errorf(":warn should show only the WARN service, got %+v", m.rows())
	}
	if m = colon(t, m, "all"); len(m.rows()) != 3 {
		t.Errorf(":all should clear the filter, got %d rows", len(m.rows()))
	}
}

func TestHotWindowConfigurable(t *testing.T) {
	t.Cleanup(func() { SetHotWindow(15*time.Minute, 4*time.Hour) })
	r := row{kind: rowService, host: "a", desc: "x", state: checkmk.SvcCrit,
		since: time.Now().Add(-10 * time.Minute)}
	if r.hot() {
		t.Error("10m CRIT is outside the default 15m–4h window")
	}
	SetHotWindow(5*time.Minute, 8*time.Hour)
	if !r.hot() {
		t.Error("10m CRIT should be hot with a 5m–8h window")
	}
}

func TestHotWindowPops(t *testing.T) {
	now := time.Now()
	freshCrit := row{kind: rowService, host: "a", desc: "fresh", state: checkmk.SvcCrit, since: now.Add(-2 * time.Minute)}
	hotCrit := row{kind: rowService, host: "b", desc: "hot", state: checkmk.SvcCrit, since: now.Add(-30 * time.Minute)}
	staleCrit := row{kind: rowService, host: "c", desc: "stale", state: checkmk.SvcCrit, since: now.Add(-9 * time.Hour)}
	ackedHotAge := row{kind: rowService, host: "d", desc: "acked", state: checkmk.SvcCrit, acked: true, since: now.Add(-30 * time.Minute)}
	hotDown := row{kind: rowHost, host: "e", state: checkmk.HostDown, since: now.Add(-time.Hour)}
	warn := row{kind: rowService, host: "f", desc: "warn", state: checkmk.SvcWarn, since: now.Add(-30 * time.Minute)}

	if freshCrit.hot() || staleCrit.hot() || ackedHotAge.hot() || warn.hot() {
		t.Error("hot window must only cover unhandled crit-level problems aged 15m–4h")
	}
	if !hotCrit.hot() || !hotDown.hot() {
		t.Error("30m CRIT and 1h DOWN should be hot")
	}

	rows := []row{staleCrit, freshCrit, hotCrit}
	sortProblems(rows)
	if rows[0].desc != "hot" {
		t.Errorf("hot crit should sort first within CRIT, got %q", rows[0].desc)
	}
}

func TestQuitIsDeliberate(t *testing.T) {
	m := testModel(t)
	upd, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = upd.(Model)
	if cmd != nil {
		t.Error("plain q must not quit")
	}

	// :q quits
	m = key(m, ":", "q")
	upd2, cmd2 := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = upd2.(Model)
	if cmd2 == nil {
		t.Fatal(":q should quit")
	}
	if _, ok := cmd2().(tea.QuitMsg); !ok {
		t.Error(":q should produce tea.Quit")
	}

	// unknown command reports instead of quitting
	m = key(m, ":", "x")
	upd3, cmd3 := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = upd3.(Model)
	if cmd3 != nil || m.cmdErr == "" {
		t.Error("unknown :command should show an error, not quit")
	}

	// ctrl+c needs a second press; another key cancels
	upd4, cmd4 := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = upd4.(Model)
	if cmd4 != nil || !m.quitArmed {
		t.Error("first ctrl+c should only arm the quit")
	}
	m = key(m, "j") // cancels
	if m.quitArmed {
		t.Error("any other key should cancel the pending quit")
	}
	upd5, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = upd5.(Model)
	upd6, cmd6 := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_ = upd6
	if cmd6 == nil {
		t.Fatal("second consecutive ctrl+c should quit")
	}
	if _, ok := cmd6().(tea.QuitMsg); !ok {
		t.Error("double ctrl+c should produce tea.Quit")
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
	m := key(testModel(t), "3", "/", "c", "p", "u", "enter") // CPU load: OK, no output cached
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

func TestTabJumpsServiceToHostAndBack(t *testing.T) {
	// open the CRIT service detail (problems row 1 after the DOWN host)
	m := key(testModel(t), "j", "enter")
	if m.detail == nil || m.detail.kind != rowService || m.detail.desc != "HTTPS certificate" {
		t.Fatalf("expected service detail, got %+v", m.detail)
	}
	m = key(m, "tab")
	if m.detail == nil || m.detail.kind != rowHost || m.detail.host != "web01" {
		t.Fatalf("tab should open the service's host detail, got %+v", m.detail)
	}
	if !m.detailHostSvcsLoading {
		t.Error("host detail should fetch the host's services")
	}
	upd, _ := m.Update(hostSvcsMsg{host: "web01", services: []checkmk.Service{
		{HostName: "web01", Description: "HTTPS certificate", State: checkmk.SvcCrit, PluginOutput: "cert expires"},
		{HostName: "web01", Description: "CPU load", State: checkmk.SvcOK, PluginOutput: "load 0.4"},
	}})
	m = upd.(Model)
	v := m.View()
	if !strings.Contains(v, "CPU load") || !strings.Contains(v, "tab back") {
		t.Error("host detail should list its services and offer tab back")
	}
	m = key(m, "tab") // back to the service
	if m.detail == nil || m.detail.desc != "HTTPS certificate" {
		t.Fatalf("tab should return to the originating service, got %+v", m.detail)
	}
	m = key(m, "esc")
	if m.detail != nil || m.detailPrev != nil {
		t.Error("esc should close and clear the return slot")
	}
}

func TestViewSwitchAndSummary(t *testing.T) {
	m := key(testModel(t), "4")
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

func TestPlainHDoesNotToggleHandled(t *testing.T) {
	m := testModel(t)
	before := len(m.rows())
	m = key(m, "h")
	if m.showHandled || len(m.rows()) != before {
		t.Errorf("h should be unbound; showHandled=%v rows %d->%d", m.showHandled, before, len(m.rows()))
	}
}

func mouse(m Model, btn tea.MouseButton, x, y int) Model {
	upd, _ := m.Update(tea.MouseMsg{Button: btn, Action: tea.MouseActionPress, X: x, Y: y})
	return upd.(Model)
}

func TestMouseClickTabSwitchesView(t *testing.T) {
	m := testModel(t)
	if m.view != viewProblems {
		t.Fatalf("expected Problems view initially, got %v", m.view)
	}
	// "4 Hosts" is the last label; find its column from the geometry helper.
	x := 1
	for i := range viewNames {
		if view(i) == viewHosts {
			break
		}
		x += lipgloss.Width(styleTabInactive.Render(fmt.Sprintf("%d %s", i+1, viewNames[i]))) + 1
	}
	// The active tab renders wider; recompute through the real helper.
	if v, ok := m.tabAt(x + 2); !ok || v != viewHosts {
		t.Fatalf("tabAt(%d) = %v,%v; want Hosts", x+2, v, ok)
	}
	m = mouse(m, tea.MouseButtonLeft, x+2, rowTabs)
	if m.view != viewHosts {
		t.Errorf("click on Hosts tab: view = %v", m.view)
	}
	if _, ok := m.tabAt(0); ok {
		t.Error("column 0 is the margin, not a tab")
	}
}

func TestMouseClickSelectsThenOpens(t *testing.T) {
	m := testModel(t)
	rows := m.rows()
	if len(rows) < 2 {
		t.Fatalf("need at least 2 rows, got %d", len(rows))
	}
	m = mouse(m, tea.MouseButtonLeft, 10, rowFirstData+1)
	if m.cursor != 1 || m.detail != nil {
		t.Fatalf("first click should select row 1 without opening; cursor=%d detail=%v", m.cursor, m.detail != nil)
	}
	m = mouse(m, tea.MouseButtonLeft, 10, rowFirstData+1)
	if m.detail == nil || m.detail.host != rows[1].host || m.detail.desc != rows[1].desc {
		t.Errorf("second click should open row 1's detail, got %+v", m.detail)
	}
	// A click on a detail does not close it; esc still does.
	m = mouse(m, tea.MouseButtonLeft, 10, rowFirstData+1)
	if m.detail == nil {
		t.Error("click inside detail should not close it")
	}
	m = key(m, "esc")
	if m.detail != nil {
		t.Error("esc should close detail")
	}
	// Clicking below the last row is a no-op.
	m = mouse(m, tea.MouseButtonLeft, 10, rowFirstData+len(rows)+3)
	if m.cursor != 1 || m.detail != nil {
		t.Errorf("click on empty space changed state: cursor=%d detail=%v", m.cursor, m.detail != nil)
	}
}

func TestMouseWheelScrolls(t *testing.T) {
	m := testModel(t)
	// Shrink the table so the three problem rows overflow a 1-row window.
	upd, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 6})
	m = upd.(Model)
	m = key(m, ":", "handled", "enter") // 3 rows visible-able, 1 row of table
	if n := len(m.rows()); n != 3 {
		t.Fatalf("want 3 rows, got %d", n)
	}
	m = mouse(m, tea.MouseButtonWheelDown, 0, rowFirstData)
	if m.scroll != 2 || m.cursor != 2 {
		t.Errorf("wheel down: scroll=%d cursor=%d, want 2/2 (clamped to the end)", m.scroll, m.cursor)
	}
	m = mouse(m, tea.MouseButtonWheelUp, 0, rowFirstData)
	if m.scroll != 0 || m.cursor != 0 {
		t.Errorf("wheel up: scroll=%d cursor=%d, want 0/0", m.scroll, m.cursor)
	}
}

func TestMouseCommandToggles(t *testing.T) {
	m := testModel(t)
	m = key(m, ":", "mouse")
	upd, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = upd.(Model)
	if !m.mouse || cmd == nil {
		t.Fatalf(":mouse should enable and emit a command; mouse=%v cmd=%v", m.mouse, cmd != nil)
	}
	if _, ok := cmd().(tea.MouseMsg); ok {
		t.Error("toggle command must not be a mouse event")
	}
	m = key(m, ":", "mouse")
	upd, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = upd.(Model)
	if m.mouse || cmd == nil {
		t.Errorf(":mouse again should disable and emit a command; mouse=%v", m.mouse)
	}
	if !New(nil, "x", 0).EnableMouse().mouse {
		t.Error("EnableMouse should set the flag")
	}
}

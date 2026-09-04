// Package ui implements the tuichk terminal interface.
package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"tuichk/internal/checkmk"
)

type view int

const (
	viewProblems view = iota
	viewDown
	viewServices
	viewHosts
	viewCount
)

var viewNames = [...]string{"Problems", "Down", "Services", "Hosts"}

// problemsMsg carries the cheap, frequently refreshed data:
// all hosts plus only the non-OK services (filtered server-side).
type problemsMsg struct {
	hosts    []checkmk.Host
	problems []checkmk.Service
	err      error
	at       time.Time
}

// allSvcsMsg carries the expensive full service list (no output),
// fetched lazily and cached for the session.
type allSvcsMsg struct {
	services []checkmk.Service
	err      error
	at       time.Time
}

// detailMsg carries a single service's live output for the detail view.
type detailMsg struct {
	svc        *checkmk.Service
	err        error
	host, desc string
}

// graphsMsg carries the GUI graphs for the open detail view.
type graphsMsg struct {
	graphs     []checkmk.Graph
	err        error
	host, desc string // desc as requested (may be checkmk.HostGraphService)
}

// hostSvcsMsg carries one host's service list for its detail view.
type hostSvcsMsg struct {
	services []checkmk.Service
	err      error
	host     string
}

type refreshTickMsg struct{}

// Model is the bubbletea model for the whole application.
type Model struct {
	client   *checkmk.Client
	site     string
	interval time.Duration

	hosts    []checkmk.Host
	problems []checkmk.Service // non-OK services, with output

	all        []checkmk.Service // full list, nil until first fetch
	allLoading bool
	allAt      time.Time

	view        view
	cursor      int
	scroll      int // first visible row index
	showHandled bool
	stateFilter int      // -1 = all; else a checkmk.Svc* class, see matchesClass
	pendingZ    bool     // a "z" was pressed, awaiting z/t/b
	quitArmed   bool     // one quit key seen; a second confirms
	mouse       bool     // mouse capture on, see mouse.go
	showNumbers bool     // line numbers in the list, for :N
	count       int      // pending vim count prefix (12G, 5j); 0 = none
	ext         External // :browser and :ssh, see open.go

	searching bool
	search    textinput.Model

	cmdMode    bool // vim-style ":" command line
	cmd        textinput.Model
	cmdHist    []string // this session's ":" lines, oldest first
	cmdHistPos int      // where up/down are in cmdHist; len = the draft
	cmdDraft   string   // what was typed before walking the history
	cmdTabs    []string // tab completion candidates being cycled
	cmdTabIdx  int
	cmdErr     string
	helpOpen   bool
	helpVP     viewport.Model // the reference is longer than most windows

	detail        *row
	detailVP      viewport.Model
	detailLoading bool

	detailGraphs        []checkmk.Graph
	detailGraphsLoading bool
	detailGraphsErr     error

	detailHostSvcs        []checkmk.Service // host detail: its services
	detailHostSvcsLoading bool
	detailPrev            *row // service to return to after tab-to-host

	spin        spinner.Model
	loading     bool
	lastRefresh time.Time
	err         error

	width, height int
}

// New builds the initial model.
func New(client *checkmk.Client, site string, refreshSeconds int) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "fuzzy filter…"
	ti.CharLimit = 128

	ci := textinput.New()
	ci.Prompt = ":"
	ci.CharLimit = 32

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(colAccent)

	return Model{
		client:      client,
		site:        site,
		interval:    time.Duration(refreshSeconds) * time.Second,
		search:      ti,
		cmd:         ci,
		stateFilter: -1,
		spin:        sp,
		loading:     true,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchProblems(), m.spin.Tick)
}

func (m Model) fetchProblems() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		// Slow sites take seconds per query; run both concurrently.
		var hosts []checkmk.Host
		var problems []checkmk.Service
		var hostErr, svcErr error
		done := make(chan struct{})
		go func() {
			hosts, hostErr = client.Hosts(ctx)
			done <- struct{}{}
		}()
		go func() {
			problems, svcErr = client.ProblemServices(ctx)
			done <- struct{}{}
		}()
		<-done
		<-done
		if hostErr != nil {
			return problemsMsg{err: hostErr, at: time.Now()}
		}
		if svcErr != nil {
			return problemsMsg{err: svcErr, at: time.Now()}
		}
		return problemsMsg{hosts: hosts, problems: problems, at: time.Now()}
	}
}

func (m Model) fetchAllServices() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		// The one heavy request: bounded generously, issued at most
		// once per session unless explicitly refreshed.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		svcs, err := client.AllServices(ctx)
		return allSvcsMsg{services: svcs, err: err, at: time.Now()}
	}
}

func (m Model) fetchDetail(host, desc string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		svc, err := client.ServiceDetail(ctx, host, desc)
		return detailMsg{svc: svc, err: err, host: host, desc: desc}
	}
}

func (m Model) fetchGraphs(host, desc string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		graphs, err := client.ServiceGraphs(ctx, host, desc)
		return graphsMsg{graphs: graphs, err: err, host: host, desc: desc}
	}
}

func (m Model) fetchHostServices(host string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		svcs, err := client.HostServices(ctx, host)
		return hostSvcsMsg{services: svcs, err: err, host: host}
	}
}

func (m Model) scheduleRefresh() tea.Cmd {
	if m.interval <= 0 {
		return nil
	}
	return tea.Tick(m.interval, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

// problemOutput looks up live output for a service from the problem list.
func (m Model) problemOutput(host, desc string) string {
	for i := range m.problems {
		if m.problems[i].HostName == host && m.problems[i].Description == desc {
			return m.problems[i].PluginOutput
		}
	}
	return ""
}

// rows returns the fully filtered, sorted rows for the current view.
func (m Model) rows() []row {
	var rows []row
	switch m.view {
	case viewProblems:
		for _, h := range m.hosts {
			if h.State != checkmk.HostUp {
				rows = append(rows, hostRow(h))
			}
		}
		for _, s := range m.problems {
			rows = append(rows, serviceRow(s))
		}
		if !m.showHandled {
			unhandled := rows[:0]
			for _, r := range rows {
				if !r.handled() {
					unhandled = append(unhandled, r)
				}
			}
			rows = unhandled
		}
		sortProblems(rows)
	case viewDown:
		// Every non-UP host, handled or not — downtimes and acks are
		// visible here (flagged D/A) instead of hidden.
		for _, h := range m.hosts {
			if h.State != checkmk.HostUp {
				rows = append(rows, hostRow(h))
			}
		}
		sortProblems(rows)
	case viewServices:
		for _, s := range m.all {
			r := serviceRow(s)
			if r.output == "" && r.state != checkmk.SvcOK {
				r.output = m.problemOutput(s.HostName, s.Description)
				r.search = strings.ToLower(svcStateNames[s.State] + " " + s.HostName + " " + s.Description + " " + r.output)
			}
			rows = append(rows, r)
		}
		sortByName(rows)
	case viewHosts:
		for _, h := range m.hosts {
			rows = append(rows, hostRow(h))
		}
		sortByName(rows)
	}
	// The hard state filter runs before fuzzy search, so a query inside
	// e.g. :crit can never surface rows of another state.
	if m.stateFilter >= 0 {
		kept := rows[:0]
		for _, r := range rows {
			if r.matchesClass(m.stateFilter) {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	return fuzzyFilter(rows, m.search.Value())
}

func (m *Model) clampCursor(n int) {
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	vis := m.tableHeight()
	if vis < 1 {
		vis = 1
	}
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+vis {
		m.scroll = m.cursor - vis + 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

// tableHeight is the number of data rows drawn on screen: everything
// minus the five chrome lines (title, summary, tabs, column header,
// footer). Every scroll/clamp computation must use this same number —
// viewTable must not consume additional lines out of it.
func (m Model) tableHeight() int {
	return m.height - 5
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.detail != nil {
			m.resizeDetail()
		}
		if m.helpOpen {
			m = m.openHelp()
		}
		return m, nil

	case problemsMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			m.hosts = msg.hosts
			m.problems = msg.problems
			m.lastRefresh = msg.at
		}
		m.clampCursor(len(m.rows()))
		return m, m.scheduleRefresh()

	case allSvcsMsg:
		m.allLoading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.all = msg.services
			m.allAt = msg.at
		}
		m.clampCursor(len(m.rows()))
		return m, nil

	case detailMsg:
		if m.detail == nil || m.detail.host != msg.host || m.detail.desc != msg.desc {
			return m, nil // detail closed or changed meanwhile
		}
		m.detailLoading = false
		if msg.err != nil {
			m.detail.output = "error fetching output: " + msg.err.Error()
		} else {
			m.detail.output = msg.svc.PluginOutput
			if msg.svc.LongOutput != "" {
				m.detail.output += "\n\n" + msg.svc.LongOutput
			}
			m.detail.state = msg.svc.State
			m.detail.acked = msg.svc.Acknowledged
			m.detail.downtime = msg.svc.InDowntime
		}
		m.detailVP.SetContent(m.detailContent(m.detailVP.Width))
		return m, nil

	case graphsMsg:
		if m.detail == nil || m.detail.host != msg.host || m.detail.graphService() != msg.desc {
			return m, nil // detail closed or changed meanwhile
		}
		m.detailGraphsLoading = false
		m.detailGraphs = msg.graphs
		m.detailGraphsErr = msg.err
		m.detailVP.SetContent(m.detailContent(m.detailVP.Width))
		return m, nil

	case hostSvcsMsg:
		if m.detail == nil || m.detail.kind != rowHost || m.detail.host != msg.host {
			return m, nil
		}
		m.detailHostSvcsLoading = false
		if msg.err == nil {
			m.detailHostSvcs = msg.services
		}
		m.detailVP.SetContent(m.detailContent(m.detailVP.Width))
		return m, nil

	case refreshTickMsg:
		if m.loading {
			return m, m.scheduleRefresh()
		}
		m.loading = true
		return m, tea.Batch(m.fetchProblems(), m.spin.Tick)

	case spinner.TickMsg:
		if !m.loading && !m.allLoading && !m.detailLoading && !m.detailGraphsLoading && !m.detailHostSvcsLoading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	case noticeMsg:
		m.cmdErr = string(msg)
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Quitting is deliberate: ":q" like vim, or ctrl+c pressed twice.
	// A stray key can't kill a long-running session.
	wasArmed := m.quitArmed
	m.quitArmed = false
	m.cmdErr = ""
	if msg.Type == tea.KeyCtrlC {
		if wasArmed {
			return m, tea.Quit
		}
		m.quitArmed = true
		return m, nil
	}

	// Help overlay: scrolls like the detail view, closes on esc/q/?.
	if m.helpOpen {
		switch msg.String() {
		case "esc", "q", "?", "enter":
			m.helpOpen = false
		case "g", "home":
			m.helpVP.GotoTop()
		case "G", "end":
			m.helpVP.GotoBottom()
		default:
			var cmd tea.Cmd
			m.helpVP, cmd = m.helpVP.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if m.cmdMode {
		return m.cmdKey(msg)
	}

	// Detail overlay has its own small keymap; the viewport already
	// understands j/k/d/u/f/b, g/G are added here.
	if m.detail != nil {
		switch msg.String() {
		case ":": // :browser and :ssh act on the open detail
			return m, m.openCmd()
		case "esc", "enter", "q":
			m.detail = nil
			m.detailLoading = false
			m.detailGraphsLoading = false
			m.detailHostSvcsLoading = false
			m.detailPrev = nil
		case "tab":
			// service → its host; host → back to the service it came from
			if m.detail.kind == rowService {
				prev := *m.detail
				mm, cmd := m.openHostOf(m.detail.host)
				mm.detailPrev = &prev
				return mm, cmd
			}
			if m.detailPrev != nil {
				prev := *m.detailPrev
				mm, cmd := m.openDetail(prev)
				mm.detailPrev = nil
				return mm, cmd
			}
		case "g":
			m.detailVP.GotoTop()
		case "G":
			m.detailVP.GotoBottom()
		default:
			var cmd tea.Cmd
			m.detailVP, cmd = m.detailVP.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// While the search input is focused, most keys type into it.
	if m.searching {
		switch msg.Type {
		case tea.KeyEsc:
			m.searching = false
			m.search.Blur()
			m.search.SetValue("")
			m.clampCursor(len(m.rows()))
			return m, nil
		case tea.KeyEnter:
			m.searching = false
			m.search.Blur()
			return m, nil
		case tea.KeyUp, tea.KeyDown:
			// fall through to list navigation below
		case tea.KeyCtrlJ, tea.KeyCtrlN: // fzf-style: move selection while typing
			m.cursor++
			m.clampCursor(len(m.rows()))
			return m, nil
		case tea.KeyCtrlK, tea.KeyCtrlP:
			m.cursor--
			m.clampCursor(len(m.rows()))
			return m, nil
		default:
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			m.cursor, m.scroll = 0, 0
			m.clampCursor(len(m.rows()))
			return m, cmd
		}
	}

	rows := m.rows()
	vis := m.tableHeight()
	if vis < 1 {
		vis = 1
	}

	// Second key of a vim "z" scroll chord (zz/zt/zb).
	if m.pendingZ {
		m.pendingZ = false
		switch msg.String() {
		case "z":
			m.scroll = m.cursor - vis/2
		case "t":
			m.scroll = m.cursor
		case "b":
			m.scroll = m.cursor - (vis - 1)
		default:
			return m, nil
		}
		if max := len(rows) - vis; m.scroll > max {
			m.scroll = max
		}
		if m.scroll < 0 {
			m.scroll = 0
		}
		return m, nil
	}

	// Digits build a vim-style count for the next motion: 12G goes to
	// row 12, 5j moves five. A leading 0 is not a count.
	if k := msg.String(); len(k) == 1 && k[0] >= '0' && k[0] <= '9' && (m.count > 0 || k != "0") {
		if m.count < 100000 {
			m.count = m.count*10 + int(k[0]-'0')
		}
		return m, nil
	}
	count, n := m.count, m.count
	m.count = 0
	if n == 0 {
		n = 1
	}

	switch msg.String() {
	case ":":
		return m, m.openCmd()
	case "z":
		m.pendingZ = true
	case "H": // top of screen
		m.cursor = m.scroll
		m.clampCursor(len(rows))
	case "M": // middle of screen
		onScreen := len(rows) - m.scroll
		if onScreen > vis {
			onScreen = vis
		}
		m.cursor = m.scroll + onScreen/2
		m.clampCursor(len(rows))
	case "L": // bottom of screen
		onScreen := len(rows) - m.scroll
		if onScreen > vis {
			onScreen = vis
		}
		m.cursor = m.scroll + onScreen - 1
		m.clampCursor(len(rows))
	case "/":
		m.searching = true
		m.search.Focus()
		return m, textinput.Blink
	case "esc":
		if m.search.Value() != "" {
			m.search.SetValue("")
			m.clampCursor(len(m.rows()))
		}
	case "tab":
		return m.switchView((m.view + 1) % viewCount)
	case "shift+tab":
		return m.switchView((m.view + viewCount - 1) % viewCount)
	case "r":
		// Refreshing the heavy full list stays deliberate: only from
		// the Services view (or :r! from anywhere).
		return m.refresh(m.view == viewServices)
	case "?":
		m = m.openHelp()
	case "j", "down":
		m.cursor += n
		m.clampCursor(len(rows))
	case "k", "up":
		m.cursor -= n
		m.clampCursor(len(rows))
	case "g", "home", "G", "end":
		// g/G go to the first/last row; with a count both go to row N.
		m.cursor = 0
		if msg.String() == "G" || msg.String() == "end" {
			m.cursor = len(rows) - 1
		}
		if count > 0 {
			m.cursor = count - 1
		}
		m.clampCursor(len(rows))
	// Plain d/u/f/b work alongside the ctrl variants so paging still
	// works inside multiplexers (zellij, tmux) that eat ctrl keys.
	case "ctrl+d", "d":
		m.cursor += n * (vis / 2)
		m.clampCursor(len(rows))
	case "ctrl+u", "u":
		m.cursor -= n * (vis / 2)
		m.clampCursor(len(rows))
	case "ctrl+f", "f", "pgdown":
		m.cursor += n * (vis - 1)
		m.clampCursor(len(rows))
	case "ctrl+b", "b", "pgup":
		m.cursor -= n * (vis - 1)
		m.clampCursor(len(rows))
	case "enter":
		if m.cursor < len(rows) {
			return m.openDetail(rows[m.cursor])
		}
	}
	return m, nil
}

// refresh triggers the cheap problems fetch; full additionally refetches
// the heavy complete service list.
func (m Model) refresh(full bool) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	if !m.loading {
		m.loading = true
		cmds = append(cmds, m.fetchProblems(), m.spin.Tick)
	}
	if full && !m.allLoading {
		m.allLoading = true
		cmds = append(cmds, m.fetchAllServices(), m.spin.Tick)
	}
	return m, tea.Batch(cmds...)
}

// runCommand executes a ":"-line, vim-style.
func (m Model) runCommand(line string) (tea.Model, tea.Cmd) {
	switch line {
	case "":
		return m, nil
	case "q", "q!", "qa", "quit", "exit":
		return m, tea.Quit
	case "p", "problems":
		return m.switchView(viewProblems)
	case "d", "down":
		return m.switchView(viewDown)
	case "s", "services":
		return m.switchView(viewServices)
	case "hosts":
		return m.switchView(viewHosts)
	case "r", "refresh":
		return m.refresh(false)
	case "r!", "refresh!": // also refetch the heavy full service list
		return m.refresh(true)
	case "handled":
		m.showHandled = !m.showHandled
		m.clampCursor(len(m.rows()))
		return m, nil
	case "mouse":
		return m.toggleMouse()
	case "numbers", "nu":
		m.showNumbers = !m.showNumbers
		return m, nil
	case "browser", "b":
		return m.openBrowser()
	case "ssh":
		return m.openSSH()
	case "wiki":
		return m.openWiki()
	case "crit", "c":
		return m.setStateFilter(checkmk.SvcCrit)
	case "warn", "w":
		return m.setStateFilter(checkmk.SvcWarn)
	case "unknown", "unkn", "u":
		return m.setStateFilter(checkmk.SvcUnknown)
	case "ok":
		return m.setStateFilter(checkmk.SvcOK)
	case "all":
		return m.setStateFilter(-1)
	case "h", "help":
		return m.openHelp(), nil
	}
	// :N jumps to row N, like a vim line number (:numbers shows them).
	if n, err := strconv.Atoi(line); err == nil && n > 0 {
		m.cursor = n - 1
		m.clampCursor(len(m.rows()))
		return m, nil
	}
	m.cmdErr = "not a command: :" + line + "  (:help lists commands)"
	return m, nil
}

func (m Model) setStateFilter(class int) (Model, tea.Cmd) {
	m.stateFilter = class
	m.cursor, m.scroll = 0, 0
	m.clampCursor(len(m.rows()))
	return m, nil
}

func (m Model) switchView(v view) (Model, tea.Cmd) {
	if m.view == v {
		return m, nil
	}
	m.view = v
	m.cursor, m.scroll = 0, 0
	m.clampCursor(len(m.rows()))
	// First visit to the Services view kicks off the one-time
	// background fetch of the full list.
	if v == viewServices && m.all == nil && !m.allLoading {
		m.allLoading = true
		return m, tea.Batch(m.fetchAllServices(), m.spin.Tick)
	}
	return m, nil
}

// graphService is the popup-endpoint service name for this row.
func (r row) graphService() string {
	if r.kind == rowHost {
		return checkmk.HostGraphService
	}
	return r.desc
}

func (m Model) openDetail(r row) (Model, tea.Cmd) {
	m.detail = &r
	m.detailGraphs = nil
	m.detailGraphsErr = nil
	m.detailGraphsLoading = true
	m.detailHostSvcs = nil
	cmds := []tea.Cmd{m.fetchGraphs(r.host, r.graphService()), m.spin.Tick}
	// Services from the cached full list carry no output; fetch it live.
	if r.kind == rowService && r.output == "" {
		m.detailLoading = true
		cmds = append(cmds, m.fetchDetail(r.host, r.desc))
	}
	// A host detail also lists the host's services (one filtered query).
	if r.kind == rowHost {
		m.detailHostSvcsLoading = true
		cmds = append(cmds, m.fetchHostServices(r.host))
	}
	m.resizeDetail()
	return m, tea.Batch(cmds...)
}

// openHostOf jumps from a service detail to its host's detail — no
// trip through the Hosts view needed.
func (m Model) openHostOf(host string) (Model, tea.Cmd) {
	for _, h := range m.hosts {
		if h.Name == host {
			return m.openDetail(hostRow(h))
		}
	}
	// Host list not loaded (yet); synthesize a bare row so graphs and
	// services still work.
	return m.openDetail(row{kind: rowHost, host: host})
}

// overlaySize is the inner size of the centred detail/help box.
func (m Model) overlaySize() (w, h int) {
	w = m.width - 8
	if w > 100 {
		w = 100
	}
	if w < 20 {
		w = 20
	}
	h = m.height - 8
	if h < 5 {
		h = 5
	}
	return w, h
}

func (m *Model) resizeDetail() {
	w, h := m.overlaySize()
	m.detailVP = viewport.New(w, h)
	if m.detail != nil {
		m.detailVP.SetContent(m.detailContent(w))
	}
}

// openHelp shows the reference in a viewport sized like the detail box,
// so the parts below the fold are reachable with the usual keys.
func (m Model) openHelp() Model {
	m.helpOpen = true
	w, h := m.overlaySize()
	y := m.helpVP.YOffset
	m.helpVP = viewport.New(w, h)
	m.helpVP.SetContent(helpContent())
	m.helpVP.SetYOffset(y)
	return m
}

// helpContent is the full key/command reference shown by :help —
// everything needed to drive the UI is on this one screen.
func helpContent() string {
	sec := lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render
	k := lipgloss.NewStyle().Foreground(colDim).Width(22).Render
	var b strings.Builder
	line := func(keys, what string) { b.WriteString("  " + k(keys) + what + "\n") }

	b.WriteString(styleTitle.Render("tuichk — keys & commands") + "\n\n")

	b.WriteString(sec("Views") + "\n")
	line("tab / shift+tab", "next / previous view: Problems · Down · Services · Hosts")
	line(":problems :down …", "a view by name (:services :hosts, :p :d :s)")
	line("", styleDim.Render("Down lists every non-UP host, incl. acked/downtime"))

	b.WriteString("\n" + sec("Movement") + "\n")
	line("j / k, ↑ / ↓", "one row down / up")
	line("12G, 5j, 3d …", "count prefix, vim-style: row 12; five down; three half pages")
	line("d / u  (^d / ^u)", "half page down / up")
	line("f / b  (^f/^b, PgDn/Up)", "full page down / up")
	line("g / G, Home / End", "first / last row")
	line("H / M / L", "move cursor to top / middle / bottom of screen")
	line("zz / zt / zb", "scroll view so cursor line sits center / top / bottom")
	line("", styleDim.Render("z-chords move the view, not the cursor — no-ops near the list top"))
	line(":N", "jump to row N")
	line(":numbers (:nu)", "toggle line numbers")

	b.WriteString("\n" + sec("Search") + "\n")
	line("/", "fuzzy filter over state, host, service, output")
	line("", styleDim.Render("e.g. \"crit nfs\", \"down web\", \"warn cert\""))
	line("!term", "exclude matches — like fzf, e.g. \"nfs !ssd\"")
	line("↑/↓  (^j/^k, ^n/^p)", "move selection while typing")
	line("enter / esc", "keep filter / clear it")
	line(":crit :warn :unknown", "hard state filter — fuzzy search stays within it")
	line(":all (:ok)", "clear the state filter (DOWN=crit, UNREACH=unknown)")

	b.WriteString("\n" + sec("Detail") + "\n")
	line("enter", "open details: live output + the GUI's graphs")
	line("tab", "service ⇄ its host (host detail lists all its services)")
	line("j/k d/u f/b g/G", "scroll · esc or q closes")

	b.WriteString("\n" + sec("Data") + "\n")
	line(":handled", "toggle handled problems (A ack, D downtime)")
	line("", styleDim.Render(fmt.Sprintf(
		"inverted badge = crit aged %s–%s: the actionable window", fmtDur(hotMinAge), fmtDur(hotMaxAge))))
	line("r, :r", "refresh hosts + problems")
	line(":r!", "also refetch the full service list (heavy)")
	line(":mouse", "toggle mouse: wheel scrolls, click selects, click again opens, click a tab")
	line(":browser", "open the selected host/service in the CheckMK web GUI")
	line(":wiki", "open the selected host in your wiki (wiki_url in the config)")
	line(":ssh", "ssh to the selected host: new pane in zellij/tmux/wezterm/kitty/ghostty, else in place")
	line("", styleDim.Render("mouse capture disables plain text selection — use shift-drag"))

	b.WriteString("\n" + sec("Quit") + "\n")
	line(":q", "quit (also :quit, :q!)")
	line("", styleDim.Render("on the : line, tab completes a command and up/down recall earlier ones"))
	line("ctrl+c ctrl+c", "quit (single press asks first)")

	b.WriteString("\n" + styleDim.Render("  ? or :help opens this screen · any key closes"))
	return b.String()
}

func (m Model) detailContent(width int) string {
	r := *m.detail
	var b strings.Builder
	label := lipgloss.NewStyle().Foreground(colDim).Width(10).Render
	write := func(k, v string) {
		b.WriteString(label(k) + " " + v + "\n")
	}
	kind := "Service"
	if r.kind == rowHost {
		kind = "Host"
	}
	title := r.host
	if r.desc != "" {
		title += " ▸ " + r.desc
	}
	b.WriteString(styleTitle.Render(title) + "\n\n")
	write("Type", kind)
	write("State", r.stateStyled())
	write("Since", fmt.Sprintf("%s (%s ago)", r.since.Format("2006-01-02 15:04:05"), age(r.since)))
	flags := []string{}
	if r.acked {
		flags = append(flags, "acknowledged")
	}
	if r.downtime {
		flags = append(flags, "in downtime")
	}
	if len(flags) > 0 {
		write("Handled", strings.Join(flags, ", "))
	}
	b.WriteString("\n" + styleDim.Render("Output") + "\n")
	if r.output == "" {
		b.WriteString(styleDim.Render("fetching live output…"))
	} else {
		b.WriteString(lipgloss.NewStyle().Width(width).Render(r.output))
	}

	// Graphs — the same ones the web GUI shows, drawn in braille.
	// They sit right under the status so a host jump shows them up top,
	// above the (potentially long) services list.
	b.WriteString("\n\n" + styleDim.Render("Graphs") + "\n")
	switch {
	case m.detailGraphsLoading:
		b.WriteString(styleDim.Render("fetching graphs…"))
	case m.detailGraphsErr != nil:
		b.WriteString(styleErr.Render("graphs: " + m.detailGraphsErr.Error()))
	case len(m.detailGraphs) == 0:
		if r.kind == rowHost {
			b.WriteString(styleDim.Render("none for this host (agent-monitored hosts have graphs per service below)"))
		} else {
			b.WriteString(styleDim.Render("no graphs available"))
		}
	default:
		for i, g := range m.detailGraphs {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(renderGraph(g, width))
		}
	}

	// Host details also list the host's services, problems first.
	if r.kind == rowHost {
		b.WriteString("\n\n" + styleDim.Render("Services") + "\n")
		switch {
		case m.detailHostSvcsLoading:
			b.WriteString(styleDim.Render("fetching services…"))
		case len(m.detailHostSvcs) == 0:
			b.WriteString(styleDim.Render("none"))
		default:
			rows := make([]row, 0, len(m.detailHostSvcs))
			for _, s := range m.detailHostSvcs {
				rows = append(rows, serviceRow(s))
			}
			sortProblems(rows)
			for _, sr := range rows {
				line := sr.stateStyled() + " " + sr.desc
				if sr.output != "" {
					line += styleDim.Render("  " + strings.ReplaceAll(sr.output, "\n", " "))
				}
				b.WriteString(truncate.StringWithTail(line, uint(width), "…") + "\n")
			}
		}
	}
	return b.String()
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	var b strings.Builder
	b.WriteString(m.viewHeader() + "\n")
	b.WriteString(m.viewSummary() + "\n")
	b.WriteString(m.viewTabs() + "\n")
	b.WriteString(m.viewTable())
	b.WriteString(m.viewFooter())

	if m.helpOpen {
		hint := "esc close · j/k scroll · d/u f/b page · g/G top/bottom"
		box := styleDetailBox.Render(m.helpVP.View() + "\n" + styleDim.Render(hint))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	if m.detail != nil {
		hint := "esc close · j/k scroll"
		if m.detail.kind == rowService {
			hint += " · tab host"
		} else if m.detailPrev != nil {
			hint += " · tab back"
		}
		box := styleDetailBox.Render(m.detailVP.View() + "\n" + styleDim.Render(hint))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	}
	return b.String()
}

func (m Model) viewHeader() string {
	left := styleTitle.Render(" tuichk ") + styleDim.Render(m.site)
	var right string
	switch {
	case m.loading:
		right = m.spin.View() + " refreshing "
	case m.allLoading:
		right = m.spin.View() + " loading all services… "
	case !m.lastRefresh.IsZero():
		right = styleDim.Render("updated " + m.lastRefresh.Format("15:04:05") + " ")
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// viewSummary derives site-wide counts. Service totals come from the
// hosts' num_services_* aggregates, so no full service fetch is needed.
func (m Model) viewSummary() string {
	var up, down, unreach int
	var ok, warn, crit, unknown int
	for _, h := range m.hosts {
		switch h.State {
		case checkmk.HostUp:
			up++
		case checkmk.HostDown:
			down++
		default:
			unreach++
		}
		ok += h.NumOK
		warn += h.NumWarn
		crit += h.NumCrit
		unknown += h.NumUnknown
	}
	part := func(style lipgloss.Style, n int, name string, always bool) string {
		if n == 0 && !always {
			return ""
		}
		return " " + style.Render(fmt.Sprintf("%d %s", n, name))
	}
	return styleDim.Render(" Hosts:") +
		part(stateStyles[0], up, "UP", true) +
		part(stateStyles[2], down, "DOWN", false) +
		part(stateStyles[3], unreach, "UNREACH", false) +
		styleDim.Render("   Services:") +
		part(stateStyles[0], ok, "OK", true) +
		part(stateStyles[1], warn, "WARN", false) +
		part(stateStyles[2], crit, "CRIT", false) +
		part(stateStyles[3], unknown, "UNKNOWN", false)
}

func (m Model) viewTabs() string {
	var tabs []string
	for i, name := range viewNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if view(i) == m.view {
			tabs = append(tabs, styleTabActive.Render(label))
		} else {
			tabs = append(tabs, styleTabInactive.Render(label))
		}
	}
	line := " " + strings.Join(tabs, " ")
	if m.stateFilter >= 0 {
		line += "  " + stateStyles[m.stateFilter].Render(svcStateNames[m.stateFilter]+" only") +
			styleDim.Render(" (:all clears)")
	}
	if m.view == viewProblems && m.showHandled {
		line += styleDim.Render("  (incl. handled)")
	}
	if m.view == viewServices && m.all != nil {
		line += styleDim.Render("  (list cached " + m.allAt.Format("15:04") + " · r to refetch)")
	}
	if m.searching || m.search.Value() != "" {
		line += "  " + m.search.View()
	}
	return line
}

func (m Model) viewTable() string {
	rows := m.rows()
	height := m.tableHeight()
	if height < 1 {
		height = 1
	}

	hostW := 22
	if m.width < 100 {
		hostW = 16
	}
	descW := 32
	if m.width < 100 {
		descW = 24
	}
	ageW := 4

	var b strings.Builder
	pad := func(s string, w int) string {
		s = truncate.StringWithTail(s, uint(w), "…")
		return s + strings.Repeat(" ", w-lipgloss.Width(s))
	}

	svcCol := m.view == viewProblems || m.view == viewServices
	// :numbers adds a right-aligned line number column, wide enough for
	// the last row, so :N has something to aim at.
	numW := 0
	if m.showNumbers {
		numW = len(strconv.Itoa(len(rows)))
		if numW < 2 {
			numW = 2
		}
	}
	number := func(s string) string {
		return strings.Repeat(" ", numW-len(s)) + s + " "
	}
	header := " " // the selection marker column
	if m.showNumbers {
		header += number("#")
	}
	header += pad("STATE", 6) + pad("F", 2) + pad("HOST", hostW+1)
	if svcCol {
		header += pad("SERVICE", descW+1)
	}
	header += pad("AGE", ageW+1) + "OUTPUT"
	b.WriteString(styleHeaderRow.Render(truncate.String(header, uint(m.width))) + "\n")

	if len(rows) == 0 {
		msg := "no results"
		switch {
		case m.view == viewServices && m.allLoading:
			msg = m.spin.View() + " fetching the full service list once — this can take a while on a big site…"
		case m.view == viewProblems && m.search.Value() == "":
			msg = "✓ no problems — everything is green"
		case m.view == viewDown && m.search.Value() == "":
			msg = "✓ no hosts down or unreachable"
		}
		b.WriteString("  " + styleDim.Render(msg) + strings.Repeat("\n", height))
		return b.String()
	}

	end := m.scroll + height
	if end > len(rows) {
		end = len(rows)
	}
	for i := m.scroll; i < end; i++ {
		r := rows[i]
		sel := i == m.cursor

		// On the selected row every cell carries the background, so the
		// highlight fills the whole line instead of breaking at each
		// cell's own reset code. Plain cells go through base; styled
		// cells get the background added.
		base := lipgloss.NewStyle()
		var bg lipgloss.TerminalColor
		if sel {
			bg = selBg
			base = base.Background(selBg).Bold(true)
		}
		paint := func(s lipgloss.Style) lipgloss.Style {
			if sel {
				return s.Background(selBg)
			}
			return s
		}

		flags := " "
		if r.acked {
			flags = "A"
		} else if r.downtime {
			flags = "D"
		}
		body := r.stateStyledBG(bg) + base.Render(" ") +
			paint(styleDim).Render(pad(flags, 2)) + base.Render(pad(r.host, hostW)+" ")
		if m.showNumbers {
			body = paint(styleDim).Render(number(strconv.Itoa(i+1))) + body
		}
		if svcCol {
			body += base.Render(pad(r.desc, descW) + " ")
		}
		ageStyle := styleDim
		if r.hot() {
			ageStyle = stateStyles[2]
		}
		body += paint(ageStyle).Render(pad(age(r.since), ageW)) + base.Render(" ")
		outW := m.width - lipgloss.Width(body) - 2
		if outW > 0 {
			body += base.Render(truncate.StringWithTail(strings.ReplaceAll(r.output, "\n", " "), uint(outW), "…"))
		}
		if sel {
			// fill the rest of the line so the band spans full width
			if padN := m.width - 1 - lipgloss.Width(body); padN > 0 {
				body += base.Render(strings.Repeat(" ", padN))
			}
			b.WriteString(styleCursorBar.Render("▌") + body + "\n")
		} else {
			b.WriteString(" " + body + "\n")
		}
	}
	for i := end - m.scroll; i < height; i++ {
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) viewFooter() string {
	if m.cmdMode {
		return m.cmdLineView()
	}
	if m.quitArmed {
		return stateStyles[1].Render(" really quit? ctrl+c again — any other key cancels (or use :q)")
	}
	if m.cmdErr != "" {
		return stateStyles[1].Render(" " + m.cmdErr)
	}
	if m.err != nil {
		return styleErr.Render(truncate.StringWithTail(" ✗ "+m.err.Error(), uint(m.width), "…"))
	}
	help := " / search · enter details · tab views · r refresh · :q quit · ? help"
	if m.view == viewProblems {
		help += " · :handled"
	}
	rows := m.rows()
	pos := ""
	if len(rows) > 0 {
		pos = fmt.Sprintf("%d/%d ", m.cursor+1, len(rows))
	}
	if m.count > 0 {
		pos = fmt.Sprintf("%d  ", m.count) + pos
	}
	gap := m.width - lipgloss.Width(help) - lipgloss.Width(pos)
	if gap < 1 {
		gap = 1
	}
	return styleDim.Render(help + strings.Repeat(" ", gap) + pos)
}

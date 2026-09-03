package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"tuicheck/internal/checkmk"
)

type rowKind int

const (
	rowHost rowKind = iota
	rowService
)

// row is one displayable line: either a host or a service.
type row struct {
	kind     rowKind
	host     string
	desc     string // service description; empty for hosts
	state    int
	acked    bool
	downtime bool
	output   string
	since    time.Time
	search   string // lowercased fuzzy-match target
}

func (r row) handled() bool { return r.acked || r.downtime }

// The "hot window": a crit-level problem aged 15min–4h (configurable)
// is the truly actionable kind — old enough not to be a transient flap,
// young enough not to be a known, stale issue. These get extra visual
// weight and sort above their peers.
var (
	hotMinAge = 15 * time.Minute
	hotMaxAge = 4 * time.Hour
)

// SetHotWindow overrides the hot-window bounds (from config).
func SetHotWindow(min, max time.Duration) {
	hotMinAge, hotMaxAge = min, max
}

func (r row) hot() bool {
	if r.handled() {
		return false
	}
	critLevel := (r.kind == rowHost && r.state == checkmk.HostDown) ||
		(r.kind == rowService && r.state == checkmk.SvcCrit)
	if !critLevel {
		return false
	}
	a := time.Since(r.since)
	return a >= hotMinAge && a <= hotMaxAge
}

// severity orders problems: the smaller, the more urgent.
// Unhandled problems always outrank handled ones.
func (r row) severity() int {
	var s int
	if r.kind == rowHost {
		switch r.state {
		case checkmk.HostDown:
			s = 0
		case checkmk.HostUnreachable:
			s = 1
		default:
			s = 6
		}
	} else {
		switch r.state {
		case checkmk.SvcCrit:
			s = 2
		case checkmk.SvcUnknown:
			s = 3
		case checkmk.SvcWarn:
			s = 4
		default:
			s = 6
		}
	}
	if r.handled() {
		s += 10
	}
	return s
}

func (r row) stateName() string {
	if r.kind == rowHost {
		return hostStateNames[r.state]
	}
	return svcStateNames[r.state]
}

func (r row) stateStyled() string { return r.stateStyledBG(nil) }

// stateStyledBG renders the state cell, optionally over a background
// (for a selected row). The hot badge keeps its own red background so it
// still stands out even when the row is highlighted.
func (r row) stateStyledBG(bg lipgloss.TerminalColor) string {
	label := fmt.Sprintf("%-5s", r.stateName())
	if r.hot() {
		return styleHotBadge.Render(label)
	}
	st := stateStyles[r.state]
	if r.kind == rowHost {
		st = hostStateStyle(r.state)
	}
	if bg != nil {
		st = st.Background(bg)
	}
	return st.Render(label)
}

// The state name leads the search target so queries like "crit nfs" or
// "down web" can filter on state and text at once.
// matchesClass reports whether the row belongs to the given service-state
// class (a hard filter, unlike fuzzy search). Host states map onto the
// classes: DOWN is crit-class, UNREACHABLE unknown-class, UP ok-class.
func (r row) matchesClass(class int) bool {
	if class < 0 {
		return true
	}
	if r.kind == rowService {
		return r.state == class
	}
	switch class {
	case checkmk.SvcOK:
		return r.state == checkmk.HostUp
	case checkmk.SvcCrit:
		return r.state == checkmk.HostDown
	case checkmk.SvcUnknown:
		return r.state == checkmk.HostUnreachable
	}
	return false
}

// fmtDur renders a duration compactly: 15m0s → 15m, 4h0m0s → 4h.
func fmtDur(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

func hostRow(h checkmk.Host) row {
	return row{
		kind:     rowHost,
		host:     h.Name,
		state:    h.State,
		acked:    h.Acknowledged,
		downtime: h.InDowntime,
		output:   h.PluginOutput,
		since:    h.LastStateChange,
		search:   strings.ToLower(hostStateNames[h.State] + " " + h.Name + " " + h.PluginOutput),
	}
}

func serviceRow(s checkmk.Service) row {
	return row{
		kind:     rowService,
		host:     s.HostName,
		desc:     s.Description,
		state:    s.State,
		acked:    s.Acknowledged,
		downtime: s.InDowntime,
		output:   s.PluginOutput,
		since:    s.LastStateChange,
		search:   strings.ToLower(svcStateNames[s.State] + " " + s.HostName + " " + s.Description + " " + s.PluginOutput),
	}
}

// sortProblems puts the most urgent problems first: severity, then the
// hot window (15min–4h crits), then most recent.
func sortProblems(rows []row) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].severity() != rows[j].severity() {
			return rows[i].severity() < rows[j].severity()
		}
		if hi, hj := rows[i].hot(), rows[j].hot(); hi != hj {
			return hi
		}
		return rows[i].since.After(rows[j].since)
	})
}

func sortByName(rows []row) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].host != rows[j].host {
			return rows[i].host < rows[j].host
		}
		return rows[i].desc < rows[j].desc
	})
}

// rowSource adapts []row for fzf-style fuzzy matching.
type rowSource []row

func (s rowSource) String(i int) string { return s[i].search }
func (s rowSource) Len() int            { return len(s) }

// fuzzyFilter returns the rows matching query, best match first.
//
// Like fzf, a term prefixed with "!" is negated: rows whose search text
// contains that term (substring, case-insensitive) are excluded. The
// remaining plain terms are fuzzy-matched. "nfs !ssd" fuzzy-matches nfs
// and drops anything mentioning ssd; a query of only negations keeps the
// list in its natural order.
func fuzzyFilter(rows []row, query string) []row {
	if strings.TrimSpace(query) == "" {
		return rows
	}

	var excludes []string
	var positive []string
	for _, tok := range strings.Fields(query) {
		if len(tok) > 1 && tok[0] == '!' {
			excludes = append(excludes, strings.ToLower(tok[1:]))
		} else if tok != "!" {
			positive = append(positive, tok)
		}
	}

	kept := rows
	if len(excludes) > 0 {
		kept = kept[:0:0] // fresh slice; never alias the caller's backing array
		for _, r := range rows {
			if !containsAny(r.search, excludes) {
				kept = append(kept, r)
			}
		}
	}

	if len(positive) == 0 {
		return kept // negations only: keep natural order
	}
	matches := fuzzy.FindFrom(strings.ToLower(strings.Join(positive, " ")), rowSource(kept))
	out := make([]row, 0, len(matches))
	for _, m := range matches {
		out = append(out, kept[m.Index])
	}
	return out
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// age renders a compact duration like "45s", "12m", "3h", "5d".
func age(t time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	}
}

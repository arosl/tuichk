package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

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

func (r row) stateStyled() string {
	if r.kind == rowHost {
		return hostStateStyle(r.state).Render(fmt.Sprintf("%-5s", r.stateName()))
	}
	return stateStyles[r.state].Render(fmt.Sprintf("%-5s", r.stateName()))
}

// The state name leads the search target so queries like "crit nfs" or
// "down web" can filter on state and text at once.
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

// sortProblems puts the most urgent, most recent problems first.
func sortProblems(rows []row) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].severity() != rows[j].severity() {
			return rows[i].severity() < rows[j].severity()
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
func fuzzyFilter(rows []row, query string) []row {
	if strings.TrimSpace(query) == "" {
		return rows
	}
	matches := fuzzy.FindFrom(strings.ToLower(query), rowSource(rows))
	out := make([]row, 0, len(matches))
	for _, m := range matches {
		out = append(out, rows[m.Index])
	}
	return out
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

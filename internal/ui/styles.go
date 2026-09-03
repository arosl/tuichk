package ui

import "github.com/charmbracelet/lipgloss"

var (
	colOK      = lipgloss.AdaptiveColor{Light: "28", Dark: "42"}
	colWarn    = lipgloss.AdaptiveColor{Light: "130", Dark: "214"}
	colCrit    = lipgloss.AdaptiveColor{Light: "160", Dark: "196"}
	colUnknown = lipgloss.AdaptiveColor{Light: "133", Dark: "141"}
	colDim     = lipgloss.AdaptiveColor{Light: "245", Dark: "243"}
	colAccent  = lipgloss.AdaptiveColor{Light: "26", Dark: "39"}

	styleTitle = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	styleDim   = lipgloss.NewStyle().Foreground(colDim)
	styleErr   = lipgloss.NewStyle().Foreground(colCrit).Bold(true)

	styleTabActive = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "231", Dark: "16"}).
			Background(colAccent).Padding(0, 1)
	styleTabInactive = lipgloss.NewStyle().Foreground(colDim).Padding(0, 1)

	// The current row's background: a distinctly blue-tinted band (tied
	// to the accent bar in front of it) so it lifts clearly off the
	// plain rows. Applied per-cell so it fills the whole line, not just
	// the segments between the cells' own reset codes.
	selBg = lipgloss.AdaptiveColor{Light: "153", Dark: "24"}

	// A bright accent bar drawn in the first column of the current row.
	styleCursorBar = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	styleHeaderRow = lipgloss.NewStyle().Foreground(colDim).Underline(true)

	// Inverted badge for problems in the actionable "hot window".
	styleHotBadge = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.AdaptiveColor{Light: "231", Dark: "231"}).
			Background(colCrit)

	styleDetailBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colAccent).Padding(0, 1)

	stateStyles = map[int]lipgloss.Style{
		0: lipgloss.NewStyle().Foreground(colOK).Bold(true),
		1: lipgloss.NewStyle().Foreground(colWarn).Bold(true),
		2: lipgloss.NewStyle().Foreground(colCrit).Bold(true),
		3: lipgloss.NewStyle().Foreground(colUnknown).Bold(true),
	}
)

var svcStateNames = map[int]string{0: "OK", 1: "WARN", 2: "CRIT", 3: "UNKN"}
var hostStateNames = map[int]string{0: "UP", 1: "DOWN", 2: "UNREA"}

// hostStateStyle maps host states onto the service color scale
// (UP→green, DOWN→red, UNREACHABLE→purple).
func hostStateStyle(state int) lipgloss.Style {
	switch state {
	case 1:
		return stateStyles[2]
	case 2:
		return stateStyles[3]
	default:
		return stateStyles[0]
	}
}

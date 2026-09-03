package ui

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"tuichk/internal/checkmk"
)

// Braille cells pack 2x4 dots each, giving a plotting resolution of
// (2*width) x (4*height) inside a width x height character block —
// plain text, so it renders anywhere (zellij included).
const (
	chartRows   = 7
	brailleBase = 0x2800
)

// dot bit positions within a braille cell, indexed [x][y].
var brailleDots = [2][4]rune{
	{0x01, 0x02, 0x04, 0x40},
	{0x08, 0x10, 0x20, 0x80},
}

// renderGraph draws one GUI graph as a colored braille chart with a
// legend, fitting the given character width.
func renderGraph(g checkmk.Graph, width int) string {
	if width < 20 {
		width = 20
	}
	cols := width - 2

	lo, hi := math.Inf(1), math.Inf(-1)
	for _, c := range g.Curve {
		for _, v := range c.Values {
			if !math.IsNaN(v) {
				lo, hi = math.Min(lo, v), math.Max(hi, v)
			}
		}
	}
	if math.IsInf(lo, 1) {
		return styleDim.Render("  (no data)")
	}
	if lo > 0 {
		lo = 0 // anchor positive-only graphs at zero, like the GUI
	}
	if hi == lo {
		hi = lo + 1
	}

	w2, h4 := cols*2, chartRows*4
	cells := make([]rune, cols*chartRows)
	owner := make([]int, cols*chartRows) // curve index per cell, -1 none
	for i := range owner {
		owner[i] = -1
	}
	set := func(x, y, curve int) {
		cx, cy := x/2, y/4
		idx := cy*cols + cx
		cells[idx] |= brailleDots[x%2][y%4]
		owner[idx] = curve
	}

	for ci, c := range g.Curve {
		n := len(c.Values)
		if n == 0 {
			continue
		}
		prevY := -1
		for x := 0; x < w2; x++ {
			v := c.Values[x*(n-1)/max(w2-1, 1)]
			if math.IsNaN(v) {
				prevY = -1
				continue
			}
			y := h4 - 1 - int(float64(h4-1)*(v-lo)/(hi-lo)+0.5)
			set(x, y, ci)
			if prevY >= 0 { // connect vertical jumps for a solid line
				for yy := min(prevY, y); yy <= max(prevY, y); yy++ {
					set(x, yy, ci)
				}
			}
			prevY = y
		}
	}

	styles := make([]lipgloss.Style, len(g.Curve))
	for i, c := range g.Curve {
		styles[i] = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Color))
	}

	var b strings.Builder
	b.WriteString("  " + styleTitle.Render(g.Title) + "\n")
	for row := 0; row < chartRows; row++ {
		b.WriteString("  ")
		for col := 0; col < cols; col++ {
			idx := row*cols + col
			if cells[idx] == 0 {
				b.WriteByte(' ')
				continue
			}
			r := string(brailleBase + cells[idx])
			if o := owner[idx]; o >= 0 {
				r = styles[o].Render(r)
			}
			b.WriteString(r)
		}
		b.WriteString("\n")
	}

	// time axis: start left, end right
	from := time.Unix(g.Start, 0).Format("15:04")
	to := time.Unix(g.End, 0).Format("15:04")
	gap := cols - len(from) - len(to)
	if gap < 1 {
		gap = 1
	}
	b.WriteString("  " + styleDim.Render(from+strings.Repeat(" ", gap)+to) + "\n")

	for i, c := range g.Curve {
		line := "  " + styles[i].Render("▬") + " " + c.Title
		if c.Last != "" {
			line += styleDim.Render(fmt.Sprintf("  %s", c.Last))
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

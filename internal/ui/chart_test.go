package ui

import (
	"math"
	"strings"
	"testing"

	"tuichk/internal/checkmk"
)

func TestRenderGraph(t *testing.T) {
	g := checkmk.Graph{
		Title: "CPU load",
		Start: 1700000000, End: 1700014400, Step: 120,
		Curve: []checkmk.Curve{{
			Title: "load1", Color: "#00ffc6", Last: "0.42",
			Values: []float64{0, 1, 2, 3, 4, 5, 4, 3, 2, 1, 0, math.NaN(), 2},
		}},
	}
	out := renderGraph(g, 60)
	if !strings.Contains(out, "CPU load") || !strings.Contains(out, "load1") ||
		!strings.Contains(out, "0.42") {
		t.Errorf("missing title/legend:\n%s", out)
	}
	braille := 0
	for _, r := range out {
		if r >= 0x2800 && r <= 0x28FF {
			braille++
		}
	}
	if braille == 0 {
		t.Error("chart contains no braille cells")
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// title + chartRows + time axis + 1 legend line
	if want := 1 + chartRows + 1 + 1; len(lines) != want {
		t.Errorf("want %d lines, got %d", want, len(lines))
	}
}

func TestRenderGraphNoData(t *testing.T) {
	g := checkmk.Graph{Title: "empty", Curve: []checkmk.Curve{{
		Title: "x", Values: []float64{math.NaN(), math.NaN()},
	}}}
	if out := renderGraph(g, 60); !strings.Contains(out, "no data") {
		t.Errorf("all-NaN graph should say no data, got:\n%s", out)
	}
}

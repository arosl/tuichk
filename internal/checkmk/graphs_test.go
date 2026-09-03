package checkmk

import (
	"math"
	"testing"
)

// A trimmed-down popup response in the exact shape the GUI emits.
const samplePopup = `<script type="text/javascript">
cmk.graphs.create_graph("<div class=\"graph\">escaped \" quote and {braces}<\/div>", {"title": "Filesystem size and used space", "width": 30, "height": 10, "start_time": 1700000000, "end_time": 1700014400, "step": 120, "curves": [{"color": "#00ffc6", "title": "Used space", "scalars": {"last": [123.0, "123 MB"], "pin": [null, "n/a"]}, "points": [[null, null], [0.0, 100.0], [0.0, 110.0], [0.0, 123.0]]}, {"color": "#1e90ff", "title": "Free space", "scalars": {"last": [7.0, "7 MB"]}, "points": [[null, 30.0], [null, 20.0], [null, 10.0], [null, 7.0]]}]}, {"foo": 1});
cmk.graphs.create_graph("<div><\/div>", {"title": "Growth", "start_time": 1700000000, "end_time": 1700014400, "step": 120, "curves": [{"color": "#ff0000", "title": "Growth", "scalars": {}, "points": [[0.0, 1.0]]}]}, {});
</script>`

func TestParseGraphPopup(t *testing.T) {
	graphs, err := parseGraphPopup(samplePopup)
	if err != nil {
		t.Fatal(err)
	}
	if len(graphs) != 2 {
		t.Fatalf("want 2 graphs, got %d", len(graphs))
	}
	g := graphs[0]
	if g.Title != "Filesystem size and used space" || g.Step != 120 ||
		g.Start != 1700000000 || g.End != 1700014400 {
		t.Errorf("bad graph header: %+v", g)
	}
	if len(g.Curve) != 2 {
		t.Fatalf("want 2 curves, got %d", len(g.Curve))
	}
	c := g.Curve[0]
	if c.Title != "Used space" || c.Color != "#00ffc6" || c.Last != "123 MB" {
		t.Errorf("bad curve: %+v", c)
	}
	if len(c.Values) != 4 || !math.IsNaN(c.Values[0]) || c.Values[3] != 123.0 {
		t.Errorf("bad values: %v", c.Values)
	}
	// second curve uses [null, value] points
	if got := g.Curve[1].Values[3]; got != 7.0 {
		t.Errorf("want 7.0 from [null, v] point, got %v", got)
	}
	if graphs[1].Title != "Growth" {
		t.Errorf("second graph mis-parsed: %+v", graphs[1])
	}
}

func TestParsePopupNoGraphs(t *testing.T) {
	graphs, err := parseGraphPopup("")
	if err != nil || len(graphs) != 0 {
		t.Errorf("empty body should parse to no graphs, got %v / %v", graphs, err)
	}
}

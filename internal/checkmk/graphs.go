package checkmk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// Graph is one graph definition exactly as the web GUI renders it,
// obtained from host_service_graph_popup.py — the same endpoint the
// GUI's hover popups use, authenticated with the user's own session.
type Graph struct {
	Title string
	Start int64
	End   int64
	Step  int
	Curve []Curve
}

// Curve is one colored series within a graph.
type Curve struct {
	Title  string
	Color  string    // "#rrggbb" as sent by the GUI
	Last   string    // rendered last value, e.g. "13.61 TB"
	Values []float64 // NaN = gap in the data
}

// HostGraphService is the pseudo-service holding a host's own graphs.
const HostGraphService = "_HOST_"

var errNeedLogin = fmt.Errorf("no GUI session")

// guiLogin performs the same form login a browser does. The session
// cookie lands in the client's cookie jar.
func (c *Client) guiLogin(ctx context.Context) error {
	form := url.Values{
		"_login":      {"1"},
		"_username":   {c.user},
		"_password":   {c.secret},
		"_origtarget": {"index.py"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/check_mk/login.py", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	for _, ck := range c.hc.Jar.Cookies(req.URL) {
		if strings.HasPrefix(ck.Name, "auth_") {
			return nil
		}
	}
	return fmt.Errorf("GUI login as %s failed (wrong credentials?)", c.user)
}

var loginMu sync.Mutex

// ServiceGraphs returns the graphs the web GUI would show for the
// service (use HostGraphService for a host's own graphs). Logs in on
// first use and transparently re-logs-in when the session expires.
func (c *Client) ServiceGraphs(ctx context.Context, host, service string) ([]Graph, error) {
	body, err := c.graphPopup(ctx, host, service)
	if err != nil {
		return nil, err
	}
	if !strings.Contains(body, "create_graph(") {
		if !looksLoggedOut(body) {
			return nil, nil // valid session, service simply has no graphs
		}
		loginMu.Lock()
		err := c.guiLogin(ctx)
		loginMu.Unlock()
		if err != nil {
			return nil, err
		}
		if body, err = c.graphPopup(ctx, host, service); err != nil {
			return nil, err
		}
		if !strings.Contains(body, "create_graph(") {
			if looksLoggedOut(body) {
				return nil, errNeedLogin
			}
			return nil, nil
		}
	}
	return parseGraphPopup(body)
}

func (c *Client) graphPopup(ctx context.Context, host, service string) (string, error) {
	q := url.Values{"host_name": {host}, "service": {service}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/check_mk/host_service_graph_popup.py?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return string(b), err
}

// looksLoggedOut detects the login page a session-less request lands on.
func looksLoggedOut(body string) bool {
	return strings.Contains(body, "_password") || strings.Contains(body, "login.py")
}

// popupGraph mirrors the JSON literal embedded in the popup's
// cmk.graphs.create_graph(...) calls.
type popupGraph struct {
	Title  string `json:"title"`
	Start  int64  `json:"start_time"`
	End    int64  `json:"end_time"`
	Step   int    `json:"step"`
	Curves []struct {
		Title   string           `json:"title"`
		Color   string           `json:"color"`
		Points  [][]*float64     `json:"points"`
		Scalars map[string][]any `json:"scalars"`
	} `json:"curves"`
}

// parseGraphPopup extracts every create_graph(html, {json}, ...) call.
func parseGraphPopup(body string) ([]Graph, error) {
	var graphs []Graph
	const marker = "cmk.graphs.create_graph("
	for pos := 0; ; {
		i := strings.Index(body[pos:], marker)
		if i < 0 {
			break
		}
		pos += i + len(marker)
		afterStr, ok := skipJSString(body[pos:])
		if !ok {
			return nil, fmt.Errorf("graph popup: malformed html argument")
		}
		rest := strings.TrimLeft(afterStr, ", \n\t\r")
		obj, ok := balancedObject(rest)
		if !ok {
			return nil, fmt.Errorf("graph popup: malformed graph object")
		}
		var pg popupGraph
		if err := json.Unmarshal([]byte(obj), &pg); err != nil {
			return nil, fmt.Errorf("graph popup: %w", err)
		}
		graphs = append(graphs, pg.toGraph())
		pos = len(body) - len(rest) + len(obj)
	}
	return graphs, nil
}

func (pg popupGraph) toGraph() Graph {
	g := Graph{Title: pg.Title, Start: pg.Start, End: pg.End, Step: pg.Step}
	for _, c := range pg.Curves {
		curve := Curve{Title: c.Title, Color: c.Color}
		if last, ok := c.Scalars["last"]; ok && len(last) == 2 {
			if s, ok := last[1].(string); ok {
				curve.Last = s
			}
		}
		curve.Values = make([]float64, len(c.Points))
		for i, p := range c.Points {
			v := math.NaN()
			// points are [base, value] pairs; the value is what to plot
			if len(p) > 1 && p[1] != nil {
				v = *p[1]
			} else if len(p) > 0 && p[0] != nil {
				v = *p[0]
			}
			curve.Values[i] = v
		}
		g.Curve = append(g.Curve, curve)
	}
	return g
}

// skipJSString consumes a double-quoted JS string (with escapes) at the
// start of s and returns what follows it.
func skipJSString(s string) (string, bool) {
	s = strings.TrimLeft(s, " \n\t\r")
	if len(s) == 0 || s[0] != '"' {
		return "", false
	}
	esc := false
	for i := 1; i < len(s); i++ {
		switch {
		case esc:
			esc = false
		case s[i] == '\\':
			esc = true
		case s[i] == '"':
			return s[i+1:], true
		}
	}
	return "", false
}

// balancedObject returns the {...} JSON object s starts with,
// respecting braces inside strings.
func balancedObject(s string) (string, bool) {
	if len(s) == 0 || s[0] != '{' {
		return "", false
	}
	depth, inStr, esc := 0, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inStr:
			if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[:i+1], true
			}
		}
	}
	return "", false
}

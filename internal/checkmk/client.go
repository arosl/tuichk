// Package checkmk is a client for the CheckMK REST API (v1.0).
// It talks to the same HTTP interface the web dashboard uses and only
// needs a monitoring user — no SSH or site-local access.
package checkmk

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

// Host state values as reported by the monitoring core.
const (
	HostUp = iota
	HostDown
	HostUnreachable
)

// Service state values as reported by the monitoring core.
const (
	SvcOK = iota
	SvcWarn
	SvcCrit
	SvcUnknown
)

// Host is a monitored host's live status.
type Host struct {
	Name            string
	State           int
	PluginOutput    string
	Acknowledged    bool
	InDowntime      bool
	LastStateChange time.Time
	NumServices     int
	NumOK           int
	NumCrit         int
	NumWarn         int
	NumUnknown      int
}

// Service is a monitored service's live status. PluginOutput and
// LongOutput are only populated by calls that request them — the full
// service list is fetched without output to keep the payload small.
type Service struct {
	HostName        string
	Description     string
	State           int
	PluginOutput    string
	LongOutput      string
	Acknowledged    bool
	InDowntime      bool
	LastStateChange time.Time
}

// Client talks to a CheckMK site's REST API.
type Client struct {
	baseURL string // e.g. https://monitoring.example.com/mysite
	user    string
	secret  string
	hc      *http.Client
}

// New creates a client for the site at baseURL (scheme://host/site).
func New(baseURL, user, secret string, insecureTLS bool) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	// The jar holds the GUI session cookie for graph fetches; REST
	// calls authenticate per-request via the Bearer header.
	jar, _ := cookiejar.New(nil)
	return &Client{
		baseURL: baseURL,
		user:    user,
		secret:  secret,
		hc: &http.Client{
			Jar: jar,
			// Large sites take minutes to serialize the full service
			// list; callers bound individual requests via context.
			Timeout:   5 * time.Minute,
			Transport: transport,
		},
	}
}

// apiObject is one element of a REST API collection response.
type apiObject struct {
	Extensions json.RawMessage `json:"extensions"`
}

type apiCollection struct {
	Value []apiObject `json:"value"`
}

type apiError struct {
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Status int    `json:"status"`
}

// do performs an API request. Methods other than GET are accepted so the
// client can grow beyond read-only use, but nothing here issues them yet.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, out any) error {
	u := c.baseURL + "/check_mk/api/1.0/" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.user+" "+c.secret)
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ae apiError
		if json.Unmarshal(body, &ae) == nil && ae.Title != "" {
			ae.Title, ae.Detail = clean(ae.Title), clean(ae.Detail)
			if ae.Detail != "" && ae.Detail != ae.Title {
				return fmt.Errorf("checkmk API: %s (%d): %s", ae.Title, resp.StatusCode, ae.Detail)
			}
			return fmt.Errorf("checkmk API: %s (%d)", ae.Title, resp.StatusCode)
		}
		return fmt.Errorf("checkmk API: HTTP %d for %s", resp.StatusCode, path)
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("decoding %s response: %w", path, err)
		}
	}
	return nil
}

// collection fetches a livestatus-backed collection. query, if non-empty,
// is a livestatus query expression in the REST API's JSON format and is
// evaluated server-side, keeping responses small.
func (c *Client) collection(ctx context.Context, domainType string, columns []string, query string) ([]apiObject, error) {
	q := url.Values{}
	for _, col := range columns {
		q.Add("columns", col)
	}
	if query != "" {
		q.Set("query", query)
	}
	var coll apiCollection
	err := c.do(ctx, http.MethodGet, "domain-types/"+domainType+"/collections/all", q, &coll)
	if err != nil {
		return nil, err
	}
	return coll.Value, nil
}

// Hosts returns live status for all monitored hosts.
func (c *Client) Hosts(ctx context.Context) ([]Host, error) {
	objs, err := c.collection(ctx, "host", []string{
		"name", "state", "plugin_output", "acknowledged",
		"scheduled_downtime_depth", "last_state_change",
		"num_services", "num_services_ok", "num_services_crit",
		"num_services_warn", "num_services_unknown",
	}, "")
	if err != nil {
		return nil, err
	}
	hosts := make([]Host, 0, len(objs))
	for _, o := range objs {
		var ext struct {
			Name                   string `json:"name"`
			State                  int    `json:"state"`
			PluginOutput           string `json:"plugin_output"`
			Acknowledged           int    `json:"acknowledged"`
			ScheduledDowntimeDepth int    `json:"scheduled_downtime_depth"`
			LastStateChange        int64  `json:"last_state_change"`
			NumServices            int    `json:"num_services"`
			NumServicesOK          int    `json:"num_services_ok"`
			NumServicesCrit        int    `json:"num_services_crit"`
			NumServicesWarn        int    `json:"num_services_warn"`
			NumServicesUnknown     int    `json:"num_services_unknown"`
		}
		if err := json.Unmarshal(o.Extensions, &ext); err != nil {
			return nil, fmt.Errorf("decoding host: %w", err)
		}
		hosts = append(hosts, Host{
			Name:            clean(ext.Name),
			State:           ext.State,
			PluginOutput:    clean(ext.PluginOutput),
			Acknowledged:    ext.Acknowledged != 0,
			InDowntime:      ext.ScheduledDowntimeDepth > 0,
			LastStateChange: time.Unix(ext.LastStateChange, 0),
			NumServices:     ext.NumServices,
			NumOK:           ext.NumServicesOK,
			NumCrit:         ext.NumServicesCrit,
			NumWarn:         ext.NumServicesWarn,
			NumUnknown:      ext.NumServicesUnknown,
		})
	}
	return hosts, nil
}

var serviceBaseColumns = []string{
	"host_name", "description", "state",
	"acknowledged", "scheduled_downtime_depth", "last_state_change",
}

func (c *Client) services(ctx context.Context, columns []string, query string) ([]Service, error) {
	objs, err := c.collection(ctx, "service", columns, query)
	if err != nil {
		return nil, err
	}
	services := make([]Service, 0, len(objs))
	for _, o := range objs {
		var ext struct {
			HostName               string `json:"host_name"`
			Description            string `json:"description"`
			State                  int    `json:"state"`
			PluginOutput           string `json:"plugin_output"`
			LongPluginOutput       string `json:"long_plugin_output"`
			Acknowledged           int    `json:"acknowledged"`
			ScheduledDowntimeDepth int    `json:"scheduled_downtime_depth"`
			LastStateChange        int64  `json:"last_state_change"`
		}
		if err := json.Unmarshal(o.Extensions, &ext); err != nil {
			return nil, fmt.Errorf("decoding service: %w", err)
		}
		services = append(services, Service{
			HostName:        clean(ext.HostName),
			Description:     clean(ext.Description),
			State:           ext.State,
			PluginOutput:    clean(ext.PluginOutput),
			LongOutput:      clean(ext.LongPluginOutput),
			Acknowledged:    ext.Acknowledged != 0,
			InDowntime:      ext.ScheduledDowntimeDepth > 0,
			LastStateChange: time.Unix(ext.LastStateChange, 0),
		})
	}
	return services, nil
}

// ProblemServices returns only services in a non-OK state, with output.
// The state filter runs server-side, so this stays fast and cheap even
// on sites with tens of thousands of services.
func (c *Client) ProblemServices(ctx context.Context) ([]Service, error) {
	return c.services(ctx,
		append([]string{"plugin_output"}, serviceBaseColumns...),
		`{"op": "!=", "left": "state", "right": "0"}`)
}

// AllServices returns every monitored service WITHOUT plugin output —
// on large sites the output column dominates the payload. Use sparingly;
// this is the one expensive call the client makes.
func (c *Client) AllServices(ctx context.Context) ([]Service, error) {
	return c.services(ctx, serviceBaseColumns, "")
}

// HostServices returns all services of one host, with output — a small
// server-side-filtered query, cheap even on large sites.
func (c *Client) HostServices(ctx context.Context, host string) ([]Service, error) {
	filter, err := json.Marshal(map[string]string{
		"op": "=", "left": "host_name", "right": host,
	})
	if err != nil {
		return nil, err
	}
	return c.services(ctx,
		append([]string{"plugin_output"}, serviceBaseColumns...), string(filter))
}

// ServiceDetail fetches one service's live status including full output.
func (c *Client) ServiceDetail(ctx context.Context, host, description string) (*Service, error) {
	filter, err := json.Marshal(map[string]any{
		"op": "and",
		"expr": []map[string]string{
			{"op": "=", "left": "host_name", "right": host},
			{"op": "=", "left": "description", "right": description},
		},
	})
	if err != nil {
		return nil, err
	}
	svcs, err := c.services(ctx,
		append([]string{"plugin_output", "long_plugin_output"}, serviceBaseColumns...),
		string(filter))
	if err != nil {
		return nil, err
	}
	if len(svcs) == 0 {
		return nil, fmt.Errorf("service %s/%s not found", host, description)
	}
	return &svcs[0], nil
}

package checkmk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServicesParsesCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer automation secret" {
			t.Errorf("bad auth header: %q", got)
		}
		if r.URL.Path != "/mysite/check_mk/api/1.0/domain-types/service/collections/all" {
			t.Errorf("bad path: %s", r.URL.Path)
		}
		if cols := r.URL.Query()["columns"]; len(cols) == 0 {
			t.Error("no columns requested")
		}
		if q := r.URL.Query().Get("query"); !strings.Contains(q, `"state"`) {
			t.Errorf("problem fetch should filter state server-side, got query=%q", q)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"value":[{"extensions":{
			"host_name":"web01","description":"HTTPS cert","state":2,
			"plugin_output":"CRIT - expires in 3 days","acknowledged":1,
			"scheduled_downtime_depth":0,"last_state_change":1700000000}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/mysite", "automation", "secret", false)
	svcs, err := c.ProblemServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(svcs) != 1 {
		t.Fatalf("want 1 service, got %d", len(svcs))
	}
	s := svcs[0]
	if s.HostName != "web01" || s.Description != "HTTPS cert" || s.State != SvcCrit {
		t.Errorf("unexpected service: %+v", s)
	}
	if !s.Acknowledged || s.InDowntime {
		t.Errorf("unexpected flags: %+v", s)
	}
}

func TestHostsParsesCollection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mysite/check_mk/api/1.0/domain-types/host/collections/all" {
			t.Errorf("bad path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"value":[{"extensions":{
			"name":"db02","state":1,"plugin_output":"Ping timed out",
			"acknowledged":0,"scheduled_downtime_depth":1,"last_state_change":1700000000,
			"num_services":12,"num_services_crit":3,"num_services_warn":1,"num_services_unknown":0}}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL+"/mysite", "automation", "secret", false)
	hosts, err := c.Hosts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 {
		t.Fatalf("want 1 host, got %d", len(hosts))
	}
	h := hosts[0]
	if h.Name != "db02" || h.State != HostDown || h.NumCrit != 3 || !h.InDowntime {
		t.Errorf("unexpected host: %+v", h)
	}
}

func TestAPIErrorSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"title":"Unauthorized","status":401,"detail":"wrong \u001b]0;pwned\u0007credentials"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "automation", "bad", false)
	_, err := c.ProblemServices(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if want := "Unauthorized"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q missing %q", err, want)
	}
	if strings.ContainsAny(err.Error(), "\x1b\x07") {
		t.Errorf("error %q leaks control characters from the server", err)
	}
}

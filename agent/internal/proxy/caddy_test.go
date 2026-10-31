package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func TestBuildCaddyConfigFromRoutes(t *testing.T) {
	data, err := buildCaddyConfig([]*agentv1.ProxyRoute{
		{Hostname: "B.Example.com", UpstreamService: "web", UpstreamPort: 8080},
		{Hostname: "a.example.com", UpstreamService: "api", UpstreamPort: 3000},
	})
	if err != nil {
		t.Fatalf("buildCaddyConfig: %v", err)
	}

	var config caddyConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("decode the config: %v", err)
	}
	if config.Admin.Listen != adminListen {
		t.Errorf("admin listen = %q, want %q", config.Admin.Listen, adminListen)
	}
	server, ok := config.Apps.HTTP.Servers["talaia"]
	if !ok {
		t.Fatal("the talaia server is missing")
	}
	if len(server.Listen) != 1 || server.Listen[0] != ":443" {
		t.Errorf("listen = %v", server.Listen)
	}
	if len(server.Routes) != 2 {
		t.Fatalf("routes = %d, want 2", len(server.Routes))
	}
	if got := server.Routes[0].Match[0].Host[0]; got != "a.example.com" {
		t.Errorf("first host = %q", got)
	}
	if got := server.Routes[0].Handle[0].Upstreams[0].Dial; got != "api:3000" {
		t.Errorf("first upstream = %q", got)
	}
	if got := server.Routes[1].Match[0].Host[0]; got != "b.example.com" {
		t.Errorf("second host = %q", got)
	}
	if got := server.Routes[1].Handle[0].Upstreams[0].Dial; got != "web:8080" {
		t.Errorf("second upstream = %q", got)
	}
	if len(server.TLSConnectionPolicies) != 1 {
		t.Errorf("TLS policies = %v", server.TLSConnectionPolicies)
	}
}

func TestBuildCaddyConfigWithoutRoutes(t *testing.T) {
	data, err := buildCaddyConfig(nil)
	if err != nil {
		t.Fatalf("buildCaddyConfig: %v", err)
	}
	var config caddyConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Apps.HTTP.Servers) != 0 {
		t.Errorf("servers = %v, want none", config.Apps.HTTP.Servers)
	}
}

func TestBuildCaddyConfigRefusesInvalidRoutes(t *testing.T) {
	cases := []struct {
		name  string
		route *agentv1.ProxyRoute
	}{
		{name: "wildcard", route: &agentv1.ProxyRoute{Hostname: "*.example.com", UpstreamService: "app", UpstreamPort: 3000}},
		{name: "empty host", route: &agentv1.ProxyRoute{UpstreamService: "app", UpstreamPort: 3000}},
		{name: "host with space", route: &agentv1.ProxyRoute{Hostname: "bad host", UpstreamService: "app", UpstreamPort: 3000}},
		{name: "host with slash", route: &agentv1.ProxyRoute{Hostname: "a/b", UpstreamService: "app", UpstreamPort: 3000}},
		{name: "empty service", route: &agentv1.ProxyRoute{Hostname: "a.example.com", UpstreamPort: 3000}},
		{name: "service with colon", route: &agentv1.ProxyRoute{Hostname: "a.example.com", UpstreamService: "app:80", UpstreamPort: 3000}},
		{name: "service with slash", route: &agentv1.ProxyRoute{Hostname: "a.example.com", UpstreamService: "../app", UpstreamPort: 3000}},
		{name: "port zero", route: &agentv1.ProxyRoute{Hostname: "a.example.com", UpstreamService: "app"}},
		{name: "port too big", route: &agentv1.ProxyRoute{Hostname: "a.example.com", UpstreamService: "app", UpstreamPort: 70000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var routes []*agentv1.ProxyRoute
			if tc.route != nil {
				routes = []*agentv1.ProxyRoute{tc.route}
			}
			if _, err := buildCaddyConfig(routes); err == nil {
				t.Fatal("buildCaddyConfig accepted an invalid route")
			}
		})
	}
}

func TestBuildCaddyConfigRefusesANilRoute(t *testing.T) {
	if _, err := buildCaddyConfig([]*agentv1.ProxyRoute{nil}); err == nil {
		t.Fatal("buildCaddyConfig accepted a nil route")
	}
}

func TestBuildCaddyConfigRefusesDuplicateHosts(t *testing.T) {
	_, err := buildCaddyConfig([]*agentv1.ProxyRoute{
		{Hostname: "same.example.com", UpstreamService: "a", UpstreamPort: 3000},
		{Hostname: "SAME.example.com", UpstreamService: "b", UpstreamPort: 3000},
	})
	if err == nil {
		t.Fatal("buildCaddyConfig accepted a duplicate host")
	}
}

func TestUnixAdminLoadsOverHTTP(t *testing.T) {
	var method, path, contentType string
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, contentType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	admin := NewUnixAdminWithClient(server.URL, server.Client())
	config := []byte(`{"admin":{"listen":"unix//run/caddy/admin.sock"}}`)
	if err := admin.Load(context.Background(), config); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if method != http.MethodPost || path != "/load" {
		t.Errorf("request = %s %s", method, path)
	}
	if contentType != "application/json" {
		t.Errorf("content type = %q", contentType)
	}
	if string(body) != string(config) {
		t.Errorf("body = %q", body)
	}
}

func TestUnixAdminReportsARefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad config", http.StatusBadRequest)
	}))
	defer server.Close()

	err := NewUnixAdminWithClient(server.URL, server.Client()).Load(context.Background(), []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "bad config") {
		t.Fatalf("error = %v", err)
	}
}

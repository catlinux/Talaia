package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	// CaddyVersion is the Caddy release Talaia runs in managed mode.
	CaddyVersion = "2.10.2"
	// CaddyImage is pinned by digest: Talaia never runs a moving tag.
	CaddyImage = "caddy:" + CaddyVersion + "@sha256:c3d7ee5d2b11f9dc54f947f68a734c84e9c9666c92c88a7f30b9cba5da182adb"

	adminSocketFile = "admin.sock"
	adminListen     = "unix//run/caddy/admin.sock"
	bootstrapFile   = "bootstrap.json"

	maxAdminResponse = 4 << 10
)

// Admin loads a whole Caddy configuration through its administration API.
type Admin interface {
	Load(ctx context.Context, config []byte) error
}

// UnixAdmin talks to the Caddy admin API through a Unix socket.
type UnixAdmin struct {
	baseURL string
	client  *http.Client
}

// NewUnixAdmin returns an admin client for socket. It never opens a TCP
// listener.
func NewUnixAdmin(socket string) *UnixAdmin {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		},
	}
	return NewUnixAdminWithClient("http://caddy", &http.Client{Transport: transport, Timeout: time.Minute})
}

// NewUnixAdminWithClient returns an admin client that talks to baseURL with
// httpClient. Tests use it with an httptest server, because Windows has no Unix
// sockets.
func NewUnixAdminWithClient(baseURL string, client *http.Client) *UnixAdmin {
	return &UnixAdmin{baseURL: baseURL, client: client}
}

// Load replaces the whole configuration of Caddy.
func (a *UnixAdmin) Load(ctx context.Context, config []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/load", bytes.NewReader(config))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := a.client.Do(request)
	if err != nil {
		return fmt.Errorf("reach the Caddy admin API: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, maxAdminResponse))
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			return fmt.Errorf("the Caddy admin API answered %s", response.Status)
		}
		return fmt.Errorf("the Caddy admin API answered %s: %s", response.Status, detail)
	}
	return nil
}

type caddyConfig struct {
	Admin caddyAdmin `json:"admin"`
	Apps  caddyApps  `json:"apps"`
}

type caddyAdmin struct {
	Listen string `json:"listen"`
}

type caddyApps struct {
	HTTP caddyHTTP `json:"http"`
}

type caddyHTTP struct {
	Servers map[string]caddyServer `json:"servers"`
}

type caddyServer struct {
	Listen                []string         `json:"listen"`
	Routes                []caddyRoute     `json:"routes,omitempty"`
	TLSConnectionPolicies []map[string]any `json:"tls_connection_policies,omitempty"`
	AutomaticHTTPS        map[string]any   `json:"automatic_https"`
}

type caddyRoute struct {
	Match    []caddyMatch   `json:"match"`
	Handle   []caddyHandler `json:"handle"`
	Terminal bool           `json:"terminal"`
}

type caddyMatch struct {
	Host []string `json:"host"`
}

type caddyHandler struct {
	Handler   string          `json:"handler"`
	Upstreams []caddyUpstream `json:"upstreams"`
}

type caddyUpstream struct {
	Dial string `json:"dial"`
}

type route struct {
	hostname string
	upstream string
}

// buildCaddyConfig turns the routes of the core into the Caddy JSON the agent
// loads. The core only sends hostnames and upstreams, never JSON.
func buildCaddyConfig(routes []*agentv1.ProxyRoute) ([]byte, error) {
	parsed, err := parseRoutes(routes)
	if err != nil {
		return nil, err
	}

	servers := map[string]caddyServer{}
	if len(parsed) > 0 {
		caddyRoutes := make([]caddyRoute, 0, len(parsed))
		for _, item := range parsed {
			caddyRoutes = append(caddyRoutes, caddyRoute{
				Match: []caddyMatch{{Host: []string{item.hostname}}},
				Handle: []caddyHandler{{
					Handler:   "reverse_proxy",
					Upstreams: []caddyUpstream{{Dial: item.upstream}},
				}},
				Terminal: true,
			})
		}
		servers["talaia"] = caddyServer{
			Listen:                []string{":443"},
			Routes:                caddyRoutes,
			TLSConnectionPolicies: []map[string]any{{}},
			AutomaticHTTPS:        map[string]any{},
		}
	}

	config := caddyConfig{
		Admin: caddyAdmin{Listen: adminListen},
		Apps:  caddyApps{HTTP: caddyHTTP{Servers: servers}},
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode the Caddy configuration: %w", err)
	}
	return append(data, '\n'), nil
}

func parseRoutes(routes []*agentv1.ProxyRoute) ([]route, error) {
	parsed := make([]route, 0, len(routes))
	seen := make(map[string]bool, len(routes))
	for _, item := range routes {
		if item == nil {
			return nil, errors.New("invalid proxy route")
		}
		hostname := strings.ToLower(strings.TrimSpace(item.GetHostname()))
		if !validHostname(hostname) {
			return nil, fmt.Errorf("invalid hostname %q", item.GetHostname())
		}
		if seen[hostname] {
			return nil, fmt.Errorf("duplicate hostname %q", hostname)
		}
		seen[hostname] = true

		service := strings.TrimSpace(item.GetUpstreamService())
		if !validServiceName(service) {
			return nil, fmt.Errorf("invalid upstream service %q", item.GetUpstreamService())
		}
		port := item.GetUpstreamPort()
		if port == 0 || port > 65535 {
			return nil, fmt.Errorf("invalid upstream port %d", port)
		}
		parsed = append(parsed, route{
			hostname: hostname,
			upstream: service + ":" + strconv.Itoa(int(port)),
		})
	}
	sort.Slice(parsed, func(i, j int) bool { return parsed[i].hostname < parsed[j].hostname })
	return parsed, nil
}

func validHostname(hostname string) bool {
	if len(hostname) == 0 || len(hostname) > 253 || strings.Contains(hostname, "*") {
		return false
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func validServiceName(service string) bool {
	if len(service) == 0 || len(service) > 128 {
		return false
	}
	for index, char := range service {
		allowed := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.'
		if !allowed {
			return false
		}
		if index == 0 && (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

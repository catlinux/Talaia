package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordedRequest struct {
	Method string
	Path   string
	Body   string
	Header http.Header
}

type harness struct {
	t          *testing.T
	server     *httptest.Server
	app        *app
	stdout     bytes.Buffer
	stderr     bytes.Buffer
	env        map[string]string
	configPath string
	mu         sync.Mutex
	requests   []recordedRequest
}

func newHarness(t *testing.T, handler http.HandlerFunc) *harness {
	t.Helper()
	h := &harness{
		t:          t,
		env:        map[string]string{},
		configPath: filepath.Join(t.TempDir(), "talaia", "config.toml"),
	}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.requests = append(h.requests, recordedRequest{
			Method: r.Method, Path: r.URL.RequestURI(), Body: string(body), Header: r.Header.Clone(),
		})
		h.mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(h.server.Close)
	h.app = &app{
		version:      "test",
		stdin:        strings.NewReader(""),
		stdout:       &h.stdout,
		stderr:       &h.stderr,
		getenv:       func(key string) string { return h.env[key] },
		configPath:   h.configPath,
		pollInterval: 5 * time.Millisecond,
	}
	return h
}

func (h *harness) run(args ...string) int {
	h.t.Helper()
	return h.app.run(args)
}

func (h *harness) setCredentials() {
	h.env["TALAIA_URL"] = h.server.URL
	h.env["TALAIA_API_KEY"] = testKey
}

func (h *harness) resetOutput() {
	h.stdout.Reset()
	h.stderr.Reset()
}

func (h *harness) lastRequest() (recordedRequest, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.requests) == 0 {
		return recordedRequest{}, false
	}
	return h.requests[len(h.requests)-1], true
}

func (h *harness) findRequest(method, path string) (recordedRequest, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, request := range h.requests {
		if request.Method == method && request.Path == path {
			return request, true
		}
	}
	return recordedRequest{}, false
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

const testKey = "tlk_testid_thisSecretMustNeverAppear"

func assertKeyHidden(t *testing.T, values ...string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(value, testKey) || strings.Contains(value, "thisSecretMustNeverAppear") {
			t.Fatalf("output exposed the API key: %q", value)
		}
	}
}

func TestVersionAndLanguage(t *testing.T) {
	h := newHarness(t, http.NotFound)
	if code := h.run("version"); code != 0 {
		t.Fatalf("version exit = %d", code)
	}
	if got := h.stdout.String(); !strings.Contains(got, "talaiactl test") {
		t.Fatalf("version output = %q", got)
	}

	h.resetOutput()
	if code := h.run("--lang", "ca", "version"); code != 0 {
		t.Fatalf("version ca exit = %d", code)
	}
	if got := h.stdout.String(); !strings.Contains(got, "talaiactl test") {
		t.Fatalf("localized version output = %q", got)
	}

	h.resetOutput()
	if code := h.run("--lang", "fr", "version"); code != 0 {
		t.Fatalf("unknown language exit = %d", code)
	}
	if !strings.Contains(h.stderr.String(), "fr") {
		t.Fatalf("unknown language warning = %q", h.stderr.String())
	}
}

func TestHelpAndUsageExitCodes(t *testing.T) {
	h := newHarness(t, http.NotFound)
	if code := h.run("help"); code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	if !strings.Contains(h.stdout.String(), "Usage:") {
		t.Fatalf("help output = %q", h.stdout.String())
	}
	if len(h.requests) != 0 {
		t.Fatal("help contacted the API")
	}

	h.resetOutput()
	if code := h.run("unknown-command"); code != 2 {
		t.Fatalf("unknown command exit = %d, want 2", code)
	}
	if !strings.Contains(h.stderr.String(), "Unknown command") {
		t.Fatalf("unknown command output = %q", h.stderr.String())
	}
}

func TestAPIErrorMessagesNeverExposeTheKey(t *testing.T) {
	tests := []struct {
		code   string
		status int
		want   string
	}{
		{"forbidden", http.StatusForbidden, "permission"},
		{"not_found", http.StatusNotFound, "not found"},
		{"invalid_api_key", http.StatusUnauthorized, "API key"},
		{"rate_limited", http.StatusTooManyRequests, "Too many requests"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, tt.status, map[string]any{"error": map[string]any{"code": tt.code}})
			})
			h.setCredentials()
			if code := h.run("servers", "list"); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if !strings.Contains(h.stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want %q", h.stderr.String(), tt.want)
			}
			assertKeyHidden(t, h.stdout.String(), h.stderr.String())
			request, _ := h.lastRequest()
			if request.Header.Get("User-Agent") != "talaiactl/test" {
				t.Fatalf("User-Agent = %q", request.Header.Get("User-Agent"))
			}
			if request.Header.Get("Authorization") != "Bearer "+testKey {
				t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
			}
		})
	}
}

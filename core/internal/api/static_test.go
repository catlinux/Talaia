package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newPanelDirectory writes a minimal compiled panel and returns its directory.
func newPanelDirectory(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	files := map[string]string{
		"index.html":     "<!doctype html><title>Talaia panel</title>",
		"assets/app.js":  "console.log('talaia')",
		"assets/app.css": "body{margin:0}",
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

func TestStaticServesThePanel(t *testing.T) {
	handler := Static(newPanelDirectory(t))

	tests := []struct {
		name        string
		method      string
		path        string
		wantStatus  int
		wantContent string
	}{
		{name: "root", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantContent: "<title>Talaia panel</title>"},
		{name: "asset", method: http.MethodGet, path: "/assets/app.js", wantStatus: http.StatusOK, wantContent: "console.log('talaia')"},
		{name: "client side route", method: http.MethodGet, path: "/servers/42", wantStatus: http.StatusOK, wantContent: "<title>Talaia panel</title>"},
		{name: "head", method: http.MethodHead, path: "/", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, nil))

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if tt.wantContent != "" && !strings.Contains(recorder.Body.String(), tt.wantContent) {
				t.Errorf("body = %q, want it to contain %q", recorder.Body.String(), tt.wantContent)
			}
		})
	}
}

func TestStaticRejectsWhatIsNotThePanel(t *testing.T) {
	handler := Static(newPanelDirectory(t))

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{name: "write", method: http.MethodPost, path: "/", want: http.StatusMethodNotAllowed},
		{name: "traversal", method: http.MethodGet, path: "/../../etc/passwd", want: http.StatusNotFound},
		{name: "encoded traversal", method: http.MethodGet, path: "/assets/%2e%2e/%2e%2e/etc/passwd", want: http.StatusNotFound},
		{name: "api", method: http.MethodGet, path: "/api/v1/auth/session", want: http.StatusNotFound},
		{name: "health", method: http.MethodGet, path: "/healthz", want: http.StatusNotFound},
		{name: "enrollment", method: http.MethodGet, path: "/enroll", want: http.StatusNotFound},
		{name: "ca", method: http.MethodGet, path: "/ca.pem", want: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, nil))

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.want)
			}
		})
	}
}

func TestStaticWithoutADirectory(t *testing.T) {
	handler := Static(filepath.Join(t.TempDir(), "missing"))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
}

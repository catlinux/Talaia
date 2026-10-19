package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDistHandler(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"install.sh":             "#!/usr/bin/env bash\n",
		"talaia-agent_amd64.deb": "amd64 package",
		"secret.txt":             "must never be served",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	tests := []struct {
		name       string
		dir        string
		path       string
		wantStatus int
		wantBody   string
	}{
		{"install script", dir, "/install.sh", http.StatusOK, files["install.sh"]},
		{"amd64 package", dir, "/agent/talaia-agent_amd64.deb", http.StatusOK, files["talaia-agent_amd64.deb"]},
		{"arm64 package is not built", dir, "/agent/talaia-agent_arm64.deb", http.StatusNotFound, ""},
		{"other file in the directory", dir, "/agent/secret.txt", http.StatusNotFound, ""},
		{"path traversal", dir, "/agent/..%2Fsecret.txt", http.StatusNotFound, ""},
		{"no directory configured", "", "/install.sh", http.StatusNotFound, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			NewDistHandler(tt.dir).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK && recorder.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", recorder.Body.String(), tt.wantBody)
			}
		})
	}
}

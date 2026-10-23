package main

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoginVerifiesAndSavesWithoutPrintingTheKey(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/me" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testKey {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"code": "invalid_api_key"}})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"user": map[string]any{"username": "admin"}})
	})
	h.env["TALAIA_URL"] = h.server.URL
	h.env["TALAIA_API_KEY"] = testKey

	if code := h.run("login"); code != 0 {
		t.Fatalf("login exit = %d, stderr = %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "admin") {
		t.Fatalf("login output = %q", h.stdout.String())
	}
	assertKeyHidden(t, h.stdout.String(), h.stderr.String())

	cfg, err := loadConfig(h.configPath)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.URL != h.server.URL || cfg.APIKey != testKey {
		t.Fatalf("saved config = %#v", cfg)
	}
	info, err := os.Stat(h.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config mode = %o, want owner-only", info.Mode().Perm())
	}
}

func TestLoginPromptsForURLAndSecretWhenMissing(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"user": map[string]any{"username": "prompted"}})
	})
	h.app.stdin = strings.NewReader(h.server.URL + "\n" + testKey + "\n")

	if code := h.run("login"); code != 0 {
		t.Fatalf("login exit = %d, stderr = %s", code, h.stderr.String())
	}
	if !strings.Contains(h.stdout.String(), "Core URL:") || !strings.Contains(h.stdout.String(), "API key:") {
		t.Fatalf("prompts = %q", h.stdout.String())
	}
	cfg, err := loadConfig(h.configPath)
	if err != nil || cfg.APIKey != testKey || cfg.URL != h.server.URL {
		t.Fatalf("config = %#v, %v", cfg, err)
	}
	assertKeyHidden(t, h.stdout.String(), h.stderr.String())
}

func TestLoginFailureDoesNotSaveTheKey(t *testing.T) {
	h := newHarness(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{"code": "invalid_api_key"}})
	})
	h.env["TALAIA_URL"] = h.server.URL
	h.env["TALAIA_API_KEY"] = testKey

	if code := h.run("login"); code != 1 {
		t.Fatalf("login exit = %d, want 1", code)
	}
	if !strings.Contains(h.stderr.String(), "API key") {
		t.Fatalf("stderr = %q", h.stderr.String())
	}
	if _, err := os.Stat(filepath.Clean(h.configPath)); !os.IsNotExist(err) {
		t.Fatalf("config was written on failure: %v", err)
	}
	assertKeyHidden(t, h.stdout.String(), h.stderr.String())
}

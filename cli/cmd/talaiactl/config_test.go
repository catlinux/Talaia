package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/catlinux/Talaia/cli/i18n"
)

func TestParseConfigReadsBothKeysAndIgnoresComments(t *testing.T) {
	cfg, err := parseConfig([]byte(`
# generated
url = "https://panel.example.com"
api_key = 'tlk_abc_def'
unknown = "ignored"
`))
	if err != nil {
		t.Fatalf("parseConfig: %v", err)
	}
	if cfg.URL != "https://panel.example.com" || cfg.APIKey != "tlk_abc_def" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestSaveConfigUsesOwnerOnlyPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "talaia", "config.toml")
	if err := saveConfig(path, config{URL: "https://panel.example.com", APIKey: "tlk_abc_def"}); err != nil {
		t.Fatalf("saveConfig: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
	cfg, err := loadConfig(path)
	if err != nil || cfg.APIKey != "tlk_abc_def" {
		t.Fatalf("loadConfig = %#v, %v", cfg, err)
	}
}

func TestLoadConfigRefusesGroupReadableFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not meaningful on Windows")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("url = \"https://panel.example.com\"\napi_key = \"tlk_abc_def\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	_, err := loadConfig(path)
	var permission *configPermissionError
	if !errors.As(err, &permission) {
		t.Fatalf("error = %v, want configPermissionError", err)
	}
}

func TestPrecedenceFlagsThenEnvironmentThenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "talaia", "config.toml")
	if err := saveConfig(path, config{URL: "https://file.example.com", APIKey: "tlk_file_secret"}); err != nil {
		t.Fatal(err)
	}
	app := &app{
		version:    "test",
		stdout:     &strings.Builder{},
		stderr:     &strings.Builder{},
		configPath: path,
		getenv: func(key string) string {
			switch key {
			case "TALAIA_URL":
				return "https://env.example.com"
			case "TALAIA_API_KEY":
				return "tlk_env_secret"
			default:
				return ""
			}
		},
	}
	localizer, err := i18n.New("en")
	if err != nil {
		t.Fatal(err)
	}

	client, code := app.prepare(localizer, options{url: "https://flag.example.com", apiKey: "tlk_flag_secret"})
	if code != -1 {
		t.Fatalf("prepare exit = %d", code)
	}
	if client.base != "https://flag.example.com" || client.apiKey != "tlk_flag_secret" {
		t.Fatalf("flag client = %#v", client)
	}

	client, code = app.prepare(localizer, options{})
	if code != -1 {
		t.Fatalf("prepare env exit = %d", code)
	}
	if client.base != "https://env.example.com" || client.apiKey != "tlk_env_secret" {
		t.Fatalf("env client = %#v", client)
	}

	app.getenv = func(string) string { return "" }
	client, code = app.prepare(localizer, options{})
	if code != -1 {
		t.Fatalf("prepare file exit = %d", code)
	}
	if client.base != "https://file.example.com" || client.apiKey != "tlk_file_secret" {
		t.Fatalf("file client = %#v", client)
	}
}

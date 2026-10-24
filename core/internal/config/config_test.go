package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/acme"
)

const testDatabaseURL = "postgres://talaia@example.com/talaia?sslmode=disable"

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

func configsEqual(a, b Config) bool {
	return a.PanelListen == b.PanelListen && a.AgentListen == b.AgentListen &&
		a.DatabaseURL == b.DatabaseURL &&
		a.LogLevel == b.LogLevel &&
		a.DataDir == b.DataDir &&
		a.WebDir == b.WebDir &&
		a.InsecureDev == b.InsecureDev &&
		slices.Equal(a.PublicHostnames, b.PublicHostnames) &&
		slices.Equal(a.AllowedOrigins, b.AllowedOrigins)
}

func TestLoad(t *testing.T) {
	defaultFile := `
panel_listen = ":9000"
database_url = "postgres://file@example.com/talaia"
log_level = "debug"
`

	enrollmentFile := `
panel_listen = "127.0.0.1:8080"
database_url = "postgres://file@example.com/talaia"
data_dir = "/tmp/talaia-file"
public_hostnames = ["core.example.com", "192.0.2.10"]
insecure_dev = true
`

	invalidTOML := `panel_listen = `

	tests := []struct {
		name     string
		file     *string
		explicit bool
		env      map[string]string
		want     Config
		wantErr  string
	}{
		{
			name: "defaults with database url from environment",
			env:  map[string]string{"TALAIA_DATABASE_URL": testDatabaseURL},
			want: Config{
				PanelListen: DefaultPanelListen,
				AgentListen: DefaultAgentListen,
				DatabaseURL: testDatabaseURL,
				LogLevel:    DefaultLogLevel,
				DataDir:     DefaultDataDir,
				WebDir:      DefaultWebDir,
			},
		},
		{
			name: "values from file",
			file: &defaultFile,
			want: Config{
				PanelListen: ":9000",
				AgentListen: DefaultAgentListen,
				DatabaseURL: "postgres://file@example.com/talaia",
				LogLevel:    "debug",
				DataDir:     DefaultDataDir,
				WebDir:      DefaultWebDir,
			},
		},
		{
			name: "enrollment values from file",
			file: &enrollmentFile,
			want: Config{
				PanelListen:     "127.0.0.1:8080",
				AgentListen:     DefaultAgentListen,
				DatabaseURL:     "postgres://file@example.com/talaia",
				LogLevel:        DefaultLogLevel,
				DataDir:         "/tmp/talaia-file",
				WebDir:          DefaultWebDir,
				PublicHostnames: []string{"core.example.com", "192.0.2.10"},
				AllowedOrigins: []string{
					"https://core.example.com",
					"https://core.example.com:8080",
					"https://192.0.2.10",
					"https://192.0.2.10:8080",
					"http://localhost:5173",
					"http://127.0.0.1:5173",
				},
				InsecureDev: true,
			},
		},
		{
			name: "environment overrides file",
			file: &defaultFile,
			env: map[string]string{
				"TALAIA_PANEL_LISTEN": ":7000",
				"TALAIA_LOG_LEVEL":    "warn",
			},
			want: Config{
				PanelListen: ":7000",
				AgentListen: DefaultAgentListen,
				DatabaseURL: "postgres://file@example.com/talaia",
				LogLevel:    "warn",
				DataDir:     DefaultDataDir,
				WebDir:      DefaultWebDir,
			},
		},
		{
			name: "environment overrides the enrollment values",
			file: &enrollmentFile,
			env: map[string]string{
				"TALAIA_DATA_DIR":         "/tmp/talaia-env",
				"TALAIA_PUBLIC_HOSTNAMES": "env.example.com, 192.0.2.20",
				"TALAIA_INSECURE_DEV":     "1",
			},
			want: Config{
				PanelListen:     "127.0.0.1:8080",
				AgentListen:     DefaultAgentListen,
				DatabaseURL:     "postgres://file@example.com/talaia",
				LogLevel:        DefaultLogLevel,
				DataDir:         "/tmp/talaia-env",
				WebDir:          DefaultWebDir,
				PublicHostnames: []string{"env.example.com", "192.0.2.20"},
				AllowedOrigins: []string{
					"https://env.example.com",
					"https://env.example.com:8080",
					"https://192.0.2.20",
					"https://192.0.2.20:8080",
					"http://localhost:5173",
					"http://127.0.0.1:5173",
				},
				InsecureDev: true,
			},
		},
		{
			name:     "missing explicit file is an error",
			explicit: true,
			env:      map[string]string{"TALAIA_DATABASE_URL": testDatabaseURL},
			wantErr:  "does not exist",
		},
		{
			name: "insecure_dev on every interface is an error",
			env: map[string]string{
				"TALAIA_DATABASE_URL": testDatabaseURL,
				"TALAIA_INSECURE_DEV": "1",
				"TALAIA_PANEL_LISTEN": ":8080",
			},
			wantErr: "insecure_dev requires a loopback listen address",
		},
		{
			name: "insecure_dev on a public address is an error",
			env: map[string]string{
				"TALAIA_DATABASE_URL": testDatabaseURL,
				"TALAIA_INSECURE_DEV": "1",
				"TALAIA_PANEL_LISTEN": "192.0.2.10:8080",
			},
			wantErr: "insecure_dev requires a loopback listen address",
		},
		{
			name:    "missing database url is an error",
			wantErr: "database_url is required",
		},
		{
			name:    "invalid log level is an error",
			env:     map[string]string{"TALAIA_DATABASE_URL": testDatabaseURL, "TALAIA_LOG_LEVEL": "trace"},
			wantErr: "invalid log_level",
		},
		{
			name:    "invalid public port is an error",
			env:     map[string]string{"TALAIA_DATABASE_URL": testDatabaseURL, "TALAIA_PANEL_PORT": "https"},
			wantErr: "invalid panel_port",
		},
		{
			name:    "invalid insecure dev is an error",
			env:     map[string]string{"TALAIA_DATABASE_URL": testDatabaseURL, "TALAIA_INSECURE_DEV": "maybe"},
			wantErr: "invalid insecure_dev value",
		},
		{
			name:    "invalid toml is an error",
			file:    &invalidTOML,
			env:     map[string]string{"TALAIA_DATABASE_URL": testDatabaseURL},
			wantErr: "parse config file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaultPath := filepath.Join(t.TempDir(), "core.toml")
			if tt.file != nil {
				if err := os.WriteFile(defaultPath, []byte(*tt.file), 0o600); err != nil {
					t.Fatalf("write config file: %v", err)
				}
			}

			env := make(map[string]string, len(tt.env)+1)
			for key, value := range tt.env {
				env[key] = value
			}
			if tt.explicit {
				env["TALAIA_CONFIG"] = defaultPath
			}

			got, err := load(lookupFrom(env), defaultPath)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !configsEqual(got, tt.want) {
				t.Fatalf("config = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMetricsRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.toml")
	if err := os.WriteFile(path, []byte("database_url = \""+testDatabaseURL+"\"\n[metrics]\nserver_raw_hours = 72\ncontainer_1h_days = 120\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := load(lookupFrom(map[string]string{"TALAIA_CONFIG": path}), DefaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Metrics.ServerRaw != 72*time.Hour || cfg.Metrics.Server5m != 30*24*time.Hour || cfg.Metrics.Container1h != 120*24*time.Hour || cfg.Metrics.ContainerRaw != 24*time.Hour {
		t.Fatalf("metrics retention = %+v", cfg.Metrics)
	}
	for _, value := range []string{"0", "3651"} {
		if err := os.WriteFile(path, []byte("database_url = \""+testDatabaseURL+"\"\n[metrics]\nserver_5m_days = "+value+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := load(lookupFrom(map[string]string{"TALAIA_CONFIG": path}), DefaultPath); err == nil || !strings.Contains(err.Error(), "metrics.server_5m_days") {
			t.Errorf("server_5m_days=%s error = %v", value, err)
		}
	}
}

func TestValidateCertificateHosts(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{name: "hostnames present", cfg: Config{PublicHostnames: []string{"core.example.com"}}},
		{name: "insecure dev without hostnames", cfg: Config{InsecureDev: true}},
		{name: "hostnames with insecure dev", cfg: Config{PublicHostnames: []string{"core.example.com"}, InsecureDev: true}},
		{name: "missing hostnames", cfg: Config{}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateCertificateHosts()
			if tt.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestLevel(t *testing.T) {
	tests := []struct {
		level string
		want  string
	}{
		{"debug", "DEBUG"},
		{"info", "INFO"},
		{"warn", "WARN"},
		{"error", "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			if got := (Config{LogLevel: tt.level}).Level().String(); got != tt.want {
				t.Fatalf("Level() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestAllowedOriginsFromTheEnvironment(t *testing.T) {
	file := `
panel_listen = "127.0.0.1:8443"
database_url = "postgres://file@example.com/talaia"
insecure_dev = true
web_dir = "/srv/talaia/web"
`

	tests := []struct {
		name  string
		env   map[string]string
		want  []string
		web   string
		wantE string
	}{
		{
			name: "origin list",
			env: map[string]string{
				"TALAIA_ALLOWED_ORIGINS": "https://panel.example.com, HTTPS://Panel.Example.COM:443/",
			},
			want: []string{"https://panel.example.com"},
			web:  "/srv/talaia/web",
		},
		{
			name: "origin from the environment only",
			env: map[string]string{
				"TALAIA_ALLOWED_ORIGINS": "http://127.0.0.1:3000",
				"TALAIA_WEB_DIR":         "/opt/talaia/web",
			},
			want: []string{"http://127.0.0.1:3000"},
			web:  "/opt/talaia/web",
		},
		{
			name:  "origin with a path",
			env:   map[string]string{"TALAIA_ALLOWED_ORIGINS": "https://panel.example.com/app"},
			wantE: "without a path",
		},
		{
			name:  "origin without a scheme",
			env:   map[string]string{"TALAIA_ALLOWED_ORIGINS": "panel.example.com"},
			wantE: "must start with http://",
		},
		{
			name:  "origin with a query",
			env:   map[string]string{"TALAIA_ALLOWED_ORIGINS": "https://panel.example.com?a=1"},
			wantE: "without a path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaultPath := filepath.Join(t.TempDir(), "core.toml")
			if err := os.WriteFile(defaultPath, []byte(file), 0o600); err != nil {
				t.Fatalf("write config file: %v", err)
			}

			got, err := load(lookupFrom(tt.env), defaultPath)
			if tt.wantE != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantE) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantE)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got.AllowedOrigins, tt.want) {
				t.Errorf("allowed origins = %v, want %v", got.AllowedOrigins, tt.want)
			}
			if got.WebDir != tt.web {
				t.Errorf("web_dir = %q, want %q", got.WebDir, tt.web)
			}
		})
	}
}

func TestDefaultOrigins(t *testing.T) {
	tests := []struct {
		name      string
		hostnames []string
		listen    string
		insecure  bool
		want      []string
	}{
		{
			name:      "port 443 is the default one",
			hostnames: []string{"panel.example.com"},
			listen:    ":443",
			want:      []string{"https://panel.example.com"},
		},
		{
			name:      "another port is spelled out",
			hostnames: []string{"panel.example.com"},
			listen:    ":8443",
			want:      []string{"https://panel.example.com", "https://panel.example.com:8443"},
		},
		{
			name:      "development adds the Vite server",
			hostnames: []string{"panel.example.com"},
			listen:    "127.0.0.1:8443",
			insecure:  true,
			want: []string{
				"https://panel.example.com",
				"https://panel.example.com:8443",
				"http://localhost:5173",
				"http://127.0.0.1:5173",
			},
		},
		{
			name:   "nothing to derive",
			listen: "127.0.0.1:8080",
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultOrigins(tt.hostnames, listenPort(tt.listen), tt.insecure)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("origins = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentURL(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want string
	}{
		{"no hostname", Config{AgentListen: ":8444"}, ""},
		{"listen port", Config{AgentListen: ":8444", PublicHostnames: []string{"192.0.2.10"}}, "https://192.0.2.10:8444"},
		{"public port wins", Config{AgentListen: ":8444", AgentPort: "9000", PublicHostnames: []string{"core.example.com", "b.example.com"}}, "https://core.example.com:9000"},
		{"default port is left out", Config{AgentListen: ":8444", AgentPort: "443", PublicHostnames: []string{"core.example.com"}}, "https://core.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.AgentURL(); got != tt.want {
				t.Fatalf("AgentURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTerminalRecordingDays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.toml")
	base := map[string]string{"TALAIA_DATABASE_URL": testDatabaseURL}
	with := func(key, value string) map[string]string {
		env := map[string]string{key: value}
		for k, v := range base {
			env[k] = v
		}
		return env
	}

	cfg, err := load(lookupFrom(base), path)
	if err != nil || cfg.TerminalRecordingDays != DefaultTerminalRecordingDays {
		t.Fatalf("default = %d, %v", cfg.TerminalRecordingDays, err)
	}
	cfg, err = load(lookupFrom(with("TALAIA_TERMINAL_RECORDING_DAYS", " 30 ")), path)
	if err != nil || cfg.TerminalRecordingDays != 30 {
		t.Errorf("from the environment = %d, %v", cfg.TerminalRecordingDays, err)
	}
	cfg, err = load(lookupFrom(with("TALAIA_TERMINAL_RECORDING_DAYS", "0")), path)
	if err != nil || cfg.TerminalRecordingDays != 0 {
		t.Errorf("keep for ever = %d, %v", cfg.TerminalRecordingDays, err)
	}
	for _, bad := range []string{"-1", "many"} {
		if _, err := load(lookupFrom(with("TALAIA_TERMINAL_RECORDING_DAYS", bad)), path); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}

	if err := os.WriteFile(path, []byte("database_url = \"postgres://file@example.com/talaia\"\nterminal_recording_days = 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := load(lookupFrom(map[string]string{"TALAIA_CONFIG": path}), path); err != nil || cfg.TerminalRecordingDays != 7 {
		t.Errorf("from the file = %d, %v", cfg.TerminalRecordingDays, err)
	}
}

func TestDeviceMFAHours(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.toml")
	base := map[string]string{"TALAIA_DATABASE_URL": testDatabaseURL}
	with := func(key, value string) map[string]string {
		env := map[string]string{key: value}
		for k, v := range base {
			env[k] = v
		}
		return env
	}

	cfg, err := load(lookupFrom(base), path)
	if err != nil || cfg.DeviceMFAHours != DefaultDeviceMFAHours {
		t.Fatalf("default = %d, %v", cfg.DeviceMFAHours, err)
	}
	cfg, err = load(lookupFrom(with("TALAIA_DEVICE_MFA_HOURS", " 24 ")), path)
	if err != nil || cfg.DeviceMFAHours != 24 {
		t.Errorf("from the environment = %d, %v", cfg.DeviceMFAHours, err)
	}
	if err := os.WriteFile(path, []byte("database_url = \"postgres://file@example.com/talaia\"\ndevice_mfa_hours = 6\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, err := load(lookupFrom(map[string]string{"TALAIA_CONFIG": path}), path); err != nil || cfg.DeviceMFAHours != 6 {
		t.Errorf("from the file = %d, %v", cfg.DeviceMFAHours, err)
	}
	for _, bad := range []string{"0", "169", "many"} {
		if _, err := load(lookupFrom(with("TALAIA_DEVICE_MFA_HOURS", bad)), path); err == nil {
			t.Errorf("device_mfa_hours %q was accepted", bad)
		}
	}
}

func TestAgentAndPanelPorts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.toml")
	env := func(extra map[string]string) func(string) (string, bool) {
		all := map[string]string{"TALAIA_DATABASE_URL": testDatabaseURL, "TALAIA_PUBLIC_HOSTNAMES": "core.example.com"}
		for k, v := range extra {
			all[k] = v
		}
		return lookupFrom(all)
	}

	cfg, err := load(env(map[string]string{
		"TALAIA_PANEL_LISTEN": ":8443", "TALAIA_AGENT_LISTEN": ":8444",
		"TALAIA_PANEL_PORT": "9443", "TALAIA_AGENT_PORT": "9444",
	}), path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.AgentURL(); got != "https://core.example.com:9444" {
		t.Errorf("AgentURL() = %q, want the agent port", got)
	}
	if !slices.Contains(cfg.AllowedOrigins, "https://core.example.com:9443") || slices.Contains(cfg.AllowedOrigins, "https://core.example.com:9444") {
		t.Errorf("origins = %v, want the panel port only", cfg.AllowedOrigins)
	}

	if _, err := load(env(map[string]string{"TALAIA_PANEL_LISTEN": ":8443", "TALAIA_AGENT_LISTEN": ":8443"}), path); err == nil || !strings.Contains(err.Error(), "different ports") {
		t.Errorf("same port for both = %v, want an error", err)
	}
	if _, err := load(env(map[string]string{"TALAIA_AGENT_PORT": "0"}), path); err == nil || !strings.Contains(err.Error(), "invalid agent_port") {
		t.Errorf("agent_port 0 = %v, want an error", err)
	}
}

func TestACME(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.toml")
	env := func(extra map[string]string) map[string]string {
		all := map[string]string{
			"TALAIA_DATABASE_URL":     testDatabaseURL,
			"TALAIA_PUBLIC_HOSTNAMES": "panel.example.com, 192.0.2.10",
		}
		for key, value := range extra {
			all[key] = value
		}
		return all
	}

	cfg, err := load(lookupFrom(env(nil)), path)
	if err != nil {
		t.Fatalf("without ACME: %v", err)
	}
	if cfg.ACMEProvider != "" {
		t.Errorf("acme_provider = %q, want it empty", cfg.ACMEProvider)
	}
	if cfg.ACMEDirectoryURL != acme.DefaultDirectoryURL {
		t.Errorf("acme_directory_url = %q, want the production one", cfg.ACMEDirectoryURL)
	}

	cfg, err = load(lookupFrom(env(map[string]string{
		"TALAIA_ACME_PROVIDER":      " OVH ",
		"TALAIA_ACME_EMAIL":         " ops@example.com ",
		"TALAIA_ACME_DIRECTORY_URL": "https://acme-staging-v02.api.letsencrypt.org/directory",
	})), path)
	if err != nil {
		t.Fatalf("with ACME: %v", err)
	}
	if cfg.ACMEProvider != "ovh" || cfg.ACMEEmail != "ops@example.com" {
		t.Errorf("provider and email = %q, %q", cfg.ACMEProvider, cfg.ACMEEmail)
	}
	if cfg.ACMEDirectoryURL != "https://acme-staging-v02.api.letsencrypt.org/directory" {
		t.Errorf("acme_directory_url = %q, want the staging one", cfg.ACMEDirectoryURL)
	}
	if got := cfg.ACMEDomains(); !slices.Equal(got, []string{"panel.example.com"}) {
		t.Errorf("ACMEDomains() = %v, want only the DNS name", got)
	}

	cfg, err = load(lookupFrom(env(map[string]string{"TALAIA_ACME_PROVIDER": "delegation"})), path)
	if err != nil {
		t.Fatalf("with delegation: %v", err)
	}
	if cfg.ACMEChallengeDir != DefaultACMEChallengeDir || cfg.ACMEDNSCheck != DefaultACMEDNSCheck {
		t.Errorf("delegation defaults = %q, %q", cfg.ACMEChallengeDir, cfg.ACMEDNSCheck)
	}

	errors := []struct {
		name    string
		extra   map[string]string
		wantErr string
	}{
		{
			name:    "unsupported provider",
			extra:   map[string]string{"TALAIA_ACME_PROVIDER": "cloudflare", "TALAIA_ACME_EMAIL": "ops@example.com"},
			wantErr: "invalid acme_provider",
		},
		{
			name: "delegation without the DNS server address",
			extra: map[string]string{
				"TALAIA_ACME_PROVIDER":  "delegation",
				"TALAIA_ACME_DNS_CHECK": "acme-dns",
			},
			wantErr: "invalid acme_dns_check",
		},
		{
			name: "delegation without a challenge directory",
			extra: map[string]string{
				"TALAIA_ACME_PROVIDER":      "delegation",
				"TALAIA_ACME_CHALLENGE_DIR": " ",
			},
			wantErr: "acme_challenge_dir is required",
		},
		{
			name: "only IP addresses",
			extra: map[string]string{
				"TALAIA_ACME_PROVIDER":    "ovh",
				"TALAIA_ACME_EMAIL":       "ops@example.com",
				"TALAIA_PUBLIC_HOSTNAMES": "192.0.2.10, 192.0.2.11",
			},
			wantErr: "at least one DNS name",
		},
		{
			name: "insecure development",
			extra: map[string]string{
				"TALAIA_ACME_PROVIDER": "ovh",
				"TALAIA_ACME_EMAIL":    "ops@example.com",
				"TALAIA_INSECURE_DEV":  "1",
				"TALAIA_PANEL_LISTEN":  "127.0.0.1:8080",
			},
			wantErr: "insecure_dev",
		},
	}
	for _, tt := range errors {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := load(lookupFrom(env(tt.extra)), path); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}

	file := `
database_url = "postgres://file@example.com/talaia"
public_hostnames = ["panel.example.com"]
acme_provider = "ovh"
acme_email = "file@example.com"
acme_directory_url = "https://acme.example/directory"
`
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = load(lookupFrom(map[string]string{"TALAIA_CONFIG": path}), path)
	if err != nil {
		t.Fatalf("from the file: %v", err)
	}
	if cfg.ACMEProvider != "ovh" || cfg.ACMEEmail != "file@example.com" || cfg.ACMEDirectoryURL != "https://acme.example/directory" {
		t.Fatalf("ACME values from the file = %+v", cfg)
	}
}

func TestUpdatesConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.toml")
	env := func(extra map[string]string) map[string]string {
		all := map[string]string{"TALAIA_DATABASE_URL": testDatabaseURL}
		for key, value := range extra {
			all[key] = value
		}
		return all
	}

	cfg, err := load(lookupFrom(env(nil)), path)
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if cfg.UpdateRepo != "catlinux/Talaia" {
		t.Errorf("update_repo = %q", cfg.UpdateRepo)
	}
	if cfg.UpdateCheckInterval != 30*time.Minute {
		t.Errorf("update_check_interval = %v", cfg.UpdateCheckInterval)
	}
	if cfg.UpdateAPIURL != "https://api.github.com" {
		t.Errorf("update_api_url = %q", cfg.UpdateAPIURL)
	}
	if want := filepath.Join(DefaultDataDir, "host"); cfg.HostDir != want {
		t.Errorf("host_dir = %q, want %q", cfg.HostDir, want)
	}

	cfg, err = load(lookupFrom(env(map[string]string{
		"TALAIA_DATA_DIR":              "/srv/talaia",
		"TALAIA_UPDATE_REPO":           "example/repo",
		"TALAIA_UPDATE_CHECK_INTERVAL": "5m",
		"TALAIA_UPDATE_API_URL":        "https://github.example/api/",
		"TALAIA_HOST_DIR":              "/srv/talaia-host",
	})), path)
	if err != nil {
		t.Fatalf("environment: %v", err)
	}
	if cfg.UpdateRepo != "example/repo" || cfg.UpdateCheckInterval != 5*time.Minute {
		t.Errorf("update values = %q, %v", cfg.UpdateRepo, cfg.UpdateCheckInterval)
	}
	if cfg.UpdateAPIURL != "https://github.example/api" {
		t.Errorf("update_api_url = %q, want it without the trailing slash", cfg.UpdateAPIURL)
	}
	if cfg.HostDir != "/srv/talaia-host" {
		t.Errorf("host_dir = %q", cfg.HostDir)
	}

	cfg, err = load(lookupFrom(env(map[string]string{"TALAIA_DATA_DIR": "/srv/talaia"})), path)
	if err != nil {
		t.Fatalf("host dir from the data dir: %v", err)
	}
	if want := filepath.Join("/srv/talaia", "host"); cfg.HostDir != want {
		t.Errorf("host_dir = %q, want %q", cfg.HostDir, want)
	}

	errors := []struct {
		name    string
		extra   map[string]string
		wantErr string
	}{
		{
			name:    "repository without owner",
			extra:   map[string]string{"TALAIA_UPDATE_REPO": "Talaia"},
			wantErr: "invalid update_repo",
		},
		{
			name:    "repository with a path",
			extra:   map[string]string{"TALAIA_UPDATE_REPO": "a/b/c"},
			wantErr: "invalid update_repo",
		},
		{
			name:    "interval too short",
			extra:   map[string]string{"TALAIA_UPDATE_CHECK_INTERVAL": "30s"},
			wantErr: "at least 1m",
		},
		{
			name:    "invalid interval",
			extra:   map[string]string{"TALAIA_UPDATE_CHECK_INTERVAL": "often"},
			wantErr: "invalid update_check_interval",
		},
		{
			name:    "invalid API URL",
			extra:   map[string]string{"TALAIA_UPDATE_API_URL": "not a url"},
			wantErr: "invalid update_api_url",
		},
	}
	for _, tt := range errors {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := load(lookupFrom(env(tt.extra)), path); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}

	file := `
database_url = "postgres://file@example.com/talaia"
update_repo = "o/r"
update_check_interval = "10m"
update_api_url = "https://api.example"
host_dir = "/tmp/talaia-host"
`
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = load(lookupFrom(map[string]string{"TALAIA_CONFIG": path}), path)
	if err != nil {
		t.Fatalf("from the file: %v", err)
	}
	if cfg.UpdateRepo != "o/r" || cfg.UpdateCheckInterval != 10*time.Minute ||
		cfg.UpdateAPIURL != "https://api.example" || cfg.HostDir != "/tmp/talaia-host" {
		t.Fatalf("update values from the file = %+v", cfg)
	}

	badFile := `
database_url = "postgres://file@example.com/talaia"
update_check_interval = "soon"
`
	if err := os.WriteFile(path, []byte(badFile), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := load(lookupFrom(map[string]string{"TALAIA_CONFIG": path}), path); err == nil || !strings.Contains(err.Error(), "invalid update_check_interval") {
		t.Fatalf("bad interval in the file = %v", err)
	}
}

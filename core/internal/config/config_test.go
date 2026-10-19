package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const testDatabaseURL = "postgres://talaia@example.com/talaia?sslmode=disable"

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

func configsEqual(a, b Config) bool {
	return a.Listen == b.Listen &&
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
listen = ":9000"
database_url = "postgres://file@example.com/talaia"
log_level = "debug"
`

	enrollmentFile := `
listen = "127.0.0.1:8080"
database_url = "postgres://file@example.com/talaia"
data_dir = "/tmp/talaia-file"
public_hostnames = ["core.example.com", "192.0.2.10"]
insecure_dev = true
`

	invalidTOML := `listen = `

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
				Listen:      DefaultListen,
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
				Listen:      ":9000",
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
				Listen:          "127.0.0.1:8080",
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
				"TALAIA_LISTEN":    ":7000",
				"TALAIA_LOG_LEVEL": "warn",
			},
			want: Config{
				Listen:      ":7000",
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
				Listen:          "127.0.0.1:8080",
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
				"TALAIA_LISTEN":       ":8080",
			},
			wantErr: "insecure_dev requires a loopback listen address",
		},
		{
			name: "insecure_dev on a public address is an error",
			env: map[string]string{
				"TALAIA_DATABASE_URL": testDatabaseURL,
				"TALAIA_INSECURE_DEV": "1",
				"TALAIA_LISTEN":       "192.0.2.10:8080",
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
listen = "127.0.0.1:8443"
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
			got := defaultOrigins(tt.hostnames, tt.listen, tt.insecure)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("origins = %v, want %v", got, tt.want)
			}
		})
	}
}

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
		a.InsecureDev == b.InsecureDev &&
		slices.Equal(a.PublicHostnames, b.PublicHostnames)
}

func TestLoad(t *testing.T) {
	defaultFile := `
listen = ":9000"
database_url = "postgres://file@example.com/talaia"
log_level = "debug"
`

	enrollmentFile := `
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
			},
		},
		{
			name: "enrollment values from file",
			file: &enrollmentFile,
			want: Config{
				Listen:          DefaultListen,
				DatabaseURL:     "postgres://file@example.com/talaia",
				LogLevel:        DefaultLogLevel,
				DataDir:         "/tmp/talaia-file",
				PublicHostnames: []string{"core.example.com", "192.0.2.10"},
				InsecureDev:     true,
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
				Listen:          DefaultListen,
				DatabaseURL:     "postgres://file@example.com/talaia",
				LogLevel:        DefaultLogLevel,
				DataDir:         "/tmp/talaia-env",
				PublicHostnames: []string{"env.example.com", "192.0.2.20"},
				InsecureDev:     true,
			},
		},
		{
			name:     "missing explicit file is an error",
			explicit: true,
			env:      map[string]string{"TALAIA_DATABASE_URL": testDatabaseURL},
			wantErr:  "does not exist",
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

package config

import (
	"os"
	"path/filepath"
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

func TestLoad(t *testing.T) {
	defaultFile := `
listen = ":9000"
database_url = "postgres://file@example.com/talaia"
log_level = "debug"
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
			},
		},
		{
			name: "values from file",
			file: &defaultFile,
			want: Config{
				Listen:      ":9000",
				DatabaseURL: "postgres://file@example.com/talaia",
				LogLevel:    "debug",
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
			if got != tt.want {
				t.Fatalf("config = %+v, want %+v", got, tt.want)
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

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lookupFrom(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

func TestLoad(t *testing.T) {
	fromFile := `
core_url = "http://core.example.com:9000"
log_level = "debug"
`

	invalidTOML := `core_url = `

	tests := []struct {
		name     string
		file     *string
		explicit bool
		env      map[string]string
		want     Config
		wantErr  string
	}{
		{
			name: "defaults",
			want: Config{CoreURL: DefaultCoreURL, LogLevel: DefaultLogLevel},
		},
		{
			name: "values from file",
			file: &fromFile,
			want: Config{CoreURL: "http://core.example.com:9000", LogLevel: "debug"},
		},
		{
			name: "environment overrides file",
			file: &fromFile,
			env: map[string]string{
				"TALAIA_CORE_URL":  "https://panel.example.com",
				"TALAIA_LOG_LEVEL": "warn",
			},
			want: Config{CoreURL: "https://panel.example.com", LogLevel: "warn"},
		},
		{
			name:     "missing explicit file is an error",
			explicit: true,
			wantErr:  "does not exist",
		},
		{
			name:    "core_url without a scheme is an error",
			env:     map[string]string{"TALAIA_CORE_URL": "core.example.com:8080"},
			wantErr: "absolute http or https URL",
		},
		{
			name:    "core_url with a non http scheme is an error",
			env:     map[string]string{"TALAIA_CORE_URL": "ftp://core.example.com"},
			wantErr: "absolute http or https URL",
		},
		{
			name:    "invalid log level is an error",
			env:     map[string]string{"TALAIA_LOG_LEVEL": "trace"},
			wantErr: "invalid log_level",
		},
		{
			name:    "invalid toml is an error",
			file:    &invalidTOML,
			wantErr: "parse config file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaultPath := filepath.Join(t.TempDir(), "agent.toml")
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
				env["TALAIA_AGENT_CONFIG"] = defaultPath
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

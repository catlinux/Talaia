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
	const fingerprint = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	fromFile := `
core_url = "https://core.example.com:9000"
log_level = "debug"
ca_fingerprint = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
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
			name: "insecure dev uses the loopback default",
			env:  map[string]string{"TALAIA_AGENT_INSECURE_DEV": "1"},
			want: Config{
				CoreURL:         DefaultCoreURL,
				LogLevel:        DefaultLogLevel,
				CertDir:         DefaultCertDir,
				EnrollTokenFile: DefaultEnrollTokenFile,
				InsecureDev:     true,
			},
		},
		{
			name: "values from file",
			file: &fromFile,
			want: Config{
				CoreURL:         "https://core.example.com:9000",
				LogLevel:        "debug",
				CAFingerprint:   fingerprint,
				CertDir:         DefaultCertDir,
				EnrollTokenFile: DefaultEnrollTokenFile,
			},
		},
		{
			name: "environment overrides file",
			file: &fromFile,
			env: map[string]string{
				"TALAIA_CORE_URL":                "https://panel.example.com",
				"TALAIA_LOG_LEVEL":               "warn",
				"TALAIA_CA_FINGERPRINT":          strings.ToUpper(fingerprint),
				"TALAIA_AGENT_CERT_DIR":          "/tmp/certs",
				"TALAIA_AGENT_ENROLL_TOKEN_FILE": "/tmp/enroll.token",
			},
			want: Config{
				CoreURL:         "https://panel.example.com",
				LogLevel:        "warn",
				CAFingerprint:   fingerprint,
				CertDir:         "/tmp/certs",
				EnrollTokenFile: "/tmp/enroll.token",
			},
		},
		{
			name:     "missing explicit file is an error",
			explicit: true,
			wantErr:  "does not exist",
		},
		{
			name:    "core_url is required when not insecure",
			wantErr: "core_url is required",
		},
		{
			name:    "plain http core_url needs insecure dev",
			env:     map[string]string{"TALAIA_CORE_URL": "http://core.example.com"},
			wantErr: "must be https://",
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
			name:    "invalid ca fingerprint is an error",
			env:     map[string]string{"TALAIA_CORE_URL": "https://core.example.com", "TALAIA_CA_FINGERPRINT": "abc"},
			wantErr: "ca_fingerprint must be 64 hexadecimal characters",
		},
		{
			name:    "invalid insecure dev is an error",
			env:     map[string]string{"TALAIA_AGENT_INSECURE_DEV": "maybe"},
			wantErr: "invalid insecure_dev value",
		},
		{
			name:    "invalid log level is an error",
			env:     map[string]string{"TALAIA_CORE_URL": "https://core.example.com", "TALAIA_LOG_LEVEL": "trace"},
			wantErr: "invalid log_level",
		},
		{
			name:    "invalid toml is an error",
			file:    &invalidTOML,
			env:     map[string]string{"TALAIA_CORE_URL": "https://core.example.com"},
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

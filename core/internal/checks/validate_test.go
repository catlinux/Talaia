package checks

import (
	"encoding/json"
	"testing"
)

func TestValidateCheck(t *testing.T) {
	tests := []struct {
		kind, target, cfg string
		valid             bool
	}{
		{"http", "ftp://example.com", `{}`, false}, {"http", "http://user:pass@example.com", `{}`, false},
		{"tcp", "localhost:0", `{}`, false}, {"tcp", "localhost:70000", `{}`, false},
		{"tcp", "localhost:bad", `{}`, false}, {"tcp", "localhost:65536", `{}`, false},
		{"http", "http://example.com", `{"method":"POST"}`, false},
		{"http", "http://example.com", `{"timeout_seconds":0}`, false},
		{"http", "http://example.com", `{"timeout_seconds":11}`, false},
		{"http", "http://example.com", `{"expected_codes":["garbage"]}`, false},
		{"wat", "x", `{}`, false}, {"http", "http://example.com", `{}`, true},
		{"http", "http://example.com", `{"expected_codes":["200-299"]}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.kind+tt.target+tt.cfg, func(t *testing.T) {
			err := ValidateCheck(tt.kind, tt.target, json.RawMessage(tt.cfg))
			if (err == nil) != tt.valid {
				t.Fatalf("ValidateCheck() err=%v valid=%v", err, tt.valid)
			}
		})
	}
}

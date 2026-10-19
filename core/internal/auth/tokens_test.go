package auth

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestNewSessionToken(t *testing.T) {
	token := NewSessionToken()
	if token == "" {
		t.Fatal("the token is empty")
	}
	if strings.ContainsAny(token, "=+/") {
		t.Errorf("token %q is padded or not urlsafe", token)
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if len(raw) != sessionTokenLength {
		t.Errorf("token entropy = %d bytes, want %d", len(raw), sessionTokenLength)
	}

	if token == NewSessionToken() {
		t.Error("two tokens must differ")
	}
}

func TestHashToken(t *testing.T) {
	token := NewSessionToken()
	hash := HashToken(token)
	if len(hash) != 32 {
		t.Fatalf("hash length = %d, want 32", len(hash))
	}
	if string(hash) != string(HashToken(token)) {
		t.Error("the hash of a token must be stable")
	}
	if string(hash) == string(HashToken(token+"x")) {
		t.Error("the hashes of different tokens must differ")
	}
	if strings.Contains(string(hash), token) {
		t.Error("the hash must not contain the token")
	}
}

func TestNewRecoveryCodes(t *testing.T) {
	codes := NewRecoveryCodes(10)
	if len(codes) != 10 {
		t.Fatalf("codes = %d, want 10", len(codes))
	}

	seen := make(map[string]bool, len(codes))
	for _, code := range codes {
		if len(code) != recoveryCodeLength+1 {
			t.Errorf("code %q must be ten characters and a dash", code)
		}
		parts := strings.Split(code, "-")
		if len(parts) != 2 || len(parts[0]) != recoveryCodeGroup || len(parts[1]) != recoveryCodeGroup {
			t.Errorf("code %q is not formatted as XXXXX-XXXXX", code)
		}
		for _, char := range strings.ReplaceAll(code, "-", "") {
			if !strings.ContainsRune(recoveryCodeAlphabet, char) {
				t.Errorf("code %q contains %q, which is not base32", code, char)
			}
		}
		if seen[code] {
			t.Errorf("code %q is repeated", code)
		}
		seen[code] = true
	}

	if got := NewRecoveryCodes(0); got != nil {
		t.Errorf("NewRecoveryCodes(0) = %v, want nil", got)
	}
}

func TestNormalizeRecoveryCode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "ABCDE-FGHIJ", want: "ABCDEFGHIJ"},
		{in: "abcde-fghij", want: "ABCDEFGHIJ"},
		{in: " ABCDE FGHIJ ", want: "ABCDEFGHIJ"},
		{in: "ABCDEFGHIJ", want: "ABCDEFGHIJ"},
	}

	for _, tt := range tests {
		if got := NormalizeRecoveryCode(tt.in); got != tt.want {
			t.Errorf("NormalizeRecoveryCode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	// A generated code normalizes to the ten characters that are hashed.
	for _, code := range NewRecoveryCodes(1) {
		if got := NormalizeRecoveryCode(code); len(got) != recoveryCodeLength {
			t.Errorf("normalized %q = %q, want ten characters", code, got)
		}
	}
}

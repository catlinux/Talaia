package auth

import (
	"bytes"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// rfc6238Secret is the SHA-1 secret of the RFC 6238 test vectors.
var rfc6238Secret = []byte("12345678901234567890")

func TestCodeMatchesTheRFC6238Vectors(t *testing.T) {
	// RFC 6238, appendix B, SHA-1. The RFC lists eight-digit codes; the six-digit
	// code is the same value modulo 10^6, that is, its last six digits.
	vectors := []struct {
		unix int64
		code string
	}{
		{unix: 59, code: "287082"},
		{unix: 1111111109, code: "081804"},
		{unix: 1111111111, code: "050471"},
		{unix: 1234567890, code: "005924"},
		{unix: 2000000000, code: "279037"},
		{unix: 20000000000, code: "353130"},
	}

	for _, vector := range vectors {
		at := time.Unix(vector.unix, 0).UTC()
		if got := Code(rfc6238Secret, Step(at)); got != vector.code {
			t.Errorf("Code at T=%d = %q, want %q", vector.unix, got, vector.code)
		}

		// The same code verifies as long as the step is newer than the last one.
		matched, ok := Verify(rfc6238Secret, vector.code, at, Step(at)-1)
		if !ok || matched != Step(at) {
			t.Errorf("Verify at T=%d = %d, %v, want step %d", vector.unix, matched, ok, Step(at))
		}
	}
}

func TestCodeIsSixDigits(t *testing.T) {
	secret := GenerateSecret()

	for step := int64(0); step < 50; step++ {
		code := Code(secret, step)
		if len(code) != 6 {
			t.Fatalf("Code at step %d = %q, want six digits", step, code)
		}
		if _, err := strconv.Atoi(code); err != nil {
			t.Fatalf("Code at step %d = %q, want digits only", step, code)
		}
	}
}

func TestVerifyAcceptsTheWindowAroundNow(t *testing.T) {
	now := time.Unix(1_111_111_111, 0).UTC()
	current := Step(now)

	for _, offset := range []int64{-1, 0, 1} {
		code := Code(rfc6238Secret, current+offset)
		matched, ok := Verify(rfc6238Secret, code, now, current-2)
		if !ok || matched != current+offset {
			t.Errorf("offset %d: Verify = %d, %v, want step %d", offset, matched, ok, current+offset)
		}
	}

	// Two steps away is outside the window.
	if _, ok := Verify(rfc6238Secret, Code(rfc6238Secret, current-2), now, current-3); ok {
		t.Error("a code two steps old must be rejected")
	}
	if _, ok := Verify(rfc6238Secret, Code(rfc6238Secret, current+2), now, current-3); ok {
		t.Error("a code two steps ahead must be rejected")
	}
}

func TestVerifyRejectsReplayedSteps(t *testing.T) {
	now := time.Unix(1_111_111_111, 0).UTC()
	current := Step(now)

	code := Code(rfc6238Secret, current)
	matched, ok := Verify(rfc6238Secret, code, now, 0)
	if !ok || matched != current {
		t.Fatalf("Verify = %d, %v, want the current step", matched, ok)
	}

	// The same step is not accepted again.
	if _, ok := Verify(rfc6238Secret, code, now, matched); ok {
		t.Error("a code must not be accepted twice")
	}
	// Nor is an older one once a newer step was recorded.
	if _, ok := Verify(rfc6238Secret, Code(rfc6238Secret, current-1), now, current); ok {
		t.Error("an older step must be rejected after a newer one")
	}
	// A newer code is accepted and moves the guard forward.
	if _, ok := Verify(rfc6238Secret, Code(rfc6238Secret, current+1), now, current); !ok {
		t.Error("a newer step must be accepted")
	}
}

func TestVerifyRejectsMalformedCodes(t *testing.T) {
	now := time.Now()
	code := Code(rfc6238Secret, Step(now))

	for _, malformed := range []string{"", "12345", "1234567", "abcdef", "12 34 56"} {
		if _, ok := Verify(rfc6238Secret, malformed, now, 0); ok {
			t.Errorf("Verify(%q) accepted a malformed code", malformed)
		}
	}

	// Surrounding spaces are tolerated, as a user may paste them.
	if _, ok := Verify(rfc6238Secret, " "+code+" ", now, 0); !ok {
		t.Error("a code with surrounding spaces must still verify")
	}
}

func TestGenerateSecret(t *testing.T) {
	first := GenerateSecret()
	if len(first) != secretLength {
		t.Fatalf("secret length = %d, want %d", len(first), secretLength)
	}
	if bytes.Equal(first, GenerateSecret()) {
		t.Error("two generated secrets must differ")
	}
}

func TestBase32RoundTrip(t *testing.T) {
	secret := GenerateSecret()
	encoded := EncodeBase32(secret)
	if strings.Contains(encoded, "=") {
		t.Errorf("encoded secret %q is padded", encoded)
	}

	decoded, err := DecodeBase32(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !bytes.Equal(decoded, secret) {
		t.Error("the decoded secret does not match")
	}

	// Lower case is accepted.
	decoded, err = DecodeBase32(strings.ToLower(encoded))
	if err != nil {
		t.Fatalf("decode lower case: %v", err)
	}
	if !bytes.Equal(decoded, secret) {
		t.Error("the decoded lower-case secret does not match")
	}

	for _, invalid := range []string{"", "not base32!", "1"} {
		if _, err := DecodeBase32(invalid); err == nil {
			t.Errorf("DecodeBase32(%q) did not fail", invalid)
		}
	}
}

func TestProvisioningURI(t *testing.T) {
	secret := EncodeBase32(GenerateSecret())
	uri := ProvisioningURI(Issuer, "admin", secret)

	if !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatalf("uri = %q", uri)
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}
	if parsed.Scheme != "otpauth" || parsed.Host != "totp" {
		t.Errorf("scheme and host = %q://%q", parsed.Scheme, parsed.Host)
	}
	if parsed.Path != "/Talaia:admin" {
		t.Errorf("label = %q, want /Talaia:admin", parsed.Path)
	}

	query := parsed.Query()
	for key, want := range map[string]string{
		"secret":    secret,
		"issuer":    "Talaia",
		"algorithm": "SHA1",
		"digits":    "6",
		"period":    "30",
	} {
		if got := query.Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}

	// An account with a space and a colon is escaped in the label.
	spaced := ProvisioningURI(Issuer, "first last", secret)
	if strings.Contains(spaced, " ") {
		t.Errorf("uri = %q, the label must not contain a raw space", spaced)
	}
	parsed, err = url.Parse(spaced)
	if err != nil {
		t.Fatalf("parse uri: %v", err)
	}
	if parsed.Path != "/Talaia:first last" {
		t.Errorf("label = %q, want /Talaia:first last", parsed.Path)
	}
}

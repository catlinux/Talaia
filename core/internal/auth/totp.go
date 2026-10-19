package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// Issuer names the panel in the authenticator apps.
	Issuer = "Talaia"

	// TOTP parameters: RFC 6238 with SHA-1, 30-second steps, six digits and a
	// one-step window on each side to absorb clock drift.
	totpStepSeconds = 30
	totpDigits      = 6
	totpWindow      = 1

	// secretLength is the entropy of the shared secret, as the design asks.
	secretLength = 20
)

// GenerateSecret returns 20 random bytes: the TOTP shared secret.
func GenerateSecret() []byte {
	secret := make([]byte, secretLength)
	rand.Read(secret)
	return secret
}

// EncodeBase32 returns the secret the way authenticator apps expect it: RFC
// 4648 base32 without padding.
func EncodeBase32(secret []byte) string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
}

// DecodeBase32 parses a base32 secret. Upper or lower case and present padding
// are accepted.
func DecodeBase32(value string) ([]byte, error) {
	normalized := strings.ToUpper(strings.TrimSpace(strings.TrimRight(value, "=")))
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil {
		return nil, fmt.Errorf("decode totp secret: %w", err)
	}
	if len(secret) == 0 {
		return nil, errors.New("the totp secret is empty")
	}
	return secret, nil
}

// ProvisioningURI returns the otpauth:// URI an authenticator app reads, for
// example otpauth://totp/Talaia:admin?secret=...&issuer=Talaia.
func ProvisioningURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)

	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", strconv.Itoa(totpDigits))
	query.Set("period", strconv.Itoa(totpStepSeconds))

	return "otpauth://totp/" + label + "?" + query.Encode()
}

// Step returns the TOTP step of a time.
func Step(now time.Time) int64 {
	return now.Unix() / int64(totpStepSeconds)
}

// Code returns the six-digit code of the secret at the given step.
func Code(secret []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))

	mac := hmac.New(sha1.New, secret)
	mac.Write(counter[:])
	sum := mac.Sum(nil)

	// RFC 4226 dynamic truncation.
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	return fmt.Sprintf("%0*d", totpDigits, value%1_000_000)
}

// Verify checks a code at now, accepting the previous and the next step. It
// returns the step that matched. Any step that is not newer than lastStep is
// rejected, which makes a captured code single use.
func Verify(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits {
		return 0, false
	}

	current := Step(now)
	for offset := int64(-totpWindow); offset <= totpWindow; offset++ {
		step := current + offset
		if step < 0 || step <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(Code(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

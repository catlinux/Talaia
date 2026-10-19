package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

const (
	// sessionTokenLength is the entropy of a session token.
	sessionTokenLength = 32

	// recoveryCodeLength is ten base32 characters, fifty bits.
	recoveryCodeLength = 10
	// recoveryCodeGroup is the size of the two groups a code is printed in.
	recoveryCodeGroup = 5

	// recoveryCodeAlphabet is the RFC 4648 base32 alphabet, in upper case.
	recoveryCodeAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
)

// NewSessionToken returns 32 random bytes in unpadded base64url: the value that
// goes in the cookie. Only its hash reaches the database.
func NewSessionToken() string {
	raw := make([]byte, sessionTokenLength)
	rand.Read(raw)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// HashToken returns the SHA-256 of a token, which is what the database stores.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// NewRecoveryCodes returns n single-use recovery codes of ten base32 characters
// formatted as XXXXX-XXXXX. This is the only moment the codes are in clear.
func NewRecoveryCodes(n int) []string {
	if n <= 0 {
		return nil
	}

	codes := make([]string, n)
	raw := make([]byte, recoveryCodeLength)
	for i := range codes {
		rand.Read(raw)

		chars := make([]byte, recoveryCodeLength)
		for j, b := range raw {
			// 256 is a multiple of 32, so the modulo does not bias the choice.
			chars[j] = recoveryCodeAlphabet[int(b)%len(recoveryCodeAlphabet)]
		}
		codes[i] = string(chars[:recoveryCodeGroup]) + "-" + string(chars[recoveryCodeGroup:])
	}
	return codes
}

// NormalizeRecoveryCode removes the dashes and spaces and upper-cases the code,
// so it matches however the user types it.
func NormalizeRecoveryCode(code string) string {
	var normalized strings.Builder
	normalized.Grow(len(code))
	for _, char := range strings.ToUpper(code) {
		if char == '-' || char == ' ' {
			continue
		}
		normalized.WriteRune(char)
	}
	return normalized.String()
}

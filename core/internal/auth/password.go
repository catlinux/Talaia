// Package auth holds the cryptographic building blocks of panel
// authentication: password hashing with argon2id, TOTP, session and recovery
// tokens, and the encryption of the TOTP secret.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	// The argon2id parameters of the design: 64 MiB of memory, three
	// iterations and one lane.
	argonMemory      uint32 = 64 * 1024 // KiB
	argonIterations  uint32 = 3
	argonParallelism uint8  = 1
	argonSaltLength         = 16
	argonKeyLength   uint32 = 32

	// maxArgonMemory is the largest memory cost accepted when verifying a stored
	// hash: a damaged row must not make the core allocate unbounded memory.
	maxArgonMemory uint32 = 1024 * 1024 // KiB

	// maxConcurrentHashes bounds the simultaneous computations to two. Each one
	// uses 64 MiB, and the core must fit in a small VPS next to PostgreSQL.
	maxConcurrentHashes = 2

	// Password policy, in runes.
	minPasswordLength = 12
	maxPasswordLength = 256

	// dummyPassword is only used to build the hash that DummyVerify spends time
	// on.
	dummyPassword = "talaia dummy password"
)

// hashSlots is the semaphore of the argon2 computations.
var hashSlots = make(chan struct{}, maxConcurrentHashes)

// dummyPHC is a valid hash of dummyPassword, computed on first use.
var dummyPHC = sync.OnceValue(func() string { return HashPassword(dummyPassword) })

// HashPassword returns the PHC string of the password: argon2id with a random
// salt. crypto/rand never returns an error (it crashes the program if the
// system source fails), so there is no failure to report.
func HashPassword(password string) string {
	salt := make([]byte, argonSaltLength)
	rand.Read(salt)

	key := argonKey(password, salt, argonMemory, argonIterations, argonParallelism, argonKeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

// VerifyPassword checks a password against a PHC argon2id string. The
// parameters are read from the string, so hashes stored with other costs keep
// working after a tuning change.
func VerifyPassword(phc, password string) (bool, error) {
	params, salt, want, err := parsePHC(phc)
	if err != nil {
		return false, err
	}

	got := argonKey(password, salt, params.memory, params.iterations, params.parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// DummyVerify spends the time of a password verification for a username that
// does not exist, so the response time does not reveal which users are real.
func DummyVerify(password string) {
	_, _ = VerifyPassword(dummyPHC(), password)
}

// ValidatePasswordPolicy checks the length of a new password: at least 12 runes
// and at most 256. It counts runes, not bytes, so every alphabet is welcome.
func ValidatePasswordPolicy(password string) error {
	if strings.TrimSpace(password) == "" {
		return errors.New("the password must not be blank")
	}
	length := utf8.RuneCountInString(password)
	if length < minPasswordLength {
		return fmt.Errorf("the password must be at least %d characters long", minPasswordLength)
	}
	if length > maxPasswordLength {
		return fmt.Errorf("the password must be at most %d characters long", maxPasswordLength)
	}
	return nil
}

// phcParams are the argon2 parameters of a PHC string.
type phcParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

// parsePHC splits an argon2id PHC string into its parameters, salt and key.
func parsePHC(phc string) (phcParams, []byte, []byte, error) {
	invalid := func(reason string) (phcParams, []byte, []byte, error) {
		return phcParams{}, nil, nil, fmt.Errorf("invalid password hash: %s", reason)
	}

	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return invalid("it is not an argon2id PHC string")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return invalid("unsupported argon2 version")
	}

	var params phcParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memory, &params.iterations, &params.parallelism); err != nil {
		return invalid("malformed parameters")
	}
	if params.memory < 8 || params.memory > maxArgonMemory {
		return invalid("memory cost out of range")
	}
	if params.iterations < 1 {
		return invalid("time cost out of range")
	}
	if params.parallelism < 1 {
		return invalid("parallelism out of range")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return invalid("malformed salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) < 16 {
		return invalid("malformed key")
	}

	return params, salt, key, nil
}

// argonKey computes an argon2id key while holding one of the hash slots.
func argonKey(password string, salt []byte, memory, iterations uint32, parallelism uint8, length uint32) []byte {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()

	return argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, length)
}

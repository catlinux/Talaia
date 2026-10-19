package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestHashPasswordIsAnArgon2idPHCString(t *testing.T) {
	phc := HashPassword("correct horse battery staple")

	if !strings.HasPrefix(phc, "$argon2id$v=19$m=65536,t=3,p=1$") {
		t.Fatalf("hash = %q, want the argon2id parameters of the design", phc)
	}

	params, salt, key, err := parsePHC(phc)
	if err != nil {
		t.Fatalf("parse hash: %v", err)
	}
	if params.memory != argonMemory || params.iterations != argonIterations || params.parallelism != argonParallelism {
		t.Errorf("parameters = %+v, want the design values", params)
	}
	if len(salt) != argonSaltLength {
		t.Errorf("salt length = %d, want %d", len(salt), argonSaltLength)
	}
	if len(key) != int(argonKeyLength) {
		t.Errorf("key length = %d, want %d", len(key), argonKeyLength)
	}

	// The design asks for the raw standard base64 alphabet, without padding.
	parts := strings.Split(phc, "$")
	for _, part := range parts[4:] {
		if strings.ContainsAny(part, "=-_") {
			t.Errorf("base64 part %q is padded or urlsafe", part)
		}
	}
}

func TestHashPasswordUsesARandomSalt(t *testing.T) {
	first := HashPassword("the same password")
	second := HashPassword("the same password")

	if first == second {
		t.Error("two hashes of the same password must not be identical")
	}
}

func TestVerifyPassword(t *testing.T) {
	const password = "correct horse battery staple"
	phc := HashPassword(password)

	ok, err := VerifyPassword(phc, password)
	if err != nil {
		t.Fatalf("verify the right password: %v", err)
	}
	if !ok {
		t.Fatal("the right password must verify")
	}

	ok, err = VerifyPassword(phc, password+"!")
	if err != nil {
		t.Fatalf("verify a wrong password: %v", err)
	}
	if ok {
		t.Error("a wrong password must not verify")
	}
}

func TestVerifyPasswordReadsTheParametersFromTheString(t *testing.T) {
	// A hash written with other costs keeps working: the verifier must not
	// assume the current constants.
	salt := []byte("0123456789abcdef")
	key := argonKey("a password hashed long ago", salt, 8*1024, 1, 1, 32)
	phc := "$argon2id$v=19$m=8192,t=1,p=1$" +
		base64.RawStdEncoding.EncodeToString(salt) + "$" +
		base64.RawStdEncoding.EncodeToString(key)

	if ok, err := VerifyPassword(phc, "a password hashed long ago"); err != nil || !ok {
		t.Fatalf("VerifyPassword = %v, %v, want true", ok, err)
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	tests := []struct {
		name string
		phc  string
	}{
		{name: "empty", phc: ""},
		{name: "not a hash", phc: "not a hash"},
		{name: "another algorithm", phc: "$argon2i$v=19$m=65536,t=3,p=1$c2FsdA$aGFzaGhhc2hoYXNoaGFzaA"},
		{name: "another version", phc: "$argon2id$v=16$m=65536,t=3,p=1$c2FsdA$aGFzaGhhc2hoYXNoaGFzaA"},
		{name: "no memory", phc: "$argon2id$v=19$m=0,t=3,p=1$c2FsdA$aGFzaGhhc2hoYXNoaGFzaA"},
		{name: "too much memory", phc: "$argon2id$v=19$m=4194304,t=3,p=1$c2FsdA$aGFzaGhhc2hoYXNoaGFzaA"},
		{name: "no iterations", phc: "$argon2id$v=19$m=65536,t=0,p=1$c2FsdA$aGFzaGhhc2hoYXNoaGFzaA"},
		{name: "no parallelism", phc: "$argon2id$v=19$m=65536,t=3,p=0$c2FsdA$aGFzaGhhc2hoYXNoaGFzaA"},
		{name: "salt not base64", phc: "$argon2id$v=19$m=65536,t=3,p=1$not base64$aGFzaGhhc2hoYXNoaGFzaA"},
		{name: "empty salt", phc: "$argon2id$v=19$m=65536,t=3,p=1$$aGFzaGhhc2hoYXNoaGFzaA"},
		{name: "key not base64", phc: "$argon2id$v=19$m=65536,t=3,p=1$c2FsdA$***"},
		{name: "short key", phc: "$argon2id$v=19$m=65536,t=3,p=1$c2FsdA$aGFzaA"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, err := VerifyPassword(tt.phc, "whatever")
			if err == nil {
				t.Fatalf("VerifyPassword(%q) = %v, want an error", tt.phc, ok)
			}
		})
	}
}

func TestValidatePasswordPolicy(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  string
	}{
		{name: "eleven characters", password: strings.Repeat("a", 11), wantErr: "at least 12"},
		{name: "twelve characters", password: strings.Repeat("a", 12)},
		{name: "twelve multi-byte runes", password: strings.Repeat("é", 12)},
		{name: "two hundred and fifty-six characters", password: strings.Repeat("a", 256)},
		{name: "two hundred and fifty-seven characters", password: strings.Repeat("a", 257), wantErr: "at most 256"},
		{name: "blank", password: strings.Repeat(" ", 12), wantErr: "must not be blank"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePasswordPolicy(tt.password)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestHashPasswordRunsAtMostTwoAtATime(t *testing.T) {
	// Fill both slots: the next hash has to wait for one of them.
	hashSlots <- struct{}{}
	hashSlots <- struct{}{}
	held := 2
	defer func() {
		for ; held > 0; held-- {
			<-hashSlots
		}
	}()

	done := make(chan struct{})
	go func() {
		HashPassword("a password that has to queue")
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("a third hash ran while both slots were taken")
	case <-time.After(50 * time.Millisecond):
	}

	// Releasing one slot lets the queued hash run.
	<-hashSlots
	held--
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the queued hash did not run after a slot was released")
	}

	<-hashSlots
	held--
}

func TestDummyVerifyTakesARealVerification(t *testing.T) {
	phc := HashPassword("known password")

	// Warm both paths so the comparison is not distorted by their first call.
	_, _ = VerifyPassword(phc, "warm up")
	DummyVerify("warm up")

	start := time.Now()
	ok, err := VerifyPassword(phc, "wrong password")
	real := time.Since(start)
	if err != nil || ok {
		t.Fatalf("VerifyPassword = %v, %v, want false without error", ok, err)
	}

	start = time.Now()
	DummyVerify("wrong password")
	dummy := time.Since(start)

	if real < time.Millisecond || dummy < time.Millisecond {
		t.Fatalf("the verifications are too fast to mean anything: real=%s dummy=%s", real, dummy)
	}
	if dummy > 4*real || real > 4*dummy {
		t.Errorf("DummyVerify takes %s but a real verification takes %s", dummy, real)
	}
}

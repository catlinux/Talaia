package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadOrCreateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.key")

	key, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if len(key) != secretKeyLength {
		t.Fatalf("key length = %d, want %d", len(key), secretKeyLength)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != keyFileMode {
		t.Errorf("key mode = %o, want %o", info.Mode().Perm(), keyFileMode)
	}

	// The second call reads the same key.
	again, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	if !bytes.Equal(again, key) {
		t.Error("the key changed between calls")
	}

	// A file of the wrong size is an error, not a new key.
	badPath := filepath.Join(t.TempDir(), "short.key")
	if err := os.WriteFile(badPath, []byte("too short"), keyFileMode); err != nil {
		t.Fatalf("write short key: %v", err)
	}
	if _, err := LoadOrCreateKey(badPath); err == nil {
		t.Error("expected an error for a file of the wrong size")
	}
}

func TestLoadOrCreateKeyTightensTheMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix modes")
	}

	path := filepath.Join(t.TempDir(), "loose.key")
	if err := os.WriteFile(path, bytes.Repeat([]byte{1}, secretKeyLength), 0o644); err != nil {
		t.Fatalf("write key: %v", err)
	}

	if _, err := LoadOrCreateKey(path); err != nil {
		t.Fatalf("load key: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if info.Mode().Perm() != keyFileMode {
		t.Errorf("key mode = %o, want %o", info.Mode().Perm(), keyFileMode)
	}
}

func TestNewSecretBox(t *testing.T) {
	if _, err := NewSecretBox(make([]byte, secretKeyLength-1)); err == nil {
		t.Error("expected an error for a short key")
	}
	if _, err := NewSecretBox(nil); err == nil {
		t.Error("expected an error for a missing key")
	}
}

func TestSecretBoxSealAndOpen(t *testing.T) {
	key := bytes.Repeat([]byte{7}, secretKeyLength)
	box, err := NewSecretBox(key)
	if err != nil {
		t.Fatalf("new box: %v", err)
	}

	plaintext := []byte("JBSWY3DPEHPK3PXP")
	sealed := box.Seal(plaintext)
	if len(sealed) != nonceLength+len(plaintext)+box.aead.Overhead() {
		t.Errorf("sealed length = %d, want nonce and ciphertext", len(sealed))
	}

	opened, err := box.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Errorf("opened = %q, want %q", opened, plaintext)
	}

	// The nonce is random, so sealing twice gives different values.
	other := box.Seal(plaintext)
	if bytes.Equal(sealed[:nonceLength], other[:nonceLength]) {
		t.Error("two seals reused the nonce")
	}
	if bytes.Equal(sealed, other) {
		t.Error("two seals produced the same value")
	}
}

func TestSecretBoxOpenRejectsAlteredValues(t *testing.T) {
	box, err := NewSecretBox(bytes.Repeat([]byte{9}, secretKeyLength))
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	sealed := box.Seal([]byte("the totp secret"))

	tests := []struct {
		name  string
		value []byte
	}{
		{name: "tampered ciphertext", value: func() []byte {
			altered := bytes.Clone(sealed)
			altered[len(altered)-1] ^= 0x01
			return altered
		}()},
		{name: "tampered nonce", value: func() []byte {
			altered := bytes.Clone(sealed)
			altered[0] ^= 0x01
			return altered
		}()},
		{name: "truncated", value: sealed[:nonceLength]},
		{name: "empty", value: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := box.Open(tt.value); err == nil {
				t.Error("Open accepted an altered value")
			}
		})
	}

	// Another key cannot open it either.
	otherBox, err := NewSecretBox(bytes.Repeat([]byte{8}, secretKeyLength))
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	if _, err := otherBox.Open(sealed); err == nil {
		t.Error("another key opened the value")
	}
}

func TestSealWithContextOnlyOpensWithTheSameContext(t *testing.T) {
	box, err := NewSecretBox(bytes.Repeat([]byte{7}, secretKeyLength))
	if err != nil {
		t.Fatal(err)
	}
	sealed := box.SealWithContext([]byte("output"), []byte("session|1"))

	if got, err := box.OpenWithContext(sealed, []byte("session|1")); err != nil || string(got) != "output" {
		t.Fatalf("OpenWithContext = %q, %v", got, err)
	}
	if _, err := box.OpenWithContext(sealed, []byte("session|2")); err == nil {
		t.Error("a value sealed for one position opened at another")
	}
	if _, err := box.Open(sealed); err == nil {
		t.Error("a value sealed with a context opened without it")
	}
}

package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// secretKeyLength is the size of the AES-256 key.
	secretKeyLength = 32
	// nonceLength is the size of the GCM nonce prepended to every value.
	nonceLength = 12

	// keyFileMode and keyDirMode protect the only key that can decrypt the TOTP
	// secrets.
	keyFileMode os.FileMode = 0o600
	keyDirMode  os.FileMode = 0o700
)

// LoadOrCreateKey returns the 32-byte key stored at path, creating it with mode
// 0600 on first use. It fails when the file has another size.
func LoadOrCreateKey(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("secrets key path is required")
	}

	key, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(key) != secretKeyLength {
			return nil, fmt.Errorf("%s must hold %d bytes, got %d", path, secretKeyLength, len(key))
		}
		// The file may come from an installation with a looser mode.
		if err := os.Chmod(path, keyFileMode); err != nil {
			return nil, fmt.Errorf("set mode of %s: %w", path, err)
		}
		return key, nil

	case errors.Is(err, os.ErrNotExist):
		return createKey(path)

	default:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
}

// createKey writes a new random key, never replacing an existing one.
func createKey(path string) ([]byte, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, keyDirMode); err != nil {
			return nil, fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	key := make([]byte, secretKeyLength)
	rand.Read(key)

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, keyFileMode)
	if errors.Is(err, os.ErrExist) {
		// Another process created the key first: use its value.
		return LoadOrCreateKey(path)
	}
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := file.Write(key); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close %s: %w", path, err)
	}
	return key, nil
}

// SecretBox encrypts small secrets with AES-256-GCM.
type SecretBox struct {
	aead cipher.AEAD
}

// NewSecretBox returns a box that uses the given 32-byte key.
func NewSecretBox(key []byte) (*SecretBox, error) {
	if len(key) != secretKeyLength {
		return nil, fmt.Errorf("the secrets key must be %d bytes, got %d", secretKeyLength, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	return &SecretBox{aead: aead}, nil
}

// Seal encrypts plaintext and returns the random nonce followed by the
// ciphertext.
func (b *SecretBox) Seal(plaintext []byte) []byte {
	return b.SealWithContext(plaintext, nil)
}

// SealWithContext is Seal, binding the value to bound: it only opens with the
// same bound data, so a sealed value cannot be moved to another place (for example
// another position of a recording) without being noticed.
func (b *SecretBox) SealWithContext(plaintext, bound []byte) []byte {
	nonce := make([]byte, nonceLength)
	rand.Read(nonce)

	// Seal appends the ciphertext to the nonce, so the nonce travels with it.
	return b.aead.Seal(nonce, nonce, plaintext, bound)
}

// Open decrypts a value returned by Seal. It fails when the value was tampered
// with or sealed with another key.
func (b *SecretBox) Open(sealed []byte) ([]byte, error) {
	return b.OpenWithContext(sealed, nil)
}

// OpenWithContext opens a value sealed with SealWithContext and the same data.
func (b *SecretBox) OpenWithContext(sealed, bound []byte) ([]byte, error) {
	if len(sealed) < nonceLength+b.aead.Overhead() {
		return nil, errors.New("the sealed value is too short")
	}

	nonce, ciphertext := sealed[:nonceLength], sealed[nonceLength:]
	plaintext, err := b.aead.Open(nil, nonce, ciphertext, bound)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"

	"github.com/catlinux/Talaia/core/internal/auth"
)

func TestOIDCConfig(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `DELETE FROM oidc_config`); err != nil {
		t.Fatalf("clear oidc config: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM oidc_config`)
	})

	config, err := s.GetOIDCConfig(ctx)
	if err != nil {
		t.Fatalf("get missing config: %v", err)
	}
	if config.Enabled || config.Issuer != "" || config.ClientID != "" || config.ClientSecretEnc != nil {
		t.Fatalf("missing config = %+v, want zero", config)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate secrets key: %v", err)
	}
	box, err := auth.NewSecretBox(key)
	if err != nil {
		t.Fatalf("create secret box: %v", err)
	}
	plain := []byte("client-secret-value")
	sealed := box.Seal(plain)

	saved, err := s.SaveOIDCConfig(ctx, OIDCConfig{
		Enabled: true, Issuer: "https://idp.example.com", ClientID: "talaia",
		ClientSecretEnc: sealed, ButtonLabel: "SSO", UpdatedBy: "admin",
	})
	if err != nil {
		t.Fatalf("save config: %v", err)
	}
	if !saved.Enabled || saved.Issuer != "https://idp.example.com" ||
		saved.ClientID != "talaia" || !bytes.Equal(saved.ClientSecretEnc, sealed) ||
		saved.ButtonLabel != "SSO" || saved.UpdatedBy != "admin" {
		t.Fatalf("saved config = %+v", saved)
	}
	if saved.UpdatedAt.IsZero() {
		t.Fatal("updated_at was not set")
	}

	var stored []byte
	if err := s.pool.QueryRow(ctx, `SELECT client_secret_enc FROM oidc_config WHERE id = true`).Scan(&stored); err != nil {
		t.Fatalf("read stored secret: %v", err)
	}
	if !bytes.Equal(stored, sealed) {
		t.Fatal("stored secret is not the encrypted value")
	}
	if bytes.Contains(stored, plain) {
		t.Fatal("the plaintext client secret is stored")
	}
	opened, err := box.Open(stored)
	if err != nil || !bytes.Equal(opened, plain) {
		t.Fatalf("open stored secret = %q, %v", opened, err)
	}

	// A nil secret keeps the existing one, and an explicit updatedBy wins.
	updated, err := s.SaveOIDCConfig(ctx, OIDCConfig{
		Enabled: false, Issuer: "https://idp.example.com", ClientID: "talaia",
		ButtonLabel: "Other",
	}, "operator")
	if err != nil {
		t.Fatalf("save without secret: %v", err)
	}
	if !bytes.Equal(updated.ClientSecretEnc, sealed) {
		t.Fatal("saving with a nil secret replaced the stored secret")
	}
	if updated.UpdatedBy != "operator" || updated.Enabled || updated.ButtonLabel != "Other" {
		t.Fatalf("updated config = %+v", updated)
	}

	config, err = s.GetOIDCConfig(ctx)
	if err != nil {
		t.Fatalf("get saved config: %v", err)
	}
	if !bytes.Equal(config.ClientSecretEnc, sealed) || config.Enabled || config.ButtonLabel != "Other" {
		t.Fatalf("read config = %+v", config)
	}
}

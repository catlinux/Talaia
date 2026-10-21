package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func newTestPasskey(t *testing.T, s *Store, userID, name string) Passkey {
	t.Helper()

	passkey, err := s.CreatePasskey(context.Background(), userID, deviceHash("credential"),
		deviceHash("public-key"), []byte("aaguid-16-bytes"), 0, []string{"internal"}, name, false, false)
	if err != nil {
		t.Fatalf("create passkey: %v", err)
	}
	return passkey
}

func TestPasskeyLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	passkey, err := s.CreatePasskey(ctx, user.ID, deviceHash("credential"),
		deviceHash("public-key"), []byte("aaguid-16-bytes"), 7, []string{"internal", "hybrid"},
		"laptop", true, false)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if passkey.ID == "" || passkey.UserID != user.ID || passkey.Name != "laptop" {
		t.Errorf("passkey = %+v", passkey)
	}
	if passkey.SignCount != 7 || !passkey.BackupEligible || passkey.BackupState {
		t.Errorf("passkey flags/counter = %+v", passkey)
	}
	if len(passkey.Transports) != 2 || passkey.Transports[1] != "hybrid" {
		t.Errorf("transports = %v", passkey.Transports)
	}
	if passkey.CreatedAt.IsZero() || !passkey.LastUsedAt.IsZero() {
		t.Errorf("timestamps = %+v", passkey)
	}

	got, err := s.GetPasskey(ctx, user.ID, passkey.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got.CredentialID, passkey.CredentialID) {
		t.Error("credential id changed")
	}
	byCredential, err := s.GetPasskeyByCredentialID(ctx, passkey.CredentialID)
	if err != nil || byCredential.ID != passkey.ID {
		t.Fatalf("get by credential = %+v, %v", byCredential, err)
	}

	usedAt := time.Now().Truncate(time.Millisecond)
	if err := s.UpdatePasskeyUse(ctx, user.ID, passkey.ID, 9, true, usedAt); err != nil {
		t.Fatalf("update use: %v", err)
	}
	got, err = s.GetPasskey(ctx, user.ID, passkey.ID)
	if err != nil {
		t.Fatalf("get after use: %v", err)
	}
	if got.SignCount != 9 || !got.BackupState || !got.LastUsedAt.Equal(usedAt) {
		t.Errorf("used passkey = %+v", got)
	}

	list, err := s.ListPasskeys(ctx, user.ID)
	if err != nil || len(list) != 1 || list[0].ID != passkey.ID {
		t.Fatalf("list = %+v, %v", list, err)
	}

	if err := s.DeletePasskey(ctx, user.ID, passkey.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.GetPasskey(ctx, user.ID, passkey.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted passkey is readable: %v", err)
	}
	if err := s.DeletePasskey(ctx, user.ID, passkey.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}

func TestPasskeyLimitAndDuplicate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	for i := 0; i < MaxPasskeys; i++ {
		newTestPasskey(t, s, user.ID, fmt.Sprintf("passkey-%d", i))
	}
	if _, err := s.CreatePasskey(ctx, user.ID, deviceHash("credential"), deviceHash("public-key"),
		[]byte("aaguid-16-bytes"), 0, nil, "one too many", false, false); !errors.Is(err, ErrPasskeyLimit) {
		t.Fatalf("eleventh passkey = %v, want ErrPasskeyLimit", err)
	}

	// Duplicate credential IDs are refused without exposing the credential.
	passkeys, err := s.ListPasskeys(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePasskey(ctx, user.ID, passkeys[len(passkeys)-1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePasskey(ctx, user.ID, passkeys[0].CredentialID, deviceHash("public-key"),
		[]byte("aaguid-16-bytes"), 0, nil, "duplicate", false, false); !errors.Is(err, ErrPasskeyExists) {
		t.Fatalf("duplicate credential = %v, want ErrPasskeyExists", err)
	}
}

func TestPasskeysArePerUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	otherName := fmt.Sprintf("other-passkeys-%d", time.Now().UnixNano())
	var otherID string
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO app_user (username, password_hash) VALUES ($1, 'pin-hash') RETURNING id`,
		otherName).Scan(&otherID); err != nil {
		t.Fatalf("create the second user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM app_user WHERE id = $1`, otherID)
	})
	otherPasskey := newTestPasskey(t, s, otherID, "other laptop")

	if _, err := s.GetPasskey(ctx, user.ID, otherPasskey.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user's passkey was readable: %v", err)
	}
	if err := s.DeletePasskey(ctx, user.ID, otherPasskey.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another user's passkey was deleted: %v", err)
	}
	if _, err := s.GetPasskey(ctx, otherID, otherPasskey.ID); err != nil {
		t.Errorf("the owner lost the passkey: %v", err)
	}
}

func TestWebAuthnCeremonies(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)
	data := []byte(`{"challenge":"test-challenge","userVerification":"required"}`)

	id, err := s.CreateWebAuthnCeremony(ctx, user.ID, "register", data, time.Now().Add(5*time.Minute))
	if err != nil {
		t.Fatalf("create ceremony: %v", err)
	}
	ceremony, err := s.TakeWebAuthnCeremony(ctx, id, "register")
	if err != nil {
		t.Fatalf("take ceremony: %v", err)
	}
	if ceremony.ID != id || ceremony.UserID != user.ID || ceremony.Kind != "register" {
		t.Errorf("ceremony = %+v", ceremony)
	}
	var got, want map[string]any
	if err := json.Unmarshal(ceremony.SessionData, &got); err != nil {
		t.Fatalf("decode stored session data: %v", err)
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatalf("decode expected session data: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("session data = %s, want %s", ceremony.SessionData, data)
	}
	if _, err := s.TakeWebAuthnCeremony(ctx, id, "register"); !errors.Is(err, ErrNotFound) {
		t.Errorf("replayed ceremony = %v, want ErrNotFound", err)
	}

	if id, err = s.CreateWebAuthnCeremony(ctx, "", "login", data, time.Now().Add(5*time.Minute)); err != nil {
		t.Fatalf("create login ceremony: %v", err)
	}
	if ceremony, err = s.TakeWebAuthnCeremony(ctx, id, "login"); err != nil || ceremony.UserID != "" {
		t.Errorf("login ceremony = %+v, %v", ceremony, err)
	}

	if _, err := s.CreateWebAuthnCeremony(ctx, user.ID, "register", nil, time.Now()); err == nil {
		t.Error("empty session data was accepted")
	}
	if _, err := s.CreateWebAuthnCeremony(ctx, user.ID, "other", data, time.Now()); err == nil {
		t.Error("invalid ceremony kind was accepted")
	}
}

func TestWebAuthnCeremonyLatestExpiryAndPurge(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)
	data := []byte(`{"challenge":"x"}`)

	old, err := s.CreateWebAuthnCeremony(ctx, user.ID, "register", data, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	newer, err := s.CreateWebAuthnCeremony(ctx, user.ID, "register", data, time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	ceremony, err := s.TakeLatestWebAuthnCeremony(ctx, user.ID, "register")
	if err != nil {
		t.Fatalf("take latest: %v", err)
	}
	if ceremony.ID != newer {
		t.Errorf("latest = %q, want %q", ceremony.ID, newer)
	}
	if _, err := s.TakeWebAuthnCeremony(ctx, newer, "register"); !errors.Is(err, ErrNotFound) {
		t.Error("the latest ceremony was not used once")
	}
	if ceremony, err = s.TakeLatestWebAuthnCeremony(ctx, user.ID, "register"); err != nil || ceremony.ID != old {
		t.Errorf("remaining ceremony = %+v, %v, want %q", ceremony, err, old)
	}

	expired, err := s.CreateWebAuthnCeremony(ctx, user.ID, "register", data, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.TakeWebAuthnCeremony(ctx, expired, "register"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired ceremony = %v, want ErrNotFound", err)
	}
	if _, err := s.TakeLatestWebAuthnCeremony(ctx, user.ID, "register"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired latest ceremony = %v, want ErrNotFound", err)
	}

	removed, err := s.PurgeExpiredWebAuthnCeremonies(ctx)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 1 {
		t.Errorf("purged = %d, want 1", removed)
	}
}

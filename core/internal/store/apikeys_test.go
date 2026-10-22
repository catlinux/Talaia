package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAPIKeyLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newRBACTestUser(t, s, "apikey")

	keyID := uniqueTestName("key")
	secretHash := []byte(uniqueTestName("secret"))
	key, err := s.CreateAPIKey(ctx, keyID, secretHash, "CLI key", user.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	if key.KeyID != keyID || key.OwnerID != user.ID || string(key.SecretHash) != string(secretHash) {
		t.Fatalf("created key = %+v", key)
	}

	looked, err := s.LookupAPIKey(ctx, keyID)
	if err != nil {
		t.Fatalf("lookup api key: %v", err)
	}
	if looked.ID != key.ID || looked.ExpiresAt.IsZero() {
		t.Fatalf("looked key = %+v", looked)
	}
	if err := s.TouchAPIKey(ctx, key.KeyID); err != nil {
		t.Fatalf("touch api key: %v", err)
	}
	looked, err = s.LookupAPIKey(ctx, keyID)
	if err != nil || looked.LastUsedAt.IsZero() {
		t.Fatalf("touched key = %+v, %v", looked, err)
	}
	ownerKeys, err := s.ListAPIKeysByOwner(ctx, user.ID)
	if err != nil || len(ownerKeys) != 1 || ownerKeys[0].ID != key.ID {
		t.Fatalf("owner keys = %+v, %v", ownerKeys, err)
	}
	allKeys, err := s.ListAPIKeys(ctx)
	if err != nil || len(allKeys) == 0 {
		t.Fatalf("all keys = %+v, %v", allKeys, err)
	}

	if err := s.RevokeAPIKey(ctx, key.KeyID); err != nil {
		t.Fatalf("revoke api key: %v", err)
	}
	if err := s.RevokeAPIKey(ctx, key.ID); err != nil {
		t.Fatalf("revoke api key by row id: %v", err)
	}
	if _, err := s.LookupActiveAPIKey(ctx, keyID); !errors.Is(err, ErrAPIKeyInactive) {
		t.Fatalf("active lookup after revoke error = %v, want ErrAPIKeyInactive", err)
	}

	expiring, err := s.CreateAPIKey(ctx, uniqueTestName("expired-key"), []byte(uniqueTestName("expired-secret")),
		"Expired", user.ID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("create expiring key: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE api_key SET expires_at = now() - interval '1 minute' WHERE id = $1`, expiring.ID); err != nil {
		t.Fatalf("expire key: %v", err)
	}
	if _, err := s.LookupActiveAPIKey(ctx, expiring.KeyID); !errors.Is(err, ErrAPIKeyInactive) {
		t.Fatalf("expired lookup error = %v, want ErrAPIKeyInactive", err)
	}

	if _, err := s.CreateAPIKey(ctx, keyID, []byte(uniqueTestName("secret")),
		"Duplicate", user.ID, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("duplicate api key id was accepted")
	}
	if _, err := s.CreateAPIKey(ctx, uniqueTestName("past-key"), []byte(uniqueTestName("secret")),
		"Past", user.ID, time.Now().Add(-time.Hour)); !errors.Is(err, ErrAPIKeyLifetime) {
		t.Fatalf("past expiry error = %v, want ErrAPIKeyLifetime", err)
	}
}

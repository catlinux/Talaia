package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestStore opens the test database and applies the migrations. It skips the
// test when TALAIA_TEST_DATABASE_URL is not set.
func newTestStore(t *testing.T) *Store {
	t.Helper()

	url := os.Getenv("TALAIA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TALAIA_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return s
}

// testName returns a name no other run uses and registers the cleanup of the
// rows this test creates, without touching anything else.
func testName(t *testing.T, s *Store) string {
	t.Helper()

	name := fmt.Sprintf("test-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = s.pool.Exec(ctx, `DELETE FROM enrollment_token WHERE server_name = $1`, name)
		_, _ = s.pool.Exec(ctx, `DELETE FROM server WHERE name = $1`, name)
	})
	return name
}

func randomToken(t *testing.T) string {
	t.Helper()

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
}

func TestEnrollmentTokenHappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	name := testName(t, s)

	token, err := s.CreateEnrollmentToken(ctx, name, 15*time.Minute)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if !strings.HasPrefix(token, tokenPrefix) {
		t.Errorf("token = %q, want the %s prefix", token, tokenPrefix)
	}

	// Only the SHA-256 of the token goes to the database.
	var stored string
	if err := s.pool.QueryRow(ctx,
		`SELECT encode(token_hash, 'hex') FROM enrollment_token WHERE server_name = $1`, name).Scan(&stored); err != nil {
		t.Fatalf("read stored hash: %v", err)
	}
	sum := sha256.Sum256([]byte(token))
	if stored != hex.EncodeToString(sum[:]) {
		t.Errorf("stored value = %q, want the sha-256 of the token", stored)
	}
	if strings.Contains(stored, strings.TrimPrefix(token, tokenPrefix)) {
		t.Error("the plaintext token appears in the table")
	}

	serverName, err := s.ConsumeEnrollmentToken(ctx, token)
	if err != nil {
		t.Fatalf("consume token: %v", err)
	}
	if serverName != name {
		t.Errorf("server name = %q, want %q", serverName, name)
	}

	// A token is consumed only once.
	if _, err := s.ConsumeEnrollmentToken(ctx, token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("second consume = %v, want ErrInvalidToken", err)
	}
}

func TestEnrollmentTokenUnknownAndExpired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	name := testName(t, s)

	if _, err := s.ConsumeEnrollmentToken(ctx, tokenPrefix+"unknown"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown token = %v, want ErrInvalidToken", err)
	}

	// CreateEnrollmentToken only accepts a positive ttl, so the expired token
	// is inserted directly.
	token := randomToken(t)
	sum := sha256.Sum256([]byte(token))
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO enrollment_token (token_hash, server_name, expires_at) VALUES ($1, $2, now() - interval '1 minute')`,
		sum[:], name); err != nil {
		t.Fatalf("insert expired token: %v", err)
	}

	if _, err := s.ConsumeEnrollmentToken(ctx, token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("expired token = %v, want ErrInvalidToken", err)
	}
}

func TestEnrollmentTokenConcurrentUse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	name := testName(t, s)

	token, err := s.CreateEnrollmentToken(ctx, name, 15*time.Minute)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ConsumeEnrollmentToken(ctx, token)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrInvalidToken):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful consumes = %d, want exactly 1", successes)
	}
}

func TestEnrollmentTokenRules(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateEnrollmentToken(ctx, "   ", time.Minute); err == nil {
		t.Error("expected an error for an empty server name")
	}
	if _, err := s.CreateEnrollmentToken(ctx, "test-name", 0); err == nil {
		t.Error("expected an error for a non positive ttl")
	}

	name := testName(t, s)
	if _, err := s.CreateServer(ctx, name); err != nil {
		t.Fatalf("create server: %v", err)
	}
	if _, err := s.CreateEnrollmentToken(ctx, name, time.Minute); err == nil {
		t.Error("expected an error for an already enrolled server")
	}

	// A name with a pending token cannot have a second one either.
	pending := testName(t, s)
	if _, err := s.CreateEnrollmentToken(ctx, pending, time.Minute); err != nil {
		t.Fatalf("create pending token: %v", err)
	}
	if _, err := s.CreateEnrollmentToken(ctx, pending, time.Minute); err == nil {
		t.Error("expected an error for a server name with a pending token")
	}
}

func TestServerLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	name := testName(t, s)

	id, err := s.CreateServer(ctx, name)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	server, err := s.GetServer(ctx, id)
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if server.Name != name {
		t.Errorf("name = %q, want %q", server.Name, name)
	}
	if server.CreatedAt.IsZero() {
		t.Error("created_at is zero")
	}
	if !server.CertNotAfter.IsZero() || !server.LastSeen.IsZero() || !server.RevokedAt.IsZero() {
		t.Errorf("a new server must have no certificate, last_seen or revoked_at: %+v", server)
	}

	notAfter := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Millisecond)
	if err := s.SetServerCertificate(ctx, id, "abcdef0123", notAfter); err != nil {
		t.Fatalf("set certificate: %v", err)
	}
	if err := s.TouchServer(ctx, id); err != nil {
		t.Fatalf("touch server: %v", err)
	}

	server, err = s.GetServer(ctx, id)
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if server.CertSerial != "abcdef0123" {
		t.Errorf("cert serial = %q", server.CertSerial)
	}
	if !server.CertNotAfter.Equal(notAfter) {
		t.Errorf("cert not after = %s, want %s", server.CertNotAfter, notAfter)
	}
	if server.LastSeen.IsZero() {
		t.Error("last_seen is zero after touching the server")
	}

	if err := s.RevokeServer(ctx, id); err != nil {
		t.Fatalf("revoke server: %v", err)
	}
	server, err = s.GetServer(ctx, id)
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if server.RevokedAt.IsZero() {
		t.Error("revoked_at is zero after revoking")
	}

	// Revoking twice is not an error.
	if err := s.RevokeServer(ctx, id); err != nil {
		t.Errorf("second revoke: %v", err)
	}

	// The name is unique.
	if _, err := s.CreateServer(ctx, name); err == nil {
		t.Error("expected an error when creating a server with a duplicate name")
	}
}

func TestServerNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	missing := "00000000-0000-0000-0000-000000000000"

	if _, err := s.GetServer(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("get = %v, want ErrNotFound", err)
	}
	if err := s.RevokeServer(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoke = %v, want ErrNotFound", err)
	}
	if err := s.TouchServer(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("touch = %v, want ErrNotFound", err)
	}
	if err := s.SetServerCertificate(ctx, missing, "x", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Errorf("set certificate = %v, want ErrNotFound", err)
	}
}

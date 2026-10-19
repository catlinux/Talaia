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

func TestAcceptsSerial(t *testing.T) {
	server := Server{CertSerial: "aa", PrevCertSerial: "bb"}

	tests := []struct {
		name   string
		serial string
		want   bool
	}{
		{name: "current serial", serial: "aa", want: true},
		{name: "previous serial", serial: "bb", want: true},
		{name: "foreign serial", serial: "cc", want: false},
		{name: "empty serial", serial: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := server.AcceptsSerial(tt.serial); got != tt.want {
				t.Errorf("AcceptsSerial(%q) = %v, want %v", tt.serial, got, tt.want)
			}
		})
	}

	// A server that never renewed only accepts its current serial.
	fresh := Server{CertSerial: "aa"}
	if fresh.AcceptsSerial("") || !fresh.AcceptsSerial("aa") || fresh.AcceptsSerial("bb") {
		t.Error("a server without a previous serial must only accept its current one")
	}
}

// testSigner returns a SignFunc that issues a certificate with a fixed serial.
func testSigner(serial string) SignFunc {
	return func(serverID string) ([]byte, string, time.Time, error) {
		return []byte("cert-for-" + serverID), serial, time.Now().Add(90 * 24 * time.Hour).Truncate(time.Millisecond), nil
	}
}

func TestEnrollServerCommitsEverything(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	name := testName(t, s)

	token, err := s.CreateEnrollmentToken(ctx, name, 15*time.Minute)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	serverID, certPEM, err := s.EnrollServer(ctx, token, testSigner("abcdef0123"))
	if err != nil {
		t.Fatalf("enroll server: %v", err)
	}
	if string(certPEM) != "cert-for-"+serverID {
		t.Errorf("certificate = %q, want the one the signer returned", certPEM)
	}

	server, err := s.GetServer(ctx, serverID)
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if server.Name != name || server.CertSerial != "abcdef0123" {
		t.Errorf("server = %+v, want name %q and the issued serial", server, name)
	}
	if server.CertNotAfter.IsZero() {
		t.Error("cert_not_after is zero after enrolling")
	}
	if server.PrevCertSerial != "" {
		t.Errorf("prev_cert_serial = %q, want empty after the first enrollment", server.PrevCertSerial)
	}

	// The token is spent.
	if _, err := s.ConsumeEnrollmentToken(ctx, token); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("consume after enrolling = %v, want ErrInvalidToken", err)
	}
}

func TestEnrollServerRollsBackWhenSigningFails(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	name := testName(t, s)

	token, err := s.CreateEnrollmentToken(ctx, name, 15*time.Minute)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	boom := errors.New("the ca is down")
	_, _, err = s.EnrollServer(ctx, token, func(string) ([]byte, string, time.Time, error) {
		return nil, "", time.Time{}, boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("enroll = %v, want the signing error", err)
	}

	// The rollback keeps the token usable and leaves no server row behind.
	if _, err := s.ConsumeEnrollmentToken(ctx, token); err != nil {
		t.Fatalf("a failed enrollment spent the token: %v", err)
	}
	var servers int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM server WHERE name = $1`, name).Scan(&servers); err != nil {
		t.Fatalf("count servers: %v", err)
	}
	if servers != 0 {
		t.Errorf("orphan server rows = %d, want 0", servers)
	}
}

func TestEnrollServerInvalidToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, _, err := s.EnrollServer(ctx, randomToken(t), testSigner("aa")); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("unknown token = %v, want ErrInvalidToken", err)
	}
}

func TestEnrollServerConcurrentUse(t *testing.T) {
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
			_, _, err := s.EnrollServer(ctx, token, testSigner("aa"))
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
		t.Fatalf("successful enrollments = %d, want exactly 1", successes)
	}
}

func TestRotateServerCertificate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	name := testName(t, s)

	id, err := s.CreateServer(ctx, name)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := s.SetServerCertificate(ctx, id, "aa", time.Now()); err != nil {
		t.Fatalf("set certificate: %v", err)
	}

	notAfter := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Millisecond)
	if err := s.RotateServerCertificate(ctx, id, "bb", notAfter); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	server, err := s.GetServer(ctx, id)
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if server.CertSerial != "bb" || server.PrevCertSerial != "aa" {
		t.Errorf("serials = %q/%q, want bb/aa", server.CertSerial, server.PrevCertSerial)
	}
	if !server.CertNotAfter.Equal(notAfter) {
		t.Errorf("cert not after = %s, want %s", server.CertNotAfter, notAfter)
	}
	if !server.AcceptsSerial("aa") || !server.AcceptsSerial("bb") || server.AcceptsSerial("cc") {
		t.Error("a rotated server must accept the current and the previous serial only")
	}

	missing := "00000000-0000-0000-0000-000000000000"
	if err := s.RotateServerCertificate(ctx, missing, "cc", notAfter); !errors.Is(err, ErrNotFound) {
		t.Errorf("rotate missing = %v, want ErrNotFound", err)
	}
}

func TestListServers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// The shared test database holds servers of other suites: this test checks
	// its own three rows and the order of the whole list.
	base := fmt.Sprintf("test-list-%d", time.Now().UnixNano())
	names := []string{base + "-b", base + "-a", base + "-c"}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		for _, name := range names {
			_, _ = s.pool.Exec(cleanupCtx, `DELETE FROM server WHERE name = $1`, name)
		}
	})

	ids := make(map[string]string, len(names))
	for _, name := range names {
		id, err := s.CreateServer(ctx, name)
		if err != nil {
			t.Fatalf("create server %q: %v", name, err)
		}
		ids[name] = id
	}
	if err := s.RevokeServer(ctx, ids[names[2]]); err != nil {
		t.Fatalf("revoke server: %v", err)
	}

	servers, err := s.ListServers(ctx)
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}

	mine := make([]Server, 0, len(names))
	for _, server := range servers {
		for _, name := range names {
			if server.Name == name {
				mine = append(mine, server)
			}
		}
	}
	if len(mine) != len(names) {
		t.Fatalf("listed %d of the test servers, want %d", len(mine), len(names))
	}

	// Ordered by name, whichever order they were created in.
	for i, want := range []string{base + "-a", base + "-b", base + "-c"} {
		if mine[i].Name != want {
			t.Errorf("server %d = %q, want %q", i, mine[i].Name, want)
		}
	}
	if mine[0].ID != ids[names[1]] || mine[0].CreatedAt.IsZero() {
		t.Errorf("the first server = %+v, want the created row", mine[0])
	}
	if !mine[1].RevokedAt.IsZero() {
		t.Errorf("the middle server is revoked")
	}
	if mine[2].RevokedAt.IsZero() {
		t.Errorf("the revoked server is not flagged")
	}

	// The full list is ordered by name too.
	for i := 1; i < len(servers); i++ {
		if servers[i-1].Name > servers[i].Name {
			t.Fatalf("the list is not ordered by name: %q before %q", servers[i-1].Name, servers[i].Name)
		}
	}
}

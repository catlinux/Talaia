package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrInvalidToken is returned for an unknown, already used or expired
// enrollment token, without telling them apart.
var ErrInvalidToken = errors.New("invalid enrollment token")

// ErrNotFound is returned when the requested row does not exist.
var ErrNotFound = errors.New("not found")

// tokenPrefix identifies an enrollment token.
const tokenPrefix = "tl_enr_"

// Server is a row of the server table. The time fields use the zero value to
// mean "not set": a server without certificate, never seen or not revoked.
type Server struct {
	ID           string
	Name         string
	CertSerial   string
	CertNotAfter time.Time
	LastSeen     time.Time
	RevokedAt    time.Time
	CreatedAt    time.Time
}

// CreateEnrollmentToken creates a one-time token for serverName and returns the
// plaintext token, which is shown once and never stored: only its SHA-256 goes
// to the database.
func (s *Store) CreateEnrollmentToken(ctx context.Context, serverName string, ttl time.Duration) (string, error) {
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return "", errors.New("server name is required")
	}
	if ttl <= 0 {
		return "", errors.New("token ttl must be positive")
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)

	// A name can only have one enrollment in flight: reject it when the server
	// is already enrolled or when it still has a pending token.
	var enrolled, pending bool
	if err := s.pool.QueryRow(ctx,
		`SELECT
		   EXISTS (SELECT 1 FROM server WHERE name = $1),
		   EXISTS (SELECT 1 FROM enrollment_token WHERE server_name = $1 AND used_at IS NULL AND expires_at > now())`,
		serverName).Scan(&enrolled, &pending); err != nil {
		return "", fmt.Errorf("check server name: %w", err)
	}
	if enrolled {
		return "", fmt.Errorf("server %q is already enrolled", serverName)
	}
	if pending {
		return "", fmt.Errorf("server %q already has a pending enrollment token", serverName)
	}

	hash := sha256.Sum256([]byte(token))
	expiresAt := time.Now().Add(ttl)
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO enrollment_token (token_hash, server_name, expires_at) VALUES ($1, $2, $3)`,
		hash[:], serverName, expiresAt); err != nil {
		return "", fmt.Errorf("store enrollment token: %w", err)
	}

	return token, nil
}

// ConsumeEnrollmentToken uses the token atomically and returns the server name
// it was created for. Unknown, used and expired tokens all return
// ErrInvalidToken.
func (s *Store) ConsumeEnrollmentToken(ctx context.Context, token string) (string, error) {
	hash := sha256.Sum256([]byte(token))

	var serverName string
	err := s.pool.QueryRow(ctx,
		`UPDATE enrollment_token SET used_at = now()
		 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
		 RETURNING server_name`,
		hash[:]).Scan(&serverName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrInvalidToken
	}
	if err != nil {
		return "", fmt.Errorf("consume enrollment token: %w", err)
	}

	return serverName, nil
}

// CreateServer inserts a server and returns its id.
func (s *Store) CreateServer(ctx context.Context, name string) (string, error) {
	var id string
	if err := s.pool.QueryRow(ctx, `INSERT INTO server (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		return "", fmt.Errorf("create server %q: %w", name, err)
	}
	return id, nil
}

// SetServerCertificate records the certificate issued to a server.
func (s *Store) SetServerCertificate(ctx context.Context, id, serial string, notAfter time.Time) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE server SET cert_serial = $2, cert_not_after = $3 WHERE id = $1`, id, serial, notAfter)
	if err != nil {
		return fmt.Errorf("set server certificate: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetServer returns one server or ErrNotFound.
func (s *Store) GetServer(ctx context.Context, id string) (Server, error) {
	var (
		server       Server
		certSerial   *string
		certNotAfter *time.Time
		lastSeen     *time.Time
		revokedAt    *time.Time
	)

	err := s.pool.QueryRow(ctx,
		`SELECT id, name, cert_serial, cert_not_after, created_at, last_seen, revoked_at
		 FROM server WHERE id = $1`, id).
		Scan(&server.ID, &server.Name, &certSerial, &certNotAfter, &server.CreatedAt, &lastSeen, &revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Server{}, ErrNotFound
	}
	if err != nil {
		return Server{}, fmt.Errorf("get server %q: %w", id, err)
	}

	server.CertNotAfter = timeValue(certNotAfter)
	server.LastSeen = timeValue(lastSeen)
	server.RevokedAt = timeValue(revokedAt)
	if certSerial != nil {
		server.CertSerial = *certSerial
	}

	return server, nil
}

// RevokeServer marks a server as revoked. Revoking twice is not an error.
func (s *Store) RevokeServer(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE server SET revoked_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("revoke server %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchServer records that a server has just been seen.
func (s *Store) TouchServer(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE server SET last_seen = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("touch server %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

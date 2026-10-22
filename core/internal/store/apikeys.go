package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// DefaultAPIKeyLifetime is used when the caller does not choose an expiry.
	DefaultAPIKeyLifetime = 90 * 24 * time.Hour
	// MaxAPIKeyLifetime is the longest API key this store accepts.
	MaxAPIKeyLifetime = 365 * 24 * time.Hour
)

var (
	// ErrAPIKeyLifetime means the requested expiry is in the past or beyond
	// MaxAPIKeyLifetime.
	ErrAPIKeyLifetime = errors.New("invalid api key lifetime")
	// ErrAPIKeyInactive means the key exists but is expired or revoked.
	ErrAPIKeyInactive = errors.New("api key is expired or revoked")
)

// APIKey is a row of api_key. SecretHash is the SHA-256 of the secret; the
// clear secret is shown once by the caller and never reaches the store.
type APIKey struct {
	ID         string
	KeyID      string
	Name       string
	OwnerID    string
	SecretHash []byte
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt time.Time
	RevokedAt  time.Time
}

const apiKeyColumns = `id, key_id, name, owner_id, secret_hash, created_at,
	expires_at, last_used_at, revoked_at`

// CreateAPIKey stores a hashed API key. A zero expiry means the default 90
// days; anything beyond one year is refused.
func (s *Store) CreateAPIKey(ctx context.Context, keyID string, secretHash []byte, name, ownerID string, expiresAt time.Time) (APIKey, error) {
	keyID = strings.TrimSpace(keyID)
	name = strings.TrimSpace(name)
	if keyID == "" {
		return APIKey{}, errors.New("api key id is required")
	}
	if len(secretHash) == 0 {
		return APIKey{}, errors.New("api key secret hash is required")
	}
	if name == "" {
		return APIKey{}, errors.New("api key name is required")
	}
	if strings.TrimSpace(ownerID) == "" {
		return APIKey{}, errors.New("api key owner is required")
	}
	now := time.Now()
	if expiresAt.IsZero() {
		expiresAt = now.Add(DefaultAPIKeyLifetime)
	}
	if !expiresAt.After(now) || expiresAt.After(now.Add(MaxAPIKeyLifetime)) {
		return APIKey{}, ErrAPIKeyLifetime
	}

	key, err := scanAPIKey(s.pool.QueryRow(ctx,
		`INSERT INTO api_key (key_id, name, owner_id, secret_hash, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+apiKeyColumns,
		keyID, name, ownerID, secretHash, expiresAt))
	if err != nil {
		if isUniqueViolation(err) {
			return APIKey{}, errors.New("api key id already exists")
		}
		return APIKey{}, fmt.Errorf("create api key: %w", err)
	}
	return key, nil
}

// LookupAPIKey returns the key with that public id, including expired and
// revoked rows so the caller can choose the right rejection. It does not
// verify the secret; the caller compares SecretHash.
func (s *Store) LookupAPIKey(ctx context.Context, keyID string) (APIKey, error) {
	key, err := scanAPIKey(s.pool.QueryRow(ctx,
		`SELECT `+apiKeyColumns+` FROM api_key WHERE key_id = $1`, strings.TrimSpace(keyID)))
	if errors.Is(err, ErrNotFound) {
		return APIKey{}, ErrNotFound
	}
	if err != nil {
		return APIKey{}, fmt.Errorf("lookup api key: %w", err)
	}
	return key, nil
}

// LookupActiveAPIKey returns a key only when it is neither revoked nor
// expired. It is a convenience for callers that do not need the reason.
func (s *Store) LookupActiveAPIKey(ctx context.Context, keyID string) (APIKey, error) {
	key, err := s.LookupAPIKey(ctx, keyID)
	if err != nil {
		return APIKey{}, err
	}
	if !key.RevokedAt.IsZero() || !key.ExpiresAt.After(time.Now()) {
		return APIKey{}, ErrAPIKeyInactive
	}
	return key, nil
}

// TouchAPIKey records that a key has just authenticated a request.
func (s *Store) TouchAPIKey(ctx context.Context, keyID string, now ...time.Time) error {
	at := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		at = now[0]
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_key SET last_used_at = $2
		 WHERE (key_id = $1 OR id::text = $1) AND revoked_at IS NULL`, strings.TrimSpace(keyID), at)
	if err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAPIKeysByOwner returns the keys of one user, newest first.
func (s *Store) ListAPIKeysByOwner(ctx context.Context, ownerID string) ([]APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+apiKeyColumns+` FROM api_key
		 WHERE owner_id = $1
		 ORDER BY created_at DESC, id DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list api keys by owner: %w", err)
	}
	defer rows.Close()
	return scanAPIKeys(rows)
}

// ListAPIKeys returns every key, newest first.
func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+apiKeyColumns+` FROM api_key
		 ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()
	return scanAPIKeys(rows)
}

// RevokeAPIKey revokes a key. Revoking an already revoked key is not an error.
func (s *Store) RevokeAPIKey(ctx context.Context, keyID string) error {
	keyID = strings.TrimSpace(keyID)
	tag, err := s.pool.Exec(ctx,
		`UPDATE api_key SET revoked_at = COALESCE(revoked_at, now())
		 WHERE key_id = $1 OR id::text = $1`, keyID)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanAPIKey(row rowScanner) (APIKey, error) {
	var (
		key        APIKey
		lastUsedAt *time.Time
		revokedAt  *time.Time
	)
	if err := row.Scan(&key.ID, &key.KeyID, &key.Name, &key.OwnerID, &key.SecretHash,
		&key.CreatedAt, &key.ExpiresAt, &lastUsedAt, &revokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APIKey{}, ErrNotFound
		}
		return APIKey{}, err
	}
	if lastUsedAt != nil {
		key.LastUsedAt = *lastUsedAt
	}
	if revokedAt != nil {
		key.RevokedAt = *revokedAt
	}
	return key, nil
}

func scanAPIKeys(rows pgx.Rows) ([]APIKey, error) {
	keys := make([]APIKey, 0, 8)
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read api keys: %w", err)
	}
	return keys, nil
}

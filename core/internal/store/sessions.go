package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// StageMFAPending is the stage between the password and the second factor:
	// it may only be used on the TOTP endpoints.
	StageMFAPending = "mfa_pending"
	// StageFull is a session that passed both factors.
	StageFull = "full"
)

// validStage reports whether stage is one of the two session stages.
func validStage(stage string) bool {
	return stage == StageMFAPending || stage == StageFull
}

// Session is a row of the session table. RevokedAt uses the zero value when the
// session has not been revoked.
type Session struct {
	ID          string
	UserID      string
	Stage       string
	CreatedAt   time.Time
	LastSeenAt  time.Time
	ExpiresAt   time.Time
	RevokedAt   time.Time
	IP          string
	UserAgent   string
	MFAFailures int
}

// CreateSession inserts a session and returns its id.
func (s *Store) CreateSession(ctx context.Context, userID string, tokenHash []byte, stage string, expiresAt time.Time, ip, userAgent string) (string, error) {
	if !validStage(stage) {
		return "", fmt.Errorf("invalid session stage %q", stage)
	}
	if len(tokenHash) == 0 {
		return "", errors.New("session token hash is required")
	}
	if expiresAt.IsZero() {
		return "", errors.New("session expiry is required")
	}

	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO session (user_id, token_hash, stage, expires_at, ip, user_agent)
		 VALUES ($1, $2, $3, $4, NULLIF($5, ''), NULLIF($6, ''))
		 RETURNING id`,
		userID, tokenHash, stage, expiresAt, ip, userAgent).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return id, nil
}

// GetSessionByTokenHash returns the live session of a token hash. It returns
// ErrNotFound when the session does not exist, was revoked or has expired. The
// idle timeout is the caller's decision: LastSeenAt is exposed for that.
func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error) {
	var (
		session   Session
		revokedAt *time.Time
	)

	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, stage, created_at, last_seen_at, expires_at, revoked_at,
		        COALESCE(ip, ''), COALESCE(user_agent, ''), mfa_failures
		 FROM session
		 WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()`, tokenHash).
		Scan(&session.ID, &session.UserID, &session.Stage, &session.CreatedAt, &session.LastSeenAt,
			&session.ExpiresAt, &revokedAt, &session.IP, &session.UserAgent, &session.MFAFailures)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}

	session.RevokedAt = timeValue(revokedAt)
	return session, nil
}

// GetRevokedSessionByTokenHash returns an unexpired session only when it has
// been revoked, allowing callers to distinguish revocation from an unknown token.
func (s *Store) GetRevokedSessionByTokenHash(ctx context.Context, tokenHash []byte) (Session, error) {
	var session Session
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, last_seen_at, revoked_at FROM session
		 WHERE token_hash = $1 AND revoked_at IS NOT NULL AND expires_at > now()`, tokenHash).
		Scan(&session.ID, &session.UserID, &session.LastSeenAt, &session.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get revoked session: %w", err)
	}
	return session, nil
}

// TouchSession records activity at most once a minute. A session touched more
// recently, or one that no longer exists, is left untouched and is not an error.
func (s *Store) TouchSession(ctx context.Context, id string, now time.Time) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE session SET last_seen_at = $2
		 WHERE id = $1 AND last_seen_at < $2::timestamptz - interval '1 minute'`, id, now); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// IncrementMFAFailures counts a wrong second factor in the session and returns
// the new count.
func (s *Store) IncrementMFAFailures(ctx context.Context, id string) (int, error) {
	var failures int
	err := s.pool.QueryRow(ctx,
		`UPDATE session SET mfa_failures = mfa_failures + 1 WHERE id = $1 RETURNING mfa_failures`, id).
		Scan(&failures)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("increment mfa failures: %w", err)
	}
	return failures, nil
}

// UpgradeSession revokes the old session and creates the new one in a single
// transaction, so the token that was just used stops working at once. The new
// session inherits the user, the address and the user agent.
func (s *Store) UpgradeSession(ctx context.Context, oldID string, newTokenHash []byte, stage string, expiresAt time.Time) (string, error) {
	if !validStage(stage) {
		return "", fmt.Errorf("invalid session stage %q", stage)
	}
	if len(newTokenHash) == 0 {
		return "", errors.New("session token hash is required")
	}
	if expiresAt.IsZero() {
		return "", errors.New("session expiry is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("upgrade session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE session SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, oldID)
	if err != nil {
		return "", fmt.Errorf("revoke the old session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}

	var newID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO session (user_id, token_hash, stage, expires_at, ip, user_agent)
		 SELECT user_id, $2, $3, $4, ip, user_agent FROM session WHERE id = $1
		 RETURNING id`, oldID, newTokenHash, stage, expiresAt).Scan(&newID); err != nil {
		return "", fmt.Errorf("create the new session: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit session upgrade: %w", err)
	}
	return newID, nil
}

// RevokeSession revokes a live session.
func (s *Store) RevokeSession(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE session SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeUserSessions revokes every live session of a user except exceptID and
// returns how many it revoked. An empty exceptID revokes all of them.
func (s *Store) RevokeUserSessions(ctx context.Context, userID, exceptID string) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE session SET revoked_at = now()
		 WHERE user_id = $1 AND revoked_at IS NULL AND id::text <> $2`, userID, exceptID)
	if err != nil {
		return 0, fmt.Errorf("revoke user sessions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

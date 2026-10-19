package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// uniqueViolation is the PostgreSQL error code of a unique constraint breach.
const uniqueViolation = "23505"

// ErrUserExists is returned when the single administrator already exists or the
// username is taken.
var ErrUserExists = errors.New("user already exists")

// User is a row of the app_user table. The zero value of the optional fields
// means "not set": no TOTP secret, TOTP not enabled, never locked, and no code
// accepted yet when TOTPLastStep is zero.
type User struct {
	ID                string
	Username          string
	PasswordHash      string
	TOTPSecretEnc     []byte
	TOTPEnabledAt     time.Time
	TOTPLastStep      int64
	FailedAttempts    int
	LockedUntil       time.Time
	PasswordChangedAt time.Time
	CreatedAt         time.Time
}

// userColumns reads a user, normalising the nullable columns to Go zero values.
const userColumns = `id, username, password_hash, totp_secret_enc, totp_enabled_at,
	COALESCE(totp_last_step, 0), failed_attempts, locked_until, password_changed_at, created_at`

// CreateUser inserts the administrator. This phase has a single user, so it
// fails with ErrUserExists when any user exists or the name is taken.
func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (string, error) {
	if strings.TrimSpace(username) == "" {
		return "", errors.New("username is required")
	}
	if strings.TrimSpace(passwordHash) == "" {
		return "", errors.New("password hash is required")
	}

	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO app_user (username, password_hash)
		 SELECT $1, $2
		 WHERE NOT EXISTS (SELECT 1 FROM app_user)
		 RETURNING id`, username, passwordHash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserExists
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return "", ErrUserExists
		}
		return "", fmt.Errorf("create user: %w", err)
	}
	return id, nil
}

// GetUser returns a user by id, or ErrNotFound.
func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	user, err := queryUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM app_user WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("get user %q: %w", id, err)
	}
	return user, nil
}

// GetUserByUsername returns a user by name, or ErrNotFound.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	user, err := queryUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM app_user WHERE username = $1`, username))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("get user %q: %w", username, err)
	}
	return user, nil
}

// queryUser scans one user row, returning ErrNotFound when there is none.
func queryUser(row pgx.Row) (User, error) {
	var (
		user        User
		secretEnc   []byte
		enabledAt   *time.Time
		lockedUntil *time.Time
	)

	err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &secretEnc, &enabledAt,
		&user.TOTPLastStep, &user.FailedAttempts, &lockedUntil, &user.PasswordChangedAt, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}

	user.TOTPSecretEnc = secretEnc
	user.TOTPEnabledAt = timeValue(enabledAt)
	user.LockedUntil = timeValue(lockedUntil)
	return user, nil
}

// execUser runs an update that must touch exactly one user.
func (s *Store) execUser(ctx context.Context, what, query string, args ...any) error {
	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetTOTP stores a pending TOTP secret. It clears the enabled state and the
// replay guard, because a new secret replaces the previous one.
func (s *Store) SetTOTP(ctx context.Context, id string, secretEnc []byte) error {
	if len(secretEnc) == 0 {
		return errors.New("the encrypted totp secret is required")
	}
	return s.execUser(ctx, "set totp secret",
		`UPDATE app_user
		 SET totp_secret_enc = $2, totp_enabled_at = NULL, totp_last_step = NULL
		 WHERE id = $1`, id, secretEnc)
}

// EnableTOTP marks TOTP as enabled and records the step of the code that
// confirmed it, so that code cannot be used again.
func (s *Store) EnableTOTP(ctx context.Context, id string, step int64) error {
	return s.execUser(ctx, "enable totp",
		`UPDATE app_user SET totp_enabled_at = now(), totp_last_step = $2 WHERE id = $1`, id, step)
}

// UpdateTOTPStep records the step of an accepted code, but only when it is newer
// than the recorded one. It returns false when the step did not advance, which
// means the code had already been used: that is the replay guard.
func (s *Store) UpdateTOTPStep(ctx context.Context, id string, step int64) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE app_user SET totp_last_step = $2
		 WHERE id = $1 AND (totp_last_step IS NULL OR totp_last_step < $2)`, id, step)
	if err != nil {
		return false, fmt.Errorf("update totp step: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ResetTOTP clears the TOTP secret, the enabled state and the replay guard, and
// deletes the recovery codes, all in one transaction.
func (s *Store) ResetTOTP(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("reset totp: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`UPDATE app_user
		 SET totp_secret_enc = NULL, totp_enabled_at = NULL, totp_last_step = NULL
		 WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("reset totp: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx, `DELETE FROM recovery_code WHERE user_id = $1`, id); err != nil {
		return fmt.Errorf("delete recovery codes: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit totp reset: %w", err)
	}
	return nil
}

// RecordFailure counts a failed password or code attempt and locks the user for
// lockFor once maxFailures is reached. A single UPDATE keeps it atomic.
func (s *Store) RecordFailure(ctx context.Context, id string, maxFailures int, lockFor time.Duration) error {
	if maxFailures <= 0 {
		return errors.New("max failures must be positive")
	}
	if lockFor <= 0 {
		return errors.New("lock duration must be positive")
	}

	tag, err := s.pool.Exec(ctx,
		`UPDATE app_user
		 SET failed_attempts = failed_attempts + 1,
		     locked_until = CASE
		         WHEN failed_attempts + 1 >= $2 THEN now() + make_interval(secs => $3)
		         ELSE locked_until
		     END
		 WHERE id = $1`, id, maxFailures, lockFor.Seconds())
	if err != nil {
		return fmt.Errorf("record failure: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetFailures clears the failure counter and the lock.
func (s *Store) ResetFailures(ctx context.Context, id string) error {
	return s.execUser(ctx, "reset failures",
		`UPDATE app_user SET failed_attempts = 0, locked_until = NULL WHERE id = $1`, id)
}

// SetPassword replaces the password hash and records when it changed.
func (s *Store) SetPassword(ctx context.Context, id, passwordHash string) error {
	if strings.TrimSpace(passwordHash) == "" {
		return errors.New("password hash is required")
	}
	return s.execUser(ctx, "set password",
		`UPDATE app_user SET password_hash = $2, password_changed_at = now() WHERE id = $1`, id, passwordHash)
}

// ReplaceRecoveryCodes replaces every recovery code of a user in one
// transaction: either the whole new set is stored or the old one stays.
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID string, hashes [][]byte) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("replace recovery codes: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM recovery_code WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("delete recovery codes: %w", err)
	}
	for _, hash := range hashes {
		if len(hash) == 0 {
			return errors.New("a recovery code hash is empty")
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, $2)`, userID, hash); err != nil {
			return fmt.Errorf("insert recovery code: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit recovery codes: %w", err)
	}
	return nil
}

// UseRecoveryCode consumes a recovery code, returning false when it does not
// belong to the user or was already used. The conditional UPDATE makes it single
// use even under concurrent attempts.
func (s *Store) UseRecoveryCode(ctx context.Context, userID string, hash []byte) (bool, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`UPDATE recovery_code SET used_at = now()
		 WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL
		 RETURNING id`, userID, hash).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("use recovery code: %w", err)
	}
	return true, nil
}

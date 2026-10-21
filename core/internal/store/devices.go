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

// MaxTrustedDevices is how many trusted devices one user may have at once.
const MaxTrustedDevices = 10

// ErrDeviceLimit is returned when a user already has MaxTrustedDevices active
// trusted devices.
var ErrDeviceLimit = errors.New("too many trusted devices")

// TrustedDevice is a row of the trusted_device table. The hash fields are only
// for the store and the API handler; they are never serialised.
type TrustedDevice struct {
	ID          string
	UserID      string
	TokenHash   []byte
	PinHash     string
	Name        string
	BrowserHash []byte
	FailedPins  int
	CreatedAt   time.Time
	LastUsedAt  time.Time
	LastMFAAt   time.Time
	RemovedAt   time.Time
}

// trustedDeviceColumns is the SELECT list shared by the device queries.
const trustedDeviceColumns = `id, user_id, token_hash, pin_hash, name, browser_hash,
	failed_pins, created_at, last_used_at, last_mfa_at, removed_at`

// CreateTrustedDevice stores a device for userID and returns it. The caller
// supplies the hashes, never the clear tokens or PIN. It serialises device
// creation per user so the limit cannot be exceeded by concurrent requests.
func (s *Store) CreateTrustedDevice(ctx context.Context, userID string, tokenHash []byte, pinHash, name string, browserHash []byte, now time.Time) (TrustedDevice, error) {
	name = strings.TrimSpace(name)
	if len(tokenHash) == 0 {
		return TrustedDevice{}, errors.New("device token hash is required")
	}
	if strings.TrimSpace(pinHash) == "" {
		return TrustedDevice{}, errors.New("pin hash is required")
	}
	if name == "" {
		return TrustedDevice{}, errors.New("device name is required")
	}
	if len(browserHash) == 0 {
		return TrustedDevice{}, errors.New("browser hash is required")
	}
	if now.IsZero() {
		now = time.Now()
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return TrustedDevice{}, fmt.Errorf("begin trusted device creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var lockedUser string
	if err := tx.QueryRow(ctx, `SELECT id FROM app_user WHERE id = $1 FOR UPDATE`, userID).Scan(&lockedUser); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TrustedDevice{}, ErrNotFound
		}
		return TrustedDevice{}, fmt.Errorf("lock the device user: %w", err)
	}

	var active int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM trusted_device WHERE user_id = $1 AND removed_at IS NULL`,
		userID).Scan(&active); err != nil {
		return TrustedDevice{}, fmt.Errorf("count trusted devices: %w", err)
	}
	if active >= MaxTrustedDevices {
		return TrustedDevice{}, ErrDeviceLimit
	}

	device, err := scanTrustedDevice(tx.QueryRow(ctx,
		`INSERT INTO trusted_device (user_id, token_hash, pin_hash, name, browser_hash, last_mfa_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+trustedDeviceColumns,
		userID, tokenHash, pinHash, name, browserHash, now))
	if err != nil {
		// A token collision must not put the token hash in a log line.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return TrustedDevice{}, errors.New("trusted device token collision")
		}
		return TrustedDevice{}, fmt.Errorf("create trusted device: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return TrustedDevice{}, fmt.Errorf("commit trusted device creation: %w", err)
	}
	return device, nil
}

// GetTrustedDeviceByTokenHash returns an active device with a PIN, or
// ErrNotFound. Removed and PIN-less devices are unknown to the PIN login.
func (s *Store) GetTrustedDeviceByTokenHash(ctx context.Context, tokenHash []byte) (TrustedDevice, error) {
	device, err := scanTrustedDevice(s.pool.QueryRow(ctx,
		`SELECT `+trustedDeviceColumns+`
		 FROM trusted_device
		 WHERE token_hash = $1 AND removed_at IS NULL AND pin_hash IS NOT NULL`, tokenHash))
	if errors.Is(err, ErrNotFound) {
		return TrustedDevice{}, ErrNotFound
	}
	if err != nil {
		return TrustedDevice{}, fmt.Errorf("get trusted device: %w", err)
	}
	return device, nil
}

// GetTrustedDevice returns an active device of userID, or ErrNotFound. The
// user id is part of the lookup so one user cannot touch another's device.
func (s *Store) GetTrustedDevice(ctx context.Context, userID, id string) (TrustedDevice, error) {
	device, err := scanTrustedDevice(s.pool.QueryRow(ctx,
		`SELECT `+trustedDeviceColumns+`
		 FROM trusted_device
		 WHERE id = $1 AND user_id = $2 AND removed_at IS NULL`, id, userID))
	if errors.Is(err, ErrNotFound) {
		return TrustedDevice{}, ErrNotFound
	}
	if err != nil {
		return TrustedDevice{}, fmt.Errorf("get trusted device %q: %w", id, err)
	}
	return device, nil
}

// ListTrustedDevices returns the active devices of a user, oldest first.
func (s *Store) ListTrustedDevices(ctx context.Context, userID string) ([]TrustedDevice, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+trustedDeviceColumns+`
		 FROM trusted_device
		 WHERE user_id = $1 AND removed_at IS NULL
		 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list trusted devices: %w", err)
	}
	defer rows.Close()

	devices := make([]TrustedDevice, 0, 4)
	for rows.Next() {
		device, err := scanTrustedDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("scan trusted device: %w", err)
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read trusted devices: %w", err)
	}
	return devices, nil
}

// TouchTrustedDevice records a successful PIN use and clears its failure count.
func (s *Store) TouchTrustedDevice(ctx context.Context, id string, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE trusted_device
		 SET last_used_at = $2, failed_pins = 0
		 WHERE id = $1 AND removed_at IS NULL`, id, now)
	if err != nil {
		return fmt.Errorf("touch trusted device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RecordPINFail counts a wrong PIN and disables the device at maxFailures. It
// returns the new count. A removed or unknown device is ErrNotFound.
func (s *Store) RecordPINFail(ctx context.Context, id string, maxFailures int) (int, error) {
	if maxFailures <= 0 {
		return 0, errors.New("max failures must be positive")
	}

	var failures int
	err := s.pool.QueryRow(ctx,
		`UPDATE trusted_device
		 SET failed_pins = failed_pins + 1,
		     pin_hash = CASE WHEN failed_pins + 1 >= $2 THEN NULL ELSE pin_hash END,
		     removed_at = CASE WHEN failed_pins + 1 >= $2 THEN now() ELSE removed_at END
		 WHERE id = $1 AND removed_at IS NULL
		 RETURNING failed_pins`, id, maxFailures).Scan(&failures)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("record pin failure: %w", err)
	}
	return failures, nil
}

// CompleteDeviceMFA records that the device just passed the second factor and
// binds it to the current browser marker.
func (s *Store) CompleteDeviceMFA(ctx context.Context, id string, browserHash []byte, now time.Time) error {
	if len(browserHash) == 0 {
		return errors.New("browser hash is required")
	}
	if now.IsZero() {
		now = time.Now()
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE trusted_device
		 SET last_mfa_at = $2, last_used_at = $2, browser_hash = $3, failed_pins = 0
		 WHERE id = $1 AND removed_at IS NULL`, id, now, browserHash)
	if err != nil {
		return fmt.Errorf("complete device mfa: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTrustedDevice removes one of the user's devices, or returns ErrNotFound
// when it does not exist, belongs to another user or was already removed.
func (s *Store) DeleteTrustedDevice(ctx context.Context, userID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE trusted_device
		 SET removed_at = now(), pin_hash = NULL
		 WHERE id = $1 AND user_id = $2 AND removed_at IS NULL`, id, userID)
	if err != nil {
		return fmt.Errorf("delete trusted device %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeUserDevices removes every active trusted device of a user and returns
// how many it removed.
func (s *Store) RevokeUserDevices(ctx context.Context, userID string) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE trusted_device
		 SET removed_at = now(), pin_hash = NULL
		 WHERE user_id = $1 AND removed_at IS NULL`, userID)
	if err != nil {
		return 0, fmt.Errorf("revoke trusted devices: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// scanTrustedDevice reads a device row. The nullable columns become zero values.
func scanTrustedDevice(row rowScanner) (TrustedDevice, error) {
	var (
		device      TrustedDevice
		pinHash     *string
		browserHash []byte
		lastUsedAt  *time.Time
		lastMFAAt   *time.Time
		removedAt   *time.Time
	)

	err := row.Scan(&device.ID, &device.UserID, &device.TokenHash, &pinHash, &device.Name,
		&browserHash, &device.FailedPins, &device.CreatedAt, &lastUsedAt, &lastMFAAt, &removedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return TrustedDevice{}, ErrNotFound
	}
	if err != nil {
		return TrustedDevice{}, err
	}

	if pinHash != nil {
		device.PinHash = *pinHash
	}
	device.BrowserHash = browserHash
	device.LastUsedAt = timeValue(lastUsedAt)
	device.LastMFAAt = timeValue(lastMFAAt)
	device.RemovedAt = timeValue(removedAt)
	return device, nil
}

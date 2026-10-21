package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// MaxPasskeys is how many passkeys one user may register.
	MaxPasskeys = 10
	// WebAuthnCeremonyTTL is how long a begin/finish ceremony stays valid.
	WebAuthnCeremonyTTL = 5 * time.Minute
)

var (
	// ErrPasskeyLimit is returned when a user already has MaxPasskeys.
	ErrPasskeyLimit = errors.New("too many passkeys")
	// ErrPasskeyExists is returned when the credential ID is already registered.
	ErrPasskeyExists = errors.New("passkey credential already registered")
)

// Passkey is a row of the passkey table. The credential material is only for
// the store and the WebAuthn verification; it is never serialised.
type Passkey struct {
	ID             string
	UserID         string
	CredentialID   []byte
	PublicKey      []byte
	AAGUID         []byte
	SignCount      uint32
	Transports     []string
	Name           string
	BackupEligible bool
	BackupState    bool
	CreatedAt      time.Time
	LastUsedAt     time.Time
}

const passkeyColumns = `id, user_id, credential_id, public_key, aaguid, sign_count,
	transports, name, backup_eligible, backup_state, created_at, last_used_at`

// CreatePasskey stores a verified credential for userID. It serialises creation
// per user so the limit cannot be exceeded by concurrent registrations.
func (s *Store) CreatePasskey(ctx context.Context, userID string, credentialID, publicKey, aaguid []byte, signCount uint32, transports []string, name string, backupEligible, backupState bool) (Passkey, error) {
	name = strings.TrimSpace(name)
	if len(credentialID) == 0 {
		return Passkey{}, errors.New("credential id is required")
	}
	if len(publicKey) == 0 {
		return Passkey{}, errors.New("credential public key is required")
	}
	if name == "" {
		return Passkey{}, errors.New("passkey name is required")
	}
	if transports == nil {
		transports = []string{}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Passkey{}, fmt.Errorf("begin passkey creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var lockedUser string
	if err := tx.QueryRow(ctx, `SELECT id FROM app_user WHERE id = $1 FOR UPDATE`, userID).Scan(&lockedUser); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Passkey{}, ErrNotFound
		}
		return Passkey{}, fmt.Errorf("lock the passkey user: %w", err)
	}

	var active int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM passkey WHERE user_id = $1`, userID).Scan(&active); err != nil {
		return Passkey{}, fmt.Errorf("count passkeys: %w", err)
	}
	if active >= MaxPasskeys {
		return Passkey{}, ErrPasskeyLimit
	}

	passkey, err := scanPasskey(tx.QueryRow(ctx,
		`INSERT INTO passkey (user_id, credential_id, public_key, aaguid, sign_count,
		                      transports, name, backup_eligible, backup_state)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+passkeyColumns,
		userID, credentialID, publicKey, aaguid, int64(signCount), transports, name,
		backupEligible, backupState))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return Passkey{}, ErrPasskeyExists
		}
		return Passkey{}, fmt.Errorf("create passkey: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Passkey{}, fmt.Errorf("commit passkey creation: %w", err)
	}
	return passkey, nil
}

// ListPasskeys returns the passkeys of a user, oldest first.
func (s *Store) ListPasskeys(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+passkeyColumns+` FROM passkey WHERE user_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list passkeys: %w", err)
	}
	defer rows.Close()

	passkeys := make([]Passkey, 0, 4)
	for rows.Next() {
		passkey, err := scanPasskey(rows)
		if err != nil {
			return nil, fmt.Errorf("scan passkey: %w", err)
		}
		passkeys = append(passkeys, passkey)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read passkeys: %w", err)
	}
	return passkeys, nil
}

// GetPasskey returns one passkey of userID, or ErrNotFound.
func (s *Store) GetPasskey(ctx context.Context, userID, id string) (Passkey, error) {
	passkey, err := scanPasskey(s.pool.QueryRow(ctx,
		`SELECT `+passkeyColumns+` FROM passkey WHERE id = $1 AND user_id = $2`, id, userID))
	if errors.Is(err, ErrNotFound) {
		return Passkey{}, ErrNotFound
	}
	if err != nil {
		return Passkey{}, fmt.Errorf("get passkey %q: %w", id, err)
	}
	return passkey, nil
}

// GetPasskeyByCredentialID returns the passkey with this credential ID, or
// ErrNotFound. It is used to find the record after discoverable login.
func (s *Store) GetPasskeyByCredentialID(ctx context.Context, credentialID []byte) (Passkey, error) {
	passkey, err := scanPasskey(s.pool.QueryRow(ctx,
		`SELECT `+passkeyColumns+` FROM passkey WHERE credential_id = $1`, credentialID))
	if errors.Is(err, ErrNotFound) {
		return Passkey{}, ErrNotFound
	}
	if err != nil {
		return Passkey{}, fmt.Errorf("get passkey by credential: %w", err)
	}
	return passkey, nil
}

// DeletePasskey removes one of the user's passkeys, or returns ErrNotFound when
// it does not exist or belongs to another user.
func (s *Store) DeletePasskey(ctx context.Context, userID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM passkey WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("delete passkey %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePasskeyUse records a successful assertion: the new counter, the backup
// state and the time. The user id is part of the lookup.
func (s *Store) UpdatePasskeyUse(ctx context.Context, userID, id string, signCount uint32, backupState bool, now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE passkey
		 SET sign_count = $3, backup_state = $4, last_used_at = $5
		 WHERE id = $1 AND user_id = $2`, id, userID, int64(signCount), backupState, now)
	if err != nil {
		return fmt.Errorf("update passkey use: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// WebAuthnCeremony is a pending registration or login challenge.
type WebAuthnCeremony struct {
	ID          string
	UserID      string
	Kind        string
	SessionData []byte
	ExpiresAt   time.Time
}

const ceremonyColumns = `id, COALESCE(user_id::text, ''), kind, session_data, expires_at`

// CreateWebAuthnCeremony stores a ceremony and returns its id. An empty userID
// means a discoverable login ceremony, which is not bound to a user.
func (s *Store) CreateWebAuthnCeremony(ctx context.Context, userID, kind string, sessionData []byte, expiresAt time.Time) (string, error) {
	if !validCeremonyKind(kind) {
		return "", fmt.Errorf("invalid webauthn ceremony kind %q", kind)
	}
	if len(sessionData) == 0 {
		return "", errors.New("webauthn session data is required")
	}
	if expiresAt.IsZero() {
		return "", errors.New("webauthn ceremony expiry is required")
	}

	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO webauthn_ceremony (user_id, kind, session_data, expires_at)
		 VALUES (NULLIF($1, '')::uuid, $2, $3, $4)
		 RETURNING id`,
		userID, kind, json.RawMessage(sessionData), expiresAt).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("create webauthn ceremony: %w", err)
	}
	return id, nil
}

// TakeWebAuthnCeremony consumes a ceremony by id and kind, returning
// ErrNotFound when it is unknown, expired or was already used.
func (s *Store) TakeWebAuthnCeremony(ctx context.Context, id, kind string) (WebAuthnCeremony, error) {
	return s.takeCeremony(ctx,
		`DELETE FROM webauthn_ceremony
		 WHERE id = $1 AND kind = $2 AND expires_at > now()
		 RETURNING `+ceremonyColumns, id, kind)
}

// TakeLatestWebAuthnCeremony consumes the newest registration ceremony of a
// user. Registration finishes carry no ceremony id because the full session
// already identifies the user.
func (s *Store) TakeLatestWebAuthnCeremony(ctx context.Context, userID, kind string) (WebAuthnCeremony, error) {
	return s.takeCeremony(ctx,
		`DELETE FROM webauthn_ceremony
		 WHERE id = (
		     SELECT id FROM webauthn_ceremony
		     WHERE user_id = $1 AND kind = $2 AND expires_at > now()
		     ORDER BY expires_at DESC
		     LIMIT 1
		 )
		 RETURNING `+ceremonyColumns, userID, kind)
}

func (s *Store) takeCeremony(ctx context.Context, query string, args ...any) (WebAuthnCeremony, error) {
	var (
		ceremony WebAuthnCeremony
		userID   string
	)
	err := s.pool.QueryRow(ctx, query, args...).Scan(&ceremony.ID, &userID, &ceremony.Kind,
		&ceremony.SessionData, &ceremony.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return WebAuthnCeremony{}, ErrNotFound
	}
	if err != nil {
		return WebAuthnCeremony{}, fmt.Errorf("take webauthn ceremony: %w", err)
	}
	ceremony.UserID = userID
	return ceremony, nil
}

// PurgeExpiredWebAuthnCeremonies deletes expired ceremonies and returns how
// many it removed.
func (s *Store) PurgeExpiredWebAuthnCeremonies(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM webauthn_ceremony WHERE expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("purge webauthn ceremonies: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func validCeremonyKind(kind string) bool {
	return kind == "register" || kind == "login"
}

// scanPasskey reads a passkey row. Nullable columns become zero values.
func scanPasskey(row rowScanner) (Passkey, error) {
	var (
		passkey    Passkey
		aaguid     []byte
		signCount  int64
		lastUsedAt *time.Time
	)
	err := row.Scan(&passkey.ID, &passkey.UserID, &passkey.CredentialID, &passkey.PublicKey,
		&aaguid, &signCount, &passkey.Transports, &passkey.Name, &passkey.BackupEligible,
		&passkey.BackupState, &passkey.CreatedAt, &lastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Passkey{}, ErrNotFound
	}
	if err != nil {
		return Passkey{}, err
	}
	passkey.AAGUID = aaguid
	passkey.SignCount = uint32(signCount)
	passkey.LastUsedAt = timeValue(lastUsedAt)
	return passkey, nil
}

package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const inviteLifetime = 72 * time.Hour

var (
	// ErrInviteInvalid means the token does not exist or was already used.
	ErrInviteInvalid = errors.New("invite is invalid or already used")
	// ErrInviteExpired means the invitation exists but is past its expiry.
	ErrInviteExpired = errors.New("invite has expired")
)

// Invite is a one-time invitation to create a user with an initial role
// binding. TokenHash is the SHA-256 of the token that is sent to the invitee;
// the clear token is never stored.
type Invite struct {
	ID            string
	TokenHash     []byte
	Username      string
	Email         string
	Role          string
	ScopeType     string
	ScopeServerID string
	ScopeGroupID  string
	ScopeName     string
	CreatedBy     string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	UsedAt        time.Time
}

const inviteColumns = `id, token_hash, username, COALESCE(email, ''), role, scope_type,
	COALESCE(scope_server_id::text, ''), COALESCE(scope_group_id::text, ''),
	COALESCE(scope_name, ''), created_by, created_at, expires_at, used_at`

// CreateInvite stores an invitation. The expiry defaults to 72 hours from now.
func (s *Store) CreateInvite(ctx context.Context, invite Invite) (Invite, error) {
	if len(invite.TokenHash) == 0 {
		return Invite{}, errors.New("invite token hash is required")
	}
	if strings.TrimSpace(invite.Username) == "" {
		return Invite{}, errors.New("invite username is required")
	}
	if !validRole(invite.Role) {
		return Invite{}, fmt.Errorf("invalid role %q", invite.Role)
	}
	if err := validateScope(invite.ScopeType, invite.ScopeServerID, invite.ScopeGroupID, invite.ScopeName); err != nil {
		return Invite{}, err
	}
	if invite.ExpiresAt.IsZero() {
		invite.ExpiresAt = time.Now().Add(inviteLifetime)
	}
	invite.Email = normalizeEmail(invite.Email)
	serverID, groupID, scopeName := scopeValues(invite.ScopeType, invite.ScopeServerID,
		invite.ScopeGroupID, invite.ScopeName)

	created, err := scanInvite(s.pool.QueryRow(ctx,
		`INSERT INTO user_invite
		    (token_hash, username, email, role, scope_type, scope_server_id,
		     scope_group_id, scope_name, created_by, expires_at)
		 VALUES ($1, $2, NULLIF($3, ''), $4, $5, $6, $7, $8, $9, $10)
		 RETURNING `+inviteColumns,
		invite.TokenHash, strings.TrimSpace(invite.Username), invite.Email, invite.Role,
		invite.ScopeType, serverID, groupID, scopeName, invite.CreatedBy, invite.ExpiresAt))
	if err != nil {
		if isUniqueViolation(err) {
			return Invite{}, fmt.Errorf("invite token already exists: %w", err)
		}
		return Invite{}, fmt.Errorf("create invite: %w", err)
	}
	return created, nil
}

// GetInviteByTokenHash returns an invite by its token hash, including used and
// expired ones, or ErrNotFound.
func (s *Store) GetInviteByTokenHash(ctx context.Context, tokenHash []byte) (Invite, error) {
	invite, err := scanInvite(s.pool.QueryRow(ctx,
		`SELECT `+inviteColumns+` FROM user_invite WHERE token_hash = $1`, tokenHash))
	if errors.Is(err, ErrNotFound) {
		return Invite{}, ErrNotFound
	}
	if err != nil {
		return Invite{}, fmt.Errorf("get invite: %w", err)
	}
	return invite, nil
}

// ConsumeInvite validates and consumes an invitation in one transaction. It
// creates the user without a TOTP secret and stores the invite's initial role
// binding. A failed user or binding insert also rolls back used_at.
func (s *Store) ConsumeInvite(ctx context.Context, tokenHash []byte, passwordHash string) (User, error) {
	if len(tokenHash) == 0 {
		return User{}, ErrInviteInvalid
	}
	if strings.TrimSpace(passwordHash) == "" {
		return User{}, errors.New("password hash is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin invite consumption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	invite, err := scanInvite(tx.QueryRow(ctx,
		`SELECT `+inviteColumns+` FROM user_invite WHERE token_hash = $1 FOR UPDATE`, tokenHash))
	if errors.Is(err, ErrNotFound) {
		return User{}, ErrInviteInvalid
	}
	if err != nil {
		return User{}, fmt.Errorf("lock invite: %w", err)
	}
	if !invite.UsedAt.IsZero() {
		return User{}, ErrInviteInvalid
	}
	if !invite.ExpiresAt.After(time.Now()) {
		return User{}, ErrInviteExpired
	}

	if _, err := tx.Exec(ctx,
		`UPDATE user_invite SET used_at = now() WHERE id = $1 AND used_at IS NULL`, invite.ID); err != nil {
		return User{}, fmt.Errorf("consume invite: %w", err)
	}

	user, err := queryUser(tx.QueryRow(ctx,
		`INSERT INTO app_user (username, password_hash, email)
		 VALUES ($1, $2, NULLIF($3, ''))
		 RETURNING `+userColumns,
		invite.Username, passwordHash, normalizeEmail(invite.Email)))
	if err != nil {
		return User{}, userInsertError(err)
	}

	serverID, groupID, scopeName := scopeValues(invite.ScopeType, invite.ScopeServerID,
		invite.ScopeGroupID, invite.ScopeName)
	if _, err := tx.Exec(ctx,
		`INSERT INTO role_binding
		    (subject_type, subject_id, scope_type, scope_server_id, scope_group_id,
		     scope_name, role, created_by)
		 VALUES ('user', $1, $2, $3, $4, $5, $6, $7)`,
		user.ID, invite.ScopeType, serverID, groupID, scopeName, invite.Role, invite.CreatedBy); err != nil {
		if isUniqueViolation(err) {
			return User{}, ErrRoleBindingExists
		}
		return User{}, fmt.Errorf("create invite role binding: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit invite consumption: %w", err)
	}
	return user, nil
}

// ListPendingInvites returns invitations that are unused and not expired,
// newest first.
func (s *Store) ListPendingInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+inviteColumns+`
		 FROM user_invite
		 WHERE used_at IS NULL AND expires_at > now()
		 ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list pending invites: %w", err)
	}
	defer rows.Close()

	invites := make([]Invite, 0, 8)
	for rows.Next() {
		invite, err := scanInvite(rows)
		if err != nil {
			return nil, fmt.Errorf("scan invite: %w", err)
		}
		invites = append(invites, invite)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read invites: %w", err)
	}
	return invites, nil
}

// RevokeInvite revokes a pending invitation by marking it used. Revoking an
// unknown or already used invite returns ErrNotFound.
func (s *Store) RevokeInvite(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE user_invite SET used_at = now() WHERE id = $1 AND used_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("revoke invite %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanInvite(row rowScanner) (Invite, error) {
	var (
		invite Invite
		usedAt *time.Time
	)
	if err := row.Scan(&invite.ID, &invite.TokenHash, &invite.Username, &invite.Email,
		&invite.Role, &invite.ScopeType, &invite.ScopeServerID, &invite.ScopeGroupID,
		&invite.ScopeName, &invite.CreatedBy, &invite.CreatedAt, &invite.ExpiresAt, &usedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Invite{}, ErrNotFound
		}
		return Invite{}, err
	}
	if usedAt != nil {
		invite.UsedAt = *usedAt
	}
	return invite, nil
}

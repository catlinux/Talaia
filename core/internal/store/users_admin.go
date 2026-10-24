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

// ErrEmailExists is returned when an email is already assigned to another
// user. Emails are compared case-insensitively.
var ErrEmailExists = errors.New("email already exists")

// ErrOIDCSubjectExists is returned when an OIDC subject is already linked to
// another user.
var ErrOIDCSubjectExists = errors.New("oidc subject already exists")
var ErrUserNotDisabled = errors.New("user is not disabled")

const oidcSubjectIndex = "app_user_oidc_subject_idx"

// normalizeEmail trims an email and stores it in the lower-case form used by
// the unique index.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// userInsertError keeps the public errors of user writes stable while the
// details of a PostgreSQL unique violation stay in the database layer.
func userInsertError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		switch pgErr.ConstraintName {
		case "app_user_email_lower_idx":
			return ErrEmailExists
		case oidcSubjectIndex:
			return ErrOIDCSubjectExists
		default:
			return ErrUserExists
		}
	}
	return fmt.Errorf("write user: %w", err)
}

// ListUsers returns every user, active or disabled, ordered by username.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM app_user ORDER BY username, id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := make([]User, 0, 8)
	for rows.Next() {
		user, err := queryUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	return users, nil
}

// UpdateUser changes the display name and email of a user. An empty email
// clears it. The email is stored lower-case.
func (s *Store) UpdateUser(ctx context.Context, id, displayName, email string) error {
	email = normalizeEmail(email)
	tag, err := s.pool.Exec(ctx,
		`UPDATE app_user SET display_name = $2, email = NULLIF($3, '') WHERE id = $1`,
		id, strings.TrimSpace(displayName), email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			if pgErr.ConstraintName == "app_user_email_lower_idx" {
				return ErrEmailExists
			}
			return ErrUserExists
		}
		return fmt.Errorf("update user %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetUserByEmail returns the user with that email, or ErrNotFound. The lookup
// is case-insensitive because the stored address is lower-case.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return User{}, ErrNotFound
	}
	user, err := queryUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM app_user WHERE email = $1`, email))
	if errors.Is(err, ErrNotFound) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user by email: %w", err)
	}
	return user, nil
}

// GetUserByOIDCSubject returns the user linked to an OIDC subject, or
// ErrNotFound.
func (s *Store) GetUserByOIDCSubject(ctx context.Context, subject string) (User, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return User{}, ErrNotFound
	}
	user, err := queryUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM app_user WHERE oidc_subject = $1`, subject))
	if errors.Is(err, ErrNotFound) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user by oidc subject: %w", err)
	}
	return user, nil
}

// SetOIDCSubject links a user to an OIDC subject. An empty value clears it.
func (s *Store) SetOIDCSubject(ctx context.Context, id, subject string) error {
	subject = strings.TrimSpace(subject)
	tag, err := s.pool.Exec(ctx,
		`UPDATE app_user SET oidc_subject = NULLIF($2, '') WHERE id = $1`, id, subject)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return ErrOIDCSubjectExists
		}
		return fmt.Errorf("set oidc subject for %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DisableUser marks a user as disabled and, in the same transaction, revokes
// their sessions, trusted devices and API keys. It refuses to disable the last
// active organization administrator with ErrLastAdmin.
func (s *Store) DisableUser(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin disable user: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockLastAdmin(ctx, tx); err != nil {
		return err
	}

	var disabledAt *time.Time
	err = tx.QueryRow(ctx, `SELECT disabled_at FROM app_user WHERE id = $1 FOR UPDATE`, id).Scan(&disabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock user %q: %w", id, err)
	}
	if disabledAt != nil {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit disable user: %w", err)
		}
		return nil
	}

	wasAdmin, err := userHasOrganizationAdmin(ctx, tx, id)
	if err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `UPDATE app_user SET disabled_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("disable user %q: %w", id, err)
	}
	if wasAdmin {
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE session SET revoked_at = COALESCE(revoked_at, now())
		 WHERE user_id = $1 AND revoked_at IS NULL`, id); err != nil {
		return fmt.Errorf("revoke sessions of %q: %w", id, err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE trusted_device SET removed_at = COALESCE(removed_at, now()), pin_hash = NULL
		 WHERE user_id = $1 AND removed_at IS NULL`, id); err != nil {
		return fmt.Errorf("revoke trusted devices of %q: %w", id, err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE api_key SET revoked_at = COALESCE(revoked_at, now())
		 WHERE owner_id = $1 AND revoked_at IS NULL`, id); err != nil {
		return fmt.Errorf("revoke api keys of %q: %w", id, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit disable user: %w", err)
	}
	return nil
}

// DeleteUser permanently removes a disabled user and credentials tied to it.
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete user: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockLastAdmin(ctx, tx); err != nil {
		return err
	}
	var disabledAt *time.Time
	err = tx.QueryRow(ctx, `SELECT disabled_at FROM app_user WHERE id = $1 FOR UPDATE`, id).Scan(&disabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock user %q: %w", id, err)
	}
	if disabledAt == nil {
		return ErrUserNotDisabled
	}
	var admin bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM role_binding WHERE subject_type='user' AND subject_id=$1 AND scope_type='organization' AND role='admin') OR EXISTS (SELECT 1 FROM team_member tm JOIN role_binding b ON b.subject_type='team' AND b.subject_id=tm.team_id WHERE tm.user_id=$1 AND b.scope_type='organization' AND b.role='admin')`, id).Scan(&admin); err != nil {
		return fmt.Errorf("check user admin binding: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM role_binding WHERE subject_type='api_key' AND subject_id IN (SELECT id FROM api_key WHERE owner_id=$1)`, id); err != nil {
		return fmt.Errorf("delete user key bindings: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM role_binding WHERE subject_type='user' AND subject_id=$1`, id); err != nil {
		return fmt.Errorf("delete user bindings: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM team_member WHERE user_id=$1`, id); err != nil {
		return fmt.Errorf("delete user team memberships: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM app_user WHERE id=$1`, id); err != nil {
		return fmt.Errorf("delete user %q: %w", id, err)
	}
	if admin {
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit delete user: %w", err)
	}
	return nil
}

// EnableUser clears the disabled flag. It does not restore sessions, devices or
// API keys revoked by DisableUser.
func (s *Store) EnableUser(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE app_user SET disabled_at = NULL WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("enable user %q: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func userHasOrganizationAdmin(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (
		    SELECT 1 FROM app_user u
		    WHERE u.id = $1 AND (
		        EXISTS (
		            SELECT 1 FROM role_binding b
		            WHERE b.subject_type = 'user' AND b.subject_id = u.id
		              AND b.scope_type = 'organization' AND b.role = 'admin'
		        )
		        OR EXISTS (
		            SELECT 1 FROM team_member tm
		            JOIN role_binding b ON b.subject_type = 'team' AND b.subject_id = tm.team_id
		            WHERE tm.user_id = u.id
		              AND b.scope_type = 'organization' AND b.role = 'admin'
		        )
		    )
		)`, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("check user admin binding: %w", err)
	}
	return exists, nil
}

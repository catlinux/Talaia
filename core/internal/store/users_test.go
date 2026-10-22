package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// testPasswordHash is a syntactically valid PHC string: the store never looks
// inside it.
const testPasswordHash = "$argon2id$v=19$m=65536,t=3,p=1$c2FsdHNhbHRzYWx0$aGFzaGhhc2hoYXNoaGFzaA"

// newTestUser clears the single-user table, creates one user and returns it.
// The integration tests run against the scratch database of
// TALAIA_TEST_DATABASE_URL, so removing the users of an earlier run is
// expected. The cleanup deletes the user it created, which cascades to its
// sessions and recovery codes.
func newTestUser(t *testing.T, s *Store) User {
	t.Helper()

	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `DELETE FROM app_user`); err != nil {
		t.Fatalf("clear users: %v", err)
	}

	username := fmt.Sprintf("test-user-%d", time.Now().UnixNano())
	id, err := s.CreateUser(ctx, username, testPasswordHash)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM app_user WHERE id = $1`, id)
	})

	user, err := s.GetUser(ctx, id)
	if err != nil {
		t.Fatalf("get the new user: %v", err)
	}
	return user
}

func TestCreateUserAndFindIt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	if user.ID == "" || user.Username == "" {
		t.Fatalf("user = %+v, want an id and a username", user)
	}
	if user.PasswordHash != testPasswordHash {
		t.Errorf("password hash = %q", user.PasswordHash)
	}
	if user.FailedAttempts != 0 {
		t.Errorf("failed attempts = %d, want 0", user.FailedAttempts)
	}
	if len(user.TOTPSecretEnc) != 0 || !user.TOTPEnabledAt.IsZero() || user.TOTPLastStep != 0 {
		t.Errorf("a new user must have no TOTP: %+v", user)
	}
	if !user.LockedUntil.IsZero() {
		t.Errorf("a new user must not be locked: %s", user.LockedUntil)
	}
	if user.CreatedAt.IsZero() || user.PasswordChangedAt.IsZero() {
		t.Errorf("timestamps = %s / %s, want them set", user.CreatedAt, user.PasswordChangedAt)
	}

	byName, err := s.GetUserByUsername(ctx, user.Username)
	if err != nil {
		t.Fatalf("get by username: %v", err)
	}
	if byName.ID != user.ID {
		t.Errorf("id by username = %q, want %q", byName.ID, user.ID)
	}

	bindings, err := s.ListRoleBindings(ctx, SubjectUser, user.ID)
	if err != nil {
		t.Fatalf("list setup bindings: %v", err)
	}
	if len(bindings) != 1 || bindings[0].ScopeType != ScopeOrganization || bindings[0].Role != RoleAdmin {
		t.Fatalf("setup bindings = %+v, want an organization admin binding", bindings)
	}
}

func TestCreateUserAllowsOnlyOneUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first := newTestUser(t, s)

	for _, username := range []string{"another-admin", first.Username} {
		if _, err := s.CreateUser(ctx, username, testPasswordHash); !errors.Is(err, ErrUserExists) {
			t.Errorf("CreateUser(%q) = %v, want ErrUserExists", username, err)
		}
	}
}

func TestCreateUserValidatesItsInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, "   ", testPasswordHash); err == nil {
		t.Error("expected an error for an empty username")
	}
	if _, err := s.CreateUser(ctx, "admin", "  "); err == nil {
		t.Error("expected an error for an empty password hash")
	}
}

func TestUserNotFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	missing := "00000000-0000-0000-0000-000000000000"

	if _, err := s.GetUser(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("get user = %v, want ErrNotFound", err)
	}
	if _, err := s.GetUserByUsername(ctx, "nobody-"+missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("get user by username = %v, want ErrNotFound", err)
	}
}

func TestSetAndEnableTOTP(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	secret := []byte("sealed secret")
	if err := s.SetTOTP(ctx, user.ID, secret); err != nil {
		t.Fatalf("set totp: %v", err)
	}

	stored, err := s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if string(stored.TOTPSecretEnc) != string(secret) {
		t.Errorf("stored secret = %q", stored.TOTPSecretEnc)
	}
	if !stored.TOTPEnabledAt.IsZero() || stored.TOTPLastStep != 0 {
		t.Errorf("a pending secret must not enable TOTP: %+v", stored)
	}

	if err := s.EnableTOTP(ctx, user.ID, 42); err != nil {
		t.Fatalf("enable totp: %v", err)
	}
	stored, err = s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if stored.TOTPEnabledAt.IsZero() {
		t.Error("totp_enabled_at is zero after enabling")
	}
	if stored.TOTPLastStep != 42 {
		t.Errorf("last step = %d, want 42", stored.TOTPLastStep)
	}

	// A new setup replaces the secret and puts TOTP back in the pending state.
	if err := s.SetTOTP(ctx, user.ID, []byte("another secret")); err != nil {
		t.Fatalf("set totp again: %v", err)
	}
	stored, err = s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if !stored.TOTPEnabledAt.IsZero() || stored.TOTPLastStep != 0 {
		t.Errorf("a new secret must reset the enabled state and the guard: %+v", stored)
	}
}

func TestUpdateTOTPStepIsTheReplayGuard(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	if err := s.SetTOTP(ctx, user.ID, []byte("secret")); err != nil {
		t.Fatalf("set totp: %v", err)
	}
	if err := s.EnableTOTP(ctx, user.ID, 100); err != nil {
		t.Fatalf("enable totp: %v", err)
	}

	advanced, err := s.UpdateTOTPStep(ctx, user.ID, 100)
	if err != nil {
		t.Fatalf("update step: %v", err)
	}
	if advanced {
		t.Error("the step of the enabling code must not be accepted again")
	}

	advanced, err = s.UpdateTOTPStep(ctx, user.ID, 99)
	if err != nil {
		t.Fatalf("update step: %v", err)
	}
	if advanced {
		t.Error("an older step must not advance the guard")
	}

	advanced, err = s.UpdateTOTPStep(ctx, user.ID, 101)
	if err != nil {
		t.Fatalf("update step: %v", err)
	}
	if !advanced {
		t.Error("a newer step must advance the guard")
	}

	// A missing user cannot advance anything; that is not an error.
	missing := "00000000-0000-0000-0000-000000000000"
	advanced, err = s.UpdateTOTPStep(ctx, missing, 200)
	if err != nil || advanced {
		t.Errorf("UpdateTOTPStep(missing) = %v, %v, want false without error", advanced, err)
	}
}

func TestResetTOTPClearsTheSecretAndTheRecoveryCodes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	if err := s.SetTOTP(ctx, user.ID, []byte("secret")); err != nil {
		t.Fatalf("set totp: %v", err)
	}
	if err := s.EnableTOTP(ctx, user.ID, 7); err != nil {
		t.Fatalf("enable totp: %v", err)
	}
	hashes := [][]byte{{1}, {2}}
	if err := s.ReplaceRecoveryCodes(ctx, user.ID, hashes); err != nil {
		t.Fatalf("replace recovery codes: %v", err)
	}

	if err := s.ResetTOTP(ctx, user.ID); err != nil {
		t.Fatalf("reset totp: %v", err)
	}

	stored, err := s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if len(stored.TOTPSecretEnc) != 0 || !stored.TOTPEnabledAt.IsZero() || stored.TOTPLastStep != 0 {
		t.Errorf("the reset did not clear TOTP: %+v", stored)
	}
	if used, err := s.UseRecoveryCode(ctx, user.ID, hashes[0]); err != nil || used {
		t.Errorf("UseRecoveryCode = %v, %v, want false: the codes must be gone", used, err)
	}

	if err := s.ResetTOTP(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Errorf("reset of a missing user = %v, want ErrNotFound", err)
	}
}

func TestRecordFailureLocksTheUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	const maxFailures = 3
	lockFor := 15 * time.Minute
	for attempt := 1; attempt <= maxFailures; attempt++ {
		if err := s.RecordFailure(ctx, user.ID, maxFailures, lockFor); err != nil {
			t.Fatalf("record failure %d: %v", attempt, err)
		}

		stored, err := s.GetUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		if stored.FailedAttempts != attempt {
			t.Errorf("failed attempts = %d, want %d", stored.FailedAttempts, attempt)
		}
		if attempt < maxFailures && !stored.LockedUntil.IsZero() {
			t.Errorf("the user was locked after %d failures", attempt)
		}
		if attempt == maxFailures {
			if stored.LockedUntil.IsZero() {
				t.Fatal("the user was not locked after reaching the limit")
			}
			if !stored.LockedUntil.After(time.Now()) {
				t.Errorf("locked until %s, want a future time", stored.LockedUntil)
			}
		}
	}

	if err := s.ResetFailures(ctx, user.ID); err != nil {
		t.Fatalf("reset failures: %v", err)
	}
	stored, err := s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if stored.FailedAttempts != 0 || !stored.LockedUntil.IsZero() {
		t.Errorf("after resetting: attempts = %d, locked until %s", stored.FailedAttempts, stored.LockedUntil)
	}

	if err := s.RecordFailure(ctx, user.ID, 0, lockFor); err == nil {
		t.Error("expected an error for a non-positive limit")
	}
	if err := s.RecordFailure(ctx, user.ID, maxFailures, 0); err == nil {
		t.Error("expected an error for a non-positive lock duration")
	}
	if err := s.RecordFailure(ctx, "00000000-0000-0000-0000-000000000000", maxFailures, lockFor); !errors.Is(err, ErrNotFound) {
		t.Errorf("record failure of a missing user = %v, want ErrNotFound", err)
	}
}

func TestSetPassword(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	const newHash = "$argon2id$v=19$m=65536,t=3,p=1$bmV3c2FsdA$aGFzaGhhc2hoYXNoaGFzaA"
	if err := s.SetPassword(ctx, user.ID, newHash); err != nil {
		t.Fatalf("set password: %v", err)
	}

	stored, err := s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if stored.PasswordHash != newHash {
		t.Errorf("password hash = %q, want the new one", stored.PasswordHash)
	}
	if stored.PasswordChangedAt.Before(user.PasswordChangedAt) {
		t.Errorf("password_changed_at = %s, want it to move forward from %s",
			stored.PasswordChangedAt, user.PasswordChangedAt)
	}

	if err := s.SetPassword(ctx, user.ID, " "); err == nil {
		t.Error("expected an error for an empty password hash")
	}
}

func TestRecoveryCodesAreSingleUse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	hashes := [][]byte{{1}, {2}, {3}}
	if err := s.ReplaceRecoveryCodes(ctx, user.ID, hashes); err != nil {
		t.Fatalf("replace recovery codes: %v", err)
	}

	used, err := s.UseRecoveryCode(ctx, user.ID, hashes[0])
	if err != nil {
		t.Fatalf("use recovery code: %v", err)
	}
	if !used {
		t.Fatal("a fresh recovery code was not accepted")
	}

	used, err = s.UseRecoveryCode(ctx, user.ID, hashes[0])
	if err != nil {
		t.Fatalf("use recovery code again: %v", err)
	}
	if used {
		t.Error("a recovery code was accepted twice")
	}

	used, err = s.UseRecoveryCode(ctx, user.ID, []byte{99})
	if err != nil {
		t.Fatalf("use an unknown code: %v", err)
	}
	if used {
		t.Error("an unknown recovery code was accepted")
	}

	// Replacing the set drops the codes that were left.
	if err := s.ReplaceRecoveryCodes(ctx, user.ID, [][]byte{{4}}); err != nil {
		t.Fatalf("replace recovery codes: %v", err)
	}
	if used, err := s.UseRecoveryCode(ctx, user.ID, hashes[1]); err != nil || used {
		t.Errorf("UseRecoveryCode of a replaced code = %v, %v, want false", used, err)
	}
	if used, err := s.UseRecoveryCode(ctx, user.ID, []byte{4}); err != nil || !used {
		t.Errorf("UseRecoveryCode of the new code = %v, %v, want true", used, err)
	}
}

func TestReplaceRecoveryCodesRollsBackOnError(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	if err := s.ReplaceRecoveryCodes(ctx, user.ID, [][]byte{{1}}); err != nil {
		t.Fatalf("replace recovery codes: %v", err)
	}

	// An empty hash in the middle aborts the transaction: the old code survives.
	if err := s.ReplaceRecoveryCodes(ctx, user.ID, [][]byte{{2}, {}}); err == nil {
		t.Fatal("expected an error for an empty hash")
	}
	if used, err := s.UseRecoveryCode(ctx, user.ID, []byte{1}); err != nil || !used {
		t.Errorf("the old code was lost: UseRecoveryCode = %v, %v", used, err)
	}
	if used, err := s.UseRecoveryCode(ctx, user.ID, []byte{2}); err != nil || used {
		t.Errorf("a code of the failed replacement was stored: %v, %v", used, err)
	}
}

func TestDeletingAUserDeletesItsRecoveryCodes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newTestUser(t, s)

	if err := s.ReplaceRecoveryCodes(ctx, user.ID, [][]byte{{7}}); err != nil {
		t.Fatalf("replace recovery codes: %v", err)
	}

	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM recovery_code WHERE user_id = $1`, user.ID).Scan(&count); err != nil {
		t.Fatalf("count recovery codes: %v", err)
	}
	if count != 1 {
		t.Fatalf("recovery codes = %d, want 1", count)
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM recovery_code WHERE user_id = $1`, user.ID).Scan(&count); err != nil {
		t.Fatalf("count recovery codes: %v", err)
	}
	if count != 0 {
		t.Errorf("recovery codes after deleting the user = %d, want 0", count)
	}
}

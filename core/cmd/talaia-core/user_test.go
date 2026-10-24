package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

// testDatabase points the configuration at a schema of the test database that
// belongs to one test, and applies the migrations. The store and API suites use
// the same database and this phase allows a single user, so a private schema
// keeps the three suites from deleting each other's user.
func testDatabase(t *testing.T) *store.Store {
	t.Helper()

	databaseURL := os.Getenv("TALAIA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TALAIA_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	schema := fmt.Sprintf("test_cmd_%d", time.Now().UnixNano())

	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to the test database: %v", err)
	}
	if _, err := bootstrap.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		_ = bootstrap.Close(ctx)
		t.Fatalf("create the test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = bootstrap.Exec(cleanupCtx, `DROP SCHEMA `+schema+` CASCADE`)
		_ = bootstrap.Close(cleanupCtx)
	})

	separator := "?"
	if strings.Contains(databaseURL, "?") {
		separator = "&"
	}
	scoped := databaseURL + separator + "search_path=" + url.QueryEscape(schema)
	t.Setenv("TALAIA_DATABASE_URL", scoped)
	t.Setenv("TALAIA_DATA_DIR", t.TempDir())

	// An empty configuration file keeps whatever lives in /etc out of the test.
	configPath := filepath.Join(t.TempDir(), "core.toml")
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatalf("write the config file: %v", err)
	}
	t.Setenv("TALAIA_CONFIG", configPath)

	st, err := store.Open(ctx, scoped)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

// createCommandUser creates the administrator with TOTP already enabled.
func createCommandUser(t *testing.T, st *store.Store) store.User {
	t.Helper()

	ctx := context.Background()
	id, err := st.CreateUser(ctx, "admin", auth.HashPassword("correct horse battery staple"))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := st.SetTOTP(ctx, id, auth.GenerateSecret()); err != nil {
		t.Fatalf("set totp: %v", err)
	}
	if err := st.EnableTOTP(ctx, id, auth.Step(time.Now())); err != nil {
		t.Fatalf("enable totp: %v", err)
	}

	user, err := st.GetUser(ctx, id)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	return user
}

// createCommandSession creates a full session and returns its token.
func createCommandSession(t *testing.T, st *store.Store, userID string) string {
	t.Helper()

	token := auth.NewSessionToken()
	if _, err := st.CreateSession(context.Background(), userID, auth.HashToken(token),
		store.StageFull, time.Now().Add(time.Hour), "", ""); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return token
}

// findAuditAction reports whether an audit entry with that action exists.
func findAuditAction(t *testing.T, st *store.Store, action string) bool {
	t.Helper()

	entries, err := st.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	for _, entry := range entries {
		if entry.Action == action && entry.Actor == cliActor && entry.Result == "success" {
			return true
		}
	}
	return false
}

func TestUserCreateCommand(t *testing.T) {
	st := testDatabase(t)

	var stdout, stderr bytes.Buffer
	input := strings.NewReader("correct horse battery staple\ncorrect horse battery staple\n")
	if err := userCreateCommand([]string{"--username", "admin"}, input, &stdout, &stderr); err != nil {
		t.Fatalf("user create: %v", err)
	}

	if !strings.Contains(stdout.String(), "created the user admin") {
		t.Errorf("stdout = %q", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "correct horse") {
		t.Error("the password was printed")
	}

	ctx := context.Background()
	user, err := st.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	valid, err := auth.VerifyPassword(user.PasswordHash, "correct horse battery staple")
	if err != nil || !valid {
		t.Errorf("the stored password does not verify: %v, %v", valid, err)
	}
	if !findAuditAction(t, st, "user_created") {
		t.Error("the creation was not audited")
	}

	// A second user is refused: this phase has one.
	var secondOut, secondErr bytes.Buffer
	input = strings.NewReader("correct horse battery staple\ncorrect horse battery staple\n")
	err = userCreateCommand([]string{"--username", "other"}, input, &secondOut, &secondErr)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second user create = %v, want an error", err)
	}
}

func TestUserCreateCommandFromStandardInput(t *testing.T) {
	st := testDatabase(t)

	var stdout, stderr bytes.Buffer
	input := strings.NewReader("correct horse battery staple\n")
	if err := userCreateCommand([]string{"--username", "admin", "--password-stdin"}, input, &stdout, &stderr); err != nil {
		t.Fatalf("user create: %v", err)
	}

	user, err := st.GetUserByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	valid, err := auth.VerifyPassword(user.PasswordHash, "correct horse battery staple")
	if err != nil || !valid {
		t.Errorf("the stored password does not verify: %v, %v", valid, err)
	}
}

func TestUserCreateCommandValidates(t *testing.T) {
	testDatabase(t)

	tests := []struct {
		name string
		args []string
		in   string
		want string
	}{
		{
			name: "no username",
			args: nil,
			in:   "correct horse battery staple\ncorrect horse battery staple\n",
			want: "--username is required",
		},
		{
			name: "short password",
			args: []string{"--username", "admin"},
			in:   "short\nshort\n",
			want: "at least 12",
		},
		{
			name: "passwords do not match",
			args: []string{"--username", "admin"},
			in:   "correct horse battery staple\nanother good password\n",
			want: "do not match",
		},
		{
			name: "unknown flag",
			args: []string{"--unknown"},
			want: "flag provided",
		},
		{
			name: "stray argument",
			args: []string{"--username", "admin", "extra"},
			want: "usage:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := userCreateCommand(tt.args, strings.NewReader(tt.in), &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestUserReset2FACommand(t *testing.T) {
	st := testDatabase(t)
	user := createCommandUser(t, st)
	token := createCommandSession(t, st, user.ID)

	var stdout, stderr bytes.Buffer
	if err := userReset2FACommand(nil, &stdout, &stderr); err != nil {
		t.Fatalf("user reset-2fa: %v", err)
	}
	if !strings.Contains(stdout.String(), "two-factor authentication reset") {
		t.Errorf("stdout = %q", stdout.String())
	}

	ctx := context.Background()
	after, err := st.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if len(after.TOTPSecretEnc) != 0 || !after.TOTPEnabledAt.IsZero() || after.TOTPLastStep != 0 {
		t.Errorf("the second factor was not cleared: %+v", after)
	}

	// The sessions are gone with it.
	if _, err := st.GetSessionByTokenHash(ctx, auth.HashToken(token)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the session survived the reset: %v", err)
	}
	if !findAuditAction(t, st, "mfa_reset") {
		t.Error("the reset was not audited")
	}
}

func TestSessionRevokeAllCommand(t *testing.T) {
	st := testDatabase(t)
	user := createCommandUser(t, st)
	tokens := []string{
		createCommandSession(t, st, user.ID),
		createCommandSession(t, st, user.ID),
	}

	var stdout, stderr bytes.Buffer
	if err := sessionRevokeAllCommand(nil, &stdout, &stderr); err != nil {
		t.Fatalf("session revoke-all: %v", err)
	}
	if !strings.Contains(stdout.String(), "revoked 2 session(s)") {
		t.Errorf("stdout = %q", stdout.String())
	}

	for _, token := range tokens {
		if _, err := st.GetSessionByTokenHash(context.Background(), auth.HashToken(token)); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("a session survived: %v", err)
		}
	}
	if !findAuditAction(t, st, "sessions_revoked") {
		t.Error("the revocation was not audited")
	}
}

func TestSingleUserCommandsWithoutAUser(t *testing.T) {
	testDatabase(t)

	var stdout, stderr bytes.Buffer
	if err := userReset2FACommand(nil, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "no user yet") {
		t.Errorf("user reset-2fa = %v, want a clear message", err)
	}
	if err := sessionRevokeAllCommand(nil, &stdout, &stderr); err == nil {
		t.Error("session revoke-all worked without a user")
	}
}

func TestUserCommandsWithSeveralUsers(t *testing.T) {
	st := testDatabase(t)
	ctx := context.Background()
	admin := createCommandUser(t, st)
	otherID, err := st.CreateUserFull(ctx, "marc", auth.HashPassword("correct horse battery staple"), "Marc", "")
	if err != nil {
		t.Fatalf("create the second user: %v", err)
	}
	if err := st.SetTOTP(ctx, otherID.ID, auth.GenerateSecret()); err != nil {
		t.Fatalf("set totp: %v", err)
	}
	if err := st.EnableTOTP(ctx, otherID.ID, auth.Step(time.Now())); err != nil {
		t.Fatalf("enable totp: %v", err)
	}

	var stdout, stderr bytes.Buffer
	for name, run := range map[string]func([]string) error{
		"reset-2fa": func(args []string) error { return userReset2FACommand(args, &stdout, &stderr) },
		"unlock":    func(args []string) error { return userUnlockCommand(args, &stdout, &stderr) },
	} {
		if err := run(nil); err == nil || !strings.Contains(err.Error(), "--username") {
			t.Errorf("%s without a name = %v, want a message asking for --username", name, err)
		}
		if err := run([]string{"--username", "nobody"}); err == nil || !strings.Contains(err.Error(), "no user named") {
			t.Errorf("%s with an unknown name = %v", name, err)
		}
	}
	// Nothing was touched by the refusals.
	if after, _ := st.GetUser(ctx, admin.ID); after.TOTPEnabledAt.IsZero() {
		t.Fatal("a refused command cleared the second factor of another user")
	}

	if err := userReset2FACommand([]string{"--username", "marc"}, &stdout, &stderr); err != nil {
		t.Fatalf("reset-2fa marc: %v", err)
	}
	marc, _ := st.GetUser(ctx, otherID.ID)
	admin, _ = st.GetUser(ctx, admin.ID)
	if !marc.TOTPEnabledAt.IsZero() || admin.TOTPEnabledAt.IsZero() {
		t.Errorf("reset-2fa must touch only marc: marc=%v admin=%v", marc.TOTPEnabledAt, admin.TOTPEnabledAt)
	}
}

func TestUserUnlockCommand(t *testing.T) {
	st := testDatabase(t)
	ctx := context.Background()
	user := createCommandUser(t, st)
	for range 5 {
		if err := st.RecordFailure(ctx, user.ID, 3, 15*time.Minute); err != nil {
			t.Fatalf("record failure: %v", err)
		}
	}
	locked, _ := st.GetUser(ctx, user.ID)
	if locked.LockedUntil.IsZero() {
		t.Fatal("the user was not locked by the failures")
	}

	var stdout, stderr bytes.Buffer
	if err := userUnlockCommand(nil, &stdout, &stderr); err != nil {
		t.Fatalf("user unlock: %v", err)
	}
	after, _ := st.GetUser(ctx, user.ID)
	if !after.LockedUntil.IsZero() || after.FailedAttempts != 0 {
		t.Errorf("the lock survived: %+v", after)
	}
	if !findAuditAction(t, st, "user_unlocked") {
		t.Error("the unlock was not audited")
	}
}

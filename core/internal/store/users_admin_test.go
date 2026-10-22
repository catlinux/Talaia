package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDisableUserRevokesCredentials(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newRBACTestUser(t, s, "disabled")
	other := newRBACTestUser(t, s, "disabled-other")
	mustCreateBinding(t, s, RoleBinding{
		SubjectType: SubjectUser, SubjectID: user.ID,
		ScopeType: ScopeOrganization, Role: RoleAdmin, CreatedBy: "test",
	})
	mustCreateBinding(t, s, RoleBinding{
		SubjectType: SubjectUser, SubjectID: other.ID,
		ScopeType: ScopeOrganization, Role: RoleAdmin, CreatedBy: "test",
	})

	sessionID, err := s.CreateSession(ctx, user.ID, []byte(uniqueTestName("session")), StageFull,
		time.Now().Add(time.Hour), "192.0.2.10", "test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	device, err := s.CreateTrustedDevice(ctx, user.ID, []byte(uniqueTestName("device")), "pin-hash",
		"test browser", []byte(uniqueTestName("browser")), time.Now())
	if err != nil {
		t.Fatalf("create trusted device: %v", err)
	}
	key, err := s.CreateAPIKey(ctx, uniqueTestName("key"), []byte(uniqueTestName("secret")),
		"test key", user.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	if err := s.DisableUser(ctx, user.ID); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	got, err := s.GetUser(ctx, user.ID)
	if err != nil || got.DisabledAt.IsZero() {
		t.Fatalf("disabled user = %+v, %v", got, err)
	}

	var sessionRevoked, deviceRemoved, keyRevoked bool
	if err := s.pool.QueryRow(ctx,
		`SELECT revoked_at IS NOT NULL FROM session WHERE id = $1`, sessionID).Scan(&sessionRevoked); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT removed_at IS NOT NULL FROM trusted_device WHERE id = $1`, device.ID).Scan(&deviceRemoved); err != nil {
		t.Fatalf("read device: %v", err)
	}
	if err := s.pool.QueryRow(ctx,
		`SELECT revoked_at IS NOT NULL FROM api_key WHERE id = $1`, key.ID).Scan(&keyRevoked); err != nil {
		t.Fatalf("read api key: %v", err)
	}
	if !sessionRevoked || !deviceRemoved || !keyRevoked {
		t.Fatalf("revoked session=%v device=%v key=%v", sessionRevoked, deviceRemoved, keyRevoked)
	}
	if err := s.DisableUser(ctx, user.ID); err != nil {
		t.Fatalf("disable already disabled user: %v", err)
	}
	if err := s.EnableUser(ctx, user.ID); err != nil {
		t.Fatalf("enable user: %v", err)
	}
	got, err = s.GetUser(ctx, user.ID)
	if err != nil || !got.DisabledAt.IsZero() {
		t.Fatalf("enabled user = %+v, %v", got, err)
	}
}

func TestUserProfileAndOIDC(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newRBACTestUser(t, s, "profile")
	email := strings.ToUpper(uniqueTestName("profile-mail")) + "@Example.COM"
	if err := s.UpdateUser(ctx, user.ID, "New Name", email); err != nil {
		t.Fatalf("update user: %v", err)
	}
	got, err := s.GetUserByEmail(ctx, strings.ToLower(email))
	if err != nil {
		t.Fatalf("get user by email: %v", err)
	}
	if got.DisplayName != "New Name" || got.Email != strings.ToLower(email) {
		t.Fatalf("updated user = %+v", got)
	}

	other := newRBACTestUser(t, s, "profile-other")
	if err := s.UpdateUser(ctx, other.ID, "Other", got.Email); !errors.Is(err, ErrEmailExists) {
		t.Fatalf("duplicate email error = %v, want ErrEmailExists", err)
	}

	subject := uniqueTestName("subject")
	if err := s.SetOIDCSubject(ctx, user.ID, subject); err != nil {
		t.Fatalf("set oidc subject: %v", err)
	}
	bySubject, err := s.GetUserByOIDCSubject(ctx, subject)
	if err != nil || bySubject.ID != user.ID {
		t.Fatalf("get user by oidc subject = %+v, %v", bySubject, err)
	}
	if err := s.SetOIDCSubject(ctx, user.ID, ""); err != nil {
		t.Fatalf("clear oidc subject: %v", err)
	}
	if _, err := s.GetUserByOIDCSubject(ctx, subject); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cleared oidc subject error = %v, want ErrNotFound", err)
	}

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	found := false
	for _, listed := range users {
		if listed.ID == user.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("user %s is missing from ListUsers", user.ID)
	}
}

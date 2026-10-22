package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInviteLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	invite, err := s.CreateInvite(ctx, Invite{
		TokenHash: []byte(uniqueTestName("invite")),
		Username:  uniqueTestName("invited"),
		Email:     "Invited@Example.COM",
		Role:      RoleViewer,
		ScopeType: ScopeOrganization,
		CreatedBy: "admin",
	})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM user_invite WHERE id::text = $1`, invite.ID)
	})
	if invite.ExpiresAt.Sub(invite.CreatedAt) < 71*time.Hour {
		t.Fatalf("invite lifetime = %v, want about 72h", invite.ExpiresAt.Sub(invite.CreatedAt))
	}

	pending, err := s.ListPendingInvites(ctx)
	if err != nil {
		t.Fatalf("list pending invites: %v", err)
	}
	if !containsInvite(pending, invite.ID) {
		t.Fatalf("pending invites do not contain %s", invite.ID)
	}

	user, err := s.ConsumeInvite(ctx, invite.TokenHash, "invited-password-hash")
	if err != nil {
		t.Fatalf("consume invite: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM role_binding WHERE subject_id::text = $1`, user.ID)
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM app_user WHERE id::text = $1`, user.ID)
	})
	if user.Username != invite.Username || user.Email != "invited@example.com" {
		t.Fatalf("consumed user = %+v", user)
	}
	if !user.TOTPEnabledAt.IsZero() {
		t.Fatal("invite created an enabled TOTP")
	}
	bindings, err := s.ListRoleBindingsForUser(ctx, user.ID)
	if err != nil || len(bindings) != 1 || bindings[0].Role != RoleViewer ||
		bindings[0].ScopeType != ScopeOrganization {
		t.Fatalf("initial bindings = %+v, %v", bindings, err)
	}
	if _, err := s.ConsumeInvite(ctx, invite.TokenHash, "another-password-hash"); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("second consume error = %v, want ErrInviteInvalid", err)
	}
	if _, err := s.CreateInvite(ctx, Invite{
		TokenHash: invite.TokenHash, Username: uniqueTestName("duplicate"),
		Role: RoleViewer, ScopeType: ScopeOrganization,
	}); err == nil {
		t.Fatal("duplicate invite token was accepted")
	}
	consumed, err := s.GetInviteByTokenHash(ctx, invite.TokenHash)
	if err != nil || consumed.UsedAt.IsZero() {
		t.Fatalf("consumed invite = %+v, %v", consumed, err)
	}

	expired, err := s.CreateInvite(ctx, Invite{
		TokenHash: []byte(uniqueTestName("expired")),
		Username:  uniqueTestName("expired-user"),
		Role:      RoleViewer,
		ScopeType: ScopeOrganization,
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("create expired invite: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM user_invite WHERE id::text = $1`, expired.ID)
	})
	if _, err := s.ConsumeInvite(ctx, expired.TokenHash, "password-hash"); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("expired invite error = %v, want ErrInviteExpired", err)
	}

	revoked, err := s.CreateInvite(ctx, Invite{
		TokenHash: []byte(uniqueTestName("revoked")),
		Username:  uniqueTestName("revoked-user"),
		Role:      RoleViewer,
		ScopeType: ScopeOrganization,
	})
	if err != nil {
		t.Fatalf("create revoked invite: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM user_invite WHERE id::text = $1`, revoked.ID)
	})
	if err := s.RevokeInvite(ctx, revoked.ID); err != nil {
		t.Fatalf("revoke invite: %v", err)
	}
	if _, err := s.ConsumeInvite(ctx, revoked.TokenHash, "password-hash"); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("revoked invite error = %v, want ErrInviteInvalid", err)
	}
}

func containsInvite(invites []Invite, id string) bool {
	for _, invite := range invites {
		if invite.ID == id {
			return true
		}
	}
	return false
}

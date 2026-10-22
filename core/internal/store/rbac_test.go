package store

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testSequence atomic.Int64

func uniqueTestName(prefix string) string {
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), testSequence.Add(1))
}

func newRBACTestUser(t *testing.T, s *Store, prefix string) User {
	t.Helper()
	user, err := s.CreateUserFull(context.Background(), uniqueTestName(prefix), "password-hash",
		"Test user", uniqueTestName("mail")+"@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = s.pool.Exec(ctx, `DELETE FROM role_binding WHERE subject_id::text = $1`, user.ID)
		_, _ = s.pool.Exec(ctx, `DELETE FROM api_key WHERE owner_id::text = $1`, user.ID)
		_, _ = s.pool.Exec(ctx, `DELETE FROM app_user WHERE id::text = $1`, user.ID)
	})
	return user
}

func newTestGroup(t *testing.T, s *Store) ServerGroup {
	t.Helper()
	group, err := s.CreateGroup(context.Background(), uniqueTestName("group"))
	if err != nil {
		t.Fatalf("create group: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM server_group WHERE id::text = $1`, group.ID)
	})
	return group
}

func newTestTeam(t *testing.T, s *Store) Team {
	t.Helper()
	team, err := s.CreateTeam(context.Background(), uniqueTestName("team"))
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = s.pool.Exec(ctx, `DELETE FROM role_binding WHERE subject_type = 'team' AND subject_id::text = $1`, team.ID)
		_, _ = s.pool.Exec(ctx, `DELETE FROM team WHERE id::text = $1`, team.ID)
	})
	return team
}

func newTestServer(t *testing.T, s *Store) string {
	t.Helper()
	id, err := s.CreateServer(context.Background(), uniqueTestName("server"))
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM server WHERE id::text = $1`, id)
	})
	return id
}

func mustCreateBinding(t *testing.T, s *Store, binding RoleBinding) RoleBinding {
	t.Helper()
	created, err := s.CreateRoleBinding(context.Background(), binding)
	if err != nil {
		t.Fatalf("create role binding: %v", err)
	}
	return created
}

func TestGroupsTeamsAndBindings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newRBACTestUser(t, s, "bindings")
	other := newRBACTestUser(t, s, "bindings-other")
	group := newTestGroup(t, s)
	team := newTestTeam(t, s)
	serverID := newTestServer(t, s)

	if err := s.SetServerGroup(ctx, serverID, group.ID); err != nil {
		t.Fatalf("set server group: %v", err)
	}
	var storedGroup string
	if err := s.pool.QueryRow(ctx, `SELECT COALESCE(group_id::text, '') FROM server WHERE id = $1`, serverID).
		Scan(&storedGroup); err != nil {
		t.Fatalf("read server group: %v", err)
	}
	if storedGroup != group.ID {
		t.Fatalf("server group = %q, want %q", storedGroup, group.ID)
	}

	if err := s.AddTeamMember(ctx, team.ID, user.ID); err != nil {
		t.Fatalf("add team member: %v", err)
	}
	members, err := s.ListTeamMembers(ctx, team.ID)
	if err != nil || len(members) != 1 || members[0].UserID != user.ID {
		t.Fatalf("team members = %+v, %v", members, err)
	}

	direct := mustCreateBinding(t, s, RoleBinding{
		SubjectType: SubjectUser, SubjectID: user.ID,
		ScopeType: ScopeServer, ScopeServerID: serverID, Role: RoleViewer, CreatedBy: "test",
	})
	teamBinding := mustCreateBinding(t, s, RoleBinding{
		SubjectType: SubjectTeam, SubjectID: team.ID,
		ScopeType: ScopeGroup, ScopeGroupID: group.ID, Role: RoleMetricsOnly, CreatedBy: "test",
	})
	if _, err := s.CreateRoleBinding(ctx, RoleBinding{
		SubjectType: SubjectUser, SubjectID: user.ID,
		ScopeType: ScopeServer, ScopeServerID: serverID, Role: RoleViewer, CreatedBy: "test",
	}); !errors.Is(err, ErrBindingExists) {
		t.Fatalf("duplicate binding error = %v, want ErrBindingExists", err)
	}
	if _, err := s.CreateRoleBinding(ctx, RoleBinding{
		SubjectType: SubjectUser, SubjectID: other.ID,
		ScopeType: ScopeStack, Role: RoleViewer,
	}); err == nil {
		t.Fatal("binding without server and stack name was accepted")
	}

	bindings, err := s.ListRoleBindingsForUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list bindings for user: %v", err)
	}
	if len(bindings) != 2 {
		t.Fatalf("bindings = %+v, want direct plus team", bindings)
	}
	seen := map[string]bool{}
	for _, binding := range bindings {
		seen[binding.ID] = true
	}
	if !seen[direct.ID] || !seen[teamBinding.ID] {
		t.Fatalf("bindings = %+v, want %s and %s", bindings, direct.ID, teamBinding.ID)
	}

	if err := s.SetServerGroup(ctx, serverID, ""); err != nil {
		t.Fatalf("clear server group: %v", err)
	}
}

func TestLastAdminInvariant(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	admin := func(t *testing.T, prefix string) User {
		user := newRBACTestUser(t, s, prefix)
		mustCreateBinding(t, s, RoleBinding{
			SubjectType: SubjectUser, SubjectID: user.ID,
			ScopeType: ScopeOrganization, Role: RoleAdmin, CreatedBy: "test",
		})
		return user
	}

	t.Run("delete binding", func(t *testing.T) {
		first, second := admin(t, "admin-one"), admin(t, "admin-two")
		count, err := s.CountActiveOrganizationAdmins(ctx)
		if err != nil {
			t.Fatalf("count admins: %v", err)
		}
		if count != 2 {
			t.Skipf("the test database has %d admins, want an isolated 2", count)
		}
		bindings, err := s.ListRoleBindings(ctx, SubjectUser, first.ID)
		if err != nil || len(bindings) != 1 {
			t.Fatalf("first bindings = %+v, %v", bindings, err)
		}
		if err := s.DeleteRoleBinding(ctx, bindings[0].ID); err != nil {
			t.Fatalf("delete first admin: %v", err)
		}
		bindings, err = s.ListRoleBindings(ctx, SubjectUser, second.ID)
		if err != nil || len(bindings) != 1 {
			t.Fatalf("second bindings = %+v, %v", bindings, err)
		}
		if err := s.DeleteRoleBinding(ctx, bindings[0].ID); !errors.Is(err, ErrLastAdmin) {
			t.Fatalf("delete last admin error = %v, want ErrLastAdmin", err)
		}
	})

	t.Run("disable user", func(t *testing.T) {
		first, second := admin(t, "disable-one"), admin(t, "disable-two")
		count, err := s.CountActiveOrganizationAdmins(ctx)
		if err != nil {
			t.Fatalf("count admins: %v", err)
		}
		if count != 2 {
			t.Skipf("the test database has %d admins, want an isolated 2", count)
		}
		if err := s.DisableUser(ctx, first.ID); err != nil {
			t.Fatalf("disable first admin: %v", err)
		}
		if err := s.DisableUser(ctx, second.ID); !errors.Is(err, ErrLastAdmin) {
			t.Fatalf("disable last admin error = %v, want ErrLastAdmin", err)
		}
		got, err := s.GetUser(ctx, second.ID)
		if err != nil || !got.DisabledAt.IsZero() {
			t.Fatalf("last admin changed: %+v, %v", got, err)
		}
	})

	t.Run("team membership", func(t *testing.T) {
		first := newRBACTestUser(t, s, "team-admin")
		countBefore, err := s.CountActiveOrganizationAdmins(ctx)
		if err != nil {
			t.Fatalf("count admins: %v", err)
		}
		if countBefore != 0 {
			t.Skipf("the test database has %d admins, want an isolated 0", countBefore)
		}
		team := newTestTeam(t, s)
		mustCreateBinding(t, s, RoleBinding{
			SubjectType: SubjectTeam, SubjectID: team.ID,
			ScopeType: ScopeOrganization, Role: RoleAdmin, CreatedBy: "test",
		})
		if err := s.AddTeamMember(ctx, team.ID, first.ID); err != nil {
			t.Fatalf("add admin to team: %v", err)
		}
		if err := s.RemoveTeamMember(ctx, team.ID, first.ID); !errors.Is(err, ErrLastAdmin) {
			t.Fatalf("remove last admin from team error = %v, want ErrLastAdmin", err)
		}
		if err := s.DeleteTeam(ctx, team.ID); !errors.Is(err, ErrLastAdmin) {
			t.Fatalf("delete last admin team error = %v, want ErrLastAdmin", err)
		}
	})

	t.Run("change role", func(t *testing.T) {
		first := newRBACTestUser(t, s, "role-admin")
		countBefore, err := s.CountActiveOrganizationAdmins(ctx)
		if err != nil {
			t.Fatalf("count admins: %v", err)
		}
		if countBefore != 0 {
			t.Skipf("the test database has %d admins, want an isolated 0", countBefore)
		}
		binding := mustCreateBinding(t, s, RoleBinding{
			SubjectType: SubjectUser, SubjectID: first.ID,
			ScopeType: ScopeOrganization, Role: RoleAdmin, CreatedBy: "test",
		})
		if err := s.UpdateRoleBindingRole(ctx, binding.ID, RoleViewer); !errors.Is(err, ErrLastAdmin) {
			t.Fatalf("demote last admin error = %v, want ErrLastAdmin", err)
		}
	})

	t.Run("concurrent deletion", func(t *testing.T) {
		first, second := admin(t, "concurrent-one"), admin(t, "concurrent-two")
		count, err := s.CountActiveOrganizationAdmins(ctx)
		if err != nil {
			t.Fatalf("count admins: %v", err)
		}
		if count != 2 {
			t.Skipf("the test database has %d admins, want an isolated 2", count)
		}
		firstBindings, _ := s.ListRoleBindings(ctx, SubjectUser, first.ID)
		secondBindings, _ := s.ListRoleBindings(ctx, SubjectUser, second.ID)
		if len(firstBindings) != 1 || len(secondBindings) != 1 {
			t.Fatal("expected one org admin binding per user")
		}

		results := make([]error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			results[0] = s.DeleteRoleBinding(ctx, firstBindings[0].ID)
		}()
		go func() {
			defer wg.Done()
			results[1] = s.DeleteRoleBinding(ctx, secondBindings[0].ID)
		}()
		wg.Wait()

		lastAdmin := 0
		success := 0
		for _, result := range results {
			switch {
			case result == nil:
				success++
			case errors.Is(result, ErrLastAdmin):
				lastAdmin++
			default:
				t.Fatalf("unexpected concurrent error: %v", result)
			}
		}
		if success != 1 || lastAdmin != 1 {
			t.Fatalf("concurrent results = %v, want one success and one ErrLastAdmin", results)
		}
	})
}

func TestAuditActorAndScope(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	user := newRBACTestUser(t, s, "audit")
	action := uniqueTestName("test.audit")
	t.Cleanup(func() {
		_, _ = s.pool.Exec(context.Background(), `DELETE FROM audit_event WHERE action = $1`, action)
	})

	scope := map[string]any{"type": "server", "server_id": user.ID}
	if err := s.AppendAudit(ctx, AuditEvent{
		Actor: user.Username, ActorID: user.ID, Action: action, IP: "192.0.2.20",
		Result: "success", Details: map[string]any{"test": true}, Scope: scope,
	}); err != nil {
		t.Fatalf("append audit: %v", err)
	}
	entries, err := s.ListAudit(ctx, 50)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action != action {
			continue
		}
		found = true
		if entry.ActorID != user.ID || entry.Scope["type"] != "server" ||
			entry.Scope["server_id"] != user.ID {
			t.Fatalf("audit entry = %+v", entry)
		}
	}
	if !found {
		t.Fatalf("audit action %s not found", action)
	}
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func meTestServers() stubServers {
	return stubServers{byID: map[string]store.Server{
		"a": {ID: "a", Name: "Server A", GroupID: "ga"},
		"b": {ID: "b", Name: "Server B", GroupID: "gb"},
	}}
}

func meTestAgents() fakeAgents {
	return fakeAgents{agents: []hub.AgentInfo{
		{
			ServerID: "a", Hostname: "a",
			ManagedStacks: []string{"web", "db"},
			ContainerList: []*agentv1.Container{
				{Id: "aw", Name: "aw", ComposeProject: "web"},
				{Id: "ad", Name: "ad", ComposeProject: "db"},
			},
		},
		{
			ServerID: "b", Hostname: "b",
			ManagedStacks: []string{"web"},
			ContainerList: []*agentv1.Container{
				{Id: "bw", Name: "bw", ComposeProject: "web"},
			},
		},
	}}
}

func getMe(t *testing.T, effective rbac.Effective) MeResponse {
	t.Helper()
	session := Session{
		User: store.User{
			ID: "u1", Username: "alice", DisplayName: "Alice", Email: "alice@example.com",
		},
		Stage: store.StageFull,
	}
	handler := NewMeHandler(MeDeps{Servers: meTestServers(), Agents: meTestAgents()})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	request = request.WithContext(withEffective(withSession(context.Background(), session), effective))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}
	var response MeResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode me response: %v", err)
	}
	return response
}

func TestMeAdminPermissions(t *testing.T) {
	response := getMe(t, rbac.Resolve([]rbac.Binding{{
		Scope: rbac.Scope{Type: rbac.ScopeOrganization},
		Role:  rbac.RoleAdmin,
	}}))
	if response.User.ID != "u1" || response.User.DisplayName != "Alice" || response.User.Email != "alice@example.com" {
		t.Fatalf("user = %+v", response.User)
	}
	if len(response.Permissions.Organization) == 0 {
		t.Fatal("organization permissions are empty")
	}
	if len(response.Permissions.Servers) != 2 {
		t.Fatalf("servers = %+v, want both", response.Permissions.Servers)
	}
	for _, serverID := range []string{"a", "b"} {
		if len(response.Permissions.Servers[serverID]) == 0 {
			t.Errorf("server %s has no permissions", serverID)
		}
		if len(response.Permissions.Stacks[serverID]) == 0 {
			t.Errorf("server %s has no stacks", serverID)
		}
		if len(response.Permissions.Containers[serverID]) == 0 {
			t.Errorf("server %s has no containers", serverID)
		}
	}
	if !containsPermission(response.Permissions.Organization, rbac.PermServerRemove) {
		t.Error("admin organization permissions lack server.remove")
	}
}

func TestMeStackOnlyScope(t *testing.T) {
	response := getMe(t, rbac.Resolve([]rbac.Binding{{
		Scope: rbac.Scope{Type: rbac.ScopeStack, ServerID: "a", Name: "web"},
		Role:  rbac.RoleAppOperator,
	}}))
	if len(response.Permissions.Organization) != 0 {
		t.Fatalf("organization permissions = %v, want none", response.Permissions.Organization)
	}
	if len(response.Permissions.Servers) != 1 || response.Permissions.Servers["a"] == nil {
		t.Fatalf("servers = %+v, want only a", response.Permissions.Servers)
	}
	if len(response.Permissions.Stacks["a"]) != 1 || response.Permissions.Stacks["a"]["web"] == nil {
		t.Fatalf("stacks = %+v, want only a/web", response.Permissions.Stacks)
	}
	if _, ok := response.Permissions.Stacks["a"]["db"]; ok {
		t.Fatal("stack-only user sees db")
	}
	if len(response.Permissions.Containers["a"]) != 1 || response.Permissions.Containers["a"]["aw"] == nil {
		t.Fatalf("containers = %+v, want only a/aw", response.Permissions.Containers)
	}
	if _, ok := response.Permissions.Containers["a"]["ad"]; ok {
		t.Fatal("stack-only user sees the db container")
	}
	if len(response.Bindings.Bindings) != 1 || response.Bindings.Bindings[0].Role != rbac.RoleAppOperator {
		t.Fatalf("bindings = %+v", response.Bindings)
	}
}

func containsPermission(permissions []rbac.Permission, wanted rbac.Permission) bool {
	for _, permission := range permissions {
		if permission == wanted {
			return true
		}
	}
	return false
}

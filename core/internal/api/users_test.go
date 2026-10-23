package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

func createAdminExtraUser(t *testing.T, panel *testPanel, username string) store.User {
	t.Helper()
	user, err := panel.store.CreateUserFull(context.Background(), username, auth.HashPassword(testPassword),
		strings.Title(username), username+"@example.com")
	if err != nil {
		t.Fatalf("create %s: %v", username, err)
	}
	return user
}

func TestUserAdministrationAPI(t *testing.T) {
	panel := newTestPanel(t)
	admin := panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	bob := createAdminExtraUser(t, panel, "bob")

	response := panel.get(t, "/api/v1/users")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list users status = %d, want 200", response.StatusCode)
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read users: %v", err)
	}
	if strings.Contains(string(raw), "password_hash") || strings.Contains(string(raw), "totp_secret") {
		t.Fatal("users response leaked a secret field")
	}
	var listed usersResponse
	if err := json.Unmarshal(raw, &listed); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(listed.Users) != 2 {
		t.Fatalf("users = %+v, want admin and bob", listed.Users)
	}
	foundAdmin := false
	for _, user := range listed.Users {
		if user.ID == admin.ID {
			foundAdmin = true
			if !user.TOTPEnabled || user.Disabled {
				t.Fatalf("admin entry = %+v", user)
			}
			if len(user.Bindings) != 1 || user.Bindings[0].Role != string(rbac.RoleAdmin) ||
				user.Bindings[0].Scope.Type != string(rbac.ScopeOrganization) {
				t.Fatalf("admin bindings = %+v", user.Bindings)
			}
		}
	}
	if !foundAdmin {
		t.Fatal("admin is missing from the user list")
	}

	if response := panel.do(t, panel.client, http.MethodPatch, "/api/v1/users/"+bob.ID,
		map[string]string{"display_name": "Bob B.", "email": "BOB2@Example.com"}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("update user status = %d, want 204", response.StatusCode)
	}
	updated, err := panel.store.GetUser(context.Background(), bob.ID)
	if err != nil || updated.DisplayName != "Bob B." || updated.Email != "bob2@example.com" {
		t.Fatalf("updated bob = %+v, %v", updated, err)
	}

	response = panel.post(t, "/api/v1/users/"+bob.ID+"/bindings", map[string]any{
		"role":  string(rbac.RoleViewer),
		"scope": map[string]any{"type": string(rbac.ScopeOrganization)},
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create binding status = %d, want 201", response.StatusCode)
	}
	var binding adminBinding
	decodeBody(t, response, &binding)
	if binding.Role != string(rbac.RoleViewer) || binding.Scope.Type != string(rbac.ScopeOrganization) {
		t.Fatalf("binding = %+v", binding)
	}

	if response := panel.post(t, "/api/v1/users/"+bob.ID+"/bindings", map[string]any{
		"role":  string(rbac.RoleViewer),
		"scope": map[string]any{"type": string(rbac.ScopeOrganization)},
	}); response.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate binding status = %d, want 409", response.StatusCode)
	}
	if response := panel.post(t, "/api/v1/users/"+bob.ID+"/bindings", map[string]any{
		"role":  "not-a-role",
		"scope": map[string]any{"type": string(rbac.ScopeOrganization)},
	}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid role status = %d, want 400", response.StatusCode)
	}
	if response := panel.post(t, "/api/v1/users/"+bob.ID+"/bindings", map[string]any{
		"role":  string(rbac.RoleViewer),
		"scope": map[string]any{"type": string(rbac.ScopeServer), "server_id": "missing"},
	}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing server status = %d, want 400", response.StatusCode)
	}

	if response := panel.do(t, panel.client, http.MethodPatch, "/api/v1/bindings/"+binding.ID,
		map[string]string{"role": string(rbac.RoleAppOperator)}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("update binding status = %d, want 204", response.StatusCode)
	}
	response = panel.get(t, "/api/v1/users/"+bob.ID+"/bindings")
	var bindings userBindingsResponse
	decodeBody(t, response, &bindings)
	if len(bindings.Bindings) != 1 || bindings.Bindings[0].Role != string(rbac.RoleAppOperator) {
		t.Fatalf("bob bindings = %+v", bindings.Bindings)
	}

	if response := panel.post(t, "/api/v1/users/"+bob.ID+"/disable", nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("disable bob status = %d, want 204", response.StatusCode)
	}
	disabled, err := panel.store.GetUser(context.Background(), bob.ID)
	if err != nil || disabled.DisabledAt.IsZero() {
		t.Fatalf("disabled bob = %+v, %v", disabled, err)
	}
	if response := panel.post(t, "/api/v1/users/"+bob.ID+"/enable", nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("enable bob status = %d, want 204", response.StatusCode)
	}

	if response := panel.post(t, "/api/v1/users/"+admin.ID+"/disable", nil); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("self disable status = %d, want 400", response.StatusCode)
	}

	adminBindings := panel.get(t, "/api/v1/users/"+admin.ID+"/bindings")
	var own userBindingsResponse
	decodeBody(t, adminBindings, &own)
	if len(own.Bindings) != 1 {
		t.Fatalf("admin bindings = %+v", own.Bindings)
	}
	if response := panel.do(t, panel.client, http.MethodPatch, "/api/v1/bindings/"+own.Bindings[0].ID,
		map[string]string{"role": string(rbac.RoleViewer)}); response.StatusCode != http.StatusConflict {
		t.Fatalf("demote last admin status = %d, want 409", response.StatusCode)
	}
	if response := panel.do(t, panel.client, http.MethodDelete, "/api/v1/bindings/"+own.Bindings[0].ID, nil); response.StatusCode != http.StatusConflict {
		t.Fatalf("delete last admin status = %d, want 409", response.StatusCode)
	}
	if response := panel.do(t, panel.client, http.MethodDelete, "/api/v1/bindings/"+binding.ID, nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete bob binding status = %d, want 204", response.StatusCode)
	}

	// Every successful mutation is audited with the acting administrator.
	entries, err := panel.store.ListAudit(context.Background(), 200)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	actions := map[string]bool{}
	for _, entry := range entries {
		if entry.ActorID == admin.ID {
			actions[entry.Action] = true
		}
	}
	for _, action := range []string{"user_update", "user_disable", "user_enable", "binding_create",
		"binding_update", "binding_delete"} {
		if !actions[action] {
			t.Errorf("missing audit action %s", action)
		}
	}

	// A viewer is refused by the route permission before any handler runs.
	viewerClient, _ := criterionUser(t, panel, "viewer-admin-route", string(rbac.RoleViewer), store.RoleBinding{
		ScopeType: store.ScopeOrganization,
	})
	if response := panel.do(t, viewerClient, http.MethodGet, "/api/v1/users", nil); response.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer users status = %d, want 403", response.StatusCode)
	}
}

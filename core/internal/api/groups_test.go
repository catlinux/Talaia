package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

func createSessionOnlyUser(t *testing.T, panel *testPanel, name string) (store.User, *http.Client) {
	t.Helper()
	ctx := context.Background()
	user, err := panel.store.CreateUserFull(ctx, name, auth.HashPassword(testPassword), name, name+"@example.com")
	if err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	client := panel.newClient(t)
	token := auth.NewSessionToken()
	if _, err := panel.store.CreateSession(ctx, user.ID, auth.HashToken(token), store.StageFull,
		panel.clock.Now().Add(fullSessionTTL), "192.0.2.10", "test"); err != nil {
		t.Fatalf("create session for %s: %v", name, err)
	}
	parsed, err := url.Parse(panel.server.URL)
	if err != nil {
		t.Fatalf("parse panel URL: %v", err)
	}
	client.Jar.SetCookies(parsed, []*http.Cookie{sessionCookie(token, store.StageFull)})
	return user, client
}

func TestGroupAdministrationAndVisibility(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	ctx := context.Background()
	serverID, err := panel.store.CreateServer(ctx, "group-server-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	response := panel.post(t, "/api/v1/groups", map[string]string{"name": "operations"})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create group status = %d, want 201", response.StatusCode)
	}
	var group groupEntry
	decodeBody(t, response, &group)
	if group.Name != "operations" || group.ID == "" {
		t.Fatalf("group = %+v", group)
	}
	if response := panel.post(t, "/api/v1/groups", map[string]string{"name": "operations"}); response.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate group status = %d, want 409", response.StatusCode)
	}
	if response := panel.post(t, "/api/v1/groups", map[string]string{"name": strings.Repeat("x", 65)}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("long group name status = %d, want 400", response.StatusCode)
	}

	response = panel.get(t, "/api/v1/groups")
	var groups groupsResponse
	decodeBody(t, response, &groups)
	if len(groups.Groups) != 1 || groups.Groups[0].ID != group.ID {
		t.Fatalf("groups = %+v", groups.Groups)
	}
	if response := panel.do(t, panel.client, http.MethodPatch, "/api/v1/groups/"+group.ID,
		map[string]string{"name": "operations-renamed"}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("update group status = %d, want 204", response.StatusCode)
	}

	if response := panel.do(t, panel.client, http.MethodPut, "/api/v1/servers/"+serverID+"/group",
		map[string]any{"group_id": group.ID}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("set server group status = %d, want 204", response.StatusCode)
	}
	server, err := panel.store.GetServer(ctx, serverID)
	if err != nil || server.GroupID != group.ID {
		t.Fatalf("server group = %+v, %v", server, err)
	}

	groupClient, _ := criterionUser(t, panel, "group-viewer", string(rbac.RoleViewer), store.RoleBinding{
		ScopeType: store.ScopeGroup, ScopeGroupID: group.ID,
	})
	visible := decodePanelJSON[serversResponse](t, panel.do(t, groupClient, http.MethodGet, "/api/v1/servers", nil))
	if len(visible.Servers) != 1 || visible.Servers[0].ID != serverID {
		t.Fatalf("group viewer servers = %+v, want only %s", visible.Servers, serverID)
	}

	if response := panel.do(t, panel.client, http.MethodPut, "/api/v1/servers/"+serverID+"/group",
		map[string]any{"group_id": nil}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("clear server group status = %d, want 204", response.StatusCode)
	}
	visible = decodePanelJSON[serversResponse](t, panel.do(t, groupClient, http.MethodGet, "/api/v1/servers", nil))
	if len(visible.Servers) != 0 {
		t.Fatalf("group viewer still sees %+v after clearing the group", visible.Servers)
	}

	if response := panel.do(t, panel.client, http.MethodDelete, "/api/v1/groups/"+group.ID, nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete group status = %d, want 204", response.StatusCode)
	}

	entries, err := panel.store.ListAudit(ctx, 200)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	actions := map[string]bool{}
	for _, entry := range entries {
		actions[entry.Action] = true
	}
	for _, action := range []string{"group_create", "group_update", "server_group_set", "group_delete"} {
		if !actions[action] {
			t.Errorf("missing audit action %s", action)
		}
	}
}

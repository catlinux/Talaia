package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/rbac"
)

func TestTeamAdministrationAndAccess(t *testing.T) {
	panel := newTestPanel(t)
	admin := panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	ctx := context.Background()
	serverID, err := panel.store.CreateServer(ctx, "team-server-"+strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", ""))
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	response := panel.post(t, "/api/v1/teams", map[string]string{"name": "support"})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create team status = %d, want 201", response.StatusCode)
	}
	var team teamEntry
	decodeBody(t, response, &team)
	if team.Name != "support" || team.ID == "" {
		t.Fatalf("team = %+v", team)
	}
	if response := panel.post(t, "/api/v1/teams", map[string]string{"name": "support"}); response.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate team status = %d, want 409", response.StatusCode)
	}
	if response := panel.post(t, "/api/v1/teams", map[string]string{"name": strings.Repeat("x", 65)}); response.StatusCode != http.StatusBadRequest {
		t.Fatalf("long team name status = %d, want 400", response.StatusCode)
	}
	if response := panel.do(t, panel.client, http.MethodPatch, "/api/v1/teams/"+team.ID,
		map[string]string{"name": "support-renamed"}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("update team status = %d, want 204", response.StatusCode)
	}
	response = panel.get(t, "/api/v1/teams")
	var teams teamsResponse
	decodeBody(t, response, &teams)
	if len(teams.Teams) != 1 || teams.Teams[0].ID != team.ID {
		t.Fatalf("teams = %+v", teams.Teams)
	}

	member, memberClient := createSessionOnlyUser(t, panel, "team-member")
	if response := panel.do(t, panel.client, http.MethodPut,
		"/api/v1/teams/"+team.ID+"/members/"+member.ID, nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("add member status = %d, want 204", response.StatusCode)
	}
	response = panel.get(t, "/api/v1/teams/"+team.ID)
	var detail teamDetail
	decodeBody(t, response, &detail)
	if len(detail.Members) != 1 || detail.Members[0] != "team-member" {
		t.Fatalf("team members = %+v", detail.Members)
	}
	if response := panel.do(t, panel.client, http.MethodPut,
		"/api/v1/teams/"+team.ID+"/members/"+member.ID, nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("duplicate member status = %d, want 204", response.StatusCode)
	}

	response = panel.post(t, "/api/v1/teams/"+team.ID+"/bindings", map[string]any{
		"role":  string(rbac.RoleViewer),
		"scope": map[string]any{"type": string(rbac.ScopeServer), "server_id": serverID},
	})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("team binding status = %d, want 201", response.StatusCode)
	}
	var binding adminBinding
	decodeBody(t, response, &binding)
	if binding.Role != string(rbac.RoleViewer) || binding.Scope.ServerID != serverID {
		t.Fatalf("team binding = %+v", binding)
	}
	visible := decodePanelJSON[serversResponse](t, panel.do(t, memberClient, http.MethodGet, "/api/v1/servers", nil))
	if len(visible.Servers) != 1 || visible.Servers[0].ID != serverID {
		t.Fatalf("team member servers = %+v, want only %s", visible.Servers, serverID)
	}

	if response := panel.do(t, panel.client, http.MethodDelete,
		"/api/v1/teams/"+team.ID+"/members/"+member.ID, nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("remove member status = %d, want 204", response.StatusCode)
	}
	visible = decodePanelJSON[serversResponse](t, panel.do(t, memberClient, http.MethodGet, "/api/v1/servers", nil))
	if len(visible.Servers) != 0 {
		t.Fatalf("former team member still sees %+v", visible.Servers)
	}
	if response := panel.do(t, panel.client, http.MethodDelete, "/api/v1/teams/"+team.ID, nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("delete team status = %d, want 204", response.StatusCode)
	}

	// A team admin who is the last active administrator cannot remove their own
	// membership or delete the team.
	lastTeamResponse := panel.post(t, "/api/v1/teams", map[string]string{"name": "last-admin-team"})
	var lastTeam teamEntry
	decodeBody(t, lastTeamResponse, &lastTeam)
	lastAdmin, lastAdminClient := createSessionOnlyUser(t, panel, "last-team-admin")
	if response := panel.do(t, panel.client, http.MethodPut,
		"/api/v1/teams/"+lastTeam.ID+"/members/"+lastAdmin.ID, nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("add last admin member status = %d, want 204", response.StatusCode)
	}
	if response := panel.post(t, "/api/v1/teams/"+lastTeam.ID+"/bindings", map[string]any{
		"role":  string(rbac.RoleAdmin),
		"scope": map[string]any{"type": string(rbac.ScopeOrganization)},
	}); response.StatusCode != http.StatusCreated {
		t.Fatalf("team admin binding status = %d, want 201", response.StatusCode)
	}
	if response := panel.do(t, lastAdminClient, http.MethodPost, "/api/v1/users/"+admin.ID+"/disable", nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("disable the other admin status = %d, want 204", response.StatusCode)
	}
	if response := panel.do(t, lastAdminClient, http.MethodDelete,
		"/api/v1/teams/"+lastTeam.ID+"/members/"+lastAdmin.ID, nil); response.StatusCode != http.StatusConflict {
		t.Fatalf("remove last admin from team status = %d, want 409", response.StatusCode)
	}
	if response := panel.do(t, lastAdminClient, http.MethodDelete, "/api/v1/teams/"+lastTeam.ID, nil); response.StatusCode != http.StatusConflict {
		t.Fatalf("delete last admin team status = %d, want 409", response.StatusCode)
	}

	entries, err := panel.store.ListAudit(ctx, 300)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	actions := map[string]bool{}
	for _, entry := range entries {
		actions[entry.Action] = true
	}
	for _, action := range []string{"team_create", "team_update", "team_delete", "team_member_add",
		"team_member_remove", "binding_create", "user_disable"} {
		if !actions[action] {
			t.Errorf("missing audit action %s", action)
		}
	}
}

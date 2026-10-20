package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func resourcesPath(serverID, action string) string {
	return "/api/v1/servers/" + serverID + "/resources/" + action
}

func TestResourceEndpointsNeedAFullSession(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)

	if got := panel.post(t, resourcesPath("x", "list"), map[string]any{"kind": "image"}).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("list status = %d, want 401", got)
	}
	if got := panel.post(t, resourcesPath("x", "remove"), map[string]any{}).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("remove status = %d, want 401", got)
	}
}

func TestListResourcesCreatesAJob(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)

	if got := panel.post(t, resourcesPath(serverID, "list"), map[string]any{"kind": "volume"}).StatusCode; got != http.StatusConflict {
		t.Errorf("offline status = %d, want 409", got)
	}

	panel.agents.set(agentWithContainers(serverID))
	response := panel.post(t, resourcesPath(serverID, "list"), map[string]any{"kind": "volume"})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", response.StatusCode)
	}
	var created jobCreatedResponse
	decodeBody(t, response, &created)

	sent := panel.dispatcher.sent()
	if len(sent) != 1 || sent[0].GetListResources().GetKind() != agentv1.ResourceKind_RESOURCE_KIND_VOLUME ||
		sent[0].GetJobId() != created.JobID {
		t.Errorf("sent = %v", sent)
	}

	for _, kind := range []string{"", "container", "VOLUME"} {
		if got := panel.post(t, resourcesPath(serverID, "list"), map[string]any{"kind": kind}).StatusCode; got != http.StatusBadRequest {
			t.Errorf("kind %q: status = %d, want 400", kind, got)
		}
	}
}

func TestRemoveResources(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithContainers(serverID))

	good := []map[string]any{
		{"kind": "image", "id": "sha256:" + strings.Repeat("a", 64)},
		{"kind": "volume", "id": "old_cache"},
		{"kind": "network", "id": strings.Repeat("b", 12)},
	}
	tests := []struct {
		name string
		body map[string]any
		want int
	}{
		{"valid and confirmed", map[string]any{"items": good, "confirm": true}, http.StatusAccepted},
		{"not confirmed", map[string]any{"items": good}, http.StatusBadRequest},
		{"nothing to remove", map[string]any{"items": []any{}, "confirm": true}, http.StatusBadRequest},
		{"unknown kind", map[string]any{"items": []map[string]any{{"kind": "container", "id": "abcdefabcdef"}}, "confirm": true}, http.StatusBadRequest},
		{"bad volume name", map[string]any{"items": []map[string]any{{"kind": "volume", "id": "../x"}}, "confirm": true}, http.StatusBadRequest},
		{"image by tag", map[string]any{"items": []map[string]any{{"kind": "image", "id": "nginx:latest"}}, "confirm": true}, http.StatusBadRequest},
		{"network by name", map[string]any{"items": []map[string]any{{"kind": "network", "id": "bridge"}}, "confirm": true}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := panel.post(t, resourcesPath(serverID, "remove"), tt.body).StatusCode; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}

	// Too many at once.
	many := make([]map[string]any, maxRemovedResources+1)
	for i := range many {
		many[i] = map[string]any{"kind": "volume", "id": "v"}
	}
	if got := panel.post(t, resourcesPath(serverID, "remove"), map[string]any{"items": many, "confirm": true}).StatusCode; got != http.StatusBadRequest {
		t.Errorf("too many: status = %d, want 400", got)
	}

	// The command that left carries the typed references.
	var remove *agentv1.RemoveResources
	for _, msg := range panel.dispatcher.sent() {
		remove = msg.GetRemoveResources()
	}
	if remove == nil || len(remove.GetItems()) != 3 || remove.GetItems()[1].GetId() != "old_cache" {
		t.Errorf("remove command = %v", remove)
	}

	// The audit log lists what was asked, so a removal can be traced.
	entries, err := panel.store.ListAudit(context.Background(), 30)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action == "resources_remove" && entry.Actor == "admin" {
			found = true
		}
	}
	if !found {
		t.Error("the removal was not audited")
	}
}

func TestRemoveResourcesWhenTheAgentIsOffline(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)

	body := map[string]any{"items": []map[string]any{{"kind": "volume", "id": "old"}}, "confirm": true}
	if got := panel.post(t, resourcesPath(serverID, "remove"), body).StatusCode; got != http.StatusConflict {
		t.Errorf("status = %d, want 409", got)
	}
}

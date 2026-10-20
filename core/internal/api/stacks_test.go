package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const sampleStackCompose = "services:\n  web:\n    image: nginx:1.27\n"

func agentWithStacks(serverID string, stacks ...string) hub.AgentInfo {
	agent := agentWithContainers(serverID)
	agent.ManagedStacks = stacks
	return agent
}

func stackPath(serverID, name, suffix string) string {
	return "/api/v1/servers/" + serverID + "/stacks/" + name + suffix
}

func TestStackEndpointsNeedAFullSession(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)

	if got := panel.get(t, "/api/v1/servers/x/stacks").StatusCode; got != http.StatusUnauthorized {
		t.Errorf("list status = %d, want 401", got)
	}
	if got := panel.do(t, panel.client, http.MethodPut, stackPath("x", "shop", ""), map[string]any{"compose": sampleStackCompose}).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("write status = %d, want 401", got)
	}
	if got := panel.post(t, stackPath("x", "shop", "/actions"), map[string]any{"action": "up"}).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("action status = %d, want 401", got)
	}
}

func TestListStacks(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := "22222222-2222-2222-2222-222222222222"

	var offline stacksResponse
	decodeBody(t, panel.get(t, "/api/v1/servers/"+serverID+"/stacks"), &offline)
	if offline.Connected || len(offline.Stacks) != 0 {
		t.Errorf("offline answer = %+v", offline)
	}

	panel.agents.set(agentWithStacks(serverID, "blog", "shop"))
	var online stacksResponse
	decodeBody(t, panel.get(t, "/api/v1/servers/"+serverID+"/stacks"), &online)
	if !online.Connected || len(online.Stacks) != 2 || online.Stacks[0] != "blog" {
		t.Errorf("online answer = %+v", online)
	}
}

func TestWriteStackSendsTheFilesAndAuditsOnlyHashes(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithStacks(serverID))

	response := panel.do(t, panel.client, http.MethodPut, stackPath(serverID, "blog", ""),
		map[string]any{"compose": sampleStackCompose, "env": "SECRET_TOKEN=hunter2\n"})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", response.StatusCode)
	}
	var created jobCreatedResponse
	decodeBody(t, response, &created)

	sent := panel.dispatcher.sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d commands, want 1", len(sent))
	}
	write := sent[0].GetStackWrite()
	if write.GetName() != "blog" || write.GetComposeYaml() != sampleStackCompose || write.GetEnv() != "SECRET_TOKEN=hunter2\n" ||
		sent[0].GetJobId() != created.JobID {
		t.Errorf("command = %v", sent[0])
	}

	// The audit log says what changed, never the contents.
	entries, err := panel.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action != "stack_write" {
			continue
		}
		found = true
		text := strings.Join(stringValues(entry.Details), "|")
		if strings.Contains(text, "hunter2") || strings.Contains(text, "nginx") {
			t.Errorf("the audit log holds file contents: %v", entry.Details)
		}
		if entry.Details["stack"] != "blog" || entry.Details["compose_sha256"] != hashOf(sampleStackCompose) {
			t.Errorf("audit details = %v", entry.Details)
		}
	}
	if !found {
		t.Error("the stack write was not audited")
	}
}

func TestWriteStackValidation(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)

	valid := map[string]any{"compose": sampleStackCompose}
	if got := panel.do(t, panel.client, http.MethodPut, stackPath(serverID, "blog", ""), valid).StatusCode; got != http.StatusConflict {
		t.Errorf("offline status = %d, want 409", got)
	}

	panel.agents.set(agentWithStacks(serverID))
	tests := []struct {
		name  string
		stack string
		body  map[string]any
		want  int
	}{
		{"valid", "blog", valid, http.StatusAccepted},
		{"name of a running project", "shop", valid, http.StatusConflict},
		{"empty compose", "blog", map[string]any{"compose": "  "}, http.StatusBadRequest},
		{"missing compose", "blog", map[string]any{}, http.StatusBadRequest},
		{"upper case name", "Shop", valid, http.StatusBadRequest},
		{"name with a dot", "a.b", valid, http.StatusBadRequest},
		{"huge compose", "blog", map[string]any{"compose": strings.Repeat("a", maxComposeBytes+1)}, http.StatusBadRequest},
		{"huge env", "blog", map[string]any{"compose": sampleStackCompose, "env": strings.Repeat("A", maxEnvBytes+1)}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := panel.do(t, panel.client, http.MethodPut, stackPath(serverID, tt.stack, ""), tt.body).StatusCode; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestStackActions(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithStacks(serverID, "shop"))

	tests := []struct {
		name  string
		stack string
		body  map[string]any
		want  int
	}{
		{"up", "shop", map[string]any{"action": "up"}, http.StatusAccepted},
		{"pull", "shop", map[string]any{"action": "pull"}, http.StatusAccepted},
		{"restart", "shop", map[string]any{"action": "restart"}, http.StatusAccepted},
		{"down needs confirmation", "shop", map[string]any{"action": "down"}, http.StatusBadRequest},
		{"down confirmed", "shop", map[string]any{"action": "down", "confirm": true}, http.StatusAccepted},
		{"remove needs confirmation", "shop", map[string]any{"action": "remove"}, http.StatusBadRequest},
		{"remove confirmed", "shop", map[string]any{"action": "remove", "confirm": true}, http.StatusAccepted},
		{"unknown action", "shop", map[string]any{"action": "explode"}, http.StatusBadRequest},
		{"unmanaged stack", "ghost", map[string]any{"action": "up"}, http.StatusNotFound},
		{"bad name", "Bad_Name", map[string]any{"action": "up"}, http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := panel.post(t, stackPath(serverID, tt.stack, "/actions"), tt.body).StatusCode; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}

	// The typed commands that left carry the stack and the action.
	var last *agentv1.CoreMsg
	for _, msg := range panel.dispatcher.sent() {
		last = msg
	}
	if last == nil || last.GetStackAction().GetName() != "shop" ||
		last.GetStackAction().GetAction() != agentv1.StackActionType_STACK_ACTION_TYPE_REMOVE {
		t.Errorf("last command = %v", last)
	}
}

func TestReadStackReturnsTheFilesInTheJobOutput(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithStacks(serverID, "shop"))

	response := panel.post(t, stackPath(serverID, "shop", "/read"), map[string]any{})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", response.StatusCode)
	}
	var created jobCreatedResponse
	decodeBody(t, response, &created)

	recorder := JobRecorder{Jobs: panel.store}
	recorder.RecordJobEvent(context.Background(), serverID, &agentv1.JobEvent{
		JobId: created.JobID, State: agentv1.JobState_JOB_STATE_RUNNING,
	})
	recorder.RecordJobEvent(context.Background(), serverID, &agentv1.JobEvent{
		JobId: created.JobID, State: agentv1.JobState_JOB_STATE_SUCCEEDED, Output: `{"compose":"x","env":"y"}`,
	})
	var job jobResponse
	decodeBody(t, panel.get(t, "/api/v1/jobs/"+created.JobID), &job)
	if job.State != store.JobSucceeded || job.Output != `{"compose":"x","env":"y"}` {
		t.Errorf("job = %+v", job)
	}

	if got := panel.post(t, stackPath(serverID, "ghost", "/read"), map[string]any{}).StatusCode; got != http.StatusNotFound {
		t.Errorf("unmanaged stack status = %d, want 404", got)
	}
}

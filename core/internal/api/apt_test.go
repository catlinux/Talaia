package api

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func aptStatusPath(serverID string) string {
	return "/api/v1/servers/" + serverID + "/apt/status"
}

func TestAptStatusNeedsAFullSession(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)

	response := panel.post(t, aptStatusPath("srv-1"), map[string]any{"refresh": false})
	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", response.StatusCode)
	}
	var body errorBody
	decodeBody(t, response, &body)
	if body.Error.Code != "session_required" {
		t.Errorf("code = %q, want session_required", body.Error.Code)
	}
}

func TestAptStatusOfflineAgent(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)

	response := panel.post(t, aptStatusPath(serverID), map[string]any{"refresh": false})
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.StatusCode)
	}
	var body errorBody
	decodeBody(t, response, &body)
	if body.Error.Code != "agent_offline" {
		t.Errorf("code = %q, want agent_offline", body.Error.Code)
	}
}

func TestAptStatusCreatesTheJobAndSendsTheCommand(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithContainers(serverID))

	response := panel.post(t, aptStatusPath(serverID), map[string]any{"refresh": true})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", response.StatusCode)
	}
	var created jobCreatedResponse
	decodeBody(t, response, &created)

	sent := panel.dispatcher.sent()
	if len(sent) != 1 || sent[0].GetJobId() != created.JobID {
		t.Fatalf("sent = %v, want one command for job %s", sent, created.JobID)
	}
	if command := sent[0].GetAptStatus(); command == nil || !command.GetRefresh() {
		t.Errorf("command = %v, want AptStatus with refresh", sent[0])
	}

	var job jobResponse
	decodeBody(t, panel.get(t, "/api/v1/jobs/"+created.JobID), &job)
	if job.Kind != "apt_refresh" || job.Params["refresh"] != true {
		t.Errorf("job = %+v, want apt_refresh", job)
	}

	entries, err := panel.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action != "apt_status" {
			continue
		}
		found = true
		if entry.Actor != "admin" || entry.Details["refresh"] != true {
			t.Errorf("audit = %+v", entry)
		}
		for _, key := range []string{"packages", "held", "output", "reboot_reasons"} {
			if _, ok := entry.Details[key]; ok {
				t.Errorf("the audit log holds the package list: %v", entry.Details)
			}
		}
	}
	if !found {
		t.Error("the apt status was not audited")
	}
}

func TestAptStatusAcceptsAnEmptyBody(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithContainers(serverID))

	response := panel.post(t, aptStatusPath(serverID), nil)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", response.StatusCode)
	}
	var created jobCreatedResponse
	decodeBody(t, response, &created)

	sent := panel.dispatcher.sent()
	if len(sent) != 1 || sent[0].GetAptStatus() == nil || sent[0].GetAptStatus().GetRefresh() {
		t.Errorf("sent = %v, want AptStatus without refresh", sent)
	}
	var job jobResponse
	decodeBody(t, panel.get(t, "/api/v1/jobs/"+created.JobID), &job)
	if job.Kind != "apt_status" || job.Params["refresh"] != false {
		t.Errorf("job = %+v, want apt_status", job)
	}
}

func TestAptStatusRejectsBadJSON(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithContainers(serverID))

	tests := []struct {
		name string
		body any
	}{
		{name: "wrong type", body: map[string]any{"refresh": "yes"}},
		{name: "not an object", body: []string{"refresh"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := panel.post(t, aptStatusPath(serverID), tt.body)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", response.StatusCode)
			}
			var body errorBody
			decodeBody(t, response, &body)
			if body.Error.Code != "invalid_request" {
				t.Errorf("code = %q, want invalid_request", body.Error.Code)
			}
			if sent := panel.dispatcher.sent(); len(sent) != 0 {
				t.Errorf("a bad request sent a command: %v", sent)
			}
		})
	}
}

func TestAptJobTimeouts(t *testing.T) {
	if got, want := jobTimeout(aptStatusKind), 2*time.Minute+30*time.Second; got != want {
		t.Errorf("apt_status timeout = %s, want %s", got, want)
	}
	if got, want := jobTimeout(aptRefreshKind), 10*time.Minute+30*time.Second; got != want {
		t.Errorf("apt_refresh timeout = %s, want %s", got, want)
	}
}

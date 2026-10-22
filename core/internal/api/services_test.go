package api

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/hub"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func newServicesPanel(t *testing.T) (*testPanel, string) {
	t.Helper()
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	return panel, serverID
}

func servicesPath(serverID string) string {
	return "/api/v1/servers/" + serverID + "/services"
}

func serviceActionPath(serverID, unit string) string {
	return "/api/v1/servers/" + serverID + "/services/" + url.PathEscape(unit) + "/actions"
}

func systemdUnit(name, description, active, sub, fileState string) *agentv1.SystemdUnit {
	return &agentv1.SystemdUnit{
		Name:          name,
		Description:   description,
		LoadState:     "loaded",
		ActiveState:   active,
		SubState:      sub,
		UnitFileState: fileState,
	}
}

func connectServicesAgent(panel *testPanel, serverID string, units []*agentv1.SystemdUnit) {
	panel.agents.set(hub.AgentInfo{ServerID: serverID, Hostname: "vps-web-01", ServiceList: units})
}

func TestServicesListIsSorted(t *testing.T) {
	panel, serverID := newServicesPanel(t)
	connectServicesAgent(panel, serverID, []*agentv1.SystemdUnit{
		systemdUnit("ssh.service", "OpenSSH", "active", "running", "enabled"),
		systemdUnit("docker.service", "Docker", "active", "running", "enabled"),
		systemdUnit("getty@tty1.service", "Getty", "inactive", "dead", "enabled"),
	})

	response := panel.get(t, servicesPath(serverID))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	var body servicesResponse
	decodeBody(t, response, &body)
	if len(body.Services) != 3 {
		t.Fatalf("services = %+v, want 3", body.Services)
	}
	names := []string{body.Services[0].Name, body.Services[1].Name, body.Services[2].Name}
	if names[0] != "docker.service" || names[1] != "getty@tty1.service" || names[2] != "ssh.service" {
		t.Errorf("names = %v, want sorted", names)
	}
	if body.Services[0].LoadState != "loaded" || body.Services[0].ActiveState != "active" ||
		body.Services[0].SubState != "running" || body.Services[0].UnitFileState != "enabled" ||
		body.Services[0].Description != "Docker" {
		t.Errorf("docker entry = %+v", body.Services[0])
	}
}

func TestServicesListNeedsAConnectedAgent(t *testing.T) {
	panel, serverID := newServicesPanel(t)

	response := panel.get(t, servicesPath(serverID))
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.StatusCode)
	}
	if code := apiCodeOf(t, response); code != "agent_offline" {
		t.Errorf("code = %q, want agent_offline", code)
	}
}

func TestServiceActionRejectsBadRequests(t *testing.T) {
	panel, serverID := newServicesPanel(t)
	connectServicesAgent(panel, serverID, []*agentv1.SystemdUnit{
		systemdUnit("docker.service", "Docker", "active", "running", "enabled"),
		systemdUnit("talaia-agent.service", "Talaia", "active", "running", "enabled"),
	})

	tests := []struct {
		name       string
		unit       string
		body       any
		wantStatus int
		wantCode   string
	}{
		{name: "leading dash", unit: "-bad.service", body: map[string]any{"action": "start", "confirm": true}, wantStatus: 400, wantCode: "invalid_request"},
		{name: "missing suffix", unit: "bad", body: map[string]any{"action": "start", "confirm": true}, wantStatus: 400, wantCode: "invalid_request"},
		{name: "not a service", unit: "bad.socket", body: map[string]any{"action": "start", "confirm": true}, wantStatus: 400, wantCode: "invalid_request"},
		{name: "unknown action", unit: "docker.service", body: map[string]any{"action": "mask", "confirm": true}, wantStatus: 400, wantCode: "invalid_request"},
		{name: "stop without confirm", unit: "docker.service", body: map[string]any{"action": "stop"}, wantStatus: 400, wantCode: "confirmation_required"},
		{name: "restart without confirm", unit: "docker.service", body: map[string]any{"action": "restart", "confirm": false}, wantStatus: 400, wantCode: "confirmation_required"},
		{name: "disable without confirm", unit: "docker.service", body: map[string]any{"action": "disable", "confirm": false}, wantStatus: 400, wantCode: "confirmation_required"},
		{name: "unknown unit", unit: "missing.service", body: map[string]any{"action": "stop", "confirm": true}, wantStatus: 404, wantCode: "not_found"},
		{name: "stop the agent", unit: "talaia-agent.service", body: map[string]any{"action": "stop", "confirm": true}, wantStatus: 400, wantCode: "self_management"},
		{name: "restart the agent", unit: "talaia-agent.service", body: map[string]any{"action": "restart", "confirm": true}, wantStatus: 400, wantCode: "self_management"},
		{name: "disable the agent", unit: "talaia-agent.service", body: map[string]any{"action": "disable", "confirm": true}, wantStatus: 400, wantCode: "self_management"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := panel.post(t, serviceActionPath(serverID, tt.unit), tt.body)
			if response.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.StatusCode, tt.wantStatus)
			}
			if code := apiCodeOf(t, response); code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
		})
	}
	if sent := panel.dispatcher.sent(); len(sent) != 0 {
		t.Errorf("a rejected request sent a command: %v", sent)
	}
}

func TestServiceActionNeedsAConnectedAgent(t *testing.T) {
	panel, serverID := newServicesPanel(t)

	response := panel.post(t, serviceActionPath(serverID, "docker.service"), map[string]any{
		"action": "start", "confirm": true,
	})
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.StatusCode)
	}
	if code := apiCodeOf(t, response); code != "agent_offline" {
		t.Errorf("code = %q, want agent_offline", code)
	}
}

func TestServiceActionSendsEachAction(t *testing.T) {
	tests := []struct {
		action  string
		want    agentv1.ServiceActionType
		confirm bool
	}{
		{action: "start", want: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_START},
		{action: "stop", want: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_STOP, confirm: true},
		{action: "restart", want: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RESTART, confirm: true},
		{action: "reload", want: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RELOAD},
		{action: "enable", want: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_ENABLE},
		{action: "disable", want: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_DISABLE, confirm: true},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			panel, serverID := newServicesPanel(t)
			connectServicesAgent(panel, serverID, []*agentv1.SystemdUnit{
				systemdUnit("docker.service", "Docker", "active", "running", "enabled"),
			})

			response := panel.post(t, serviceActionPath(serverID, "docker.service"), map[string]any{
				"action": tt.action, "confirm": tt.confirm,
			})
			if response.StatusCode != http.StatusAccepted {
				t.Fatalf("status = %d, want 202: %s", response.StatusCode, response.Status)
			}
			var created jobCreatedResponse
			decodeBody(t, response, &created)

			sent := panel.dispatcher.sent()
			if len(sent) != 1 || sent[0].GetJobId() != created.JobID {
				t.Fatalf("sent = %v, want one command for job %s", sent, created.JobID)
			}
			command := sent[0].GetServiceAction()
			if command == nil || command.GetUnit() != "docker.service" || command.GetAction() != tt.want {
				t.Errorf("command = %v, want %s on docker.service", sent[0], tt.action)
			}

			var job jobResponse
			decodeBody(t, panel.get(t, "/api/v1/jobs/"+created.JobID), &job)
			if job.Kind != serviceActionKind || job.Params["unit"] != "docker.service" || job.Params["action"] != tt.action {
				t.Errorf("job = %+v, want service_action", job)
			}

			entries, err := panel.store.ListAudit(context.Background(), 20)
			if err != nil {
				t.Fatalf("list audit: %v", err)
			}
			found := false
			for _, entry := range entries {
				if entry.Action != "service_"+tt.action {
					continue
				}
				found = true
				if entry.Actor != "admin" || entry.Details["unit"] != "docker.service" {
					t.Errorf("audit = %+v", entry)
				}
			}
			if !found {
				t.Errorf("the service_%s action was not audited", tt.action)
			}
		})
	}
}

func TestValidServiceUnit(t *testing.T) {
	tests := []struct {
		unit string
		ok   bool
	}{
		{unit: "docker.service", ok: true},
		{unit: "getty@tty1.service", ok: true},
		{unit: "getty@.service", ok: true},
		{unit: "a_b.service", ok: true},
		{unit: "a:b.service", ok: true},
		{unit: "-docker.service", ok: false},
		{unit: "../docker.service", ok: false},
		{unit: "etc/docker.service", ok: false},
		{unit: "docker", ok: false},
		{unit: "docker.socket", ok: false},
		{unit: "docker service", ok: false},
	}
	for _, tt := range tests {
		if got := validServiceUnit(tt.unit); got != tt.ok {
			t.Errorf("validServiceUnit(%q) = %t, want %t", tt.unit, got, tt.ok)
		}
	}
}

func TestServiceActionTimeout(t *testing.T) {
	if got, want := jobTimeout(serviceActionKind), 90*time.Second+30*time.Second; got != want {
		t.Errorf("service action timeout = %s, want %s", got, want)
	}
}

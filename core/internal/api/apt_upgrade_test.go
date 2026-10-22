package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/hub"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func aptUpgradePath(serverID string) string {
	return "/api/v1/servers/" + serverID + "/apt/upgrade"
}

func hostRebootPath(serverID string) string {
	return "/api/v1/servers/" + serverID + "/reboot"
}

// newAptWritePanel creates the signed-in panel and one enrolled server.
func newAptWritePanel(t *testing.T) (*testPanel, string) {
	t.Helper()
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	return panel, serverID
}

// connectAptAgent reports a connected agent with the given hostname.
func connectAptAgent(panel *testPanel, serverID, hostname string) {
	panel.agents.set(hub.AgentInfo{ServerID: serverID, Hostname: hostname})
}

func apiCodeOf(t *testing.T, response *http.Response) string {
	t.Helper()
	var body errorBody
	decodeBody(t, response, &body)
	return body.Error.Code
}

func TestAptUpgradeRejectsBadRequests(t *testing.T) {
	panel, serverID := newAptWritePanel(t)

	tests := []struct {
		name       string
		body       any
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing confirmation",
			body:       map[string]any{"mode": "security"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "confirmation_required",
		},
		{
			name:       "false confirmation",
			body:       map[string]any{"mode": "all", "confirm": false},
			wantStatus: http.StatusBadRequest,
			wantCode:   "confirmation_required",
		},
		{
			name:       "unknown mode",
			body:       map[string]any{"mode": "full", "confirm": true},
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "malformed body",
			body:       "not-an-object",
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_request",
		},
		{
			name:       "agent offline",
			body:       map[string]any{"mode": "security", "confirm": true},
			wantStatus: http.StatusConflict,
			wantCode:   "agent_offline",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := panel.post(t, aptUpgradePath(serverID), tt.body)
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

func TestAptUpgradeSendsTheSelectedMode(t *testing.T) {
	tests := []struct {
		mode string
		want agentv1.AptUpgradeMode
	}{
		{mode: "security", want: agentv1.AptUpgradeMode_APT_UPGRADE_MODE_SECURITY},
		{mode: "all", want: agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			panel, serverID := newAptWritePanel(t)
			connectAptAgent(panel, serverID, "vps-web-01")

			response := panel.post(t, aptUpgradePath(serverID), map[string]any{
				"mode": tt.mode, "confirm": true,
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
			upgrade := sent[0].GetAptUpgrade()
			if upgrade == nil || upgrade.GetMode() != tt.want {
				t.Errorf("command = %v, want mode %s", sent[0], tt.want)
			}

			var job jobResponse
			decodeBody(t, panel.get(t, "/api/v1/jobs/"+created.JobID), &job)
			if job.Kind != aptUpgradeKind || job.Params["mode"] != tt.mode {
				t.Errorf("job = %+v, want apt_upgrade mode %q", job, tt.mode)
			}

			entries, err := panel.store.ListAudit(context.Background(), 20)
			if err != nil {
				t.Fatalf("list audit: %v", err)
			}
			found := false
			for _, entry := range entries {
				if entry.Action != "apt_upgrade" {
					continue
				}
				found = true
				if entry.Actor != "admin" || entry.Details["mode"] != tt.mode {
					t.Errorf("audit = %+v", entry)
				}
				for _, key := range []string{"packages", "package_names", "command"} {
					if _, ok := entry.Details[key]; ok {
						t.Errorf("the audit log holds package data: %v", entry.Details)
					}
				}
			}
			if !found {
				t.Error("the apt upgrade was not audited")
			}
		})
	}
}

func TestHostRebootRejectsWrongConfirmations(t *testing.T) {
	tests := []struct {
		name       string
		connected  bool
		body       any
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing hostname",
			connected:  true,
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
			wantCode:   "confirmation_required",
		},
		{
			name:       "wrong hostname",
			connected:  true,
			body:       map[string]any{"confirm": "other-server"},
			wantStatus: http.StatusBadRequest,
			wantCode:   "confirmation_required",
		},
		{
			name:       "agent offline",
			connected:  false,
			body:       map[string]any{"confirm": "vps-web-01"},
			wantStatus: http.StatusConflict,
			wantCode:   "agent_offline",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panel, serverID := newAptWritePanel(t)
			if tt.connected {
				connectAptAgent(panel, serverID, "vps-web-01")
			}

			response := panel.post(t, hostRebootPath(serverID), tt.body)
			if response.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.StatusCode, tt.wantStatus)
			}
			if code := apiCodeOf(t, response); code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if sent := panel.dispatcher.sent(); len(sent) != 0 {
				t.Errorf("a rejected request sent a command: %v", sent)
			}
		})
	}
}

func TestHostRebootUsesTheReportedHostname(t *testing.T) {
	panel, serverID := newAptWritePanel(t)
	connectAptAgent(panel, serverID, "vps-web-01")

	response := panel.post(t, hostRebootPath(serverID), map[string]any{
		"confirm": "  VPS-WEB-01 ",
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
	reboot := sent[0].GetHostReboot()
	if reboot == nil || reboot.GetHostname() != "vps-web-01" {
		t.Errorf("command = %v, want the reported hostname", sent[0])
	}

	var job jobResponse
	decodeBody(t, panel.get(t, "/api/v1/jobs/"+created.JobID), &job)
	if job.Kind != hostRebootKind || job.Params["hostname"] != "vps-web-01" {
		t.Errorf("job = %+v, want host_reboot", job)
	}

	entries, err := panel.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action != "host_reboot" {
			continue
		}
		found = true
		if entry.Actor != "admin" || entry.Details["hostname"] != "vps-web-01" {
			t.Errorf("audit = %+v", entry)
		}
	}
	if !found {
		t.Error("the host reboot was not audited")
	}
}

func TestAptWritesRefuseAnActiveOperation(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		path     func(string) string
		body     map[string]any
	}{
		{
			name:     "existing upgrade blocks upgrade",
			existing: aptUpgradeKind,
			path:     aptUpgradePath,
			body:     map[string]any{"mode": "security", "confirm": true},
		},
		{
			name:     "existing upgrade blocks reboot",
			existing: aptUpgradeKind,
			path:     hostRebootPath,
			body:     map[string]any{"confirm": "vps-web-01"},
		},
		{
			name:     "existing reboot blocks upgrade",
			existing: hostRebootKind,
			path:     aptUpgradePath,
			body:     map[string]any{"mode": "all", "confirm": true},
		},
		{
			name:     "existing reboot blocks reboot",
			existing: hostRebootKind,
			path:     hostRebootPath,
			body:     map[string]any{"confirm": "vps-web-01"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			panel, serverID := newAptWritePanel(t)
			connectAptAgent(panel, serverID, "vps-web-01")
			if _, err := panel.store.CreateJob(context.Background(), serverID, tt.existing, nil, "admin"); err != nil {
				t.Fatalf("create active job: %v", err)
			}

			response := panel.post(t, tt.path(serverID), tt.body)
			if response.StatusCode != http.StatusConflict {
				t.Fatalf("status = %d, want 409", response.StatusCode)
			}
			if code := apiCodeOf(t, response); code != "operation_in_progress" {
				t.Errorf("code = %q, want operation_in_progress", code)
			}
			if sent := panel.dispatcher.sent(); len(sent) != 0 {
				t.Errorf("the blocked request sent a command: %v", sent)
			}
		})
	}
}

func TestAptWriteJobTimeouts(t *testing.T) {
	if got, want := jobTimeout(aptUpgradeKind), 60*time.Minute+30*time.Second; got != want {
		t.Errorf("apt_upgrade timeout = %s, want %s", got, want)
	}
	if got, want := jobTimeout(hostRebootKind), time.Minute+30*time.Second; got != want {
		t.Errorf("host_reboot timeout = %s, want %s", got, want)
	}
}

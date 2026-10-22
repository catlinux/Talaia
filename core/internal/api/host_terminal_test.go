package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/terminal"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func hostTerminalPath(serverID string) string {
	return "/api/v1/servers/" + serverID + "/terminal"
}

func (r *terminalRig) setHostTerminal(enabled bool) {
	agent := agentWithContainers(r.serverID)
	agent.HostTerminal = enabled
	r.panel.agents.set(agent)
}

func hostOpenBody(code string) map[string]any {
	return map[string]any{"confirm": true, "code": code, "shell": "sh", "cols": 90, "rows": 30}
}

func TestHostTerminalOpensARecordedPrivilegedSession(t *testing.T) {
	r := newTerminalRig(t)
	r.setHostTerminal(true)
	code := r.panel.code(r.secret)
	before := len(r.panel.terminalAgent.messages())

	response, id := r.openPath(t, r.panel.client, hostTerminalPath(r.serverID), hostOpenBody(code), false)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", response.StatusCode)
	}
	if id == "" {
		t.Fatal("the core did not ask the agent for a host shell")
	}
	var opened terminalOpenResponse
	decodeBody(t, response, &opened)
	if opened.SessionID != id || opened.Ticket == "" || !opened.Privileged {
		t.Errorf("answer = %+v", opened)
	}

	sent := r.panel.terminalAgent.messages()
	if len(sent) != before+1 {
		t.Fatalf("sent = %v", sent)
	}
	open := sent[len(sent)-1].GetTerminalOpen()
	if open == nil || open.GetTarget() != agentv1.TerminalTarget_TERMINAL_TARGET_HOST || open.GetContainer() != "" {
		t.Errorf("host open = %v", open)
	}

	row, err := r.panel.store.GetTerminalSession(context.Background(), opened.SessionID)
	if err != nil {
		t.Fatalf("get terminal session: %v", err)
	}
	if row.Container != terminal.HostContainer || !row.Privileged {
		t.Errorf("stored session = %+v", row)
	}

	entries, err := r.panel.store.ListAudit(context.Background(), 50)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	foundOpen := false
	for _, entry := range entries {
		if entry.Action != "host_terminal_open" {
			continue
		}
		foundOpen = true
		if entry.Details["host"] != true {
			t.Errorf("host audit details = %v", entry.Details)
		}
		if strings.Contains(fmt.Sprint(entry.Details), code) {
			t.Errorf("the audit log holds the code: %v", entry.Details)
		}
	}
	if !foundOpen {
		t.Error("the host session was not audited")
	}

	r.panel.terminals.TerminalClosed(r.serverID, &agentv1.TerminalClosed{
		SessionId: opened.SessionID, Reason: "exited", ExitCode: 0,
	})
	eventually(t, func() bool {
		entries, err := r.panel.store.ListAudit(context.Background(), 50)
		if err != nil {
			return false
		}
		for _, entry := range entries {
			if entry.Action == "host_terminal_close" {
				if strings.Contains(fmt.Sprint(entry.Details), code) {
					t.Errorf("the close audit holds the code: %v", entry.Details)
				}
				return true
			}
		}
		return false
	})

	// The same code cannot complete a login after it opened the host shell.
	r.panel.logout(t, r.panel.client)
	if next := r.panel.login(t, "admin", testPassword); next != "totp" {
		t.Fatalf("login next = %q, want totp", next)
	}
	if response := r.panel.post(t, "/api/v1/auth/totp", map[string]string{"code": code}); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("reused login code status = %d, want 401", response.StatusCode)
	}
}

func TestHostTerminalRequiresConfirmTOTPAndCapability(t *testing.T) {
	r := newTerminalRig(t)
	r.setHostTerminal(true)

	// Missing confirmation fails before the code is checked or spent.
	before := len(r.panel.terminalAgent.messages())
	response, _ := r.openPath(t, r.panel.client, hostTerminalPath(r.serverID), map[string]any{
		"code": r.panel.code(r.secret), "shell": "sh",
	}, false)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing confirmation status = %d, want 400", response.StatusCode)
	}
	if code := apiCodeOf(t, response); code != "confirmation_required" {
		t.Errorf("code = %q, want confirmation_required", code)
	}

	// A wrong code is refused and never reaches the agent.
	wrong := auth.Code(r.secret, auth.Step(r.panel.clock.Now())+100)
	response, _ = r.openPath(t, r.panel.client, hostTerminalPath(r.serverID), hostOpenBody(wrong), false)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong code status = %d, want 403", response.StatusCode)
	}
	if code := apiCodeOf(t, response); code != "code_invalid" {
		t.Errorf("code = %q, want code_invalid", code)
	}

	// The agent can report that host terminals are disabled.
	r.setHostTerminal(false)
	r.panel.clock.Advance(30 * time.Second)
	response, _ = r.openPath(t, r.panel.client, hostTerminalPath(r.serverID), hostOpenBody(r.panel.code(r.secret)), false)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("disabled status = %d, want 409", response.StatusCode)
	}
	if code := apiCodeOf(t, response); code != "host_terminal_disabled" {
		t.Errorf("code = %q, want host_terminal_disabled", code)
	}

	// A disconnected agent is also a conflict.
	r.panel.agents.set()
	r.panel.clock.Advance(30 * time.Second)
	response, _ = r.openPath(t, r.panel.client, hostTerminalPath(r.serverID), hostOpenBody(r.panel.code(r.secret)), false)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("offline status = %d, want 409", response.StatusCode)
	}
	if code := apiCodeOf(t, response); code != "agent_offline" {
		t.Errorf("code = %q, want agent_offline", code)
	}
	if sent := len(r.panel.terminalAgent.messages()); sent != before {
		t.Errorf("refused requests reached the agent: %d messages", sent)
	}

	// A fresh code opens the session; reusing it is refused.
	r.setHostTerminal(true)
	r.panel.clock.Advance(30 * time.Second)
	code := r.panel.code(r.secret)
	response, id := r.openPath(t, r.panel.client, hostTerminalPath(r.serverID), hostOpenBody(code), false)
	if response.StatusCode != http.StatusCreated || id == "" {
		t.Fatalf("valid status = %d, id = %q", response.StatusCode, id)
	}
	response, _ = r.openPath(t, r.panel.client, hostTerminalPath(r.serverID), hostOpenBody(code), false)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("reused code status = %d, want 403", response.StatusCode)
	}
	if code := apiCodeOf(t, response); code != "code_invalid" {
		t.Errorf("code = %q, want code_invalid", code)
	}
}

func TestHostTerminalCannotReuseAPasswordChangeCode(t *testing.T) {
	r := newTerminalRig(t)
	r.setHostTerminal(true)
	r.panel.clock.Advance(30 * time.Second)
	code := r.panel.code(r.secret)

	response := r.panel.post(t, "/api/v1/auth/password", passwordRequest{
		CurrentPassword: testPassword,
		NewPassword:     "a much better password",
		Code:            code,
	})
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("password change status = %d, want 204", response.StatusCode)
	}

	response, _ = r.openPath(t, r.panel.client, hostTerminalPath(r.serverID), hostOpenBody(code), false)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("reused password code status = %d, want 403", response.StatusCode)
	}
	if apiCode := apiCodeOf(t, response); apiCode != "code_invalid" {
		t.Errorf("code = %q, want code_invalid", apiCode)
	}
}

func TestHostTerminalSharesTheTOTPRateLimiter(t *testing.T) {
	r := newTerminalRig(t, withLimiter(NewRateLimiterWithClock(4, time.Minute, time.Now)))
	r.setHostTerminal(true)
	r.panel.clock.Advance(30 * time.Second)
	wrong := auth.Code(r.secret, auth.Step(r.panel.clock.Now())+100)
	if response, _ := r.openPath(t, r.panel.client, hostTerminalPath(r.serverID), hostOpenBody(wrong), false); response.StatusCode != http.StatusForbidden {
		t.Fatalf("first attempt status = %d, want 403", response.StatusCode)
	}
	response, _ := r.openPath(t, r.panel.client, hostTerminalPath(r.serverID), hostOpenBody(wrong), false)
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("limited status = %d, want 429", response.StatusCode)
	}
	if code := apiCodeOf(t, response); code != "rate_limited" {
		t.Errorf("code = %q, want rate_limited", code)
	}
}

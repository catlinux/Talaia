package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
	"github.com/go-chi/chi/v5"
)

type recordingAlertHooks struct {
	containers []string
	stacks     []string
	silences   []struct {
		duration time.Duration
		reason   string
	}
}

func (h *recordingAlertHooks) Unwatch(_ context.Context, _, name string) {
	h.containers = append(h.containers, name)
}
func (h *recordingAlertHooks) UnwatchStack(_ context.Context, _, name string) {
	h.stacks = append(h.stacks, name)
}
func (h *recordingAlertHooks) SilenceServer(_ context.Context, _ string, d time.Duration, reason string) {
	h.silences = append(h.silences, struct {
		duration time.Duration
		reason   string
	}{d, reason})
}

func alertHookRequest(handler http.Handler, method, path, serverID, param, value, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("serverID", serverID)
	ctx.URLParams.Add(param, value)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, ctx))
	session := Session{User: store.User{Username: "admin"}, Stage: store.StageFull}
	r = r.WithContext(withEffective(withSession(r.Context(), session), rbac.Resolve([]rbac.Binding{{Scope: rbac.Scope{Type: rbac.ScopeOrganization}, Role: rbac.RoleAdmin}})))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestContainerAlertHookUsesName(t *testing.T) {
	for _, reference := range []string{"web", "aaaaaaaaaaaa"} {
		hooks := &recordingAlertHooks{}
		agents := &controlledAgents{}
		panel := newTestPanel(t)
		serverID := enrollTestServer(t, panel)
		agents.set(hub.AgentInfo{ServerID: serverID, ContainerList: []*agentv1.Container{{Id: "aaaaaaaaaaaa", Name: "web"}}})
		handler := NewContainerActionHandler(ContainerDeps{Agents: agents, Dispatcher: &fakeDispatcher{}, Jobs: panel.store, Audit: panel.store, Alerts: hooks})
		w := alertHookRequest(handler, http.MethodPost, "/", serverID, "container", reference, `{"action":"stop"}`)
		if w.Code != http.StatusAccepted || len(hooks.containers) != 1 || hooks.containers[0] != "web" {
			t.Fatalf("reference %q: status %d, hooks %v", reference, w.Code, hooks.containers)
		}
	}
}

func newAlertHookPanel(t *testing.T, agent hub.AgentInfo) (*testPanel, string) {
	t.Helper()
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	agent.ServerID = serverID
	panel.agents.set(agent)
	return panel, serverID
}

func TestContainerActionsCallTheAlertHookOnlyWhenStopping(t *testing.T) {
	tests := []struct {
		action string
		body   map[string]any
		hooked bool
	}{
		{"stop", map[string]any{"action": "stop"}, true},
		{"remove", map[string]any{"action": "remove", "confirm": true}, true},
		{"start", map[string]any{"action": "start"}, false},
		{"restart", map[string]any{"action": "restart"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			panel, serverID := newAlertHookPanel(t, hub.AgentInfo{ContainerList: []*agentv1.Container{{Id: "aaaaaaaaaaaa", Name: "web"}}})
			if got := panel.post(t, containerPath(serverID, "web"), tt.body).StatusCode; got != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", got)
			}
			if got := len(panel.alerts.containers) == 1 && panel.alerts.containers[0] == "web"; got != tt.hooked {
				t.Errorf("hook calls = %v, want hooked=%v", panel.alerts.containers, tt.hooked)
			}
		})
	}
}

func TestContainerActionsWithoutHooksDoNotPanic(t *testing.T) {
	agents := &controlledAgents{}
	panel := newTestPanel(t)
	serverID := enrollTestServer(t, panel)
	agents.set(hub.AgentInfo{ServerID: serverID, ContainerList: []*agentv1.Container{{Id: "aaaaaaaaaaaa", Name: "web"}}})
	handler := NewContainerActionHandler(ContainerDeps{Agents: agents, Dispatcher: &fakeDispatcher{}, Jobs: panel.store, Audit: panel.store})
	if w := alertHookRequest(handler, http.MethodPost, "/", serverID, "container", "web", `{"action":"stop"}`); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestStackActionsCallTheAlertHookOnlyWhenRemovingDeployments(t *testing.T) {
	tests := []struct {
		action string
		body   map[string]any
		hooked bool
	}{
		{"down", map[string]any{"action": "down", "confirm": true}, true},
		{"remove", map[string]any{"action": "remove", "confirm": true}, true},
		{"up", map[string]any{"action": "up"}, false},
		{"pull", map[string]any{"action": "pull"}, false},
		{"restart", map[string]any{"action": "restart"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			panel := newTestPanel(t)
			panel.createUser(t, "admin", testPassword)
			panel.enableTOTP(t, "admin", testPassword)
			serverID := enrollTestServer(t, panel)
			panel.agents.set(agentWithStacks(serverID, "shop"))
			if got := panel.post(t, stackPath(serverID, "shop", "/actions"), tt.body).StatusCode; got != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", got)
			}
			if got := len(panel.alerts.stacks) == 1 && panel.alerts.stacks[0] == "shop"; got != tt.hooked {
				t.Errorf("hook calls = %v, want hooked=%v", panel.alerts.stacks, tt.hooked)
			}
		})
	}
}

func TestHostRebootSilencesTheServerOnlyWhenAccepted(t *testing.T) {
	for _, tt := range []struct {
		name    string
		confirm string
		want    int
	}{
		{"accepted", "vps-web-01", 1},
		{"wrong confirmation", "other", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			panel, serverID := newAlertHookPanel(t, hub.AgentInfo{Hostname: "vps-web-01"})
			panel.post(t, hostRebootPath(serverID), map[string]any{"confirm": tt.confirm})
			if len(panel.alerts.silences) != tt.want {
				t.Fatalf("silences = %v, want %d", panel.alerts.silences, tt.want)
			}
			if tt.want == 1 && (panel.alerts.silences[0].duration != 10*time.Minute || panel.alerts.silences[0].reason != "reboot") {
				t.Errorf("silence = %+v", panel.alerts.silences[0])
			}
		})
	}
}

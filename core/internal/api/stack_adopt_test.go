package api

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/catlinux/Talaia/core/internal/hub"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func agentWithAdopted(serverID string) hub.AgentInfo {
	agent := agentWithContainers(serverID)
	agent.ManagedStacks = []string{"blog", "wiki"}
	agent.AdoptedStacks = []string{"wiki"}
	return agent
}

func TestStackListSaysWhatIsAdoptedAndWhatCanBe(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := "33333333-3333-3333-3333-333333333333"

	// The containers belong to the project "shop", which Talaia does not manage.
	panel.agents.set(agentWithAdopted(serverID))
	var got stacksResponse
	decodeBody(t, panel.get(t, "/api/v1/servers/"+serverID+"/stacks"), &got)

	if !reflect.DeepEqual(got.Stacks, []string{"blog", "wiki"}) || !reflect.DeepEqual(got.Adopted, []string{"wiki"}) ||
		!reflect.DeepEqual(got.Adoptable, []string{"shop"}) {
		t.Errorf("answer = %+v", got)
	}

	// Once it is managed it is no longer offered.
	agent := agentWithAdopted(serverID)
	agent.ManagedStacks = append(agent.ManagedStacks, "shop")
	panel.agents.set(agent)
	decodeBody(t, panel.get(t, "/api/v1/servers/"+serverID+"/stacks"), &got)
	if len(got.Adoptable) != 0 {
		t.Errorf("adoptable = %v, want none", got.Adoptable)
	}
}

func TestAdoptStack(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)

	if got := panel.post(t, stackPath(serverID, "shop", "/adopt"), map[string]any{}).StatusCode; got != http.StatusConflict {
		t.Errorf("offline status = %d, want 409", got)
	}

	panel.agents.set(agentWithAdopted(serverID))
	tests := []struct {
		name  string
		stack string
		want  int
	}{
		{"a project the agent runs", "shop", http.StatusAccepted},
		{"already managed", "blog", http.StatusConflict},
		{"already adopted", "wiki", http.StatusConflict},
		{"a project nobody runs", "ghost", http.StatusNotFound},
		{"bad name", "Bad_Name", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := panel.post(t, stackPath(serverID, tt.stack, "/adopt"), map[string]any{}).StatusCode; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}

	// The command carries only the name: the core never sends a path.
	var adopt *agentv1.StackAdopt
	for _, msg := range panel.dispatcher.sent() {
		adopt = msg.GetStackAdopt()
	}
	if adopt == nil || adopt.GetName() != "shop" {
		t.Errorf("adopt command = %v", adopt)
	}
}

func TestAdoptNeedsAFullSession(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)

	if got := panel.post(t, stackPath("x", "shop", "/adopt"), map[string]any{}).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", got)
	}
}

func TestForgetAndRemoveFollowTheKindOfStack(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithAdopted(serverID))

	tests := []struct {
		name   string
		stack  string
		action map[string]any
		want   int
	}{
		{"forget an adopted stack", "wiki", map[string]any{"action": "forget"}, http.StatusAccepted},
		{"forget needs no confirmation", "wiki", map[string]any{"action": "forget"}, http.StatusAccepted},
		{"remove an adopted stack is refused", "wiki", map[string]any{"action": "remove", "confirm": true}, http.StatusBadRequest},
		{"forget a stack of Talaia is refused", "blog", map[string]any{"action": "forget"}, http.StatusBadRequest},
		{"remove a stack of Talaia", "blog", map[string]any{"action": "remove", "confirm": true}, http.StatusAccepted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := panel.post(t, stackPath(serverID, tt.stack, "/actions"), tt.action).StatusCode; got != tt.want {
				t.Errorf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

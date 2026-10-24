package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func criterionAgent(serverID string, stacks ...string) hub.AgentInfo {
	agent := hub.AgentInfo{ServerID: serverID, Hostname: serverID, DockerAvailable: true, ManagedStacks: stacks}
	for _, stack := range stacks {
		agent.ContainerList = append(agent.ContainerList, &agentv1.Container{
			Id: serverID + stack, Name: serverID + "-" + stack + "-ct", Image: "nginx:1.27",
			State: "running", Status: "Up", ComposeProject: stack,
		})
	}
	return agent
}

func criterionUser(t *testing.T, panel *testPanel, name, role string, scope store.RoleBinding) (*http.Client, store.User) {
	t.Helper()
	ctx := context.Background()
	user, err := panel.store.CreateUserFull(ctx, name, auth.HashPassword(testPassword), name, name+"@example.com")
	if err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	scope.SubjectType = store.SubjectUser
	scope.SubjectID = user.ID
	scope.Role = role
	if _, err := panel.store.CreateRoleBinding(ctx, scope); err != nil {
		t.Fatalf("create binding for %s: %v", name, err)
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
	return client, user
}

func criterionPath(pattern, serverID, container, stack string) string {
	return "/api/v1" + strings.NewReplacer(
		"{serverID}", serverID,
		"{container}", container,
		"{name}", stack,
		"{unit}", "ssh.service",
		"{jobID}", "00000000-0000-0000-0000-000000000001",
		"{sessionID}", "00000000-0000-0000-0000-000000000002",
		"{userID}", "00000000-0000-0000-0000-000000000003",
		"{bindingID}", "00000000-0000-0000-0000-000000000004",
		"{inviteID}", "00000000-0000-0000-0000-000000000005",
		"{groupID}", "00000000-0000-0000-0000-000000000006",
		"{teamID}", "00000000-0000-0000-0000-000000000007",
		"{id}", "00000000-0000-0000-0000-000000000008",
	).Replace(pattern)
}

func decodePanelJSON[T any](t *testing.T, response *http.Response) T {
	t.Helper()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", response.StatusCode, response.Body)
	}
	var body T
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}

func TestSingleStackCriterion(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	serverA := enrollTestServer(t, panel)
	serverB := enrollTestServer(t, panel)
	panel.agents.set(criterionAgent(serverA, "web", "db"), criterionAgent(serverB, "web"))

	stackClient, _ := criterionUser(t, panel, "stack-user", string(rbac.RoleAppOperator), store.RoleBinding{
		ScopeType: store.ScopeStack, ScopeServerID: serverA, ScopeName: "web",
	})

	rawServers := panel.do(t, stackClient, http.MethodGet, "/api/v1/servers", nil)
	var stackServers struct {
		Servers             []map[string]any `json:"servers"`
		AgentLatestVersion  string           `json:"agent_latest_version"`
		AgentUpgradeCommand string           `json:"agent_upgrade_command"`
	}
	if err := json.NewDecoder(rawServers.Body).Decode(&stackServers); err != nil {
		t.Fatalf("decode stack servers: %v", err)
	}
	if len(stackServers.Servers) != 1 {
		t.Fatalf("stack servers = %v, want only %s", stackServers.Servers, serverA)
	}
	if stackServers.AgentLatestVersion != "" || stackServers.AgentUpgradeCommand != "" {
		t.Fatalf("stack-only response exposes agent update info: version=%q command=%q", stackServers.AgentLatestVersion, stackServers.AgentUpgradeCommand)
	}
	redacted := stackServers.Servers[0]
	wantKeys := map[string]bool{"id": true, "name": true, "status": true, "limited": true}
	if len(redacted) != len(wantKeys) {
		t.Fatalf("redacted server keys = %v, want %v", redacted, wantKeys)
	}
	for key := range wantKeys {
		if _, ok := redacted[key]; !ok {
			t.Fatalf("redacted server is missing %q: %v", key, redacted)
		}
	}
	for key := range redacted {
		if !wantKeys[key] {
			t.Fatalf("redacted server leaks %q: %v", key, redacted)
		}
	}
	if redacted["id"] != serverA || redacted["status"] != "online" || redacted["limited"] != true {
		t.Fatalf("redacted server = %v", redacted)
	}

	servers := decodePanelJSON[serversResponse](t, panel.do(t, stackClient, http.MethodGet, "/api/v1/servers", nil))
	if len(servers.Servers) != 1 || servers.Servers[0].ID != serverA {
		t.Fatalf("visible servers = %+v, want only %s", servers.Servers, serverA)
	}

	containers := decodePanelJSON[containersResponse](t, panel.do(t, stackClient, http.MethodGet,
		"/api/v1/servers/"+serverA+"/containers", nil))
	if len(containers.Containers) != 1 || containers.Containers[0].Name != serverA+"-web-ct" {
		t.Fatalf("visible containers = %+v, want only the web container", containers.Containers)
	}

	stacks := decodePanelJSON[stacksResponse](t, panel.do(t, stackClient, http.MethodGet,
		"/api/v1/servers/"+serverA+"/stacks", nil))
	if len(stacks.Stacks) != 1 || stacks.Stacks[0] != "web" {
		t.Fatalf("visible stacks = %+v, want only web", stacks.Stacks)
	}

	allStacks := decodePanelJSON[allStacksResponse](t, panel.do(t, stackClient, http.MethodGet, "/api/v1/stacks", nil))
	if len(allStacks.Stacks) != 1 || allStacks.Stacks[0].ServerID != serverA || allStacks.Stacks[0].Name != "web" {
		t.Fatalf("global stacks = %+v, want only %s/web", allStacks.Stacks, serverA)
	}

	me := decodePanelJSON[MeResponse](t, panel.do(t, stackClient, http.MethodGet, "/api/v1/me", nil))
	if len(me.Permissions.Organization) != 0 {
		t.Fatalf("organization permissions = %v, want none", me.Permissions.Organization)
	}
	if len(me.Permissions.Servers) != 1 || me.Permissions.Servers[serverA] == nil {
		t.Fatalf("me servers = %+v, want only %s", me.Permissions.Servers, serverA)
	}
	for _, permission := range me.Permissions.Servers[serverA] {
		if permission == rbac.PermServerView {
			t.Fatalf("me grants server.view on %s to a stack-only user", serverA)
		}
	}
	if len(me.Permissions.Stacks[serverA]) != 1 || me.Permissions.Stacks[serverA]["web"] == nil {
		t.Fatalf("me stacks = %+v, want only web", me.Permissions.Stacks)
	}
	if _, ok := me.Permissions.Stacks[serverA]["db"]; ok {
		t.Fatal("me exposes stack db")
	}

	dispatched := len(panel.dispatcher.sent())
	skipped := map[routeKey]bool{
		{http.MethodGet, "/servers"}:                                            true,
		{http.MethodGet, "/me"}:                                                 true,
		{http.MethodGet, "/stacks"}:                                             true,
		{http.MethodGet, "/servers/{serverID}/containers"}:                      true,
		{http.MethodGet, "/servers/{serverID}/stacks"}:                          true,
		{http.MethodPost, "/servers/{serverID}/containers/{container}/actions"}: true,
		{http.MethodPost, "/servers/{serverID}/containers/{container}/logs"}:    true,
		{http.MethodPost, "/servers/{serverID}/stacks/{name}/actions"}:          true,
		{http.MethodGet, "/jobs/{jobID}"}:                                       true,
		{http.MethodGet, "/terminal/{sessionID}/ws"}:                            true,
		{http.MethodPost, "/apikeys"}:                                           true,
		{http.MethodGet, "/apikeys"}:                                            true,
		{http.MethodDelete, "/apikeys/{id}"}:                                    true,
	}
	for key := range routePerms {
		if skipped[key] {
			continue
		}
		path := criterionPath(key.Pattern, serverA, serverA+"-web-ct", "web")
		response := panel.do(t, stackClient, key.Method, path, nil)
		if response.StatusCode != http.StatusForbidden && response.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 403 or 404", key.Method, path, response.StatusCode)
		}
	}
	if got := len(panel.dispatcher.sent()); got != dispatched {
		t.Fatalf("a denied route dispatched a command: %d -> %d", dispatched, got)
	}

	notFound := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/servers/" + serverB + "/containers"},
		{http.MethodGet, "/api/v1/servers/" + serverB + "/stacks"},
		{http.MethodPost, "/api/v1/servers/" + serverB + "/reboot"},
		{http.MethodPost, "/api/v1/servers/" + serverA + "/stacks/db/read"},
		{http.MethodPost, "/api/v1/servers/" + serverA + "/containers/" + serverA + "-db-ct/logs"},
		{http.MethodPost, "/api/v1/servers/" + serverA + "/containers/" + serverA + "-db-ct/actions"},
		{http.MethodPost, "/api/v1/servers/" + serverA + "/stacks/db/actions"},
	}
	for _, test := range notFound {
		response := panel.do(t, stackClient, test.method, test.path, nil)
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404", test.method, test.path, response.StatusCode)
		}
	}

	// stack.deploy runs what is deployed but never removes or forgets the stack.
	for _, action := range []string{"remove", "forget"} {
		before := len(panel.dispatcher.sent())
		response := panel.do(t, stackClient, http.MethodPost, "/api/v1/servers/"+serverA+"/stacks/web/actions",
			map[string]any{"action": action, "confirm": true})
		if response.StatusCode != http.StatusForbidden {
			t.Errorf("app operator %s own stack: status = %d, want 403", action, response.StatusCode)
		}
		if got := len(panel.dispatcher.sent()); got != before {
			t.Errorf("app operator %s own stack dispatched a command", action)
		}
	}

	otherJob, err := panel.store.CreateJob(context.Background(), serverA, "container_logs",
		map[string]any{"container": "web"}, "admin")
	if err != nil {
		t.Fatalf("create another user's job: %v", err)
	}
	if response := panel.do(t, stackClient, http.MethodGet, "/api/v1/jobs/"+otherJob.ID, nil); response.StatusCode != http.StatusNotFound {
		t.Errorf("another user's job status = %d, want 404", response.StatusCode)
	}

	// The same binding with server_operator still cannot reach the host or the
	// other stack.
	operatorClient, _ := criterionUser(t, panel, "stack-operator", string(rbac.RoleServerOperator), store.RoleBinding{
		ScopeType: store.ScopeStack, ScopeServerID: serverA, ScopeName: "web",
	})
	for _, test := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/servers/" + serverA + "/reboot"},
		{http.MethodPost, "/api/v1/servers/" + serverA + "/journal"},
		{http.MethodPost, "/api/v1/servers/" + serverA + "/terminal"},
		{http.MethodGet, "/api/v1/servers/" + serverA + "/services"},
		{http.MethodPost, "/api/v1/servers/" + serverA + "/resources/list"},
	} {
		response := panel.do(t, operatorClient, test.method, test.path, nil)
		if response.StatusCode != http.StatusForbidden {
			t.Errorf("stack operator %s %s: status = %d, want 403", test.method, test.path, response.StatusCode)
		}
	}
	if response := panel.do(t, operatorClient, http.MethodPost,
		"/api/v1/servers/"+serverA+"/stacks/db/read", nil); response.StatusCode != http.StatusNotFound {
		t.Errorf("stack operator on db status = %d, want 404", response.StatusCode)
	}

	// A viewer on the whole server can list everything there but not act.
	viewerClient, _ := criterionUser(t, panel, "viewer", string(rbac.RoleViewer), store.RoleBinding{
		ScopeType: store.ScopeServer, ScopeServerID: serverA,
	})
	viewerServers := decodePanelJSON[serversResponse](t, panel.do(t, viewerClient, http.MethodGet, "/api/v1/servers", nil))
	if len(viewerServers.Servers) != 1 || viewerServers.Servers[0].ID != serverA {
		t.Fatalf("viewer servers = %+v, want only %s", viewerServers.Servers, serverA)
	}
	if viewerServers.Servers[0].Live == nil || viewerServers.Servers[0].Limited {
		t.Fatalf("viewer server = %+v, want the full entry", viewerServers.Servers[0])
	}
	viewerContainers := decodePanelJSON[containersResponse](t, panel.do(t, viewerClient, http.MethodGet,
		"/api/v1/servers/"+serverA+"/containers", nil))
	if len(viewerContainers.Containers) != 2 {
		t.Fatalf("viewer containers = %+v, want both", viewerContainers.Containers)
	}
	if response := panel.do(t, viewerClient, http.MethodPost,
		"/api/v1/servers/"+serverA+"/containers/"+serverA+"-web-ct/actions",
		map[string]any{"action": "restart"}); response.StatusCode != http.StatusForbidden {
		t.Errorf("viewer action status = %d, want 403", response.StatusCode)
	}

	// The organization admin sees both servers and all their stacks.
	adminServers := decodePanelJSON[serversResponse](t, panel.do(t, panel.client, http.MethodGet, "/api/v1/servers", nil))
	if len(adminServers.Servers) != 2 {
		t.Fatalf("admin servers = %+v, want both", adminServers.Servers)
	}
	for _, server := range adminServers.Servers {
		if server.Live == nil || server.Limited {
			t.Fatalf("admin server = %+v, want the full entry", server)
		}
	}
	adminStacks := decodePanelJSON[allStacksResponse](t, panel.do(t, panel.client, http.MethodGet, "/api/v1/stacks", nil))
	if len(adminStacks.Stacks) != 3 {
		t.Fatalf("admin stacks = %+v, want three", adminStacks.Stacks)
	}
	adminMe := decodePanelJSON[MeResponse](t, panel.do(t, panel.client, http.MethodGet, "/api/v1/me", nil))
	if len(adminMe.Permissions.Organization) == 0 || len(adminMe.Permissions.Servers) != 2 {
		t.Fatalf("admin me = %+v", adminMe.Permissions)
	}
}

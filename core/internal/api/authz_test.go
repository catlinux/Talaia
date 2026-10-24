package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/rbac"
	"github.com/catlinux/Talaia/core/internal/store"
)

type stubServers struct {
	byID map[string]store.Server
	err  error
}

func (s stubServers) ListServers(context.Context) ([]store.Server, error) {
	if s.err != nil {
		return nil, s.err
	}
	servers := make([]store.Server, 0, len(s.byID))
	for _, server := range s.byID {
		servers = append(servers, server)
	}
	return servers, nil
}

func (s stubServers) GetServer(_ context.Context, id string) (store.Server, error) {
	if s.err != nil {
		return store.Server{}, s.err
	}
	server, ok := s.byID[id]
	if !ok {
		return store.Server{}, store.ErrNotFound
	}
	return server, nil
}

func (s stubServers) RemoveServer(context.Context, string) error { return nil }

type stubBindings struct {
	byUser    map[string][]store.RoleBinding
	bySubject map[string][]store.RoleBinding
	err       error
}

func (s stubBindings) ListRoleBindingsForUser(_ context.Context, userID string) ([]store.RoleBinding, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.byUser[userID], nil
}

func (s stubBindings) ListRoleBindings(_ context.Context, subjectType, subjectID string) ([]store.RoleBinding, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.bySubject[subjectType+"/"+subjectID], nil
}

func authzServers() stubServers {
	return stubServers{byID: map[string]store.Server{
		"s1": {ID: "s1", Name: "one", GroupID: "g1"},
		"s2": {ID: "s2", Name: "two", GroupID: "g2"},
	}}
}

func authzRequest(method, path string, session Session) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	return request.WithContext(withSession(context.Background(), session))
}

func fullRouter(t *testing.T) http.Handler {
	t.Helper()
	servers := authzServers()
	agents := fakeAgents{agents: []hub.AgentInfo{agentWithContainers("s1")}}
	return NewRouter(RouterDeps{
		DB:                 fakePinger{},
		WebDir:             t.TempDir(),
		Session:            SessionDeps{},
		Servers:            servers,
		Agents:             agents,
		ServerRemover:      servers,
		ServerDisconnector: &fakeDisconnect{},
		ServerAudit:        &fakeAudit{},
		AuditLog:           &fakeAuditReader{},
		AgentPackages:      AgentPackages{},
		AgentUpdates:       &AgentUpdateDeps{Agents: agents, Servers: servers},
		Enrollment:         &EnrollmentTokenDeps{},
		Containers:         &ContainerDeps{Agents: agents, Servers: servers},
		Metrics:            &MetricsDeps{Reader: fakeMetricsReader{}, Servers: servers, Agents: agents},
		Alerts:             &AlertsDeps{Store: (*store.Store)(nil), Hooks: &recordingAlertHooks{}, Checks: inertAlertChecks{}, Notifier: inertChannelTester{}, Servers: servers},
		Terminal:           NewTerminalDeps(TerminalDeps{}),
		Recordings:         &RecordingDeps{},
		System:             &SystemDeps{},
		Authz:              NewAuthorizer(AuthorizerDeps{}),
		Auth:               AuthDeps{},
	})
}

func TestRouteGuardian(t *testing.T) {
	router := fullRouter(t)
	mux, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("router has type %T, want chi.Routes", router)
	}

	registered := make(map[routeKey]bool)
	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/v1") || strings.HasPrefix(route, "/api/v1/auth") {
			return nil
		}
		pattern := strings.TrimPrefix(route, "/api/v1")
		key := routeKey{Method: method, Pattern: pattern}
		if _, ok := routePerms[key]; !ok {
			t.Errorf("protected route %s %s has no routePerms entry", method, route)
		}
		registered[key] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk routes: %v", err)
	}

	for key := range routePerms {
		if !registered[key] {
			t.Errorf("routePerms entry %s %s has no registered route", key.Method, key.Pattern)
		}
	}
}

func routePath(method string, pattern string) string {
	path := strings.NewReplacer(
		"{serverID}", "s1",
		"{container}", "web",
		"{name}", "shop",
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
	return "/api/v1" + path
}

func routeTestRouter(t *testing.T, authorizer *Authorizer) http.Handler {
	t.Helper()
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		for key := range routePerms {
			registerRoute(r, authorizer, key.Method, key.Pattern, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
		}
	})
	return router
}

func doAuthorized(t *testing.T, handler http.Handler, request *http.Request) int {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

func TestPermissionMappingByRole(t *testing.T) {
	session := Session{User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull}

	tests := []struct {
		name       string
		role       rbac.Role
		method     string
		pattern    string
		wantStatus int
	}{
		{name: "viewer cannot reboot", role: rbac.RoleViewer, method: http.MethodPost,
			pattern: "/servers/{serverID}/reboot", wantStatus: http.StatusForbidden},
		{name: "viewer can see services", role: rbac.RoleViewer, method: http.MethodGet,
			pattern: "/servers/{serverID}/services", wantStatus: http.StatusNoContent},
		{name: "app operator cannot edit a stack", role: rbac.RoleAppOperator, method: http.MethodPut,
			pattern: "/servers/{serverID}/stacks/{name}", wantStatus: http.StatusForbidden},
		{name: "app operator cannot open a container terminal", role: rbac.RoleAppOperator,
			method: http.MethodPost, pattern: "/servers/{serverID}/containers/{container}/terminal",
			wantStatus: http.StatusForbidden},
		{name: "app operator can deploy a stack", role: rbac.RoleAppOperator, method: http.MethodPost,
			pattern: "/servers/{serverID}/stacks/{name}/actions", wantStatus: http.StatusNoContent},
		{name: "stack-bound server operator cannot reboot", role: rbac.RoleServerOperator,
			method: http.MethodPost, pattern: "/servers/{serverID}/reboot",
			wantStatus: http.StatusForbidden},
		{name: "stack-bound server operator cannot control services", role: rbac.RoleServerOperator,
			method: http.MethodPost, pattern: "/servers/{serverID}/services/{unit}/actions",
			wantStatus: http.StatusForbidden},
		{name: "stack-bound server operator can open a container terminal", role: rbac.RoleServerOperator,
			method: http.MethodPost, pattern: "/servers/{serverID}/containers/{container}/terminal",
			wantStatus: http.StatusNoContent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scope := store.RoleBinding{
				SubjectType: store.SubjectUser, SubjectID: "u1",
				ScopeType: store.ScopeOrganization, Role: string(test.role),
			}
			if test.role == rbac.RoleServerOperator && test.name == "stack-bound server operator cannot reboot" ||
				test.role == rbac.RoleServerOperator && test.name == "stack-bound server operator cannot control services" ||
				test.role == rbac.RoleServerOperator && test.name == "stack-bound server operator can open a container terminal" {
				scope = store.RoleBinding{
					SubjectType: store.SubjectUser, SubjectID: "u1",
					ScopeType: store.ScopeStack, ScopeServerID: "s1", ScopeName: "shop",
					Role: string(test.role),
				}
			}
			authorizer := NewAuthorizer(AuthorizerDeps{
				Bindings: stubBindings{byUser: map[string][]store.RoleBinding{"u1": {scope}}},
				Servers:  authzServers(),
				Agents:   fakeAgents{agents: []hub.AgentInfo{agentWithContainers("s1")}},
				Audit:    &fakeAudit{},
			})
			handler := routeTestRouter(t, authorizer)
			if got := doAuthorized(t, handler, authzRequest(test.method, routePath(test.method, test.pattern), session)); got != test.wantStatus {
				t.Fatalf("status = %d, want %d", got, test.wantStatus)
			}
		})
	}
}

func TestContainerMetricsPermissionsRespectContainerScopes(t *testing.T) {
	makeHandler := func(scope store.RoleBinding) http.Handler {
		authorizer := NewAuthorizer(AuthorizerDeps{
			Bindings: stubBindings{byUser: map[string][]store.RoleBinding{"u1": {scope}}},
			Servers:  authzServers(),
			Agents:   fakeAgents{agents: []hub.AgentInfo{agentWithContainers("s1")}},
		})
		return routeTestRouter(t, authorizer)
	}
	session := Session{User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull}
	stackHandler := makeHandler(store.RoleBinding{SubjectType: store.SubjectUser, SubjectID: "u1", ScopeType: store.ScopeStack, ScopeServerID: "s1", ScopeName: "shop", Role: string(rbac.RoleViewer)})
	for _, test := range []struct {
		container string
		want      int
	}{{"web", http.StatusNoContent}, {"db", http.StatusNotFound}} {
		path := "/api/v1/servers/s1/containers/" + test.container + "/metrics"
		if got := doAuthorized(t, stackHandler, authzRequest(http.MethodGet, path, session)); got != test.want {
			t.Errorf("stack-bound metrics for %s: status %d, want %d", test.container, got, test.want)
		}
	}
	viewerHandler := makeHandler(store.RoleBinding{SubjectType: store.SubjectUser, SubjectID: "u1", ScopeType: store.ScopeOrganization, Role: string(rbac.RoleViewer)})
	for _, container := range []string{"web", "db"} {
		path := "/api/v1/servers/s1/containers/" + container + "/metrics"
		if got := doAuthorized(t, viewerHandler, authzRequest(http.MethodGet, path, session)); got != http.StatusNoContent {
			t.Errorf("organization viewer metrics for %s: status %d, want 204", container, got)
		}
	}
	if got := doAuthorized(t, viewerHandler, httptest.NewRequest(http.MethodGet, "/api/v1/servers/s1/containers/web/metrics", nil)); got != http.StatusUnauthorized {
		t.Errorf("metrics without a session: status %d, want 401", got)
	}
}

func TestOrganizationAdminPassesEveryRoute(t *testing.T) {
	session := Session{User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull}
	authorizer := NewAuthorizer(AuthorizerDeps{
		Bindings: stubBindings{byUser: map[string][]store.RoleBinding{"u1": {{
			SubjectType: store.SubjectUser, SubjectID: "u1",
			ScopeType: store.ScopeOrganization, Role: string(rbac.RoleAdmin),
		}}}},
		Servers: authzServers(),
		Agents:  fakeAgents{agents: []hub.AgentInfo{agentWithContainers("s1")}},
		Audit:   &fakeAudit{},
	})
	handler := routeTestRouter(t, authorizer)
	for key := range routePerms {
		if got := doAuthorized(t, handler, authzRequest(key.Method, routePath(key.Method, key.Pattern), session)); got != http.StatusNoContent {
			t.Errorf("admin %s %s: status = %d, want 204", key.Method, key.Pattern, got)
		}
	}
}

func TestDenialSemanticsAndAudit(t *testing.T) {
	session := Session{User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull}
	bindings := stubBindings{byUser: map[string][]store.RoleBinding{"u1": {{
		SubjectType: store.SubjectUser, SubjectID: "u1",
		ScopeType: store.ScopeServer, ScopeServerID: "s1", Role: string(rbac.RoleViewer),
	}}}}
	audit := &fakeAudit{}
	authorizer := NewAuthorizer(AuthorizerDeps{
		Bindings: bindings,
		Servers:  authzServers(),
		Agents:   fakeAgents{agents: []hub.AgentInfo{agentWithContainers("s1")}},
		Audit:    audit,
	})
	handler := routeTestRouter(t, authorizer)

	if status := doAuthorized(t, handler, authzRequest(http.MethodPost, "/api/v1/servers/s2/reboot", session)); status != http.StatusNotFound {
		t.Fatalf("unseen server status = %d, want 404", status)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	event := audit.events[0]
	if event.Action != "authz_denied" || event.Result != "denied" || event.Actor != "alice" || event.ActorID != "u1" {
		t.Fatalf("denial audit = %+v", event)
	}
	if event.Details["permission"] != string(rbac.PermHostReboot) ||
		event.Details["method"] != http.MethodPost || event.Details["path"] != "/api/v1/servers/s2/reboot" {
		t.Fatalf("denial details = %+v", event.Details)
	}

	if status := doAuthorized(t, handler, authzRequest(http.MethodPost, "/api/v1/servers/s1/reboot", session)); status != http.StatusForbidden {
		t.Fatalf("visible server status = %d, want 403", status)
	}
	if len(audit.events) != 2 || audit.events[1].Result != "denied" {
		t.Fatalf("second denial was not audited: %+v", audit.events)
	}

	stackBindings := stubBindings{byUser: map[string][]store.RoleBinding{"u1": {{
		SubjectType: store.SubjectUser, SubjectID: "u1",
		ScopeType: store.ScopeStack, ScopeServerID: "s1", ScopeName: "shop", Role: string(rbac.RoleViewer),
	}}}}
	stackAuthorizer := NewAuthorizer(AuthorizerDeps{
		Bindings: stackBindings, Servers: authzServers(),
		Agents: fakeAgents{agents: []hub.AgentInfo{agentWithContainers("s1")}},
	})
	stackHandler := routeTestRouter(t, stackAuthorizer)
	if status := doAuthorized(t, stackHandler, authzRequest(http.MethodPost, "/api/v1/servers/s1/reboot", session)); status != http.StatusForbidden {
		t.Fatalf("stack user on host status = %d, want 403", status)
	}
	if status := doAuthorized(t, stackHandler, authzRequest(http.MethodGet, "/api/v1/servers/s2/containers", session)); status != http.StatusNotFound {
		t.Fatalf("stack user on another server status = %d, want 404", status)
	}
	if status := doAuthorized(t, stackHandler, authzRequest(http.MethodPost, "/api/v1/servers/s1/containers/unknown/logs", session)); status != http.StatusNotFound {
		t.Fatalf("unknown container status = %d, want 404", status)
	}
}

func TestAuthorizerResolvesBindingsOnce(t *testing.T) {
	calls := 0
	bindings := countingBindings{calls: &calls}
	authorizer := NewAuthorizer(AuthorizerDeps{Bindings: bindings, Servers: authzServers()})
	handler := authorizer.requireRoute(routePerms[routeKey{http.MethodGet, "/servers"}], http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorize(r, rbac.PermServerView, rbac.Target{ServerID: "s1"})
		w.WriteHeader(http.StatusNoContent)
	}))
	session := Session{User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull}
	if status := doAuthorized(t, handler, authzRequest(http.MethodGet, "/api/v1/servers", session)); status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", status)
	}
	if calls != 1 {
		t.Fatalf("binding calls = %d, want 1", calls)
	}
}

type countingBindings struct{ calls *int }

func (c countingBindings) ListRoleBindingsForUser(context.Context, string) ([]store.RoleBinding, error) {
	(*c.calls)++
	return nil, nil
}

func (c countingBindings) ListRoleBindings(context.Context, string, string) ([]store.RoleBinding, error) {
	return nil, nil
}

func TestAuthorizerFailsClosed(t *testing.T) {
	session := Session{User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull}
	handler := routeTestRouter(t, nil)
	if status := doAuthorized(t, handler, authzRequest(http.MethodGet, "/api/v1/servers", session)); status != http.StatusForbidden {
		t.Fatalf("nil authorizer status = %d, want 403", status)
	}

	broken := NewAuthorizer(AuthorizerDeps{Bindings: stubBindings{err: errors.New("database down")}})
	handler = routeTestRouter(t, broken)
	if status := doAuthorized(t, handler, authzRequest(http.MethodGet, "/api/v1/servers", session)); status != http.StatusInternalServerError {
		t.Fatalf("broken bindings status = %d, want 500", status)
	}
}

func TestUnknownRouteIsNotRegistered(t *testing.T) {
	// A route missing from routePerms must not be open even if a caller mounts
	// it with the helper: registerRoute installs a deny-all handler.
	authorizer := NewAuthorizer(AuthorizerDeps{})
	router := chi.NewRouter()
	registerRoute(router, authorizer, http.MethodGet, "/unknown", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	session := Session{User: store.User{ID: "u1"}, Stage: store.StageFull}
	if status := doAuthorized(t, router, authzRequest(http.MethodGet, "/unknown", session)); status != http.StatusForbidden {
		t.Fatalf("unlisted route status = %d, want 403", status)
	}
}

func TestContainerRemoveNeedsTheRemovePermission(t *testing.T) {
	session := Session{User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull}
	audit := &fakeAudit{}
	authorizer := NewAuthorizer(AuthorizerDeps{
		Bindings: stubBindings{byUser: map[string][]store.RoleBinding{"u1": {{
			SubjectType: store.SubjectUser, SubjectID: "u1",
			ScopeType: store.ScopeOrganization, Role: string(rbac.RoleAppOperator),
		}}}},
		Servers: authzServers(),
		Agents:  fakeAgents{agents: []hub.AgentInfo{agentWithContainers("s1")}},
		Audit:   audit,
	})
	router := chi.NewRouter()
	router.Route("/api/v1", func(r chi.Router) {
		registerRoute(r, authorizer, http.MethodPost, "/servers/{serverID}/containers/{container}/actions",
			NewContainerActionHandler(ContainerDeps{
				Agents:     fakeAgents{agents: []hub.AgentInfo{agentWithContainers("s1")}},
				Dispatcher: &fakeDispatcher{},
				Audit:      audit,
			}))
	})
	request := httptest.NewRequest(http.MethodPost,
		"/api/v1/servers/s1/containers/web/actions", strings.NewReader(`{"action":"remove","confirm":true}`))
	request = request.WithContext(withSession(context.Background(), session))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", recorder.Code, recorder.Body.String())
	}
	if len(audit.events) != 1 || audit.events[0].Details["permission"] != string(rbac.PermContainerRemove) {
		t.Fatalf("remove denial was not audited: %+v", audit.events)
	}
}

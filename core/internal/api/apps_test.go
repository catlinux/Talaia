package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/deploy"
	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/catlinux/Talaia/core/internal/templates"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type memAppStore struct {
	apps        map[string]store.App
	envs        map[string]map[string]store.AppEnv
	domains     map[string]store.Domain
	deployments map[string]store.Deployment
	next        int
}

func newMemAppStore(apps ...store.App) *memAppStore {
	state := &memAppStore{
		apps: map[string]store.App{}, envs: map[string]map[string]store.AppEnv{},
		domains: map[string]store.Domain{}, deployments: map[string]store.Deployment{},
	}
	for _, app := range apps {
		if app.ID == "" {
			state.next++
			app.ID = fmt.Sprintf("app-%d", state.next)
		}
		state.apps[app.ID] = app
	}
	return state
}

func (s *memAppStore) CreateApp(_ context.Context, app store.App) (store.App, error) {
	for _, existing := range s.apps {
		if existing.ServerID == app.ServerID && existing.StackName == app.StackName {
			return store.App{}, store.ErrAppExists
		}
	}
	s.next++
	app.ID = fmt.Sprintf("app-%d", s.next)
	app.CreatedAt = time.Now()
	app.UpdatedAt = app.CreatedAt
	s.apps[app.ID] = app
	return app, nil
}

func (s *memAppStore) UpdateApp(_ context.Context, app store.App) (store.App, error) {
	if _, ok := s.apps[app.ID]; !ok {
		return store.App{}, store.ErrNotFound
	}
	app.UpdatedAt = time.Now()
	s.apps[app.ID] = app
	return app, nil
}

func (s *memAppStore) DeleteApp(_ context.Context, id string) error {
	if _, ok := s.apps[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.apps, id)
	return nil
}

func (s *memAppStore) GetApp(_ context.Context, id string) (store.App, error) {
	app, ok := s.apps[id]
	if !ok {
		return store.App{}, store.ErrNotFound
	}
	return app, nil
}

func (s *memAppStore) ListApps(context.Context) ([]store.App, error) {
	result := make([]store.App, 0, len(s.apps))
	for _, app := range s.apps {
		result = append(result, app)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ServerID != result[j].ServerID {
			return result[i].ServerID < result[j].ServerID
		}
		return result[i].StackName < result[j].StackName
	})
	return result, nil
}

func (s *memAppStore) ListAppsByServer(_ context.Context, serverID string) ([]store.App, error) {
	all, _ := s.ListApps(context.Background())
	result := make([]store.App, 0)
	for _, app := range all {
		if app.ServerID == serverID {
			result = append(result, app)
		}
	}
	return result, nil
}

func (s *memAppStore) SetAppEnv(_ context.Context, env store.AppEnv) error {
	if s.envs[env.AppID] == nil {
		s.envs[env.AppID] = map[string]store.AppEnv{}
	}
	s.envs[env.AppID][env.Key] = env
	return nil
}

func (s *memAppStore) DeleteAppEnv(_ context.Context, appID, key string) error {
	if s.envs[appID] == nil || s.envs[appID][key].Key == "" {
		return store.ErrNotFound
	}
	delete(s.envs[appID], key)
	return nil
}

func (s *memAppStore) ListAppEnvPublic(_ context.Context, appID string) ([]store.AppEnv, error) {
	result := make([]store.AppEnv, 0)
	for _, env := range s.envs[appID] {
		item := env
		if item.Secret {
			item.ValueEnc = nil
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result, nil
}

func (s *memAppStore) AddDomain(_ context.Context, domain store.Domain) (store.Domain, error) {
	for _, existing := range s.domains {
		if existing.Hostname == domain.Hostname {
			return store.Domain{}, store.ErrDomainExists
		}
	}
	s.next++
	domain.ID = fmt.Sprintf("domain-%d", s.next)
	domain.CreatedAt = time.Now()
	s.domains[domain.ID] = domain
	return domain, nil
}

func (s *memAppStore) RemoveDomain(_ context.Context, id string) error {
	if _, ok := s.domains[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.domains, id)
	return nil
}

func (s *memAppStore) GetDomain(_ context.Context, id string) (store.Domain, error) {
	domain, ok := s.domains[id]
	if !ok {
		return store.Domain{}, store.ErrNotFound
	}
	return domain, nil
}

func (s *memAppStore) ListAppDomains(_ context.Context, appID string) ([]store.Domain, error) {
	result := make([]store.Domain, 0)
	for _, domain := range s.domains {
		if domain.AppID == appID {
			result = append(result, domain)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Hostname < result[j].Hostname })
	return result, nil
}

func (s *memAppStore) ListDomains(context.Context) ([]store.Domain, error) {
	result := make([]store.Domain, 0, len(s.domains))
	for _, domain := range s.domains {
		result = append(result, domain)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Hostname < result[j].Hostname })
	return result, nil
}

func (s *memAppStore) ListDeployments(_ context.Context, appID string, limit int) ([]store.Deployment, error) {
	result := make([]store.Deployment, 0)
	for _, item := range s.deployments {
		if item.AppID == appID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Number > result[j].Number })
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (s *memAppStore) GetDeployment(_ context.Context, id string) (store.Deployment, error) {
	item, ok := s.deployments[id]
	if !ok {
		return store.Deployment{}, store.ErrNotFound
	}
	return item, nil
}

type fakeQueue struct {
	calls []struct{ appID, trigger, commit string }
	err   error
}

func (f *fakeQueue) Enqueue(_ context.Context, appID, trigger, commit string) error {
	f.calls = append(f.calls, struct{ appID, trigger, commit string }{appID, trigger, commit})
	return f.err
}

type fakeResolver struct {
	hosts map[string][]string
	err   map[string]error
}

func (f fakeResolver) LookupHost(_ context.Context, host string) ([]string, error) {
	if err := f.err[host]; err != nil {
		return nil, err
	}
	addresses, ok := f.hosts[host]
	if !ok {
		return nil, fmt.Errorf("no such host")
	}
	return addresses, nil
}

type fakeAppCleaner struct {
	calls         []string
	removeErr     error
	cleanupErr    error
	proxyErr      error
	cleanupOutput string
	proxyRoutes   []*agentv1.ProxyRoute
}

func (f *fakeAppCleaner) RemoveStack(_ context.Context, _, name string) (string, error) {
	f.calls = append(f.calls, "remove:"+name)
	return "stack removed", f.removeErr
}

func (f *fakeAppCleaner) CleanupApp(_ context.Context, _, name string) (string, error) {
	f.calls = append(f.calls, "cleanup:"+name)
	return f.cleanupOutput, f.cleanupErr
}

func (f *fakeAppCleaner) ApplyProxyRoutes(_ context.Context, _ string, routes []*agentv1.ProxyRoute) (string, error) {
	f.calls = append(f.calls, "proxy")
	f.proxyRoutes = append([]*agentv1.ProxyRoute(nil), routes...)
	return "proxy routes applied", f.proxyErr
}

func newAppAPI(t *testing.T, state *memAppStore) (AppDeps, *fakeAudit, *auth.SecretBox) {
	t.Helper()
	key := make([]byte, 32)
	box, err := auth.NewSecretBox(key)
	if err != nil {
		t.Fatal(err)
	}
	audit := &fakeAudit{}
	deps := AppDeps{
		Store: state, Box: box, Queue: &fakeQueue{}, Cleaner: &fakeAppCleaner{}, Audit: audit,
		Servers: stubServers{byID: map[string]store.Server{"s1": {ID: "s1", Name: "one", GroupID: "g1"}}},
		Agents:  fakeAgents{agents: []hub.AgentInfo{{ServerID: "s1", Hostname: "vps.example.com"}}},
		Resolver: fakeResolver{hosts: map[string][]string{
			"app.example.com": {"203.0.113.10"}, "vps.example.com": {"203.0.113.20"},
		}},
		PublicURL: "https://panel.example.com",
	}
	return deps, audit, box
}

func apiRequest(method, target string, body string) *http.Request {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	request.Header.Set("Content-Type", "application/json")
	return request.WithContext(withSession(request.Context(), Session{
		User: store.User{ID: "u1", Username: "alice"}, Stage: store.StageFull,
	}))
}

func serveRoute(t *testing.T, method, pattern, target string, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := chi.NewRouter()
	router.Method(method, pattern, handler)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, apiRequest(method, target, body))
	return response
}

type fakeAPIJobs struct{}

func (fakeAPIJobs) CreateJob(_ context.Context, serverID, kind string, _ map[string]any, createdBy, createdByID string) (store.Job, error) {
	return store.Job{ID: "job-1", ServerID: serverID, Kind: kind, CreatedBy: createdBy, CreatedByID: createdByID}, nil
}

func (fakeAPIJobs) GetJob(context.Context, string) (store.Job, error) {
	return store.Job{}, store.ErrNotFound
}

func (fakeAPIJobs) UpdateJobState(context.Context, string, string, string, string, string) (bool, error) {
	return true, nil
}

func (fakeAPIJobs) FailStaleJob(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}

func (fakeAPIJobs) HasActiveJobWithin(context.Context, string, string, time.Duration) (bool, error) {
	return false, nil
}

func TestProxyActionsAreQueuedAndAudited(t *testing.T) {
	app := store.App{ID: "app-1", ServerID: "s1", StackName: "shop", Service: "app", Port: 3000, HostPort: 23456}
	state := newMemAppStore(app)
	state.domains["domain-1"] = store.Domain{ID: "domain-1", AppID: app.ID, Hostname: "app.example.com", Service: "app", Port: 3000}
	deps, audit, _ := newAppAPI(t, state)
	dispatcher := &fakeDispatcher{}
	proxyDeps := ProxyDeps{
		AppDeps: deps,
		Containers: &ContainerDeps{
			Agents:     fakeAgents{agents: []hub.AgentInfo{{ServerID: "s1", Hostname: "vps.example.com", ProxyMode: agentv1.ProxyMode_PROXY_MODE_MANAGED}}},
			Dispatcher: dispatcher, Jobs: fakeAPIJobs{}, Audit: audit, Servers: deps.Servers,
		},
	}
	proxyDeps.Agents = proxyDeps.Containers.Agents

	installed := serveRoute(t, http.MethodPost, "/api/v1/servers/{serverID}/proxy",
		"/api/v1/servers/s1/proxy", NewProxyActionHandler(proxyDeps), `{"action":"install"}`)
	if installed.Code != http.StatusAccepted || len(dispatcher.messages) != 1 || dispatcher.messages[0].GetProxyInstall() == nil {
		t.Fatalf("install status=%d messages=%v", installed.Code, dispatcher.messages)
	}
	applied := serveRoute(t, http.MethodPost, "/api/v1/servers/{serverID}/proxy",
		"/api/v1/servers/s1/proxy", NewProxyActionHandler(proxyDeps), `{"action":"apply"}`)
	if applied.Code != http.StatusAccepted || len(dispatcher.messages) != 2 {
		t.Fatalf("apply status=%d messages=%v", applied.Code, dispatcher.messages)
	}
	apply := dispatcher.messages[1].GetProxyApply()
	if apply == nil || len(apply.GetRoutes()) != 1 || apply.GetRoutes()[0].GetHostname() != "app.example.com" {
		t.Fatalf("apply message = %v", dispatcher.messages[1])
	}
	actions := map[string]bool{}
	for _, event := range audit.events {
		actions[event.Action] = true
	}
	if !actions["proxy_install"] || !actions["proxy_apply"] {
		t.Errorf("audit actions = %v", actions)
	}
}

func TestAppCreateShowsSecretsOnceAndNeverInReadEndpoints(t *testing.T) {
	state := newMemAppStore()
	deps, audit, _ := newAppAPI(t, state)
	response := serveRoute(t, http.MethodPost, "/api/v1/servers/{serverID}/apps",
		"/api/v1/servers/s1/apps", NewAppCreateHandler(deps),
		`{"stack_name":"shop","source":"git","repo_url":"https://github.com/example/shop.git","branch":"main","service":"app","port":3000}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	var created appCreatedResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.DeployKeyPublic == "" || created.WebhookSecret == "" || created.WebhookURL == "" {
		t.Fatalf("created response lacks its once-only secrets: %+v", created)
	}
	if created.CredentialUser != "" || created.CredentialPassword != "" {
		t.Fatalf("a Git app returned a template credential: %+v", created)
	}
	if strings.Contains(response.Body.String(), "PRIVATE KEY") ||
		strings.Contains(response.Body.String(), "deploy_key_enc") ||
		strings.Contains(response.Body.String(), "webhook_secret_enc") {
		t.Fatalf("private material leaked: %s", response.Body.String())
	}
	if len(audit.events) == 0 {
		t.Fatal("app creation was not audited")
	}
	createdEvent := audit.events[0]
	if createdEvent.Action != "app_created" {
		t.Fatalf("audit = %+v", audit.events)
	}
	for _, event := range audit.events {
		if strings.Contains(fmt.Sprint(event.Details), created.WebhookSecret) {
			t.Fatal("the webhook secret reached the audit log")
		}
	}

	detail := serveRoute(t, http.MethodGet, "/api/v1/apps/{appID}", "/api/v1/apps/"+created.ID,
		NewAppHandler(deps), "")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d", detail.Code)
	}
	if strings.Contains(detail.Body.String(), created.DeployKeyPublic) ||
		strings.Contains(detail.Body.String(), created.WebhookSecret) ||
		strings.Contains(detail.Body.String(), "PRIVATE KEY") {
		t.Fatalf("read endpoint leaked a once-only secret: %s", detail.Body.String())
	}
}

func TestAppSecretsAreHiddenAndAuditHasNoValues(t *testing.T) {
	app := store.App{ID: "app-1", ServerID: "s1", StackName: "shop", Source: store.AppSourceGit,
		Service: "app", Port: 3000, Build: store.AppBuildDockerfile}
	state := newMemAppStore(app)
	deps, audit, box := newAppAPI(t, state)

	set := serveRoute(t, http.MethodPut, "/api/v1/apps/{appID}/env/{key}",
		"/api/v1/apps/app-1/env/DATABASE_PASSWORD", NewAppEnvSetHandler(deps),
		`{"value":"topsecret","secret":true,"build_time":false}`)
	if set.Code != http.StatusNoContent {
		t.Fatalf("set status = %d, body %s", set.Code, set.Body.String())
	}
	if strings.Contains(string(state.envs["app-1"]["DATABASE_PASSWORD"].ValueEnc), "topsecret") {
		t.Fatal("the secret variable was stored in clear text")
	}
	opened, err := box.Open(state.envs["app-1"]["DATABASE_PASSWORD"].ValueEnc)
	if err != nil || string(opened) != "topsecret" {
		t.Fatalf("sealed value = %q, %v", opened, err)
	}
	if strings.Contains(fmt.Sprint(audit.events[0].Details), "topsecret") {
		t.Fatal("the secret value reached the audit log")
	}

	list := serveRoute(t, http.MethodGet, "/api/v1/apps/{appID}/env", "/api/v1/apps/app-1/env",
		NewAppEnvListHandler(deps), "")
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d", list.Code)
	}
	if strings.Contains(list.Body.String(), "topsecret") {
		t.Fatalf("the secret value reached the response: %s", list.Body.String())
	}
	var envs []appEnvResponse
	if err := json.Unmarshal(list.Body.Bytes(), &envs); err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || !envs[0].Secret || envs[0].Value != "" {
		t.Fatalf("envs = %+v", envs)
	}
}

func TestDomainAddWarnsWithoutBlocking(t *testing.T) {
	app := store.App{ID: "app-1", ServerID: "s1", StackName: "shop", Service: "app", Port: 3000}
	state := newMemAppStore(app)
	deps, audit, _ := newAppAPI(t, state)
	response := serveRoute(t, http.MethodPost, "/api/v1/apps/{appID}/domains",
		"/api/v1/apps/app-1/domains", NewDomainAddHandler(deps), `{"hostname":"app.example.com"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	var domain domainResponse
	if err := json.Unmarshal(response.Body.Bytes(), &domain); err != nil {
		t.Fatal(err)
	}
	if domain.Warning != "the hostname does not resolve to the server" {
		t.Errorf("warning = %q", domain.Warning)
	}
	if len(state.domains) != 1 {
		t.Error("the warning blocked the domain")
	}
	if len(audit.events) != 1 || audit.events[0].Action != "domain_added" {
		t.Errorf("audit = %+v", audit.events)
	}
}

func TestExternalProxyAnswerHasApacheNginxAndCertbot(t *testing.T) {
	app := store.App{ID: "app-1", ServerID: "s1", StackName: "shop", Service: "app", Port: 3000, HostPort: 23456}
	state := newMemAppStore(app)
	state.domains["domain-1"] = store.Domain{ID: "domain-1", AppID: app.ID, Hostname: "app.example.com", Service: "app", Port: 3000}
	deps, _, _ := newAppAPI(t, state)
	proxyDeps := ProxyDeps{AppDeps: deps}
	agents := fakeAgents{agents: []hub.AgentInfo{{ServerID: "s1", Hostname: "vps.example.com", ProxyMode: agentv1.ProxyMode_PROXY_MODE_EXTERNAL}}}
	proxyDeps.Agents = agents
	response := serveRoute(t, http.MethodGet, "/api/v1/servers/{serverID}/proxy",
		"/api/v1/servers/s1/proxy", NewProxyHandler(proxyDeps), "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	var payload proxyResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Mode != "external" || len(payload.Domains) != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	item := payload.Domains[0]
	for _, want := range []string{"ProxyPass", "X-Forwarded-Proto", "Upgrade", "127.0.0.1:23456"} {
		if !strings.Contains(item.Apache, want) {
			t.Errorf("apache block lacks %q: %s", want, item.Apache)
		}
	}
	for _, want := range []string{"proxy_pass", "X-Forwarded-Proto", "Upgrade", "127.0.0.1:23456"} {
		if !strings.Contains(item.Nginx, want) {
			t.Errorf("nginx block lacks %q: %s", want, item.Nginx)
		}
	}
	if !strings.Contains(item.Certbot, "certbot --apache") || !strings.Contains(item.Certbot, "certbot --nginx") {
		t.Errorf("certbot steps = %q", item.Certbot)
	}
}

func TestTemplatesEndpointIncludesLocalizedMetadata(t *testing.T) {
	catalog, err := templates.LoadFS(os.DirFS("../../.."), "templates")
	if err != nil {
		t.Fatal(err)
	}
	state := newMemAppStore()
	deps, _, _ := newAppAPI(t, state)
	deps.Templates = catalog
	response := serveRoute(t, http.MethodGet, "/api/v1/templates", "/api/v1/templates",
		NewTemplatesHandler(deps), "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var templatesResponse []templateResponse
	if err := json.Unmarshal(response.Body.Bytes(), &templatesResponse); err != nil {
		t.Fatal(err)
	}
	if len(templatesResponse) != 18 {
		t.Fatalf("templates = %d", len(templatesResponse))
	}
	first := templatesResponse[0]
	if first.Names["ca"] == "" || first.Names["es"] == "" || first.Names["en"] == "" ||
		first.Descriptions["en"] == "" || first.RAMMB <= 0 {
		t.Errorf("template = %+v", first)
	}
}

func TestCreateFromTemplateUsesTemplateSource(t *testing.T) {
	catalog, err := templates.LoadFS(os.DirFS("../../.."), "templates")
	if err != nil {
		t.Fatal(err)
	}
	state := newMemAppStore()
	deps, audit, _ := newAppAPI(t, state)
	deps.Templates = catalog
	queue := &fakeQueue{}
	deps.Queue = queue
	response := serveRoute(t, http.MethodPost, "/api/v1/servers/{serverID}/apps",
		"/api/v1/servers/s1/apps", NewAppCreateHandler(deps),
		`{"stack_name":"uptime","source":"template","template_id":"uptime-kuma"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	var created appCreatedResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Source != store.AppSourceTemplate || created.TemplateID != "uptime-kuma" || created.Port != 3001 {
		t.Fatalf("created app = %+v", created)
	}
	if created.DeployKeyPublic != "" || created.WebhookSecret != "" {
		t.Error("a template app generated Git secrets")
	}
	if created.CredentialUser != "" || created.CredentialPassword != "" {
		t.Error("a template without credential_length returned a credential")
	}
	if strings.Contains(response.Body.String(), "credential_") {
		t.Errorf("creation response has credential fields for a template without one: %s", response.Body.String())
	}
	if len(queue.calls) != 0 {
		t.Errorf("a template app was deployed before its domain was configured: %+v", queue.calls)
	}
	password := state.envs[created.ID]["TEMPLATE_PASSWORD"]
	if password.Key == "" || !password.Secret || len(password.ValueEnc) == 0 {
		t.Fatalf("template password was not stored: %+v", password)
	}
	if len(audit.events) == 0 || audit.events[0].Action != "app_created" {
		t.Errorf("audit = %+v", audit.events)
	}
}

func TestCreateFromTemplateReturnsFirstLoginCredentialOnce(t *testing.T) {
	catalog, err := templates.LoadFS(os.DirFS("../../.."), "templates")
	if err != nil {
		t.Fatal(err)
	}
	state := newMemAppStore()
	deps, audit, box := newAppAPI(t, state)
	deps.Templates = catalog
	response := serveRoute(t, http.MethodPost, "/api/v1/servers/{serverID}/apps",
		"/api/v1/servers/s1/apps", NewAppCreateHandler(deps),
		`{"stack_name":"news","source":"template","template_id":"miniflux"}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
	var created appCreatedResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.CredentialUser != "admin" || len(created.CredentialPassword) != 32 {
		t.Fatalf("credential = %q/%q", created.CredentialUser, created.CredentialPassword)
	}
	stored := state.envs[created.ID]["TEMPLATE_PASSWORD"]
	plain, err := box.Open(stored.ValueEnc)
	if err != nil {
		t.Fatal(err)
	}
	password := string(plain)
	if created.CredentialPassword != password[:32] {
		t.Fatalf("credential password is not the stored prefix")
	}
	template, ok := catalog.Get("miniflux")
	if !ok {
		t.Fatal("miniflux template is missing")
	}
	rendered, err := template.Render(templates.RenderValues{
		Domain: "news.example.com", Password: password, HostPort: 23456,
		Stack: "news", AppID: created.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered, created.CredentialPassword) {
		t.Fatalf("the returned credential is not rendered by the template")
	}

	detail := serveRoute(t, http.MethodGet, "/api/v1/apps/{appID}", "/api/v1/apps/"+created.ID,
		NewAppHandler(deps), "")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status = %d", detail.Code)
	}
	if strings.Contains(detail.Body.String(), created.CredentialPassword) ||
		strings.Contains(detail.Body.String(), password) ||
		strings.Contains(detail.Body.String(), "credential_") {
		t.Fatalf("a read endpoint leaked the template credential: %s", detail.Body.String())
	}
	for _, event := range audit.events {
		if strings.Contains(fmt.Sprint(event.Details), created.CredentialPassword) {
			t.Fatal("the template credential reached the audit log")
		}
	}
}

func TestCreateAppCredentialResponse(t *testing.T) {
	catalog, err := templates.LoadFS(os.DirFS("../../.."), "templates")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name              string
		body              string
		wantUser          string
		wantPasswordChars int
	}{
		{
			name: "git app",
			body: `{"stack_name":"shop","source":"git","repo_url":"https://github.com/example/shop.git","branch":"main","service":"app","port":3000}`,
		},
		{
			name: "template without credential",
			body: `{"stack_name":"uptime","source":"template","template_id":"uptime-kuma"}`,
		},
		{
			name:              "template with user",
			body:              `{"stack_name":"news","source":"template","template_id":"miniflux"}`,
			wantUser:          "admin",
			wantPasswordChars: 32,
		},
		{
			name:              "template without user",
			body:              `{"stack_name":"cache","source":"template","template_id":"redis"}`,
			wantPasswordChars: 32,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			state := newMemAppStore()
			deps, _, _ := newAppAPI(t, state)
			deps.Templates = catalog
			response := serveRoute(t, http.MethodPost, "/api/v1/servers/{serverID}/apps",
				"/api/v1/servers/s1/apps", NewAppCreateHandler(deps), test.body)
			if response.Code != http.StatusCreated {
				t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
			}
			var created appCreatedResponse
			if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			if created.CredentialUser != test.wantUser {
				t.Errorf("credential_user = %q, want %q", created.CredentialUser, test.wantUser)
			}
			if len(created.CredentialPassword) != test.wantPasswordChars {
				t.Errorf("credential_password has %d chars, want %d",
					len(created.CredentialPassword), test.wantPasswordChars)
			}
			if test.wantPasswordChars == 0 && strings.Contains(response.Body.String(), "credential_") {
				t.Errorf("response has credential fields: %s", response.Body.String())
			}
		})
	}
}

func TestDeleteAppRequiresTheStackName(t *testing.T) {
	app := store.App{ID: "app-1", ServerID: "s1", StackName: "shop", Service: "app", Port: 3000}
	state := newMemAppStore(app)
	deps, audit, _ := newAppAPI(t, state)
	cleaner := deps.Cleaner.(*fakeAppCleaner)

	response := serveRoute(t, http.MethodDelete, "/api/v1/apps/{appID}", "/api/v1/apps/app-1",
		NewAppDeleteHandler(deps), `{"confirm_name":"other"}`)
	if response.Code != http.StatusBadRequest || errorCodeOf(t, response) != "invalid_request" {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	if _, ok := state.apps[app.ID]; !ok {
		t.Error("the app was deleted with a wrong confirmation")
	}
	if len(cleaner.calls) != 0 || len(audit.events) != 0 {
		t.Errorf("calls = %v, audit = %v", cleaner.calls, audit.events)
	}
}

func TestDeleteAppRefusesARunningDeploymentAndAnOfflineAgent(t *testing.T) {
	for _, test := range []struct {
		name   string
		setup  func(*memAppStore, *AppDeps)
		status int
		code   string
	}{
		{
			name: "building",
			setup: func(state *memAppStore, _ *AppDeps) {
				state.deployments["dep-1"] = store.Deployment{ID: "dep-1", AppID: "app-1", State: store.DeploymentBuilding}
			},
			status: http.StatusConflict,
			code:   "deployment_running",
		},
		{
			name: "releasing",
			setup: func(state *memAppStore, _ *AppDeps) {
				state.deployments["dep-1"] = store.Deployment{ID: "dep-1", AppID: "app-1", State: store.DeploymentReleasing}
			},
			status: http.StatusConflict,
			code:   "deployment_running",
		},
		{
			name: "offline",
			setup: func(_ *memAppStore, deps *AppDeps) {
				deps.Agents = fakeAgents{}
			},
			status: http.StatusConflict,
			code:   "agent_offline",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := store.App{ID: "app-1", ServerID: "s1", StackName: "shop", Service: "app", Port: 3000}
			state := newMemAppStore(app)
			deps, audit, _ := newAppAPI(t, state)
			test.setup(state, &deps)
			cleaner := deps.Cleaner.(*fakeAppCleaner)

			response := serveRoute(t, http.MethodDelete, "/api/v1/apps/{appID}", "/api/v1/apps/app-1",
				NewAppDeleteHandler(deps), `{"confirm_name":"shop"}`)
			if response.Code != test.status || errorCodeOf(t, response) != test.code {
				t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
			}
			if _, ok := state.apps[app.ID]; !ok {
				t.Error("the app was deleted")
			}
			if len(cleaner.calls) != 0 || len(audit.events) != 0 {
				t.Errorf("calls = %v, audit = %v", cleaner.calls, audit.events)
			}
		})
	}
}

func TestDeleteAppRunsTheServerStepsInOrderAndThenDeletesTheRows(t *testing.T) {
	app := store.App{ID: "app-1", ServerID: "s1", StackName: "shop", Service: "app", Port: 3000}
	other := store.App{ID: "app-2", ServerID: "s1", StackName: "blog", Service: "app", Port: 3001}
	state := newMemAppStore(app, other)
	state.domains["domain-1"] = store.Domain{ID: "domain-1", AppID: app.ID, Hostname: "shop.example.com", Service: "app", Port: 3000}
	state.domains["domain-2"] = store.Domain{ID: "domain-2", AppID: other.ID, Hostname: "blog.example.com", Service: "app", Port: 3001}
	deps, audit, _ := newAppAPI(t, state)
	deps.Agents = fakeAgents{agents: []hub.AgentInfo{{
		ServerID: "s1", Hostname: "vps.example.com", ProxyMode: agentv1.ProxyMode_PROXY_MODE_MANAGED,
	}}}
	cleaner := deps.Cleaner.(*fakeAppCleaner)
	cleaner.cleanupOutput = "removed image talaia/shop:abcdef123456\nremoved build workspace /var/lib/talaia/builds/shop"

	response := serveRoute(t, http.MethodDelete, "/api/v1/apps/{appID}", "/api/v1/apps/app-1",
		NewAppDeleteHandler(deps), `{"confirm_name":"shop"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	if want := []string{"remove:shop", "cleanup:shop", "proxy"}; strings.Join(cleaner.calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", cleaner.calls, want)
	}
	if len(cleaner.proxyRoutes) != 1 || cleaner.proxyRoutes[0].GetHostname() != "blog.example.com" {
		t.Errorf("proxy routes = %v", cleaner.proxyRoutes)
	}
	if _, ok := state.apps[app.ID]; ok {
		t.Error("the app rows were not deleted")
	}
	if _, ok := state.apps[other.ID]; !ok {
		t.Error("another app was deleted")
	}
	var body appDeleteResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.Output, "removed image talaia/shop:abcdef123456") ||
		!strings.Contains(body.Output, "Named volumes were kept.") {
		t.Errorf("output = %q", body.Output)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "app_deleted" {
		t.Fatalf("audit = %+v", audit.events)
	}
	details := audit.events[0].Details
	if details["stack"] != "shop" || details["volumes_kept"] != true {
		t.Errorf("audit details = %v", details)
	}
	removed, ok := details["removed_images"].([]string)
	if !ok || len(removed) != 1 || removed[0] != "talaia/shop:abcdef123456" {
		t.Errorf("removed images = %#v", details["removed_images"])
	}
}

func TestDeleteAppKeepsTheRowsWhenAnAgentStepFails(t *testing.T) {
	for _, test := range []struct {
		name   string
		setErr func(*fakeAppCleaner)
		reason string
		calls  []string
	}{
		{
			name:   "stack",
			setErr: func(cleaner *fakeAppCleaner) { cleaner.removeErr = errors.New("agent failed") },
			reason: "remove the app stack",
			calls:  []string{"remove:shop"},
		},
		{
			name:   "cleanup",
			setErr: func(cleaner *fakeAppCleaner) { cleaner.cleanupErr = errors.New("agent failed") },
			reason: "remove the app images and build workspace",
			calls:  []string{"remove:shop", "cleanup:shop"},
		},
		{
			name:   "proxy",
			setErr: func(cleaner *fakeAppCleaner) { cleaner.proxyErr = errors.New("agent failed") },
			reason: "apply the proxy routes",
			calls:  []string{"remove:shop", "cleanup:shop", "proxy"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := store.App{ID: "app-1", ServerID: "s1", StackName: "shop", Service: "app", Port: 3000}
			state := newMemAppStore(app)
			deps, audit, _ := newAppAPI(t, state)
			deps.Agents = fakeAgents{agents: []hub.AgentInfo{{
				ServerID: "s1", Hostname: "vps.example.com", ProxyMode: agentv1.ProxyMode_PROXY_MODE_MANAGED,
			}}}
			cleaner := deps.Cleaner.(*fakeAppCleaner)
			test.setErr(cleaner)

			response := serveRoute(t, http.MethodDelete, "/api/v1/apps/{appID}", "/api/v1/apps/app-1",
				NewAppDeleteHandler(deps), `{"confirm_name":"shop"}`)
			if response.Code != http.StatusInternalServerError || errorCodeOf(t, response) != "app_cleanup_failed" {
				t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
			}
			var body errorBody
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Reason != test.reason {
				t.Errorf("reason = %q, want %q", body.Error.Reason, test.reason)
			}
			if strings.Join(cleaner.calls, ",") != strings.Join(test.calls, ",") {
				t.Errorf("calls = %v, want %v", cleaner.calls, test.calls)
			}
			if _, ok := state.apps[app.ID]; !ok {
				t.Error("the app rows were deleted after a failed step")
			}
			if len(audit.events) != 0 {
				t.Errorf("audit = %v", audit.events)
			}
		})
	}
}

func TestDeleteAppDoesNotApplyProxyInExternalMode(t *testing.T) {
	app := store.App{ID: "app-1", ServerID: "s1", StackName: "shop", Service: "app", Port: 3000}
	state := newMemAppStore(app)
	deps, _, _ := newAppAPI(t, state)
	cleaner := deps.Cleaner.(*fakeAppCleaner)

	response := serveRoute(t, http.MethodDelete, "/api/v1/apps/{appID}", "/api/v1/apps/app-1",
		NewAppDeleteHandler(deps), `{"confirm_name":"shop"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	if strings.Join(cleaner.calls, ",") != "remove:shop,cleanup:shop" {
		t.Errorf("calls = %v", cleaner.calls)
	}
	if len(cleaner.proxyRoutes) != 0 {
		t.Errorf("proxy routes = %v", cleaner.proxyRoutes)
	}
}

func TestDeleteAppIgnoresAMissingStack(t *testing.T) {
	app := store.App{ID: "app-1", ServerID: "s1", StackName: "shop", Service: "app", Port: 3000}
	state := newMemAppStore(app)
	deps, _, _ := newAppAPI(t, state)
	cleaner := deps.Cleaner.(*fakeAppCleaner)
	cleaner.removeErr = deploy.ErrStackNotFound

	response := serveRoute(t, http.MethodDelete, "/api/v1/apps/{appID}", "/api/v1/apps/app-1",
		NewAppDeleteHandler(deps), `{"confirm_name":"shop"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	if _, ok := state.apps[app.ID]; ok {
		t.Error("the app rows were not deleted")
	}
}

func TestDeploymentEndpointsQueueAndAudit(t *testing.T) {
	app := store.App{ID: "app-1", ServerID: "s1", StackName: "shop", Service: "app", Port: 3000}
	state := newMemAppStore(app)
	state.deployments["dep-1"] = store.Deployment{
		ID: "dep-1", AppID: app.ID, Number: 1, Trigger: "push", State: store.DeploymentSucceeded,
		CommitSHA: "abcdef1234567890abcdef1234567890abcdef12", Log: "build output",
	}
	deps, audit, _ := newAppAPI(t, state)
	queue := &fakeQueue{}
	deps.Queue = queue

	created := serveRoute(t, http.MethodPost, "/api/v1/apps/{appID}/deployments",
		"/api/v1/apps/app-1/deployments", NewDeploymentCreateHandler(deps), `{}`)
	if created.Code != http.StatusAccepted || len(queue.calls) != 1 || queue.calls[0].trigger != "manual" {
		t.Fatalf("create status=%d calls=%+v", created.Code, queue.calls)
	}
	rolled := serveRoute(t, http.MethodPost, "/api/v1/apps/{appID}/deployments/{deploymentID}/rollback",
		"/api/v1/apps/app-1/deployments/dep-1/rollback", NewDeploymentRollbackHandler(deps), "")
	if rolled.Code != http.StatusAccepted || len(queue.calls) != 2 || queue.calls[1].trigger != "rollback" ||
		queue.calls[1].commit != "abcdef1234567890abcdef1234567890abcdef12" {
		t.Fatalf("rollback status=%d calls=%+v", rolled.Code, queue.calls)
	}
	detail := serveRoute(t, http.MethodGet, "/api/v1/apps/{appID}/deployments/{deploymentID}",
		"/api/v1/apps/app-1/deployments/dep-1", NewDeploymentGetHandler(deps), "")
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "build output") {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	actions := map[string]bool{}
	for _, event := range audit.events {
		actions[event.Action] = true
	}
	if !actions["deployment_requested"] || !actions["deployment_rollback"] {
		t.Errorf("audit actions = %v", actions)
	}
}

package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/catlinux/Talaia/core/internal/templates"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeStore struct {
	apps        map[string]store.App
	envs        map[string][]store.AppEnv
	domains     []store.Domain
	deployments []store.Deployment
	next        int
}

func newFakeStore(app store.App) *fakeStore {
	return &fakeStore{apps: map[string]store.App{app.ID: app}, envs: map[string][]store.AppEnv{}}
}

func (f *fakeStore) GetApp(_ context.Context, id string) (store.App, error) {
	app, ok := f.apps[id]
	if !ok {
		return store.App{}, store.ErrNotFound
	}
	return app, nil
}

func (f *fakeStore) ListAppEnv(_ context.Context, appID string) ([]store.AppEnv, error) {
	return append([]store.AppEnv(nil), f.envs[appID]...), nil
}

func (f *fakeStore) ListAppDomains(_ context.Context, appID string) ([]store.Domain, error) {
	var result []store.Domain
	for _, domain := range f.domains {
		if domain.AppID == appID {
			result = append(result, domain)
		}
	}
	return result, nil
}

func (f *fakeStore) ListDeployments(_ context.Context, appID string, limit int) ([]store.Deployment, error) {
	var result []store.Deployment
	for _, item := range f.deployments {
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

func (f *fakeStore) CreateDeployment(_ context.Context, deployment store.Deployment) (store.Deployment, error) {
	f.next++
	deployment.ID = fmt.Sprintf("dep-%d", f.next)
	number := 0
	for _, item := range f.deployments {
		if item.AppID == deployment.AppID && item.Number > number {
			number = item.Number
		}
	}
	deployment.Number = number + 1
	if deployment.State == "" {
		deployment.State = store.DeploymentQueued
	}
	f.deployments = append(f.deployments, deployment)
	return deployment, nil
}

func (f *fakeStore) UpdateDeployment(_ context.Context, deployment store.Deployment) error {
	for index := range f.deployments {
		if f.deployments[index].ID == deployment.ID {
			f.deployments[index] = deployment
			return nil
		}
	}
	return store.ErrNotFound
}

func (f *fakeStore) SupersedeQueuedDeployments(_ context.Context, appID string) (int64, error) {
	var count int64
	for index := range f.deployments {
		if f.deployments[index].AppID == appID && f.deployments[index].State == store.DeploymentQueued {
			f.deployments[index].State = store.DeploymentSuperseded
			count++
		}
	}
	return count, nil
}

func (f *fakeStore) ClaimNextDeployment(_ context.Context) (store.Deployment, error) {
	active := map[string]bool{}
	for _, item := range f.deployments {
		if item.State == store.DeploymentBuilding || item.State == store.DeploymentReleasing {
			active[item.AppID] = true
		}
	}
	for index := range f.deployments {
		if f.deployments[index].State != store.DeploymentQueued || active[f.deployments[index].AppID] {
			continue
		}
		f.deployments[index].State = store.DeploymentBuilding
		f.deployments[index].StartedAt = time.Now()
		return f.deployments[index], nil
	}
	return store.Deployment{}, store.ErrNotFound
}

func (f *fakeStore) SetAppHostPort(_ context.Context, appID string, port int) error {
	app, ok := f.apps[appID]
	if !ok {
		return store.ErrNotFound
	}
	app.HostPort = port
	f.apps[appID] = app
	return nil
}

func (f *fakeStore) deployment(id string) store.Deployment {
	for _, item := range f.deployments {
		if item.ID == id {
			return item
		}
	}
	return store.Deployment{}
}

func testApp() store.App {
	return store.App{
		ID:             "app-1",
		ServerID:       "server-1",
		StackName:      "shop",
		Source:         store.AppSourceGit,
		RepoURL:        "https://github.com/example/shop.git",
		Branch:         "main",
		Build:          store.AppBuildDockerfile,
		DockerfilePath: "Dockerfile",
		BuildContext:   ".",
		Service:        "app",
		Port:           3000,
		ZeroDowntime:   true,
	}
}

type fakeAgent struct {
	calls        []string
	source       SourceResult
	sourceErr    error
	buildErr     error
	releaseErr   error
	upErr        error
	readErr      error
	writeErr     error
	stackExists  bool
	stackCompose string
	stackEnv     string
	writes       []string
	buildArgs    []*agentv1.BuildArg
	releaseTag   string
	releaseKeep  []string
	releaseCalls int
	upCalls      int
	port         int
	portErr      error
	portCalls    int
}

func (f *fakeAgent) Source(context.Context, string, *agentv1.AppSource) (SourceResult, error) {
	f.calls = append(f.calls, "source")
	return f.source, f.sourceErr
}

func (f *fakeAgent) Build(_ context.Context, _ string, msg *agentv1.AppBuild) (string, error) {
	f.calls = append(f.calls, "build")
	f.buildArgs = msg.GetBuildArgs()
	return "build output", f.buildErr
}

func (f *fakeAgent) Release(_ context.Context, _ string, msg *agentv1.AppRelease) (string, error) {
	f.calls = append(f.calls, "release")
	f.releaseCalls++
	f.releaseTag = msg.GetTag()
	f.releaseKeep = append([]string(nil), msg.GetKeepTags()...)
	return "release output", f.releaseErr
}

func (f *fakeAgent) ReadStack(context.Context, string, string) (string, string, error) {
	f.calls = append(f.calls, "read")
	if f.readErr != nil {
		return "", "", f.readErr
	}
	if !f.stackExists {
		return "", "", ErrStackNotFound
	}
	return f.stackCompose, f.stackEnv, nil
}

func (f *fakeAgent) WriteStack(_ context.Context, _ string, msg *agentv1.StackWrite) (string, error) {
	f.calls = append(f.calls, "write")
	f.writes = append(f.writes, msg.GetComposeYaml())
	if f.writeErr != nil {
		return "", f.writeErr
	}
	f.stackExists = true
	f.stackCompose = msg.GetComposeYaml()
	f.stackEnv = msg.GetEnv()
	return "stack saved", nil
}

func (f *fakeAgent) UpStack(context.Context, string, string) (string, error) {
	f.calls = append(f.calls, "up")
	f.upCalls++
	return "up output", f.upErr
}

func (f *fakeAgent) PickPort(context.Context, string, string) (int, error) {
	f.calls = append(f.calls, "port")
	f.portCalls++
	if f.portErr != nil {
		return 0, f.portErr
	}
	if f.port == 0 {
		return 23456, nil
	}
	return f.port, nil
}

type fakeBox struct {
	values map[string]string
	err    error
}

type fakeModes struct {
	mode agentv1.ProxyMode
}

func (f fakeModes) ProxyMode(context.Context, string) agentv1.ProxyMode {
	return f.mode
}

func (f fakeBox) Open(sealed []byte) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	if value, ok := f.values[string(sealed)]; ok {
		return []byte(value), nil
	}
	return []byte("plain"), nil
}

func newTestManager(t *testing.T, app store.App, agent Agent) (*Manager, *fakeStore) {
	t.Helper()
	state := newFakeStore(app)
	manager := New(Options{
		Store:  state,
		Agent:  agent,
		Box:    fakeBox{values: map[string]string{"sealed": "plain"}},
		Modes:  fakeModes{mode: agentv1.ProxyMode_PROXY_MODE_EXTERNAL},
		Logger: slogDiscard(),
	})
	return manager, state
}

func slogDiscard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func processQueued(t *testing.T, manager *Manager, state *fakeStore) store.Deployment {
	t.Helper()
	deployment, err := state.ClaimNextDeployment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	manager.process(context.Background(), deployment)
	return state.deployment(deployment.ID)
}

func TestDeploySucceedsThroughSourceBuildAndRelease(t *testing.T) {
	app := testApp()
	agent := &fakeAgent{source: SourceResult{
		Commit:  "abcdef123456" + strings.Repeat("0", 28),
		Subject: "first deploy",
	}}
	manager, state := newTestManager(t, app, agent)
	state.envs[app.ID] = []store.AppEnv{
		{AppID: app.ID, Key: "BUILD_ONLY", ValueEnc: []byte("sealed"), BuildTime: true},
		{AppID: app.ID, Key: "SECRET_BUILD", ValueEnc: []byte("sealed"), BuildTime: true, Secret: true},
	}
	state.deployments = append(state.deployments,
		store.Deployment{ID: "old-1", AppID: app.ID, Number: 1, State: store.DeploymentSucceeded, ImageRef: "talaia/shop:111111111111"},
		store.Deployment{ID: "old-2", AppID: app.ID, Number: 2, State: store.DeploymentSucceeded, ImageRef: "talaia/shop:222222222222"},
	)

	if err := manager.Enqueue(context.Background(), app.ID, "push", ""); err != nil {
		t.Fatal(err)
	}
	deployment := processQueued(t, manager, state)
	if deployment.State != store.DeploymentSucceeded {
		t.Fatalf("deployment = %+v", deployment)
	}
	if deployment.ImageRef != "talaia/shop:abcdef123456" {
		t.Errorf("image = %q", deployment.ImageRef)
	}
	if deployment.CommitSubject != "first deploy" || deployment.CommitSHA == "" {
		t.Errorf("commit = %q %q", deployment.CommitSHA, deployment.CommitSubject)
	}
	if len(agent.calls) != 6 || agent.calls[0] != "source" || agent.calls[1] != "build" || agent.calls[2] != "port" || agent.calls[3] != "read" || agent.calls[4] != "write" || agent.calls[5] != "release" {
		t.Errorf("calls = %v", agent.calls)
	}
	if len(agent.buildArgs) != 1 || agent.buildArgs[0].GetKey() != "BUILD_ONLY" || agent.buildArgs[0].GetValue() != "plain" {
		t.Errorf("build args = %v", agent.buildArgs)
	}
	if len(agent.writes) != 1 || !strings.Contains(agent.writes[0], "image: talaia/shop:abcdef123456") {
		t.Errorf("writes = %v", agent.writes)
	}
	if len(agent.writes) == 1 && !strings.Contains(agent.writes[0], "127.0.0.1:23456:3000") {
		t.Errorf("compose does not use the picked host port:\n%s", agent.writes[0])
	}
	if got := state.apps[app.ID].HostPort; got != 23456 {
		t.Errorf("stored host port = %d", got)
	}
	if agent.releaseTag != "abcdef123456" {
		t.Errorf("release tag = %q", agent.releaseTag)
	}
	if len(agent.releaseKeep) != 2 {
		t.Errorf("keep tags = %v", agent.releaseKeep)
	}
	if !strings.Contains(deployment.Log, "deployment succeeded") {
		t.Errorf("log = %q", deployment.Log)
	}
}

func TestBuildFailureNeverTouchesTheStack(t *testing.T) {
	app := testApp()
	agent := &fakeAgent{
		source:   SourceResult{Commit: "abcdef123456" + strings.Repeat("0", 28)},
		buildErr: errors.New("docker build failed"),
	}
	manager, state := newTestManager(t, app, agent)
	if err := manager.Enqueue(context.Background(), app.ID, "push", ""); err != nil {
		t.Fatal(err)
	}
	deployment := processQueued(t, manager, state)
	if deployment.State != store.DeploymentFailed || !strings.Contains(deployment.Error, "docker build failed") {
		t.Fatalf("deployment = %+v", deployment)
	}
	for _, call := range agent.calls {
		if call == "read" || call == "write" || call == "release" || call == "up" || call == "port" {
			t.Errorf("the stack or port was touched after a build failure: %v", agent.calls)
		}
	}
}

func TestEnqueueSupersedesOnlyTheWaitingDeployment(t *testing.T) {
	app := testApp()
	manager, state := newTestManager(t, app, &fakeAgent{})
	if err := manager.Enqueue(context.Background(), app.ID, "push", "a"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Enqueue(context.Background(), app.ID, "push", "b"); err != nil {
		t.Fatal(err)
	}
	if state.deployments[0].State != store.DeploymentSuperseded {
		t.Errorf("first deployment = %s, want superseded", state.deployments[0].State)
	}
	if state.deployments[1].State != store.DeploymentQueued {
		t.Errorf("second deployment = %s, want queued", state.deployments[1].State)
	}

	building := store.Deployment{ID: "building", AppID: app.ID, Number: 3, State: store.DeploymentBuilding}
	state.deployments = append(state.deployments, building)
	if err := manager.Enqueue(context.Background(), app.ID, "push", "c"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Enqueue(context.Background(), app.ID, "push", "d"); err != nil {
		t.Fatal(err)
	}
	if state.deployments[len(state.deployments)-2].State != store.DeploymentSuperseded {
		t.Errorf("the waiting deployment was not superseded: %+v", state.deployments)
	}
	if state.deployments[len(state.deployments)-1].State != store.DeploymentQueued {
		t.Errorf("the newest deployment is not queued: %+v", state.deployments)
	}
	if state.deployment("building").State != store.DeploymentBuilding {
		t.Error("the building deployment was superseded")
	}
}

func TestHandEditedStackOnlyChangesTheImage(t *testing.T) {
	app := testApp()
	agent := &fakeAgent{
		source:       SourceResult{Commit: "abcdef123456" + strings.Repeat("0", 28)},
		stackExists:  true,
		stackCompose: "services:\n  app:\n    image: old\n    environment:\n      KEEP: yes\n  sidecar:\n    image: busybox\n",
	}
	manager, state := newTestManager(t, app, agent)
	if err := manager.Enqueue(context.Background(), app.ID, "push", ""); err != nil {
		t.Fatal(err)
	}
	deployment := processQueued(t, manager, state)
	if deployment.State != store.DeploymentSucceeded {
		t.Fatalf("deployment = %+v", deployment)
	}
	if len(agent.writes) != 1 {
		t.Fatalf("writes = %v", agent.writes)
	}
	written := agent.writes[0]
	if !strings.Contains(written, "image: talaia/shop:abcdef123456") || !strings.Contains(written, "KEEP: yes") || !strings.Contains(written, "image: busybox") {
		t.Errorf("hand-edited compose = %q", written)
	}
	if !strings.Contains(deployment.Log, "hand-edited") {
		t.Errorf("log = %q", deployment.Log)
	}
}

func TestHandEditedStackWarnsWhenTheServiceIsGone(t *testing.T) {
	app := testApp()
	agent := &fakeAgent{
		source:       SourceResult{Commit: "abcdef123456" + strings.Repeat("0", 28)},
		stackExists:  true,
		stackCompose: "services:\n  sidecar:\n    image: busybox\n",
	}
	manager, state := newTestManager(t, app, agent)
	if err := manager.Enqueue(context.Background(), app.ID, "push", ""); err != nil {
		t.Fatal(err)
	}
	deployment := processQueued(t, manager, state)
	if deployment.State != store.DeploymentFailed {
		t.Fatalf("deployment = %+v", deployment)
	}
	if !strings.Contains(deployment.Log, "no longer exists") {
		t.Errorf("log = %q", deployment.Log)
	}
	if len(agent.writes) != 0 {
		t.Errorf("writes = %v", agent.writes)
	}
	if agent.releaseCalls != 0 {
		t.Error("the app was released without its service")
	}
}

func TestRollbackSkipsSourceAndBuild(t *testing.T) {
	app := testApp()
	agent := &fakeAgent{}
	manager, state := newTestManager(t, app, agent)
	commit := "123456789abc" + strings.Repeat("0", 28)
	if err := manager.Enqueue(context.Background(), app.ID, "rollback", commit); err != nil {
		t.Fatal(err)
	}
	deployment := processQueued(t, manager, state)
	if deployment.State != store.DeploymentSucceeded {
		t.Fatalf("deployment = %+v", deployment)
	}
	if agent.calls[0] == "source" || agent.calls[0] == "build" {
		t.Errorf("rollback ran the source or build: %v", agent.calls)
	}
	if deployment.ImageRef != "talaia/shop:123456789abc" || agent.releaseTag != "123456789abc" {
		t.Errorf("image = %q, release tag = %q", deployment.ImageRef, agent.releaseTag)
	}
}

func TestDeployReusesAStoredHostPort(t *testing.T) {
	app := testApp()
	app.HostPort = 24567
	agent := &fakeAgent{source: SourceResult{Commit: "abcdef123456" + strings.Repeat("0", 28)}}
	manager, state := newTestManager(t, app, agent)
	if err := manager.Enqueue(context.Background(), app.ID, "push", ""); err != nil {
		t.Fatal(err)
	}
	deployment := processQueued(t, manager, state)
	if deployment.State != store.DeploymentSucceeded {
		t.Fatalf("deployment = %+v", deployment)
	}
	if agent.portCalls != 0 {
		t.Errorf("the stored host port was picked again")
	}
	if len(agent.writes) != 1 || !strings.Contains(agent.writes[0], "127.0.0.1:24567:3000") {
		t.Errorf("compose = %v", agent.writes)
	}
}

func TestDeployRefusesAnInvalidPickedPort(t *testing.T) {
	for _, port := range []int{3000, 80, 30000} {
		t.Run(fmt.Sprintf("%d", port), func(t *testing.T) {
			app := testApp()
			agent := &fakeAgent{
				source: SourceResult{Commit: "abcdef123456" + strings.Repeat("0", 28)},
				port:   port,
			}
			manager, state := newTestManager(t, app, agent)
			if err := manager.Enqueue(context.Background(), app.ID, "push", ""); err != nil {
				t.Fatal(err)
			}
			deployment := processQueued(t, manager, state)
			if deployment.State != store.DeploymentFailed {
				t.Fatalf("deployment = %+v", deployment)
			}
			if len(agent.writes) != 0 || agent.releaseCalls != 0 {
				t.Errorf("the stack was touched with an invalid port: %v", agent.calls)
			}
			if got := state.apps[app.ID].HostPort; got != 0 {
				t.Errorf("invalid port was stored: %d", got)
			}
		})
	}
}

func TestTemplateAppRendersAndStartsTheStack(t *testing.T) {
	catalog, err := templates.LoadFS(os.DirFS("../../.."), "templates")
	if err != nil {
		t.Fatal(err)
	}
	app := testApp()
	app.Source = store.AppSourceTemplate
	app.TemplateID = "uptime-kuma"
	app.TemplateVersion = "1.0.0"
	app.Image = "louislam/uptime-kuma:1.23.16"
	app.Service = "app"
	app.Port = 3001
	agent := &fakeAgent{}
	manager, state := newTestManager(t, app, agent)
	manager.templates = catalog
	state.envs[app.ID] = []store.AppEnv{{AppID: app.ID, Key: "TEMPLATE_PASSWORD", ValueEnc: []byte("sealed"), Secret: true}}
	state.domains = []store.Domain{{ID: "domain-1", AppID: app.ID, Hostname: "uptime.example.com", Service: "app", Port: 3001}}

	if err := manager.Enqueue(context.Background(), app.ID, "create", ""); err != nil {
		t.Fatal(err)
	}
	deployment := processQueued(t, manager, state)
	if deployment.State != store.DeploymentSucceeded {
		t.Fatalf("deployment = %+v", deployment)
	}
	if agent.upCalls != 1 || agent.releaseCalls != 0 {
		t.Errorf("up=%d release=%d calls=%v", agent.upCalls, agent.releaseCalls, agent.calls)
	}
	for _, call := range agent.calls {
		if call == "source" || call == "build" {
			t.Errorf("template ran a build step: %v", agent.calls)
		}
	}
	if len(agent.writes) != 1 || !strings.Contains(agent.writes[0], "uptime.example.com") ||
		!strings.Contains(agent.writes[0], "127.0.0.1:23456:3001") {
		t.Errorf("rendered stack = %v", agent.writes)
	}
}

func TestGenerateCompose(t *testing.T) {
	app := testApp()
	external, err := generateCompose(app, "app", "talaia/shop:abcdef123456", false, 23456)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  app:\n",
		"image: talaia/shop:abcdef123456",
		"restart: unless-stopped",
		"cpus: \"1.0\"",
		"mem_limit: 512m",
		"\"127.0.0.1:23456:3000\"",
		"dev.talaia.app: app-1",
	} {
		if !strings.Contains(external, want) {
			t.Errorf("external compose does not contain %q:\n%s", want, external)
		}
	}
	if strings.Contains(external, "talaia-proxy") {
		t.Errorf("external compose uses the managed proxy network:\n%s", external)
	}

	managed, err := generateCompose(app, "app", "talaia/shop:abcdef123456", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(managed, "  talaia-proxy:\n    external: true") {
		t.Errorf("managed compose has no proxy network:\n%s", managed)
	}
	if strings.Contains(managed, "127.0.0.1") {
		t.Errorf("managed compose publishes a host port:\n%s", managed)
	}
}

func TestManagedModeUsesTheProxyNetwork(t *testing.T) {
	app := testApp()
	agent := &fakeAgent{source: SourceResult{Commit: "abcdef123456" + strings.Repeat("0", 28)}}
	manager, state := newTestManager(t, app, agent)
	manager.modes = fakeModes{mode: agentv1.ProxyMode_PROXY_MODE_MANAGED}
	if err := manager.Enqueue(context.Background(), app.ID, "push", ""); err != nil {
		t.Fatal(err)
	}
	deployment := processQueued(t, manager, state)
	if deployment.State != store.DeploymentSucceeded {
		t.Fatalf("deployment = %+v", deployment)
	}
	if len(agent.writes) != 1 {
		t.Fatalf("writes = %v", agent.writes)
	}
	if !strings.Contains(agent.writes[0], "talaia-proxy") || strings.Contains(agent.writes[0], "127.0.0.1") {
		t.Errorf("managed compose = %q", agent.writes[0])
	}
	if agent.portCalls != 0 {
		t.Errorf("managed mode picked a host port")
	}
}

func TestDeploymentLogKeepsTheEnd(t *testing.T) {
	log := &deploymentLog{}
	log.add(strings.Repeat("x", store.MaxDeploymentLog+100))
	if len(log.String()) != store.MaxDeploymentLog {
		t.Errorf("log length = %d", len(log.String()))
	}
	if !strings.HasSuffix(log.String(), "x") {
		t.Error("log does not keep the end")
	}
}

func TestDeployImageSourceStartsTheStack(t *testing.T) {
	app := testApp()
	app.Source = store.AppSourceImage
	app.Image = "nginx:1.27"
	agent := &fakeAgent{}
	manager, state := newTestManager(t, app, agent)
	if err := manager.Enqueue(context.Background(), app.ID, "manual", ""); err != nil {
		t.Fatal(err)
	}
	deployment := processQueued(t, manager, state)
	if deployment.State != store.DeploymentSucceeded {
		t.Fatalf("deployment = %+v", deployment)
	}
	if agent.upCalls != 1 || agent.releaseCalls != 0 {
		t.Errorf("up calls = %d, release calls = %d", agent.upCalls, agent.releaseCalls)
	}
	if len(agent.writes) != 1 || !strings.Contains(agent.writes[0], "image: nginx:1.27") {
		t.Errorf("writes = %v", agent.writes)
	}
}

func TestSetServiceImageChangesOnlyTheSelectedService(t *testing.T) {
	input := "services:\n  app:\n    image: old\n    environment:\n      A: b\n  db:\n    image: postgres\n"
	got, found := setServiceImage(input, "app", "talaia/shop:abcdef123456")
	if !found {
		t.Fatal("the service was not found")
	}
	if !strings.Contains(got, "image: talaia/shop:abcdef123456") || !strings.Contains(got, "image: postgres") || !strings.Contains(got, "A: b") {
		t.Errorf("compose = %q", got)
	}
}

func TestSetServiceImageReportsAMissingService(t *testing.T) {
	if _, found := setServiceImage("services:\n  db:\n    image: postgres\n", "app", "talaia/shop:abcdef123456"); found {
		t.Fatal("a missing service was reported as found")
	}
}

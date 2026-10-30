package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testStackSeq atomic.Uint64

func testStackName() string {
	return fmt.Sprintf("stack-%d-%d", time.Now().UnixNano(), testStackSeq.Add(1))
}

func createTestApp(t *testing.T, s *Store, serverID string) App {
	t.Helper()

	app, err := s.CreateApp(context.Background(), App{
		ServerID:       serverID,
		StackName:      testStackName(),
		Source:         AppSourceGit,
		RepoURL:        "https://github.com/example/app.git",
		Branch:         "main",
		Build:          AppBuildDockerfile,
		DockerfilePath: "Dockerfile",
		BuildContext:   ".",
		Service:        "app",
		AutoDeploy:     true,
		ZeroDowntime:   true,
		UpdatedBy:      "admin",
	})
	if err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	return app
}

func TestAppLifecycleAndUniqueStackName(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)

	app := createTestApp(t, s, serverID)
	if app.ID == "" || app.CreatedAt.IsZero() || app.UpdatedAt.IsZero() {
		t.Fatalf("created app = %+v", app)
	}
	got, err := s.GetApp(ctx, app.ID)
	if err != nil || got.StackName != app.StackName || got.ServerID != serverID {
		t.Fatalf("GetApp = %+v, %v", got, err)
	}

	apps, err := s.ListApps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !containsApp(apps, app.ID) {
		t.Fatal("ListApps did not return the app")
	}
	byServer, err := s.ListAppsByServer(ctx, serverID)
	if err != nil || len(byServer) != 1 || byServer[0].ID != app.ID {
		t.Fatalf("ListAppsByServer = %+v, %v", byServer, err)
	}

	got.Branch = "release"
	got.Port = 8080
	got.AutoDeploy = false
	updated, err := s.UpdateApp(ctx, got)
	if err != nil {
		t.Fatalf("UpdateApp: %v", err)
	}
	if updated.Branch != "release" || updated.Port != 8080 || updated.AutoDeploy {
		t.Fatalf("updated app = %+v", updated)
	}

	if _, err := s.CreateApp(ctx, App{
		ServerID:  serverID,
		StackName: app.StackName,
		Source:    AppSourceImage,
		Build:     AppBuildDockerfile,
	}); !errors.Is(err, ErrAppExists) {
		t.Fatalf("duplicate app = %v, want ErrAppExists", err)
	}

	otherServerID := newTestServerID(t, s)
	if _, err := s.CreateApp(ctx, App{
		ServerID:  otherServerID,
		StackName: app.StackName,
		Source:    AppSourceImage,
		Build:     AppBuildDockerfile,
	}); err != nil {
		t.Fatalf("same stack name on another server: %v", err)
	}

	sameServer := createTestApp(t, s, serverID)
	sameServer.StackName = app.StackName
	if _, err := s.UpdateApp(ctx, sameServer); !errors.Is(err, ErrAppExists) {
		t.Fatalf("update to a duplicate stack name = %v, want ErrAppExists", err)
	}

	if err := s.DeleteApp(ctx, app.ID); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}
	if _, err := s.GetApp(ctx, app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted app = %v, want ErrNotFound", err)
	}
	if err := s.DeleteApp(ctx, app.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DeleteApp = %v, want ErrNotFound", err)
	}
}

func containsApp(apps []App, id string) bool {
	for _, app := range apps {
		if app.ID == id {
			return true
		}
	}
	return false
}

func containsDomain(domains []Domain, hostname string) bool {
	for _, domain := range domains {
		if domain.Hostname == hostname {
			return true
		}
	}
	return false
}

func TestPollEnabledAndListPollableApps(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)

	pollable := createTestApp(t, s, serverID)
	if pollable.PollEnabled {
		t.Fatal("a new app has polling enabled by default")
	}
	pollable.PollEnabled = true
	pollable, err := s.UpdateApp(ctx, pollable)
	if err != nil {
		t.Fatalf("enable polling: %v", err)
	}
	if !pollable.PollEnabled {
		t.Fatal("polling was not stored")
	}

	notPolled := createTestApp(t, s, serverID)
	off := createTestApp(t, s, serverID)
	off.PollEnabled = true
	off.AutoDeploy = false
	if _, err := s.UpdateApp(ctx, off); err != nil {
		t.Fatalf("disable auto deploy: %v", err)
	}
	image := createTestApp(t, s, serverID)
	image.Source = AppSourceImage
	image.PollEnabled = true
	if _, err := s.UpdateApp(ctx, image); err != nil {
		t.Fatalf("make image app: %v", err)
	}

	apps, err := s.ListPollableApps(ctx)
	if err != nil {
		t.Fatalf("ListPollableApps: %v", err)
	}
	var ids []string
	for _, app := range apps {
		if app.ServerID == serverID {
			ids = append(ids, app.ID)
		}
	}
	if len(ids) != 1 || ids[0] != pollable.ID {
		t.Fatalf("pollable apps = %v, want only %s (notPolled=%s)", ids, pollable.ID, notPolled.ID)
	}
}

func TestAppEnvPublicListingHidesSecretValues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	app := createTestApp(t, s, newTestServerID(t, s))

	if err := s.SetAppEnv(ctx, AppEnv{
		AppID: app.ID, Key: "TOKEN", ValueEnc: []byte("sealed-token"), Secret: true,
	}); err != nil {
		t.Fatalf("SetAppEnv secret: %v", err)
	}
	if err := s.SetAppEnv(ctx, AppEnv{
		AppID: app.ID, Key: "PORT", ValueEnc: []byte("sealed-port"), BuildTime: true,
	}); err != nil {
		t.Fatalf("SetAppEnv plain: %v", err)
	}

	all, err := s.ListAppEnv(ctx, app.ID)
	if err != nil || len(all) != 2 {
		t.Fatalf("ListAppEnv = %+v, %v", all, err)
	}
	if string(all[0].ValueEnc) != "sealed-port" || string(all[1].ValueEnc) != "sealed-token" {
		t.Fatalf("ListAppEnv values = %+v", all)
	}

	public, err := s.ListAppEnvPublic(ctx, app.ID)
	if err != nil || len(public) != 2 {
		t.Fatalf("ListAppEnvPublic = %+v, %v", public, err)
	}
	if public[0].Secret || string(public[0].ValueEnc) != "sealed-port" {
		t.Fatalf("public plain variable = %+v", public[0])
	}
	if !public[1].Secret || public[1].ValueEnc != nil {
		t.Fatalf("public secret variable = %+v", public[1])
	}

	// Upserting a variable replaces its value and flags.
	if err := s.SetAppEnv(ctx, AppEnv{
		AppID: app.ID, Key: "PORT", ValueEnc: []byte("new"), Secret: true,
	}); err != nil {
		t.Fatalf("update SetAppEnv: %v", err)
	}
	public, _ = s.ListAppEnvPublic(ctx, app.ID)
	if !public[0].Secret || public[0].ValueEnc != nil {
		t.Fatalf("updated public variable = %+v", public[0])
	}

	if err := s.DeleteAppEnv(ctx, app.ID, "TOKEN"); err != nil {
		t.Fatalf("DeleteAppEnv: %v", err)
	}
	if err := s.DeleteAppEnv(ctx, app.ID, "TOKEN"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DeleteAppEnv = %v, want ErrNotFound", err)
	}
}

func TestDeploymentNumberingLogAndSupersede(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	app := createTestApp(t, s, newTestServerID(t, s))
	otherApp := createTestApp(t, s, app.ServerID)

	first, err := s.CreateDeployment(ctx, Deployment{
		AppID: app.ID, Trigger: "create", CommitSHA: "a", State: DeploymentQueued,
	})
	if err != nil {
		t.Fatalf("CreateDeployment first: %v", err)
	}
	second, err := s.CreateDeployment(ctx, Deployment{AppID: app.ID, Trigger: "manual"})
	if err != nil {
		t.Fatalf("CreateDeployment second: %v", err)
	}
	third, err := s.CreateDeployment(ctx, Deployment{AppID: app.ID, Trigger: "push"})
	if err != nil {
		t.Fatalf("CreateDeployment third: %v", err)
	}
	if first.Number != 1 || second.Number != 2 || third.Number != 3 {
		t.Fatalf("numbers = %d, %d, %d", first.Number, second.Number, third.Number)
	}
	other, err := s.CreateDeployment(ctx, Deployment{AppID: otherApp.ID, Trigger: "manual"})
	if err != nil || other.Number != 1 {
		t.Fatalf("other app deployment = %+v, %v", other, err)
	}

	list, err := s.ListDeployments(ctx, app.ID, 10)
	if err != nil || len(list) != 3 || list[0].ID != third.ID || list[2].ID != first.ID {
		t.Fatalf("ListDeployments = %+v, %v", list, err)
	}
	limited, err := s.ListDeployments(ctx, app.ID, 1)
	if err != nil || len(limited) != 1 || limited[0].ID != third.ID {
		t.Fatalf("limited deployments = %+v, %v", limited, err)
	}

	first.State = DeploymentBuilding
	first.StartedAt = time.Now().UTC()
	if err := s.UpdateDeployment(ctx, first); err != nil {
		t.Fatalf("UpdateDeployment: %v", err)
	}
	if err := s.AppendDeploymentLog(ctx, first.ID, strings.Repeat("x", MaxDeploymentLog+100)); err != nil {
		t.Fatalf("AppendDeploymentLog: %v", err)
	}
	if err := s.AppendDeploymentLog(ctx, first.ID, "END"); err != nil {
		t.Fatalf("AppendDeploymentLog END: %v", err)
	}
	got, err := s.GetDeployment(ctx, first.ID)
	if err != nil {
		t.Fatalf("GetDeployment: %v", err)
	}
	if got.State != DeploymentBuilding || got.StartedAt.IsZero() || !got.FinishedAt.IsZero() {
		t.Fatalf("updated deployment = %+v", got)
	}
	if len(got.Log) != MaxDeploymentLog || !strings.HasSuffix(got.Log, "END") {
		t.Fatalf("log length = %d, want %d and the END suffix", len(got.Log), MaxDeploymentLog)
	}

	second.State = DeploymentReleasing
	if err := s.UpdateDeployment(ctx, second); err != nil {
		t.Fatalf("release UpdateDeployment: %v", err)
	}
	superseded, err := s.SupersedeQueuedDeployments(ctx, app.ID)
	if err != nil || superseded != 1 {
		t.Fatalf("SupersedeQueuedDeployments = %d, %v, want 1", superseded, err)
	}
	for _, id := range []string{first.ID, second.ID} {
		got, err := s.GetDeployment(ctx, id)
		if err != nil || got.State == DeploymentSuperseded {
			t.Errorf("running deployment %s = %+v, %v", id, got, err)
		}
	}
	got, _ = s.GetDeployment(ctx, third.ID)
	if got.State != DeploymentSuperseded {
		t.Fatalf("queued deployment state = %q, want %q", got.State, DeploymentSuperseded)
	}
	if n, err := s.SupersedeQueuedDeployments(ctx, app.ID); err != nil || n != 0 {
		t.Fatalf("second supersede = %d, %v, want 0", n, err)
	}

	got, _ = s.GetDeployment(ctx, first.ID)
	got.State = DeploymentFailed
	got.FinishedAt = time.Now().UTC()
	got.Error = "build failed"
	if err := s.UpdateDeployment(ctx, got); err != nil {
		t.Fatalf("finish UpdateDeployment: %v", err)
	}
	got, _ = s.GetDeployment(ctx, first.ID)
	if got.State != DeploymentFailed || got.FinishedAt.IsZero() || got.Error != "build failed" {
		t.Fatalf("finished deployment = %+v", got)
	}
}

func TestCreateDeploymentAllocatesConcurrentNumbers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	app := createTestApp(t, s, newTestServerID(t, s))

	type result struct {
		number int
		err    error
	}
	const count = 6
	results := make(chan result, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			deployment, err := s.CreateDeployment(ctx, Deployment{AppID: app.ID, Trigger: "push"})
			results <- result{number: deployment.Number, err: err}
		}()
	}
	wg.Wait()
	close(results)

	seen := make(map[int]bool, count)
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent CreateDeployment: %v", result.err)
		}
		if result.number < 1 || result.number > count || seen[result.number] {
			t.Fatalf("concurrent number = %d, already seen = %v", result.number, seen)
		}
		seen[result.number] = true
	}
	if len(seen) != count {
		t.Fatalf("assigned %d numbers, want %d", len(seen), count)
	}
}

func TestDomainsAndCascadeDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	app := createTestApp(t, s, newTestServerID(t, s))
	otherApp := createTestApp(t, s, app.ServerID)
	hostname := fmt.Sprintf("app-%d.example.com", time.Now().UnixNano())

	domain, err := s.AddDomain(ctx, Domain{
		AppID: app.ID, Hostname: hostname, Service: "app", Port: 3000,
	})
	if err != nil {
		t.Fatalf("AddDomain: %v", err)
	}
	if domain.ID == "" || domain.CreatedAt.IsZero() {
		t.Fatalf("domain = %+v", domain)
	}
	if _, err := s.AddDomain(ctx, Domain{
		AppID: otherApp.ID, Hostname: hostname, Service: "app", Port: 3000,
	}); !errors.Is(err, ErrDomainExists) {
		t.Fatalf("duplicate hostname = %v, want ErrDomainExists", err)
	}
	otherHostname := fmt.Sprintf("other-%d.example.com", time.Now().UnixNano())
	if _, err := s.AddDomain(ctx, Domain{
		AppID: otherApp.ID, Hostname: otherHostname, Service: "web", Port: 3001,
	}); err != nil {
		t.Fatalf("AddDomain other: %v", err)
	}

	domains, err := s.ListDomains(ctx)
	if err != nil || !containsDomain(domains, hostname) || !containsDomain(domains, otherHostname) {
		t.Fatalf("ListDomains = %+v, %v", domains, err)
	}
	appDomains, err := s.ListAppDomains(ctx, app.ID)
	if err != nil || len(appDomains) != 1 || appDomains[0].Hostname != hostname {
		t.Fatalf("ListAppDomains = %+v, %v", appDomains, err)
	}

	if err := s.SetAppEnv(ctx, AppEnv{AppID: app.ID, Key: "TOKEN", ValueEnc: []byte("sealed")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDeployment(ctx, Deployment{AppID: app.ID, Trigger: "create"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteApp(ctx, app.ID); err != nil {
		t.Fatalf("DeleteApp: %v", err)
	}

	var envs, deployments, remainingDomains int
	if err := s.pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM app_env WHERE app_id = $1),
		        (SELECT count(*) FROM deployment WHERE app_id = $1),
		        (SELECT count(*) FROM domain WHERE app_id = $1)`,
		app.ID).Scan(&envs, &deployments, &remainingDomains); err != nil {
		t.Fatalf("count children: %v", err)
	}
	if envs != 0 || deployments != 0 || remainingDomains != 0 {
		t.Fatalf("cascade left env=%d deployments=%d domains=%d", envs, deployments, remainingDomains)
	}
	if err := s.RemoveDomain(ctx, domain.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RemoveDomain after cascade = %v, want ErrNotFound", err)
	}
	if otherDomains, err := s.ListAppDomains(ctx, otherApp.ID); err != nil || len(otherDomains) != 1 {
		t.Fatalf("other app domains = %+v, %v", otherDomains, err)
	}
}

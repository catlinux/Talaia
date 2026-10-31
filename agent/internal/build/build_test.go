package build

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type runnerCall struct {
	dir  string
	args []string
}

type fakeRunner struct {
	mu    sync.Mutex
	calls []runnerCall
	run   func(context.Context, string, []string) (string, error)
}

func (f *fakeRunner) Run(ctx context.Context, dir string, args ...string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, runnerCall{dir: dir, args: append([]string(nil), args...)})
	f.mu.Unlock()
	if f.run != nil {
		return f.run(ctx, dir, args)
	}
	return "", nil
}

func (f *fakeRunner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func newTestBuilder(t *testing.T, runner Runner, free uint64) (*Builder, string) {
	t.Helper()
	root := t.TempDir()
	builder := New(root, runner)
	builder.WithFreeBytes(func(string) (uint64, error) { return free, nil })
	return builder, root
}

func prepareWorkspace(t *testing.T, builder *Builder, name string) string {
	t.Helper()
	workspace := filepath.Join(builder.root, name)
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return workspace
}

func appBuild(overrides func(*agentv1.AppBuild)) *agentv1.AppBuild {
	app := &agentv1.AppBuild{Name: "shop", Tag: "abcdef123456"}
	if overrides != nil {
		overrides(app)
	}
	return app
}

func TestBuildUsesExactDockerArgumentsAndCleansItsWorkspace(t *testing.T) {
	runner := &fakeRunner{run: func(context.Context, string, []string) (string, error) {
		return "built image", nil
	}}
	builder, root := newTestBuilder(t, runner, MinFreeBytes+1)
	workspace := prepareWorkspace(t, builder, "shop")
	if err := os.MkdirAll(filepath.Join(workspace, "docker"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "docker", "Dockerfile"), []byte("FROM scratch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "app"), 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "other")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}

	output, err := builder.Build(context.Background(), appBuild(func(app *agentv1.AppBuild) {
		app.DockerfilePath = "docker/Dockerfile"
		app.BuildContext = "app"
		app.BuildArgs = []*agentv1.BuildArg{{Key: "NODE_ENV", Value: "production"}}
	}))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if output != "built image" {
		t.Fatalf("output = %q", output)
	}

	want := []string{
		"build", "--pull", "--tag", "talaia/shop:abcdef123456",
		"--file", filepath.Join(workspace, "docker", "Dockerfile"),
		"--build-arg", "NODE_ENV=production",
		filepath.Join(workspace, "app"),
	}
	if runner.count() != 1 {
		t.Fatalf("docker ran %d times, want 1", runner.count())
	}
	if runner.calls[0].dir != workspace {
		t.Errorf("docker dir = %q, want %q", runner.calls[0].dir, workspace)
	}
	if strings.Join(runner.calls[0].args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("docker args = %q, want %q", runner.calls[0].args, want)
	}
	for _, arg := range runner.calls[0].args {
		if arg == "--privileged" || arg == "host" || arg == "--network" {
			t.Errorf("forbidden docker argument %q", arg)
		}
	}
	if _, err := os.Stat(workspace); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("workspace still exists: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("another workspace was removed: %v", err)
	}
}

func TestBuildOnlyPassesTheBuildArgumentsItWasGiven(t *testing.T) {
	t.Setenv("DATABASE_PASSWORD", "topsecret")
	runner := &fakeRunner{}
	builder, _ := newTestBuilder(t, runner, MinFreeBytes+1)
	prepareWorkspace(t, builder, "shop")

	_, err := builder.Build(context.Background(), appBuild(func(app *agentv1.AppBuild) {
		app.BuildArgs = []*agentv1.BuildArg{{Key: "PUBLIC_FLAG", Value: "1"}}
	}))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if runner.count() != 1 {
		t.Fatalf("docker ran %d times, want 1", runner.count())
	}
	args := runner.calls[0].args
	buildArgs := 0
	for i, arg := range args {
		if arg == "topsecret" || strings.Contains(arg, "DATABASE_PASSWORD") {
			t.Errorf("a secret from the environment reached docker: %q", arg)
		}
		if arg == "--build-arg" {
			buildArgs++
			if i+1 >= len(args) || args[i+1] != "PUBLIC_FLAG=1" {
				t.Errorf("unexpected build argument after %q: %v", arg, args)
			}
		}
	}
	if buildArgs != 1 {
		t.Errorf("docker got %d build arguments, want 1: %v", buildArgs, args)
	}
}

func TestBuildRefusesLessThanTwoGiBFree(t *testing.T) {
	runner := &fakeRunner{}
	builder, _ := newTestBuilder(t, runner, MinFreeBytes-1)
	workspace := prepareWorkspace(t, builder, "shop")

	_, err := builder.Build(context.Background(), appBuild(nil))
	if !errors.Is(err, ErrNotEnoughSpace) {
		t.Fatalf("error = %v, want ErrNotEnoughSpace", err)
	}
	if runner.count() != 0 {
		t.Errorf("docker ran %d times, want 0", runner.count())
	}
	if _, statErr := os.Stat(workspace); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("workspace still exists: %v", statErr)
	}
}

func TestBuildUsesAThirtyMinuteTimeout(t *testing.T) {
	runner := &fakeRunner{run: func(ctx context.Context, _ string, _ []string) (string, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("docker build has no deadline")
			return "", nil
		}
		if left := time.Until(deadline); left < 29*time.Minute || left > Timeout+time.Second {
			t.Errorf("deadline is %s away, want about %s", left, Timeout)
		}
		return "ok", nil
	}}
	builder, _ := newTestBuilder(t, runner, MinFreeBytes+1)
	prepareWorkspace(t, builder, "shop")

	if _, err := builder.Build(context.Background(), appBuild(nil)); err != nil {
		t.Fatalf("Build: %v", err)
	}
}

func TestBuildRefusesASecondConcurrentBuild(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	runner := &fakeRunner{run: func(context.Context, string, []string) (string, error) {
		once.Do(func() { close(started) })
		<-release
		return "ok", nil
	}}
	builder, _ := newTestBuilder(t, runner, MinFreeBytes+1)
	prepareWorkspace(t, builder, "shop")

	first := make(chan error, 1)
	go func() {
		_, err := builder.Build(context.Background(), appBuild(nil))
		first <- err
	}()
	<-started

	_, err := builder.Build(context.Background(), appBuild(nil))
	if !errors.Is(err, ErrBuildRunning) {
		t.Errorf("second build error = %v, want ErrBuildRunning", err)
	}
	close(release)
	if err := <-first; err != nil {
		t.Errorf("first build: %v", err)
	}
	if runner.count() != 1 {
		t.Errorf("docker ran %d times, want 1", runner.count())
	}
}

func TestBuildRefusesPathsOutsideTheWorkspace(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*agentv1.AppBuild)
	}{
		{name: "dockerfile parent", apply: func(app *agentv1.AppBuild) { app.DockerfilePath = "../Dockerfile" }},
		{name: "dockerfile absolute", apply: func(app *agentv1.AppBuild) { app.DockerfilePath = "/etc/passwd" }},
		{name: "dockerfile windows absolute", apply: func(app *agentv1.AppBuild) { app.DockerfilePath = `C:\Windows\win.ini` }},
		{name: "dockerfile climbs", apply: func(app *agentv1.AppBuild) { app.DockerfilePath = "a/../../Dockerfile" }},
		{name: "context parent", apply: func(app *agentv1.AppBuild) { app.BuildContext = "../other" }},
		{name: "context absolute", apply: func(app *agentv1.AppBuild) { app.BuildContext = "/tmp" }},
		{name: "context windows absolute", apply: func(app *agentv1.AppBuild) { app.BuildContext = `C:\tmp` }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			builder, _ := newTestBuilder(t, runner, MinFreeBytes+1)
			prepareWorkspace(t, builder, "shop")
			app := appBuild(tc.apply)

			if _, err := builder.Build(context.Background(), app); err == nil {
				t.Fatal("Build accepted a path outside the workspace")
			}
			if runner.count() != 0 {
				t.Errorf("docker ran %d times, want 0", runner.count())
			}
		})
	}
}

func TestBuildRefusesInvalidBuildArguments(t *testing.T) {
	cases := []struct {
		name  string
		build []*agentv1.BuildArg
	}{
		{name: "invalid key", build: []*agentv1.BuildArg{{Key: "BAD-KEY", Value: "x"}}},
		{name: "empty key", build: []*agentv1.BuildArg{{Key: "", Value: "x"}}},
		{name: "duplicate", build: []*agentv1.BuildArg{{Key: "TOKEN", Value: "a"}, {Key: "TOKEN", Value: "b"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			builder, _ := newTestBuilder(t, runner, MinFreeBytes+1)
			prepareWorkspace(t, builder, "shop")
			app := appBuild(func(app *agentv1.AppBuild) { app.BuildArgs = tc.build })

			if _, err := builder.Build(context.Background(), app); err == nil {
				t.Fatal("Build accepted invalid build arguments")
			}
			if runner.count() != 0 {
				t.Errorf("docker ran %d times, want 0", runner.count())
			}
		})
	}
}

func TestBuildRefusesInvalidNameAndTag(t *testing.T) {
	cases := []struct {
		name string
		app  *agentv1.AppBuild
	}{
		{name: "name", app: appBuild(func(app *agentv1.AppBuild) { app.Name = "../shop" })},
		{name: "name case", app: appBuild(func(app *agentv1.AppBuild) { app.Name = "Shop" })},
		{name: "empty tag", app: appBuild(func(app *agentv1.AppBuild) { app.Tag = "" })},
		{name: "short tag", app: appBuild(func(app *agentv1.AppBuild) { app.Tag = "abcdef" })},
		{name: "upper tag", app: appBuild(func(app *agentv1.AppBuild) { app.Tag = "ABCDEF123456" })},
		{name: "long tag", app: appBuild(func(app *agentv1.AppBuild) { app.Tag = "abcdef1234567" })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			builder, _ := newTestBuilder(t, runner, MinFreeBytes+1)
			if _, err := builder.Build(context.Background(), tc.app); err == nil {
				t.Fatal("Build accepted an invalid name or tag")
			}
			if runner.count() != 0 {
				t.Errorf("docker ran %d times, want 0", runner.count())
			}
		})
	}
}

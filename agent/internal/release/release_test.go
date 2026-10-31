package release

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeStack struct {
	compose string
	env     string
	dir     string
	prefix  []string
	writes  []string
}

func newFakeStack(compose string) *fakeStack {
	return &fakeStack{
		compose: compose,
		dir:     "/opt/talaia/stacks/shop",
		prefix:  []string{"compose", "--project-name", "shop"},
	}
}

func (f *fakeStack) Read(context.Context, string) (string, error) {
	data, err := json.Marshal(stackContents{Compose: f.compose, Env: f.env})
	return string(data), err
}

func (f *fakeStack) Write(_ context.Context, _ string, compose, env string) (string, error) {
	f.writes = append(f.writes, compose)
	f.compose = compose
	f.env = env
	return "stack saved", nil
}

func (f *fakeStack) ComposeCommand(string) (string, []string, error) {
	return f.dir, append([]string(nil), f.prefix...), nil
}

type fakeRunner struct {
	calls []runnerCall
	run   func([]string) (string, error)
}

type runnerCall struct {
	dir  string
	args []string
}

func (f *fakeRunner) Run(_ context.Context, dir string, args ...string) (string, error) {
	f.calls = append(f.calls, runnerCall{dir: dir, args: append([]string(nil), args...)})
	if f.run != nil {
		return f.run(args)
	}
	return "", nil
}

func (f *fakeRunner) has(want []string) bool {
	for _, call := range f.calls {
		if contains(call.args, want) {
			return true
		}
	}
	return false
}

func contains(args []string, want []string) bool {
	if len(want) == 0 || len(args) < len(want) {
		return false
	}
	for index := 0; index+len(want) <= len(args); index++ {
		if strings.Join(args[index:index+len(want)], "\x00") == strings.Join(want, "\x00") {
			return true
		}
	}
	return false
}

type fakeDialer struct {
	addresses []string
	err       error
}

func (f *fakeDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	f.addresses = append(f.addresses, address)
	if f.err != nil {
		return nil, f.err
	}
	return fakeConn{}, nil
}

type fakeConn struct{}

func (fakeConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (fakeConn) Write(data []byte) (int, error)   { return len(data), nil }
func (fakeConn) Close() error                     { return nil }
func (fakeConn) LocalAddr() net.Addr              { return fakeAddr{} }
func (fakeConn) RemoteAddr() net.Addr             { return fakeAddr{} }
func (fakeConn) SetDeadline(time.Time) error      { return nil }
func (fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (fakeConn) SetWriteDeadline(time.Time) error { return nil }

type fakeAddr struct{}

func (fakeAddr) Network() string { return "tcp" }
func (fakeAddr) String() string  { return "127.0.0.1:0" }

func compose(image string) string {
	return "services:\n  app:\n    image: " + image + "\n    ports:\n      - \"127.0.0.1:20000:3000\"\n"
}

func appRelease(overrides func(*agentv1.AppRelease)) *agentv1.AppRelease {
	app := &agentv1.AppRelease{
		Name:         "shop",
		Tag:          "abcdef123456",
		Service:      "app",
		ZeroDowntime: true,
		Port:         3000,
		KeepTags:     []string{"keep1"},
	}
	if overrides != nil {
		overrides(app)
	}
	return app
}

func TestReleaseWithoutDowntimeReplacesTheOldContainer(t *testing.T) {
	stacks := newFakeStack(compose("talaia/shop:oldtag1234"))
	psCalls := 0
	runner := &fakeRunner{run: func(args []string) (string, error) {
		switch {
		case contains(args, []string{"config", "--format", "json"}):
			return `{"services":{"app":{}}}`, nil
		case contains(args, []string{"ps", "-q", "app"}):
			psCalls++
			if psCalls == 1 {
				return "oldcontainer\n", nil
			}
			return "oldcontainer\nnewcontainer\n", nil
		case contains(args, []string{"inspect", "--format", "{{json .State}}", "newcontainer"}):
			return `{"Status":"running","Running":true,"Health":{"Status":"healthy"}}`, nil
		case contains(args, []string{"rm", "-f", "oldcontainer"}):
			return "", nil
		case args[0] == "image" && args[1] == "ls":
			return "talaia/shop:abcdef123456\ntalaia/shop:keep1\ntalaia/shop:old\ntalaia/other:x\nnginx:latest\n", nil
		case args[0] == "image" && args[1] == "rm":
			return "", nil
		default:
			return "", nil
		}
	}}
	manager := New(Options{Stacks: stacks, Runner: runner})

	_, err := manager.Release(context.Background(), appRelease(nil))
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if !strings.Contains(stacks.writes[0], "image: talaia/shop:abcdef123456") {
		t.Errorf("compose was not updated:\n%s", stacks.writes[0])
	}
	if len(stacks.writes) != 1 {
		t.Errorf("writes = %d, want 1", len(stacks.writes))
	}
	if !runner.has([]string{"compose", "--project-name", "shop", "up", "-d", "--no-recreate", "--scale", "app=2"}) {
		t.Error("the service was not scaled to two")
	}
	if !runner.has([]string{"inspect", "--format", "{{json .State}}", "newcontainer"}) {
		t.Error("the new container was not inspected")
	}
	if !runner.has([]string{"rm", "-f", "oldcontainer"}) {
		t.Error("the old container was not removed")
	}
	if !runner.has([]string{"compose", "--project-name", "shop", "up", "-d", "--no-recreate", "--scale", "app=1"}) {
		t.Error("the service was not scaled back to one")
	}
	if !runner.has([]string{"image", "rm", "talaia/shop:old"}) {
		t.Error("the old image was not removed")
	}
	for _, ref := range []string{"talaia/shop:abcdef123456", "talaia/shop:keep1", "talaia/other:x", "nginx:latest"} {
		if runner.has([]string{"image", "rm", ref}) {
			t.Errorf("image %s was removed", ref)
		}
	}
}

func TestReleaseKeepsTheOldContainerWhenTheNewOneIsUnhealthy(t *testing.T) {
	stacks := newFakeStack(compose("talaia/shop:oldtag1234"))
	psCalls := 0
	runner := &fakeRunner{run: func(args []string) (string, error) {
		switch {
		case contains(args, []string{"config", "--format", "json"}):
			return `{"services":{"app":{}}}`, nil
		case contains(args, []string{"ps", "-q", "app"}):
			psCalls++
			if psCalls == 1 {
				return "oldcontainer\n", nil
			}
			return "oldcontainer\nnewcontainer\n", nil
		case contains(args, []string{"inspect", "--format", "{{json .State}}", "newcontainer"}):
			return `{"Status":"running","Running":true,"Health":{"Status":"unhealthy"}}`, nil
		}
		return "", nil
	}}
	manager := New(Options{Stacks: stacks, Runner: runner})

	_, err := manager.Release(context.Background(), appRelease(nil))
	if err == nil {
		t.Fatal("Release succeeded with an unhealthy container")
	}
	if !runner.has([]string{"rm", "-f", "newcontainer"}) {
		t.Error("the unhealthy container was not removed")
	}
	if runner.has([]string{"rm", "-f", "oldcontainer"}) {
		t.Error("the old container was removed")
	}
	if !strings.Contains(stacks.writes[len(stacks.writes)-1], "image: talaia/shop:oldtag1234") {
		t.Errorf("the compose file was not restored:\n%s", stacks.writes[len(stacks.writes)-1])
	}
	if runner.has([]string{"image", "rm", "talaia/shop:old"}) {
		t.Error("images were cleaned after a failed release")
	}
}

func TestReleaseFallsBackToPlainUpWhenZeroDowntimeIsUnsafe(t *testing.T) {
	cases := []struct {
		name   string
		config string
		app    *agentv1.AppRelease
	}{
		{name: "not asked", config: `{"services":{"app":{}}}`, app: appRelease(func(app *agentv1.AppRelease) { app.ZeroDowntime = false })},
		{name: "two services", config: `{"services":{"app":{},"db":{}}}`, app: appRelease(nil)},
		{name: "volumes", config: `{"services":{"app":{"volumes":[{"type":"volume","source":"data","target":"/data"}]}}}`, app: appRelease(nil)},
		{name: "container name", config: `{"services":{"app":{"container_name":"shop"}}}`, app: appRelease(nil)},
		{name: "published port", config: `{"services":{"app":{"ports":[{"host_ip":"127.0.0.1","target":8080,"published":"20001"}]}}}`, app: appRelease(nil)},
		{name: "replicas", config: `{"services":{"app":{"deploy":{"replicas":2}}}}`, app: appRelease(nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stacks := newFakeStack(compose("talaia/shop:oldtag1234"))
			runner := &fakeRunner{run: func(args []string) (string, error) {
				if contains(args, []string{"config", "--format", "json"}) {
					return tc.config, nil
				}
				return "", nil
			}}
			manager := New(Options{Stacks: stacks, Runner: runner})

			if _, err := manager.Release(context.Background(), tc.app); err != nil {
				t.Fatalf("Release: %v", err)
			}
			if !runner.has([]string{"compose", "--project-name", "shop", "up", "-d"}) {
				t.Error("the app was not started with plain up -d")
			}
			if runner.has([]string{"ps", "-q", "app"}) {
				t.Error("the release inspected containers instead of using plain up")
			}
		})
	}
}

func TestReleaseWaitsForATCPHealthCheck(t *testing.T) {
	stacks := newFakeStack(compose("talaia/shop:oldtag1234"))
	psCalls := 0
	runner := &fakeRunner{run: func(args []string) (string, error) {
		switch {
		case contains(args, []string{"config", "--format", "json"}):
			return `{"services":{"app":{}}}`, nil
		case contains(args, []string{"ps", "-q", "app"}):
			psCalls++
			if psCalls == 1 {
				return "oldcontainer\n", nil
			}
			return "oldcontainer\nnewcontainer\n", nil
		case contains(args, []string{"inspect", "--format", "{{json .State}}", "newcontainer"}):
			return `{"Status":"running","Running":true}`, nil
		case contains(args, []string{"inspect", "--format", "{{json .NetworkSettings.Networks}}", "newcontainer"}):
			return `{"talaia-proxy":{"IPAddress":"172.18.0.5"}}`, nil
		case args[0] == "image" && args[1] == "ls":
			return "", nil
		}
		return "", nil
	}}
	dialer := &fakeDialer{}
	manager := New(Options{Stacks: stacks, Runner: runner, Dialer: dialer})

	if _, err := manager.Release(context.Background(), appRelease(nil)); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if len(dialer.addresses) == 0 || dialer.addresses[0] != "172.18.0.5:3000" {
		t.Errorf("dialed %v, want 172.18.0.5:3000", dialer.addresses)
	}
}

func TestReleaseTimesOutAnUnhealthyNewContainer(t *testing.T) {
	stacks := newFakeStack(compose("talaia/shop:oldtag1234"))
	psCalls := 0
	runner := &fakeRunner{run: func(args []string) (string, error) {
		switch {
		case contains(args, []string{"config", "--format", "json"}):
			return `{"services":{"app":{}}}`, nil
		case contains(args, []string{"ps", "-q", "app"}):
			psCalls++
			if psCalls == 1 {
				return "oldcontainer\n", nil
			}
			return "oldcontainer\nnewcontainer\n", nil
		case contains(args, []string{"inspect", "--format", "{{json .State}}", "newcontainer"}):
			return `{"Status":"running","Running":true,"Health":{"Status":"starting"}}`, nil
		}
		return "", nil
	}}
	manager := New(Options{
		Stacks:        stacks,
		Runner:        runner,
		HealthTimeout: 60 * time.Millisecond,
		PollInterval:  10 * time.Millisecond,
	})

	_, err := manager.Release(context.Background(), appRelease(nil))
	if err == nil {
		t.Fatal("Release succeeded while the container was only starting")
	}
	if !runner.has([]string{"rm", "-f", "newcontainer"}) {
		t.Error("the new container was not removed after the timeout")
	}
	if runner.has([]string{"rm", "-f", "oldcontainer"}) {
		t.Error("the old container was removed after the timeout")
	}
}

func TestReleaseRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name string
		app  *agentv1.AppRelease
	}{
		{name: "name", app: appRelease(func(app *agentv1.AppRelease) { app.Name = "../shop" })},
		{name: "tag", app: appRelease(func(app *agentv1.AppRelease) { app.Tag = "short" })},
		{name: "service", app: appRelease(func(app *agentv1.AppRelease) { app.Service = "app;rm" })},
		{name: "port", app: appRelease(func(app *agentv1.AppRelease) { app.Port = 70000 })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			manager := New(Options{Stacks: newFakeStack(compose("talaia/shop:oldtag1234")), Runner: runner})
			if _, err := manager.Release(context.Background(), tc.app); err == nil {
				t.Fatal("Release accepted invalid input")
			}
			if len(runner.calls) != 0 {
				t.Errorf("docker ran: %v", runner.calls)
			}
		})
	}
}

func TestSetServiceImageChangesOnlyTheImage(t *testing.T) {
	input := "services:\n  app:\n    image: old\n    environment:\n      A: b\n  db:\n    image: postgres\n"
	want := "services:\n  app:\n    image: talaia/shop:abcdef123456\n    environment:\n      A: b\n  db:\n    image: postgres\n"
	got, err := setServiceImage(input, "app", "talaia/shop:abcdef123456")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("compose = %q, want %q", got, want)
	}
}

func TestSetServiceImageAddsAMissingImage(t *testing.T) {
	input := "services:\n  app:\n    build: .\n"
	got, err := setServiceImage(input, "app", "talaia/shop:abcdef123456")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "    image: talaia/shop:abcdef123456\n") || !strings.Contains(got, "    build: .\n") {
		t.Errorf("compose = %q", got)
	}
}

func TestSetServiceImageKeepsCRLF(t *testing.T) {
	input := "services:\r\n  app:\r\n    image: old\r\n"
	got, err := setServiceImage(input, "app", "talaia/shop:abcdef123456")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "\r\n") || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("line endings changed: %q", got)
	}
}

func TestSetServiceImageIgnoresANestedImageKey(t *testing.T) {
	input := "services:\n  app:\n    environment:\n      image: nested\n    image: old\n"
	got, err := setServiceImage(input, "app", "talaia/shop:abcdef123456")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "      image: nested\n") || !strings.Contains(got, "    image: talaia/shop:abcdef123456\n") {
		t.Errorf("compose = %q", got)
	}
}

func TestSetServiceImageRefusesAnUnknownService(t *testing.T) {
	if _, err := setServiceImage("services:\n  app:\n    image: old\n", "missing", "talaia/shop:abcdef123456"); err == nil {
		t.Fatal("setServiceImage accepted an unknown service")
	}
}

func TestReleaseReportsARunnerFailure(t *testing.T) {
	stacks := newFakeStack(compose("talaia/shop:oldtag1234"))
	runner := &fakeRunner{run: func(args []string) (string, error) {
		if contains(args, []string{"up", "-d"}) {
			return "boom", errors.New("docker failed")
		}
		return "", nil
	}}
	manager := New(Options{Stacks: stacks, Runner: runner})
	if _, err := manager.Release(context.Background(), appRelease(func(app *agentv1.AppRelease) { app.ZeroDowntime = false })); err == nil {
		t.Fatal("Release hid the docker failure")
	}
	if !strings.Contains(stacks.writes[len(stacks.writes)-1], "image: talaia/shop:oldtag1234") {
		t.Error("the compose file was not restored after the failure")
	}
}

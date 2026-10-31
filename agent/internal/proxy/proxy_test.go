package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeStacks struct {
	mu      sync.Mutex
	compose string
	writes  []string
	actions []agentv1.StackActionType
	err     error
}

func (f *fakeStacks) Write(_ context.Context, name, compose, env string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, name+"\x00"+compose+"\x00"+env)
	f.compose = compose
	return "stack saved", f.err
}

func (f *fakeStacks) Do(_ context.Context, _ string, action agentv1.StackActionType) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	return "done", f.err
}

type fakePorts struct {
	busy map[uint16]bool
}

func (f fakePorts) Free(port uint16) bool { return !f.busy[port] }

type fakeAdmin struct {
	mu      sync.Mutex
	configs [][]byte
	err     error
}

func (f *fakeAdmin) Load(_ context.Context, config []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configs = append(f.configs, append([]byte(nil), config...))
	return f.err
}

func newTestManager(t *testing.T, ports PortChecker, stacks StackManager, admin Admin) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	return New(Options{
		Root:   root,
		Stacks: stacks,
		Ports:  ports,
		Admin:  admin,
	}), root
}

func TestInstallRefusesWhen80Or443IsBusy(t *testing.T) {
	for _, busy := range []uint16{80, 443} {
		t.Run(strconv.Itoa(int(busy)), func(t *testing.T) {
			stacks := &fakeStacks{}
			manager, _ := newTestManager(t, fakePorts{busy: map[uint16]bool{busy: true}}, stacks, &fakeAdmin{})

			if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL); err == nil {
				t.Fatal("Install accepted a busy port")
			}
			if len(stacks.writes) != 0 || len(stacks.actions) != 0 {
				t.Errorf("the proxy stack was touched: writes=%d actions=%v", len(stacks.writes), stacks.actions)
			}
			if got := manager.Status(context.Background()).GetMode(); got != agentv1.ProxyMode_PROXY_MODE_EXTERNAL {
				t.Errorf("mode = %v, want external", got)
			}
		})
	}
}

func TestInstallWritesAPinnedCaddyStack(t *testing.T) {
	stacks := &fakeStacks{}
	admin := &fakeAdmin{}
	manager, root := newTestManager(t, fakePorts{}, stacks, admin)

	output, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if output != "done" {
		t.Errorf("output = %q", output)
	}
	if len(stacks.writes) != 1 || len(stacks.actions) != 1 || stacks.actions[0] != agentv1.StackActionType_STACK_ACTION_TYPE_UP {
		t.Fatalf("stack calls: writes=%d actions=%v", len(stacks.writes), stacks.actions)
	}
	compose := stacks.compose
	for _, want := range []string{
		"image: " + CaddyImage,
		"caddy:2.10.2@sha256:",
		`"80:80"`,
		`"443:443"`,
		`command: ["run", "--config", "/run/caddy/bootstrap.json", "--adapter", "json"]`,
		StackName,
		strconv.Quote(root) + ":/run/caddy",
		strconv.Quote(filepath.Join(root, "data")) + ":/data",
		strconv.Quote(filepath.Join(root, "config")) + ":/config",
		"XDG_DATA_HOME: /data",
		bootstrapFile,
	} {
		if !strings.Contains(compose, want) {
			t.Errorf("compose does not contain %q:\n%s", want, compose)
		}
	}
	for _, forbidden := range []string{"apache", "nginx", "httpd"} {
		if strings.Contains(strings.ToLower(compose), forbidden) {
			t.Errorf("compose mentions %q", forbidden)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, bootstrapFile))
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !strings.Contains(string(data), adminListen) {
		t.Errorf("bootstrap = %q", data)
	}
	if mode := manager.Status(context.Background()).GetMode(); mode != agentv1.ProxyMode_PROXY_MODE_MANAGED {
		t.Errorf("mode = %v, want managed", mode)
	}
	if version := manager.Status(context.Background()).GetCaddyVersion(); version != CaddyVersion {
		t.Errorf("Caddy version = %q, want %q", version, CaddyVersion)
	}

	if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if len(stacks.writes) != 1 || len(stacks.actions) != 1 {
		t.Errorf("a second install touched the stack: writes=%d actions=%v", len(stacks.writes), stacks.actions)
	}
}

func TestApplyBuildsAndLoadsTheCaddyConfiguration(t *testing.T) {
	stacks := &fakeStacks{}
	admin := &fakeAdmin{}
	manager, _ := newTestManager(t, fakePorts{}, stacks, admin)
	if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL); err != nil {
		t.Fatalf("Install: %v", err)
	}

	output, err := manager.Apply(context.Background(), []*agentv1.ProxyRoute{
		{Hostname: "app.example.com", UpstreamService: "app", UpstreamPort: 3000},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if output != "proxy configuration applied" {
		t.Errorf("output = %q", output)
	}
	if len(admin.configs) != 1 {
		t.Fatalf("admin calls = %d, want 1", len(admin.configs))
	}
	var config caddyConfig
	if err := json.Unmarshal(admin.configs[0], &config); err != nil {
		t.Fatalf("decode the loaded config: %v", err)
	}
	server := config.Apps.HTTP.Servers["talaia"]
	if got := server.Routes[0].Match[0].Host[0]; got != "app.example.com" {
		t.Errorf("host = %q", got)
	}
	if got := server.Routes[0].Handle[0].Upstreams[0].Dial; got != "app:3000" {
		t.Errorf("upstream = %q", got)
	}
	data, err := os.ReadFile(filepath.Join(manager.root, bootstrapFile))
	if err != nil {
		t.Fatalf("saved configuration: %v", err)
	}
	if !strings.Contains(string(data), "app.example.com") {
		t.Errorf("saved configuration = %q", data)
	}
}

func TestApplyNeedsTheManagedMode(t *testing.T) {
	admin := &fakeAdmin{}
	manager, _ := newTestManager(t, fakePorts{}, &fakeStacks{}, admin)
	if _, err := manager.Apply(context.Background(), nil); err == nil {
		t.Fatal("Apply accepted a configuration in external mode")
	}
	if len(admin.configs) != 0 {
		t.Errorf("admin calls = %d, want 0", len(admin.configs))
	}
}

func TestApplyRefusesInvalidRoutesBeforeCallingCaddy(t *testing.T) {
	admin := &fakeAdmin{}
	manager, _ := newTestManager(t, fakePorts{}, &fakeStacks{}, admin)
	if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), []*agentv1.ProxyRoute{{Hostname: "*.example.com", UpstreamService: "app", UpstreamPort: 3000}}); err == nil {
		t.Fatal("Apply accepted a wildcard")
	}
	if len(admin.configs) != 0 {
		t.Errorf("admin calls = %d, want 0", len(admin.configs))
	}
}

func TestApplyReportsACaddyFailure(t *testing.T) {
	admin := &fakeAdmin{err: errors.New("bad config")}
	manager, _ := newTestManager(t, fakePorts{}, &fakeStacks{}, admin)
	if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), nil); err == nil {
		t.Fatal("Apply hid a Caddy failure")
	}
}

func TestStatusReportsPortsAndMode(t *testing.T) {
	manager, _ := newTestManager(t, fakePorts{busy: map[uint16]bool{80: true}}, &fakeStacks{}, &fakeAdmin{})
	status := manager.Status(context.Background())
	if status.GetMode() != agentv1.ProxyMode_PROXY_MODE_EXTERNAL {
		t.Errorf("mode = %v", status.GetMode())
	}
	if status.GetPort_80Free() {
		t.Error("port 80 is reported free while it is busy")
	}
	if !status.GetPort_443Free() {
		t.Error("port 443 is reported busy while it is free")
	}
	if status.GetCaddyVersion() != "" {
		t.Errorf("Caddy version = %q, want empty", status.GetCaddyVersion())
	}
}

func TestManagedModeSurvivesANewManager(t *testing.T) {
	first, root := newTestManager(t, fakePorts{}, &fakeStacks{}, &fakeAdmin{})
	if _, err := first.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Apply(context.Background(), []*agentv1.ProxyRoute{
		{Hostname: "app.example.com", UpstreamService: "app", UpstreamPort: 3000},
	}); err != nil {
		t.Fatal(err)
	}
	second := New(Options{Root: root, Stacks: &fakeStacks{}, Ports: fakePorts{}, Admin: &fakeAdmin{}})
	status := second.Status(context.Background())
	if got := status.GetMode(); got != agentv1.ProxyMode_PROXY_MODE_MANAGED {
		t.Errorf("mode after restart = %v, want managed", got)
	}
	if len(status.GetCertificates()) != 1 || status.GetCertificates()[0].GetState() != "pending" {
		t.Errorf("certificates after restart = %v", status.GetCertificates())
	}
}

func TestRemoveDropsTheManagedProxy(t *testing.T) {
	stacks := &fakeStacks{}
	manager, root := newTestManager(t, fakePorts{}, stacks, &fakeAdmin{})
	if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_REMOVE); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(stacks.actions) != 2 || stacks.actions[1] != agentv1.StackActionType_STACK_ACTION_TYPE_REMOVE {
		t.Errorf("actions = %v", stacks.actions)
	}
	if _, err := os.Stat(filepath.Join(root, bootstrapFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("bootstrap still exists: %v", err)
	}
	status := manager.Status(context.Background())
	if status.GetMode() != agentv1.ProxyMode_PROXY_MODE_EXTERNAL || status.GetCaddyVersion() != "" {
		t.Errorf("status after remove = %v", status)
	}
	if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_REMOVE); err != nil {
		t.Fatalf("removing twice: %v", err)
	}
	if len(stacks.actions) != 2 {
		t.Errorf("a second remove touched the stack: %v", stacks.actions)
	}
}

func TestInstallRejectsAnUnspecifiedAction(t *testing.T) {
	manager, _ := newTestManager(t, fakePorts{}, &fakeStacks{}, &fakeAdmin{})
	if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_UNSPECIFIED); err == nil {
		t.Fatal("Install accepted an unspecified action")
	}
}

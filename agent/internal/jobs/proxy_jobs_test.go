package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeProxyManager struct {
	mu      sync.Mutex
	actions []agentv1.ProxyInstallAction
	routes  [][]*agentv1.ProxyRoute
	output  string
	err     error
}

func (f *fakeProxyManager) Install(_ context.Context, action agentv1.ProxyInstallAction) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	return f.output, f.err
}

func (f *fakeProxyManager) Apply(_ context.Context, routes []*agentv1.ProxyRoute) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routes = append(f.routes, routes)
	return f.output, f.err
}

func proxyInstallCommand(jobID string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_ProxyInstall{
			ProxyInstall: &agentv1.ProxyInstall{Action: agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL},
		},
	}
}

func proxyApplyCommand(jobID string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_ProxyApply{
			ProxyApply: &agentv1.ProxyApply{Routes: []*agentv1.ProxyRoute{
				{Hostname: "app.example.com", UpstreamService: "app", UpstreamPort: 3000},
			}},
		},
	}
}

func TestHandleInstallsTheProxy(t *testing.T) {
	manager := &fakeProxyManager{output: "installed"}
	out := newCollector()
	executor := New(&fakeActions{}).WithProxy(manager)

	executor.Handle(context.Background(), proxyInstallCommand("job-proxy-install"), out.emit)

	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || final.GetOutput() != "installed" {
		t.Errorf("final event = %v", final)
	}
	if len(manager.actions) != 1 || manager.actions[0] != agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL {
		t.Errorf("actions = %v", manager.actions)
	}
}

func TestHandleAppliesTheProxyConfiguration(t *testing.T) {
	manager := &fakeProxyManager{output: "applied"}
	out := newCollector()
	executor := New(&fakeActions{}).WithProxy(manager)

	executor.Handle(context.Background(), proxyApplyCommand("job-proxy-apply"), out.emit)

	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || final.GetOutput() != "applied" {
		t.Errorf("final event = %v", final)
	}
	if len(manager.routes) != 1 || len(manager.routes[0]) != 1 ||
		manager.routes[0][0].GetHostname() != "app.example.com" {
		t.Errorf("routes = %v", manager.routes)
	}
}

func TestHandleReportsProxyFailuresAndMissingManager(t *testing.T) {
	out := newCollector()
	executor := New(&fakeActions{}).WithProxy(&fakeProxyManager{err: errors.New("port 80 is busy")})
	executor.Handle(context.Background(), proxyInstallCommand("job-proxy-fail"), out.emit)
	out.next(t) // running
	if final := out.next(t); final.GetState() != agentv1.JobState_JOB_STATE_FAILED || final.GetMessage() != "port 80 is busy" {
		t.Errorf("final event = %v", final)
	}

	out = newCollector()
	New(&fakeActions{}).Handle(context.Background(), proxyApplyCommand("job-proxy-none"), out.emit)
	out.next(t) // running
	if final := out.next(t); final.GetState() != agentv1.JobState_JOB_STATE_FAILED || final.GetMessage() != errNoProxy.Error() {
		t.Errorf("final event = %v", final)
	}
}

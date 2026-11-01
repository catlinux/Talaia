package deploy

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestManagerProbePassesTheBranchAndKey(t *testing.T) {
	app := testApp()
	app.DeployKeyEnc = []byte("sealed")
	const commit = "abcdef1234567890abcdef1234567890abcdef12"
	agent := &fakeAgent{probeCommit: commit}
	manager, _ := newTestManager(t, app, agent)
	manager.box = fakeBox{values: map[string]string{"sealed": "private-key"}}

	start := time.Now()
	got, err := manager.Probe(context.Background(), app)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got != commit {
		t.Errorf("commit = %q", got)
	}
	if agent.probeCalls != 1 || agent.probeServer != app.ServerID {
		t.Errorf("probe calls = %d, server = %q", agent.probeCalls, agent.probeServer)
	}
	probe := agent.probeMsg
	if probe.GetRepoUrl() != app.RepoURL || probe.GetBranch() != app.Branch {
		t.Errorf("probe = %v", probe)
	}
	if probe.GetDeployKey() != "private-key" {
		t.Errorf("deploy key = %q", probe.GetDeployKey())
	}
	left := agent.probeDeadline.Sub(start)
	if left > 45*time.Second+100*time.Millisecond || left < 40*time.Second {
		t.Errorf("probe deadline is %s away, want about 45s", left)
	}
}

func TestManagerProbeSendsNoKeyForAPublicApp(t *testing.T) {
	app := testApp()
	agent := &fakeAgent{probeCommit: "abcdef1234567890abcdef1234567890abcdef12"}
	manager, _ := newTestManager(t, app, agent)

	if _, err := manager.Probe(context.Background(), app); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if agent.probeMsg.GetDeployKey() != "" {
		t.Errorf("deploy key = %q, want empty", agent.probeMsg.GetDeployKey())
	}
}

func TestManagerProbeRedactsTheKeyFromErrors(t *testing.T) {
	app := testApp()
	app.DeployKeyEnc = []byte("sealed")
	const key = "private-key-material"
	agent := &fakeAgent{probeErr: fmt.Errorf("the agent rejected %s", key)}
	manager, _ := newTestManager(t, app, agent)
	manager.box = fakeBox{values: map[string]string{"sealed": key}}

	_, err := manager.Probe(context.Background(), app)
	if err == nil {
		t.Fatal("Probe succeeded with a failing agent")
	}
	if strings.Contains(err.Error(), key) {
		t.Fatalf("error leaks the deploy key: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("error was not redacted: %v", err)
	}
}

package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const probeDeployKey = "private-deploy-key-material"

func probeCommand(jobID string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_GitProbe{
			GitProbe: &agentv1.GitProbe{
				RepoUrl:   "https://github.com/example/shop.git",
				Branch:    "main",
				DeployKey: probeDeployKey,
			},
		},
	}
}

func TestHandleProbesTheGitBranch(t *testing.T) {
	const commit = "abcdef1234567890abcdef1234567890abcdef12"
	fetcher := &fakeSourceFetcher{probeCommit: commit}
	out := newCollector()
	executor := New(&fakeActions{}).WithSource(fetcher)

	if !executor.Handle(context.Background(), probeCommand("job-probe"), out.emit) {
		t.Fatal("the git probe command was not handled")
	}
	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED {
		t.Fatalf("final event = %v", final)
	}
	if final.GetOutput() != `{"commit":"`+commit+`"}` {
		t.Errorf("output = %q", final.GetOutput())
	}
	if strings.Contains(final.GetOutput(), probeDeployKey) {
		t.Fatal("the output contains the deploy key")
	}

	fetcher.mu.Lock()
	probes := append([]*agentv1.GitProbe(nil), fetcher.probes...)
	fetcher.mu.Unlock()
	if len(probes) != 1 {
		t.Fatalf("probes = %v", probes)
	}
	probe := probes[0]
	if probe.GetRepoUrl() != "https://github.com/example/shop.git" ||
		probe.GetBranch() != "main" || probe.GetDeployKey() != probeDeployKey {
		t.Errorf("probe = %v", probe)
	}
}

func TestHandleReportsAProbeFailureWithoutTheDeployKey(t *testing.T) {
	fetcher := &fakeSourceFetcher{probeErr: errors.New("probe failed")}
	out := newCollector()
	executor := New(&fakeActions{}).WithSource(fetcher)

	executor.Handle(context.Background(), probeCommand("job-probe-fail"), out.emit)

	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_FAILED || final.GetMessage() != "probe failed" {
		t.Errorf("final event = %v", final)
	}
	if strings.Contains(final.GetMessage(), probeDeployKey) {
		t.Fatal("the failure message contains the deploy key")
	}
}

func TestHandleRefusesProbeWithoutASource(t *testing.T) {
	out := newCollector()
	New(&fakeActions{}).Handle(context.Background(), probeCommand("job-probe-no-source"), out.emit)

	out.next(t) // running
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_FAILED ||
		final.GetMessage() != errNoSource.Error() {
		t.Errorf("final event = %v", final)
	}
	if strings.Contains(final.GetMessage(), probeDeployKey) {
		t.Fatal("the failure message contains the deploy key")
	}
}

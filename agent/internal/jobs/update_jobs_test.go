package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// fakeUpdater records the update orders and answers with output and err.
type fakeUpdater struct {
	mu     sync.Mutex
	calls  []string
	output string
	err    error
}

func (f *fakeUpdater) Update(_ context.Context, version, sha256Hex string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, version+":"+sha256Hex)
	return f.output, f.err
}

func (f *fakeUpdater) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func updateCommand(jobID, version, sha256Hex string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_AgentUpdate{
			AgentUpdate: &agentv1.AgentUpdate{Version: version, Sha256: sha256Hex},
		},
	}
}

func TestHandleRunsAgentUpdates(t *testing.T) {
	const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	updater := &fakeUpdater{output: "update to 1.2.4 started; the agent will restart"}
	out := newCollector()
	executor := New(&fakeActions{}).WithUpdates(updater)

	if !executor.Handle(context.Background(), updateCommand("job-u1", "1.2.4", sha), out.emit) {
		t.Fatal("the update command was not handled")
	}

	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_RUNNING {
		t.Errorf("first event = %v", got)
	}
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || got.GetOutput() != updater.output {
		t.Errorf("final event = %v", got)
	}
	if calls := updater.recorded(); len(calls) != 1 || calls[0] != "1.2.4:"+sha {
		t.Errorf("calls = %v", calls)
	}
}

func TestHandleReportsUpdateFailures(t *testing.T) {
	out := newCollector()
	executor := New(&fakeActions{}).WithUpdates(&fakeUpdater{err: errors.New("the agent package does not match the SHA-256 of the order")})

	executor.Handle(context.Background(), updateCommand("job-u2", "1.2.4", strings.Repeat("b", 64)), out.emit)

	out.next(t) // running
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_FAILED ||
		got.GetMessage() != "the agent package does not match the SHA-256 of the order" {
		t.Errorf("final event = %v", got)
	}
}

func TestUpdateCommandsFailCleanlyWithoutAnUpdater(t *testing.T) {
	out := newCollector()

	New(&fakeActions{}).Handle(context.Background(), updateCommand("job-u3", "1.2.4", strings.Repeat("c", 64)), out.emit)

	out.next(t)
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_FAILED || got.GetMessage() != errNoUpdates.Error() {
		t.Errorf("final event = %v", got)
	}
}

func TestRepeatedUpdateJobDoesNotRunTwice(t *testing.T) {
	updater := &fakeUpdater{output: "started"}
	out := newCollector()
	executor := New(&fakeActions{}).WithUpdates(updater)
	msg := updateCommand("job-u4", "1.2.4", strings.Repeat("d", 64))

	executor.Handle(context.Background(), msg, out.emit)
	out.next(t) // running
	first := out.next(t)

	executor.Handle(context.Background(), msg, out.emit)
	again := out.next(t)

	if again.GetJobId() != first.GetJobId() || again.GetState() != first.GetState() {
		t.Errorf("repeated job answered %v, want the first outcome %v", again, first)
	}
	if calls := updater.recorded(); len(calls) != 1 {
		t.Errorf("the update ran %d times, want 1 (%v)", len(calls), calls)
	}
}

func TestUpdateCommandWithoutAJobIDIsDropped(t *testing.T) {
	executor := New(&fakeActions{})
	if !executor.Handle(context.Background(), updateCommand("", "1.2.4", fmt.Sprintf("%064d", 0)), func(*agentv1.AgentMsg) {
		t.Error("emitted")
	}) {
		t.Error("a command without a job id was not recognised")
	}
}

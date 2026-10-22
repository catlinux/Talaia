package jobs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeAptStatus struct {
	mu      sync.Mutex
	refresh []bool
	output  string
	err     error
}

func (f *fakeAptStatus) StatusJSON(_ context.Context, refresh bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refresh = append(f.refresh, refresh)
	return f.output, f.err
}

func (f *fakeAptStatus) calls() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.refresh...)
}

func aptStatusCommand(jobID string, refresh bool) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd:   &agentv1.CoreMsg_AptStatus{AptStatus: &agentv1.AptStatus{Refresh: refresh}},
	}
}

func TestHandleRunsAptStatus(t *testing.T) {
	const output = `{"packages":[],"held":[]}`
	reader := &fakeAptStatus{output: output}
	out := newCollector()
	executor := New(&fakeActions{}).WithApt(reader)

	if !executor.Handle(context.Background(), aptStatusCommand("job-apt-1", false), out.emit) {
		t.Fatal("the apt status command was not handled")
	}
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_RUNNING {
		t.Errorf("first event = %v", got)
	}
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || got.GetOutput() != output {
		t.Errorf("final event = %v", got)
	}
	if calls := reader.calls(); len(calls) != 1 || calls[0] {
		t.Errorf("refresh calls = %v, want [false]", calls)
	}
}

func TestHandlePassesTheRefreshFlag(t *testing.T) {
	reader := &fakeAptStatus{output: "{}"}
	out := newCollector()
	executor := New(&fakeActions{}).WithApt(reader)

	executor.Handle(context.Background(), aptStatusCommand("job-apt-2", true), out.emit)

	out.next(t)
	out.next(t)
	if calls := reader.calls(); len(calls) != 1 || !calls[0] {
		t.Errorf("refresh calls = %v, want [true]", calls)
	}
}

func TestHandleReportsAptStatusFailures(t *testing.T) {
	out := newCollector()
	executor := New(&fakeActions{}).WithApt(&fakeAptStatus{err: errors.New("apt-get update: lock held")})

	executor.Handle(context.Background(), aptStatusCommand("job-apt-3", true), out.emit)

	out.next(t)
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_FAILED ||
		!strings.Contains(got.GetMessage(), "lock held") {
		t.Errorf("final event = %v", got)
	}
}

func TestAptStatusFailsWithoutAReader(t *testing.T) {
	out := newCollector()

	New(&fakeActions{}).Handle(context.Background(), aptStatusCommand("job-apt-4", false), out.emit)

	out.next(t)
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_FAILED || got.GetMessage() != errNoApt.Error() {
		t.Errorf("final event = %v", got)
	}
}

func TestAptStatusFailsWhenTheJSONIsTooLarge(t *testing.T) {
	out := newCollector()
	executor := New(&fakeActions{}).WithApt(&fakeAptStatus{
		output: strings.Repeat("x", maxAptStatusBytes+1),
	})

	executor.Handle(context.Background(), aptStatusCommand("job-apt-5", false), out.emit)

	out.next(t)
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_FAILED || !strings.Contains(got.GetMessage(), "too large") {
		t.Errorf("final event = %v", got)
	}
}

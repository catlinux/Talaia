package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// fakeActions records the calls and answers with err.
type fakeActions struct {
	mu    sync.Mutex
	calls []string
	err   error
	logs  string
}

func (f *fakeActions) Logs(_ context.Context, container string, tail int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("logs:%s:%d", container, tail))
	return f.logs, f.err
}

func (f *fakeActions) Do(_ context.Context, container string, action agentv1.ContainerActionType) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, action.String()+":"+container)
	return f.err
}

func (f *fakeActions) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// collector gathers the emitted events.
type collector struct {
	events chan *agentv1.JobEvent
}

func newCollector() *collector { return &collector{events: make(chan *agentv1.JobEvent, 16)} }

func (c *collector) emit(msg *agentv1.AgentMsg) { c.events <- msg.GetJobEvent() }

func (c *collector) next(t *testing.T) *agentv1.JobEvent {
	t.Helper()
	select {
	case event := <-c.events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("no job event")
		return nil
	}
}

func command(jobID, container string, action agentv1.ContainerActionType) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_ContainerAction{
			ContainerAction: &agentv1.ContainerAction{Container: container, Action: action},
		},
	}
}

func TestHandleRunsTheActionAndReports(t *testing.T) {
	actions := &fakeActions{}
	out := newCollector()
	executor := New(actions)

	handled := executor.Handle(context.Background(), command("job-1", "web", agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_RESTART), out.emit)
	if !handled {
		t.Fatal("the command was not handled")
	}

	if got := out.next(t); got.GetJobId() != "job-1" || got.GetState() != agentv1.JobState_JOB_STATE_RUNNING {
		t.Errorf("first event = %v", got)
	}
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || got.GetMessage() != "" {
		t.Errorf("final event = %v", got)
	}
	if want := "CONTAINER_ACTION_TYPE_RESTART:web"; actions.count() != 1 || actions.calls[0] != want {
		t.Errorf("calls = %v, want [%s]", actions.calls, want)
	}
}

func TestHandleReportsFailures(t *testing.T) {
	out := newCollector()
	executor := New(&fakeActions{err: errors.New("docker: no such container")})

	executor.Handle(context.Background(), command("job-2", "gone", agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_START), out.emit)

	out.next(t) // running
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_FAILED || got.GetMessage() != "docker: no such container" {
		t.Errorf("final event = %v", got)
	}
}

func TestHandleRepeatedJobDoesNotRunTwice(t *testing.T) {
	actions := &fakeActions{}
	out := newCollector()
	executor := New(actions)
	msg := command("job-3", "web", agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_STOP)

	executor.Handle(context.Background(), msg, out.emit)
	out.next(t) // running
	first := out.next(t)

	executor.Handle(context.Background(), msg, out.emit)
	again := out.next(t)

	if again.GetState() != first.GetState() || again.GetJobId() != "job-3" {
		t.Errorf("repeated job answered %v, want the first outcome %v", again, first)
	}
	if actions.count() != 1 {
		t.Errorf("the action ran %d times, want 1", actions.count())
	}
}

func TestHandleRefusesInvalidContainerReferences(t *testing.T) {
	for _, ref := range []string{"", "../etc", "a b", "web;rm", "$(id)", "-rf", "web/../x"} {
		actions := &fakeActions{}
		out := newCollector()

		New(actions).Handle(context.Background(), command("job-"+ref, ref, agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_REMOVE), out.emit)

		out.next(t) // running
		got := out.next(t)
		if got.GetState() != agentv1.JobState_JOB_STATE_FAILED {
			t.Errorf("ref %q: state = %v, want failed", ref, got.GetState())
		}
		if actions.count() != 0 {
			t.Errorf("ref %q reached the actions", ref)
		}
	}
}

func TestHandleIgnoresOtherMessages(t *testing.T) {
	executor := New(&fakeActions{})
	ack := &agentv1.CoreMsg{Cmd: &agentv1.CoreMsg_HeartbeatAck{HeartbeatAck: &agentv1.HeartbeatAck{Seq: 1}}}

	if executor.Handle(context.Background(), ack, func(*agentv1.AgentMsg) { t.Error("emitted") }) {
		t.Error("a heartbeat ack was handled as a command")
	}
	// A command without a job id cannot be reported, so it is dropped.
	if !executor.Handle(context.Background(), command("", "web", agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_START), func(*agentv1.AgentMsg) { t.Error("emitted") }) {
		t.Error("a command without a job id was not recognised")
	}
}

func logsCommand(jobID, container string, tail uint32) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd:   &agentv1.CoreMsg_ReadLogs{ReadLogs: &agentv1.ReadLogs{Container: container, Tail: tail}},
	}
}

func TestHandleReadsLogsAndReturnsTheOutput(t *testing.T) {
	actions := &fakeActions{logs: "2026-10-20T10:00:00Z hello\n"}
	out := newCollector()
	executor := New(actions)

	if !executor.Handle(context.Background(), logsCommand("job-l1", "web", 50), out.emit) {
		t.Fatal("the logs command was not handled")
	}

	out.next(t) // running
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || got.GetOutput() != actions.logs {
		t.Errorf("final event = %v", got)
	}
	if want := "logs:web:50"; actions.count() != 1 || actions.calls[0] != want {
		t.Errorf("calls = %v, want [%s]", actions.calls, want)
	}
}

func TestHandleLogsOfAnInvalidReferenceFail(t *testing.T) {
	actions := &fakeActions{}
	out := newCollector()

	New(actions).Handle(context.Background(), logsCommand("job-l2", "../x", 10), out.emit)

	out.next(t) // running
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_FAILED {
		t.Errorf("state = %v, want failed", got.GetState())
	}
	if actions.count() != 0 {
		t.Error("an invalid reference reached the actions")
	}
}

func TestRepeatedLogsJobDoesNotKeepTheOutput(t *testing.T) {
	actions := &fakeActions{logs: "line\n"}
	out := newCollector()
	executor := New(actions)
	msg := logsCommand("job-l3", "web", 5)

	executor.Handle(context.Background(), msg, out.emit)
	out.next(t)
	out.next(t)
	executor.Handle(context.Background(), msg, out.emit)
	again := out.next(t)

	if again.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || again.GetOutput() != "" {
		t.Errorf("repeated job answered %v", again)
	}
	if actions.count() != 1 {
		t.Errorf("the logs were read %d times, want 1", actions.count())
	}
}

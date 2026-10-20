package jobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// fakeStacks records the stack calls and answers with output and err.
type fakeStacks struct {
	mu     sync.Mutex
	calls  []string
	output string
	err    error
}

func (f *fakeStacks) Write(_ context.Context, name, compose, env string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("write:%s:%s:%s", name, compose, env))
	return f.output, f.err
}

func (f *fakeStacks) Do(_ context.Context, name string, action agentv1.StackActionType) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf("do:%s:%s", name, action))
	return f.output, f.err
}

func (f *fakeStacks) Read(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "read:"+name)
	return f.output, f.err
}

func (f *fakeStacks) Adopt(_ context.Context, name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "adopt:"+name)
	return f.output, f.err
}

func (f *fakeStacks) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func stackWriteCommand(jobID string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd:   &agentv1.CoreMsg_StackWrite{StackWrite: &agentv1.StackWrite{Name: "shop", ComposeYaml: "services: {}", Env: "A=1"}},
	}
}

func stackActionCommand(jobID string, action agentv1.StackActionType) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd:   &agentv1.CoreMsg_StackAction{StackAction: &agentv1.StackAction{Name: "shop", Action: action}},
	}
}

func TestHandleRunsStackCommands(t *testing.T) {
	stacks := &fakeStacks{output: "done text"}
	executor := New(&fakeActions{}).WithStacks(stacks)

	messages := []*agentv1.CoreMsg{
		stackWriteCommand("j1"),
		stackActionCommand("j2", agentv1.StackActionType_STACK_ACTION_TYPE_UP),
		{JobId: "j3", Cmd: &agentv1.CoreMsg_StackRead{StackRead: &agentv1.StackRead{Name: "shop"}}},
	}
	for _, msg := range messages {
		out := newCollector()
		if !executor.Handle(context.Background(), msg, out.emit) {
			t.Fatalf("command %s was not handled", msg.GetJobId())
		}
		out.next(t) // running
		got := out.next(t)
		if got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || got.GetOutput() != "done text" {
			t.Errorf("%s final event = %v", msg.GetJobId(), got)
		}
	}

	want := []string{
		"write:shop:services: {}:A=1",
		"do:shop:STACK_ACTION_TYPE_UP",
		"read:shop",
	}
	got := stacks.recorded()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStackCommandFailuresCarryTheMessage(t *testing.T) {
	out := newCollector()
	executor := New(&fakeActions{}).WithStacks(&fakeStacks{err: errors.New("compose validation failed: bad yaml")})

	executor.Handle(context.Background(), stackWriteCommand("j4"), out.emit)

	out.next(t)
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_FAILED || got.GetMessage() != "compose validation failed: bad yaml" {
		t.Errorf("final event = %v", got)
	}
}

func TestStackCommandsFailCleanlyWithoutAStackManager(t *testing.T) {
	out := newCollector()

	New(&fakeActions{}).Handle(context.Background(), stackActionCommand("j5", agentv1.StackActionType_STACK_ACTION_TYPE_DOWN), out.emit)

	out.next(t)
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_FAILED || got.GetMessage() == "" {
		t.Errorf("final event = %v", got)
	}
}

func TestHandleRunsAdoption(t *testing.T) {
	stacks := &fakeStacks{output: "stack adopted"}
	out := newCollector()

	New(&fakeActions{}).WithStacks(stacks).Handle(context.Background(),
		&agentv1.CoreMsg{JobId: "j6", Cmd: &agentv1.CoreMsg_StackAdopt{StackAdopt: &agentv1.StackAdopt{Name: "wiki"}}}, out.emit)

	out.next(t)
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || got.GetOutput() != "stack adopted" {
		t.Errorf("final event = %v", got)
	}
	if calls := stacks.recorded(); len(calls) != 1 || calls[0] != "adopt:wiki" {
		t.Errorf("calls = %v", calls)
	}
}

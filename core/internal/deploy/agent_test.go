package deploy

import (
	"context"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeDispatcher struct {
	messages []*agentv1.CoreMsg
	err      error
}

func (f *fakeDispatcher) Dispatch(_ string, msg *agentv1.CoreMsg) error {
	f.messages = append(f.messages, msg)
	return f.err
}

type fakeJobs struct {
	calls    map[string]int
	outputs  map[string][]store.Job
	created  []store.Job
	updated  []store.Job
	lastKind string
}

func newFakeJobs() *fakeJobs {
	return &fakeJobs{calls: map[string]int{}, outputs: map[string][]store.Job{}}
}

func (f *fakeJobs) CreateJob(_ context.Context, serverID, kind string, _ map[string]any, _ string) (store.Job, error) {
	f.lastKind = kind
	job := store.Job{ID: "job-1", ServerID: serverID, Kind: kind, State: store.JobPending}
	f.created = append(f.created, job)
	return job, nil
}

func (f *fakeJobs) GetJob(_ context.Context, id string) (store.Job, error) {
	index := f.calls[id]
	f.calls[id]++
	sequence := f.outputs[id]
	if len(sequence) == 0 {
		return store.Job{ID: id, State: store.JobPending}, nil
	}
	if index >= len(sequence) {
		return sequence[len(sequence)-1], nil
	}
	return sequence[index], nil
}

func (f *fakeJobs) UpdateJobState(_ context.Context, id, serverID, state, message, output string) (bool, error) {
	f.updated = append(f.updated, store.Job{ID: id, ServerID: serverID, State: state, Message: message, Output: output})
	return true, nil
}

func TestHubAgentSourceAndStackNotFound(t *testing.T) {
	jobs := newFakeJobs()
	jobs.outputs["job-1"] = []store.Job{
		{ID: "job-1", State: store.JobRunning},
		{ID: "job-1", State: store.JobSucceeded, Output: `{"commit":"abcdef1234567890abcdef1234567890abcdef12","subject":"first"}`},
	}
	dispatcher := &fakeDispatcher{}
	agent := NewHubAgent(HubAgentOptions{Dispatcher: dispatcher, Jobs: jobs, PollInterval: time.Millisecond})

	result, err := agent.Source(context.Background(), "server-1", &agentv1.AppSource{Name: "shop", Branch: "main"})
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if result.Commit != "abcdef1234567890abcdef1234567890abcdef12" || result.Subject != "first" {
		t.Errorf("result = %+v", result)
	}
	if len(dispatcher.messages) != 1 || dispatcher.messages[0].GetAppSource() == nil {
		t.Errorf("messages = %v", dispatcher.messages)
	}

	jobs.outputs["job-1"] = []store.Job{{ID: "job-1", State: store.JobFailed, Message: "the stack does not exist"}}
	if _, _, err := agent.ReadStack(context.Background(), "server-1", "shop"); err != ErrStackNotFound {
		t.Fatalf("ReadStack error = %v, want ErrStackNotFound", err)
	}
}

func TestHubAgentReadsTheStack(t *testing.T) {
	jobs := newFakeJobs()
	jobs.outputs["job-1"] = []store.Job{{
		ID: "job-1", State: store.JobSucceeded, Output: `{"compose":"services: {}","env":"A=B"}`,
	}}
	agent := NewHubAgent(HubAgentOptions{Dispatcher: &fakeDispatcher{}, Jobs: jobs, PollInterval: time.Millisecond})
	compose, env, err := agent.ReadStack(context.Background(), "server-1", "shop")
	if err != nil {
		t.Fatal(err)
	}
	if compose != "services: {}" || env != "A=B" {
		t.Errorf("stack = %q / %q", compose, env)
	}
}

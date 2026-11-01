package deploy

import (
	"context"
	"errors"
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

func (f *fakeJobs) CreateJob(_ context.Context, serverID, kind string, _ map[string]any, createdBy, createdByID string) (store.Job, error) {
	f.lastKind = kind
	job := store.Job{
		ID: "job-1", ServerID: serverID, Kind: kind, State: store.JobPending,
		CreatedBy: createdBy, CreatedByID: createdByID,
	}
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
	if len(jobs.created) != 1 || jobs.created[0].CreatedBy != "deploy" || jobs.created[0].CreatedByID != "" {
		t.Errorf("system job = %+v", jobs.created)
	}

	jobs.outputs["job-1"] = []store.Job{{ID: "job-1", State: store.JobFailed, Message: "the stack does not exist"}}
	if _, _, err := agent.ReadStack(context.Background(), "server-1", "shop"); !errors.Is(err, ErrStackNotFound) {
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

func TestHubAgentPicksAPort(t *testing.T) {
	jobs := newFakeJobs()
	jobs.outputs["job-1"] = []store.Job{{ID: "job-1", State: store.JobSucceeded, Output: "23456"}}
	dispatcher := &fakeDispatcher{}
	agent := NewHubAgent(HubAgentOptions{Dispatcher: dispatcher, Jobs: jobs, PollInterval: time.Millisecond})
	port, err := agent.PickPort(context.Background(), "server-1", "shop")
	if err != nil {
		t.Fatal(err)
	}
	if port != 23456 {
		t.Errorf("port = %d", port)
	}
	if len(dispatcher.messages) != 1 || dispatcher.messages[0].GetAppPortPick().GetName() != "shop" {
		t.Errorf("messages = %v", dispatcher.messages)
	}
}

func TestHubAgentRemoveStackAndCleanUp(t *testing.T) {
	jobs := newFakeJobs()
	jobs.outputs["job-1"] = []store.Job{{ID: "job-1", State: store.JobSucceeded, Output: "removed"}}
	dispatcher := &fakeDispatcher{}
	agent := NewHubAgent(HubAgentOptions{Dispatcher: dispatcher, Jobs: jobs, PollInterval: time.Millisecond})

	output, err := agent.RemoveStack(context.Background(), "server-1", "shop")
	if err != nil {
		t.Fatal(err)
	}
	if output != "removed" || len(dispatcher.messages) != 1 {
		t.Fatalf("output = %q, messages = %v", output, dispatcher.messages)
	}
	action := dispatcher.messages[0].GetStackAction()
	if action.GetName() != "shop" || action.GetAction() != agentv1.StackActionType_STACK_ACTION_TYPE_REMOVE {
		t.Errorf("remove command = %v", action)
	}

	jobs.outputs["job-1"] = []store.Job{{ID: "job-1", State: store.JobSucceeded, Output: "cleaned"}}
	output, err = agent.CleanupApp(context.Background(), "server-1", "shop")
	if err != nil {
		t.Fatal(err)
	}
	if output != "cleaned" || dispatcher.messages[1].GetAppCleanup().GetName() != "shop" {
		t.Errorf("cleanup output = %q, messages = %v", output, dispatcher.messages)
	}
}

func TestHubAgentTreatsAMissingStackAsRemoved(t *testing.T) {
	jobs := newFakeJobs()
	jobs.outputs["job-1"] = []store.Job{{ID: "job-1", State: store.JobFailed, Message: "the stack does not exist"}}
	agent := NewHubAgent(HubAgentOptions{Dispatcher: &fakeDispatcher{}, Jobs: jobs, PollInterval: time.Millisecond})
	if _, err := agent.RemoveStack(context.Background(), "server-1", "shop"); !errors.Is(err, ErrStackNotFound) {
		t.Fatalf("RemoveStack error = %v, want ErrStackNotFound", err)
	}
}

func TestHubAgentAppliesProxyRoutes(t *testing.T) {
	jobs := newFakeJobs()
	jobs.outputs["job-1"] = []store.Job{{ID: "job-1", State: store.JobSucceeded, Output: "applied"}}
	dispatcher := &fakeDispatcher{}
	agent := NewHubAgent(HubAgentOptions{Dispatcher: dispatcher, Jobs: jobs, PollInterval: time.Millisecond})
	routes := []*agentv1.ProxyRoute{{Hostname: "app.example.com", UpstreamService: "app", UpstreamPort: 3000}}
	output, err := agent.ApplyProxyRoutes(context.Background(), "server-1", routes)
	if err != nil {
		t.Fatal(err)
	}
	if output != "applied" || len(dispatcher.messages) != 1 {
		t.Fatalf("output = %q, messages = %v", output, dispatcher.messages)
	}
	applied := dispatcher.messages[0].GetProxyApply().GetRoutes()
	if len(applied) != 1 || applied[0].GetHostname() != "app.example.com" {
		t.Errorf("routes = %v", applied)
	}
}

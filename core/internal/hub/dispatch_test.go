package hub

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func restartCommand(jobID string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_ContainerAction{ContainerAction: &agentv1.ContainerAction{
			Container: "web",
			Action:    agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_RESTART,
		}},
	}
}

// recordedEvent is one job event the hub handed to the sink.
type recordedEvent struct {
	serverID string
	event    *agentv1.JobEvent
}

type recordingEvents struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (r *recordingEvents) RecordJobEvent(_ context.Context, serverID string, event *agentv1.JobEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{serverID: serverID, event: event})
}

func (r *recordingEvents) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func TestDispatchSendsACommandToTheConnectedAgent(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))
	stream, _ := startStreamedAgent(t, h, "server-1", "aa")

	if err := h.Dispatch("server-1", restartCommand("job-1")); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	select {
	case msg := <-stream.out:
		if msg.GetJobId() != "job-1" || msg.GetContainerAction().GetContainer() != "web" {
			t.Errorf("the agent received %v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the agent did not receive the command")
	}
}

func TestDispatchToAnAgentThatIsNotConnected(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))

	if err := h.Dispatch("server-1", restartCommand("job-1")); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Dispatch = %v, want ErrNotConnected", err)
	}

	stream, done := startStreamedAgent(t, h, "server-1", "aa")
	stream.close()
	if err := <-done; err != nil {
		t.Fatalf("serve = %v after a clean close", err)
	}
	if err := h.Dispatch("server-1", restartCommand("job-2")); !errors.Is(err, ErrNotConnected) {
		t.Errorf("Dispatch after the agent left = %v, want ErrNotConnected", err)
	}
}

func TestDispatchRefusesWhenTheAgentHasTooManyPendingCommands(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))
	// A stream nobody reads from: the writer blocks on the first message and the
	// outbox fills up.
	stream := &blockedStream{in: make(chan *agentv1.AgentMsg, 8)}
	go func() { _ = h.serve(withConnection(context.Background(), "server-1", "aa"), stream) }()
	stream.in <- inventoryMsg("vps-web-01")
	waitFor(t, func() bool { return len(h.Connected()) == 1 })

	var full error
	for i := 0; i < outboxSize+5 && full == nil; i++ {
		full = h.Dispatch("server-1", restartCommand("job"))
	}
	if !errors.Is(full, ErrAgentBusy) {
		t.Errorf("Dispatch never reported ErrAgentBusy, last error = %v", full)
	}
	close(stream.in)
}

// blockedStream receives from in and never completes a Send.
type blockedStream struct {
	in chan *agentv1.AgentMsg
}

func (b *blockedStream) Receive() (*agentv1.AgentMsg, error) {
	msg, ok := <-b.in
	if !ok {
		return nil, errors.New("closed")
	}
	return msg, nil
}

func (b *blockedStream) Send(*agentv1.CoreMsg) error {
	select {}
}

func TestJobEventsReachTheSinkWithTheServerID(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))
	sink := &recordingEvents{}
	h.SetJobEvents(sink)
	stream, _ := startStreamedAgent(t, h, "server-1", "aa")

	stream.in <- &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_JobEvent{JobEvent: &agentv1.JobEvent{
		JobId: "job-1",
		State: agentv1.JobState_JOB_STATE_SUCCEEDED,
	}}}

	waitFor(t, func() bool { return sink.count() == 1 })
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if got := sink.events[0]; got.serverID != "server-1" || got.event.GetJobId() != "job-1" {
		t.Errorf("sink received %+v", got)
	}
}

func TestConnectedAgentsExposeTheirContainers(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))
	startStreamedAgent(t, h, "server-1", "aa")

	agents := h.Connected()
	if len(agents) != 1 || len(agents[0].ContainerList) != 3 {
		t.Fatalf("connected = %+v", agents)
	}
}

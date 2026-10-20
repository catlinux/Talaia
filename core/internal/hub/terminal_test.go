package hub

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// recordingTerminals notes what the hub passes on, with the server it came from.
type recordingTerminals struct {
	mu    sync.Mutex
	lines []string
}

func (r *recordingTerminals) add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line)
}

func (r *recordingTerminals) TerminalOpened(serverID string, msg *agentv1.TerminalOpened) {
	r.add("opened:" + serverID + ":" + msg.GetSessionId())
}

func (r *recordingTerminals) TerminalOutput(serverID string, msg *agentv1.TerminalOutput) {
	r.add("output:" + serverID + ":" + msg.GetSessionId() + ":" + string(msg.GetData()))
}

func (r *recordingTerminals) TerminalClosed(serverID string, msg *agentv1.TerminalClosed) {
	r.add("closed:" + serverID + ":" + msg.GetSessionId() + ":" + msg.GetReason())
}

func (r *recordingTerminals) AgentGone(serverID string) { r.add("gone:" + serverID) }

func (r *recordingTerminals) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

func TestTerminalMessagesReachTheSinkWithTheServerOfTheStream(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))
	sink := &recordingTerminals{}
	h.SetTerminals(sink)
	stream, done := startStreamedAgent(t, h, "server-1", "aa")

	// The session id is whatever the agent says; the server is the one that
	// authenticated the stream.
	stream.in <- &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_TerminalOpened{TerminalOpened: &agentv1.TerminalOpened{SessionId: "s1"}}}
	stream.in <- &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_TerminalOutput{TerminalOutput: &agentv1.TerminalOutput{SessionId: "s1", Data: []byte("hi")}}}
	stream.in <- &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_TerminalClosed{TerminalClosed: &agentv1.TerminalClosed{SessionId: "s1", Reason: "exited"}}}
	waitFor(t, func() bool { return len(sink.got()) == 3 })

	want := []string{"opened:server-1:s1", "output:server-1:s1:hi", "closed:server-1:s1:exited"}
	for i, line := range sink.got() {
		if line != want[i] {
			t.Errorf("line %d = %q, want %q", i, line, want[i])
		}
	}

	stream.close()
	if err := <-done; err != nil {
		t.Fatalf("serve = %v after a clean close", err)
	}
	waitFor(t, func() bool { return len(sink.got()) == 4 })
	if last := sink.got()[3]; last != "gone:server-1" {
		t.Errorf("last = %q, want the agent gone", last)
	}
}

func TestOldConnectionDoesNotReportTheAgentGone(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))
	sink := &recordingTerminals{}
	h.SetTerminals(sink)

	first, firstDone := startStreamedAgent(t, h, "server-1", "aa")
	// The agent reconnects before the core noticed the old stream was dead.
	second := newFakeStream()
	go func() { _ = h.serve(withConnection(context.Background(), "server-1", "aa"), second) }()
	second.in <- inventoryMsg("vps-web-01")
	waitFor(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return h.tokens == 2
	})
	time.Sleep(20 * time.Millisecond)

	first.close()
	if err := <-firstDone; err != nil {
		t.Fatalf("serve = %v after a clean close", err)
	}
	time.Sleep(50 * time.Millisecond)
	for _, line := range sink.got() {
		if line == "gone:server-1" {
			t.Error("the replaced connection reported the new one gone")
		}
	}
}

func TestDispatchWaitWaitsForRoomAndGivesUp(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))
	stream := &blockedStream{in: make(chan *agentv1.AgentMsg, 8)}
	go func() { _ = h.serve(withConnection(context.Background(), "server-1", "aa"), stream) }()
	stream.in <- inventoryMsg("vps-web-01")
	waitFor(t, func() bool { return len(h.Connected()) == 1 })

	// Fill the queue.
	for i := 0; i < outboxSize+2; i++ {
		_ = h.Dispatch("server-1", restartCommand("job"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := h.DispatchWait(ctx, "server-1", restartCommand("job")); !errors.Is(err, ErrAgentBusy) {
		t.Errorf("DispatchWait on a full queue = %v, want ErrAgentBusy", err)
	}
	if time.Since(start) < 25*time.Millisecond {
		t.Error("DispatchWait did not wait")
	}

	// Once the stream ends, a waiting caller is released at once.
	released := make(chan error, 1)
	go func() { released <- h.DispatchWait(context.Background(), "server-1", restartCommand("job")) }()
	time.Sleep(20 * time.Millisecond)
	close(stream.in)
	select {
	case err := <-released:
		if !errors.Is(err, ErrNotConnected) {
			t.Errorf("DispatchWait after the stream ended = %v, want ErrNotConnected", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DispatchWait stayed blocked after the connection ended")
	}
}

func TestDispatchWaitSendsWhenThereIsRoom(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))
	stream, _ := startStreamedAgent(t, h, "server-1", "aa")

	if err := h.DispatchWait(context.Background(), "server-1", restartCommand("job-1")); err != nil {
		t.Fatalf("DispatchWait: %v", err)
	}
	select {
	case msg := <-stream.out:
		if msg.GetJobId() != "job-1" {
			t.Errorf("received %v", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing arrived")
	}
	if err := h.DispatchWait(context.Background(), "nobody", restartCommand("x")); !errors.Is(err, ErrNotConnected) {
		t.Errorf("unknown server = %v", err)
	}
}

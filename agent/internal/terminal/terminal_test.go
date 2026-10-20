package terminal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	sessionA = "11111111-1111-4111-8111-111111111111"
	sessionB = "22222222-2222-4222-8222-222222222222"
)

// fakeProcess is a shell the test drives: it writes the output into output and
// reads what the session typed from written.
type fakeProcess struct {
	reader *io.PipeReader
	output *io.PipeWriter
	// stall makes Write wait until the process is closed, like a shell that does
	// not read.
	stall  bool
	closed chan struct{}
	once   sync.Once

	mu      sync.Mutex
	written bytes.Buffer
	sizes   [][2]uint32
	code    int
}

func newFakeProcess() *fakeProcess {
	reader, output := io.Pipe()
	return &fakeProcess{reader: reader, output: output, closed: make(chan struct{})}
}

func (p *fakeProcess) Read(b []byte) (int, error) { return p.reader.Read(b) }

func (p *fakeProcess) Write(b []byte) (int, error) {
	if p.stall {
		<-p.closed
		return 0, io.ErrClosedPipe
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.Write(b)
}

func (p *fakeProcess) Close() error {
	p.once.Do(func() { close(p.closed) })
	return p.output.Close()
}

func (p *fakeProcess) Resize(_ context.Context, cols, rows uint32) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sizes = append(p.sizes, [2]uint32{cols, rows})
	return nil
}

func (p *fakeProcess) ExitCode(context.Context) (int, error) { return p.code, nil }

func (p *fakeProcess) typed() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.written.String()
}

func (p *fakeProcess) resized() [][2]uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][2]uint32(nil), p.sizes...)
}

// fakeBackend hands out the process the test prepared.
type fakeBackend struct {
	process    *fakeProcess
	privileged bool
	err        error

	mu       sync.Mutex
	started  []string
	commands [][]string
}

func (b *fakeBackend) Start(_ context.Context, container string, shell []string, cols, rows uint32) (Process, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.started = append(b.started, container)
	b.commands = append(b.commands, shell)
	if b.err != nil {
		return nil, false, b.err
	}
	return b.process, b.privileged, nil
}

// collector gathers what the agent sends.
type collector struct{ messages chan *agentv1.AgentMsg }

func newCollector() *collector { return &collector{messages: make(chan *agentv1.AgentMsg, 256)} }

func (c *collector) emit(msg *agentv1.AgentMsg) { c.messages <- msg }

func (c *collector) next(t *testing.T) *agentv1.AgentMsg {
	t.Helper()
	select {
	case msg := <-c.messages:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("the agent sent nothing")
		return nil
	}
}

func (c *collector) none(t *testing.T) {
	t.Helper()
	select {
	case msg := <-c.messages:
		t.Fatalf("unexpected message %v", msg)
	case <-time.After(50 * time.Millisecond):
	}
}

// closedMessage reads messages until the session closes and returns that one.
func (c *collector) closedMessage(t *testing.T) *agentv1.TerminalClosed {
	t.Helper()
	for {
		if closed := c.next(t).GetTerminalClosed(); closed != nil {
			return closed
		}
	}
}

func testLimits() Limits {
	return Limits{MaxSessions: 2, Idle: time.Hour, MaxDuration: time.Hour, Chunk: 16, Tick: 10 * time.Millisecond}
}

func openCommand(id, container string, shell agentv1.TerminalShell) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{Cmd: &agentv1.CoreMsg_TerminalOpen{TerminalOpen: &agentv1.TerminalOpen{
		SessionId: id, Container: container, Shell: shell, Cols: 100, Rows: 30,
	}}}
}

func inputCommand(id string, data []byte) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{Cmd: &agentv1.CoreMsg_TerminalInput{TerminalInput: &agentv1.TerminalInput{SessionId: id, Data: data}}}
}

func closeCommand(id string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{Cmd: &agentv1.CoreMsg_TerminalClose{TerminalClose: &agentv1.TerminalClose{SessionId: id}}}
}

// openSession starts a session and waits until it runs.
func openSession(t *testing.T, m *Manager, out *collector, id string) *agentv1.TerminalOpened {
	t.Helper()
	if !m.Handle(context.Background(), openCommand(id, "web", agentv1.TerminalShell_TERMINAL_SHELL_SH), out.emit) {
		t.Fatal("the open command was not handled")
	}
	got := out.next(t).GetTerminalOpened()
	if got == nil || got.GetError() != "" {
		t.Fatalf("session did not open: %v", got)
	}
	return got
}

func TestSessionCarriesInputAndOutput(t *testing.T) {
	process := newFakeProcess()
	backend := &fakeBackend{process: process, privileged: true}
	m := New(backend, testLimits())
	out := newCollector()

	if got := openSession(t, m, out, sessionA); !got.GetPrivileged() || got.GetSessionId() != sessionA {
		t.Errorf("opened = %v", got)
	}
	if backend.commands[0][0] != "/bin/sh" || len(backend.commands[0]) != 1 {
		t.Errorf("command = %v, want only /bin/sh", backend.commands[0])
	}

	// Output longer than a chunk arrives in several messages, in order.
	go func() { _, _ = process.output.Write([]byte("0123456789abcdefghijklmnopqrstuvwxyz")) }()
	var got []byte
	for len(got) < 36 {
		message := out.next(t).GetTerminalOutput()
		if message == nil || message.GetSessionId() != sessionA || len(message.GetData()) > 16 {
			t.Fatalf("output = %v", message)
		}
		got = append(got, message.GetData()...)
	}
	if string(got) != "0123456789abcdefghijklmnopqrstuvwxyz" {
		t.Errorf("output = %q", got)
	}

	m.Handle(context.Background(), inputCommand(sessionA, []byte("ls\n")), out.emit)
	waitFor(t, func() bool { return process.typed() == "ls\n" })

	m.Handle(context.Background(), closeCommand(sessionA), out.emit)
	if closed := out.closedMessage(t); closed.GetReason() != ReasonClosed || closed.GetSessionId() != sessionA {
		t.Errorf("closed = %v", closed)
	}
	waitFor(t, func() bool { return m.Count() == 0 })
}

func TestSessionEndsWhenTheShellExits(t *testing.T) {
	process := newFakeProcess()
	process.code = 7
	m := New(&fakeBackend{process: process}, testLimits())
	out := newCollector()
	openSession(t, m, out, sessionA)

	_ = process.output.Close()
	if closed := out.closedMessage(t); closed.GetReason() != ReasonExited || closed.GetExitCode() != 7 {
		t.Errorf("closed = %v", closed)
	}
}

func TestOpenRefusals(t *testing.T) {
	m := New(&fakeBackend{process: newFakeProcess()}, testLimits())
	out := newCollector()
	ctx := context.Background()

	tests := []struct {
		name      string
		command   *agentv1.CoreMsg
		wantError string
	}{
		{"shell not in the list", openCommand(sessionA, "web", agentv1.TerminalShell_TERMINAL_SHELL_UNSPECIFIED), "shell"},
		{"container with a path", openCommand(sessionA, "../etc", agentv1.TerminalShell_TERMINAL_SHELL_SH), "container"},
		{"container with a space", openCommand(sessionA, "web app", agentv1.TerminalShell_TERMINAL_SHELL_BASH), "container"},
		{"huge size", &agentv1.CoreMsg{Cmd: &agentv1.CoreMsg_TerminalOpen{TerminalOpen: &agentv1.TerminalOpen{
			SessionId: sessionA, Container: "web", Shell: agentv1.TerminalShell_TERMINAL_SHELL_SH, Cols: 5000, Rows: 24,
		}}}, "size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m.Handle(ctx, tt.command, out.emit)
			got := out.next(t).GetTerminalOpened()
			if got == nil || !strings.Contains(got.GetError(), tt.wantError) {
				t.Errorf("answer = %v, want an error about %q", got, tt.wantError)
			}
			if m.Count() != 0 {
				t.Errorf("a refused session was kept")
			}
		})
	}

	// A session id that is not a UUID cannot be routed: nothing is answered.
	m.Handle(ctx, openCommand("not-an-id", "web", agentv1.TerminalShell_TERMINAL_SHELL_SH), out.emit)
	out.none(t)
}

func TestOpenRefusesDuplicatesAndTooManySessions(t *testing.T) {
	backend := &fakeBackend{process: newFakeProcess()}
	m := New(backend, Limits{MaxSessions: 1, Idle: time.Hour, MaxDuration: time.Hour, Chunk: 16, Tick: time.Second})
	out := newCollector()
	openSession(t, m, out, sessionA)

	m.Handle(context.Background(), openCommand(sessionA, "web", agentv1.TerminalShell_TERMINAL_SHELL_SH), out.emit)
	if got := out.next(t).GetTerminalOpened(); !strings.Contains(got.GetError(), "already") {
		t.Errorf("duplicate answer = %v", got)
	}
	m.Handle(context.Background(), openCommand(sessionB, "web", agentv1.TerminalShell_TERMINAL_SHELL_SH), out.emit)
	if got := out.next(t).GetTerminalOpened(); !strings.Contains(got.GetError(), "too many") {
		t.Errorf("limit answer = %v", got)
	}
	if len(backend.started) != 1 {
		t.Errorf("a refused session started a shell: %v", backend.started)
	}
	// The first session still works.
	m.Handle(context.Background(), closeCommand(sessionA), out.emit)
	out.closedMessage(t)
}

func TestFailedStartFreesTheSlot(t *testing.T) {
	backend := &fakeBackend{err: errors.New("the container is not running")}
	m := New(backend, testLimits())
	out := newCollector()

	m.Handle(context.Background(), openCommand(sessionA, "web", agentv1.TerminalShell_TERMINAL_SHELL_SH), out.emit)
	if got := out.next(t).GetTerminalOpened(); got.GetError() != "the container is not running" {
		t.Errorf("answer = %v", got)
	}
	waitFor(t, func() bool { return m.Count() == 0 })
}

func TestIdleAndMaxDuration(t *testing.T) {
	for name, limits := range map[string]Limits{
		ReasonIdle:        {MaxSessions: 1, Idle: 40 * time.Millisecond, MaxDuration: time.Hour, Chunk: 16, Tick: 10 * time.Millisecond},
		ReasonMaxDuration: {MaxSessions: 1, Idle: time.Hour, MaxDuration: 40 * time.Millisecond, Chunk: 16, Tick: 10 * time.Millisecond},
	} {
		t.Run(name, func(t *testing.T) {
			m := New(&fakeBackend{process: newFakeProcess()}, limits)
			out := newCollector()
			openSession(t, m, out, sessionA)
			if closed := out.closedMessage(t); closed.GetReason() != name {
				t.Errorf("reason = %q, want %q", closed.GetReason(), name)
			}
		})
	}
}

func TestActivityKeepsASessionFromBeingIdle(t *testing.T) {
	process := newFakeProcess()
	m := New(&fakeBackend{process: process}, Limits{MaxSessions: 1, Idle: 150 * time.Millisecond, MaxDuration: time.Hour, Chunk: 16, Tick: 10 * time.Millisecond})
	out := newCollector()
	openSession(t, m, out, sessionA)

	for range 6 {
		time.Sleep(50 * time.Millisecond)
		m.Handle(context.Background(), inputCommand(sessionA, []byte("x")), out.emit)
	}
	if m.Count() != 1 {
		t.Fatal("a session with input was closed as idle")
	}
	m.Handle(context.Background(), closeCommand(sessionA), out.emit)
	out.closedMessage(t)
}

func TestShellThatDoesNotReadIsClosedWithoutStallingTheAgent(t *testing.T) {
	process := newFakeProcess()
	process.stall = true
	m := New(&fakeBackend{process: process}, testLimits())
	out := newCollector()
	openSession(t, m, out, sessionA)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			m.Handle(context.Background(), inputCommand(sessionA, []byte("a")), out.emit)
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle blocked on a shell that does not read")
	}
	if closed := out.closedMessage(t); closed.GetReason() != ReasonSlowShell {
		t.Errorf("reason = %q", closed.GetReason())
	}
}

func TestInvalidInputAndResizeEndTheSession(t *testing.T) {
	for name, command := range map[string]*agentv1.CoreMsg{
		"input too large": inputCommand(sessionA, make([]byte, maxInputBytes+1)),
		"resize to zero":  {Cmd: &agentv1.CoreMsg_TerminalResize{TerminalResize: &agentv1.TerminalResize{SessionId: sessionA, Cols: 0, Rows: 10}}},
		"resize too big":  {Cmd: &agentv1.CoreMsg_TerminalResize{TerminalResize: &agentv1.TerminalResize{SessionId: sessionA, Cols: 10, Rows: 1001}}},
	} {
		t.Run(name, func(t *testing.T) {
			m := New(&fakeBackend{process: newFakeProcess()}, testLimits())
			out := newCollector()
			openSession(t, m, out, sessionA)
			m.Handle(context.Background(), command, out.emit)
			if closed := out.closedMessage(t); closed.GetReason() != ReasonProtocol {
				t.Errorf("reason = %q", closed.GetReason())
			}
		})
	}
}

func TestResizeReachesTheShell(t *testing.T) {
	process := newFakeProcess()
	m := New(&fakeBackend{process: process}, testLimits())
	out := newCollector()
	openSession(t, m, out, sessionA)

	m.Handle(context.Background(), &agentv1.CoreMsg{Cmd: &agentv1.CoreMsg_TerminalResize{
		TerminalResize: &agentv1.TerminalResize{SessionId: sessionA, Cols: 120, Rows: 40},
	}}, out.emit)
	waitFor(t, func() bool {
		sizes := process.resized()
		return len(sizes) == 1 && sizes[0] == [2]uint32{120, 40}
	})
}

func TestMessagesOfOtherSessionsAreIgnored(t *testing.T) {
	process := newFakeProcess()
	m := New(&fakeBackend{process: process}, testLimits())
	out := newCollector()
	openSession(t, m, out, sessionA)

	m.Handle(context.Background(), inputCommand(sessionB, []byte("rm -rf /\n")), out.emit)
	m.Handle(context.Background(), closeCommand(sessionB), out.emit)
	out.none(t)
	if process.typed() != "" || m.Count() != 1 {
		t.Errorf("a message for another session reached this one: %q, %d sessions", process.typed(), m.Count())
	}
}

func TestCloseAllEndsEverySession(t *testing.T) {
	m := New(&fakeBackend{process: newFakeProcess()}, testLimits())
	out := newCollector()
	openSession(t, m, out, sessionA)

	m.CloseAll()
	if closed := out.closedMessage(t); closed.GetReason() != ReasonDisconnected {
		t.Errorf("reason = %q", closed.GetReason())
	}
	waitFor(t, func() bool { return m.Count() == 0 })
}

func TestOutputIsRateLimited(t *testing.T) {
	process := newFakeProcess()
	m := New(&fakeBackend{process: process}, Limits{MaxSessions: 1, Idle: time.Hour, MaxDuration: time.Hour, Chunk: 16, BytesPerSecond: 160, Tick: time.Second})
	out := newCollector()
	openSession(t, m, out, sessionA)

	start := time.Now()
	go func() { _, _ = process.output.Write(bytes.Repeat([]byte("x"), 64)) }()
	total := 0
	for total < 64 {
		total += len(out.next(t).GetTerminalOutput().GetData())
	}
	// 64 bytes at 160 bytes a second take 0.4 s; the last chunk is not delayed.
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Errorf("64 bytes took %s, the rate limit did not apply", elapsed)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

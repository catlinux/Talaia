package terminal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const serverOne, serverTwo = "server-1", "server-2"

// fakeStore keeps the sessions and chunks in memory.
type fakeStore struct {
	mu       sync.Mutex
	next     int
	sessions map[string]*store.TerminalSession
	chunks   map[string][]store.TerminalChunk
	failAdd  bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{sessions: map[string]*store.TerminalSession{}, chunks: map[string][]store.TerminalChunk{}}
}

func (f *fakeStore) CreateTerminalSession(_ context.Context, serverID, container, shell, createdBy string) (store.TerminalSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", f.next)
	session := &store.TerminalSession{ID: id, ServerID: serverID, Container: container, Shell: shell, CreatedBy: createdBy}
	f.sessions[id] = session
	return *session, nil
}

func (f *fakeStore) SetTerminalPrivileged(_ context.Context, id string, privileged bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[id].Privileged = privileged
	return nil
}

func (f *fakeStore) AppendTerminalChunk(_ context.Context, id string, seq int, sealed []byte, plain int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAdd {
		return errors.New("database down")
	}
	f.chunks[id] = append(f.chunks[id], store.TerminalChunk{Seq: seq, Sealed: sealed})
	f.sessions[id].RecordedBytes += int64(plain)
	return nil
}

func (f *fakeStore) CloseTerminalSession(_ context.Context, id, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[id].ClosedAt = time.Now()
	f.sessions[id].CloseReason = reason
	return nil
}

func (f *fakeStore) GetTerminalSession(_ context.Context, id string) (store.TerminalSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.sessions[id]; ok {
		return *s, nil
	}
	return store.TerminalSession{}, store.ErrNotFound
}

func (f *fakeStore) TerminalChunks(_ context.Context, id string) ([]store.TerminalChunk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.TerminalChunk(nil), f.chunks[id]...), nil
}

func (f *fakeStore) row(id string) store.TerminalSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	return *f.sessions[id]
}

func (f *fakeStore) chunksOf(id string) []store.TerminalChunk {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.TerminalChunk(nil), f.chunks[id]...)
}

func (f *fakeStore) setChunks(id string, chunks []store.TerminalChunk) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chunks[id] = chunks
}

// fakeAgents records what is sent to each server and can be told to refuse.
type fakeAgents struct {
	mu    sync.Mutex
	sent  map[string][]*agentv1.CoreMsg
	err   error
	block bool // DispatchWait waits for its context, like a full queue
}

func (f *fakeAgents) DispatchWait(ctx context.Context, serverID string, msg *agentv1.CoreMsg) error {
	f.mu.Lock()
	block, err := f.block, f.err
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		return hub.ErrAgentBusy
	}
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sent == nil {
		f.sent = map[string][]*agentv1.CoreMsg{}
	}
	f.sent[serverID] = append(f.sent[serverID], msg)
	return nil
}

func (f *fakeAgents) messages(serverID string) []*agentv1.CoreMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*agentv1.CoreMsg(nil), f.sent[serverID]...)
}

func (f *fakeAgents) setBlock(block bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.block = block
}

type fakeAudit struct {
	mu     sync.Mutex
	events []store.AuditEvent
}

func (f *fakeAudit) AppendAudit(_ context.Context, event store.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
	return nil
}

func (f *fakeAudit) actions() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, e := range f.events {
		out = append(out, e.Action)
	}
	return strings.Join(out, ",")
}

type rig struct {
	m      *Manager
	st     *fakeStore
	agents *fakeAgents
	audit  *fakeAudit
	box    *auth.SecretBox
}

func newRig(t *testing.T, mutate func(*Config)) *rig {
	t.Helper()
	box, err := auth.NewSecretBox(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.OpenTimeout = 2 * time.Second
	cfg.FlushEvery = 10 * time.Millisecond
	cfg.Tick = 10 * time.Millisecond
	if mutate != nil {
		mutate(&cfg)
	}
	r := &rig{st: newFakeStore(), agents: &fakeAgents{}, audit: &fakeAudit{}, box: box}
	r.m = New(r.st, r.agents, r.audit, box, cfg)
	return r
}

func request(serverID, container string) OpenRequest {
	return OpenRequest{ServerID: serverID, Container: container, Shell: "sh", Actor: "admin", PanelSession: "panel-1", IP: "192.0.2.1", Cols: 100, Rows: 30}
}

// openedID returns the id of the last session the core asked a server to open.
func (r *rig) openedID(serverID string) string {
	id := ""
	for _, msg := range r.agents.messages(serverID) {
		if open := msg.GetTerminalOpen(); open != nil {
			id = open.GetSessionId()
		}
	}
	return id
}

// open starts a session and plays the agent: it answers once the open request
// has been sent.
func (r *rig) open(t *testing.T, req OpenRequest, privileged bool) *Session {
	t.Helper()
	type result struct {
		s   *Session
		err error
	}
	before := len(r.agents.messages(req.ServerID))
	done := make(chan result, 1)
	go func() {
		s, err := r.m.Open(context.Background(), req)
		done <- result{s, err}
	}()

	waitUntil(t, func() bool { return len(r.agents.messages(req.ServerID)) > before })
	r.m.TerminalOpened(req.ServerID, &agentv1.TerminalOpened{SessionId: r.openedID(req.ServerID), Privileged: privileged})
	got := <-done
	if got.err != nil {
		t.Fatalf("Open: %v", got.err)
	}
	return got.s
}

func waitUntil(t *testing.T, condition func() bool) {
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

func waitDone(t *testing.T, s *Session) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the session did not end")
	}
}

func TestSessionOpensCarriesOutputAndIsRecorded(t *testing.T) {
	r := newRig(t, nil)
	s := r.open(t, request(serverOne, "web"), true)
	if !s.Privileged() || s.Actor() != "admin" || s.PanelSession() != "panel-1" {
		t.Errorf("session = %+v", s)
	}

	want := "hello \xe2\x9c\x94 done\x1b[0m\n"
	r.m.TerminalOutput(serverOne, &agentv1.TerminalOutput{SessionId: s.ID(), Data: []byte("hello ")})
	r.m.TerminalOutput(serverOne, &agentv1.TerminalOutput{SessionId: s.ID(), Data: []byte("\xe2\x9c")}) // half a character
	r.m.TerminalOutput(serverOne, &agentv1.TerminalOutput{SessionId: s.ID(), Data: []byte("\x94 done\x1b[0m\n")})
	var seen []byte
	for len(seen) < len(want) {
		select {
		case chunk := <-s.Output():
			seen = append(seen, chunk...)
		case <-time.After(2 * time.Second):
			t.Fatal("the output did not arrive")
		}
	}

	if err := s.Input(context.Background(), []byte("ls\n")); err != nil {
		t.Fatalf("Input: %v", err)
	}
	if err := s.Resize(context.Background(), 120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	var kinds []string
	for _, msg := range r.agents.messages(serverOne) {
		switch {
		case msg.GetTerminalOpen() != nil:
			kinds = append(kinds, "open")
		case msg.GetTerminalInput() != nil:
			kinds = append(kinds, "input:"+string(msg.GetTerminalInput().GetData()))
		case msg.GetTerminalResize() != nil:
			kinds = append(kinds, "resize")
		}
	}
	if strings.Join(kinds, ",") != "open,input:ls\n,resize" {
		t.Errorf("sent to the agent: %q", kinds)
	}

	r.m.TerminalClosed(serverOne, &agentv1.TerminalClosed{SessionId: s.ID(), Reason: "exited", ExitCode: 4})
	waitDone(t, s)
	if s.Reason() != "exited" || s.ExitCode() != 4 {
		t.Errorf("ended with %q, %d", s.Reason(), s.ExitCode())
	}
	if _, open := <-s.Output(); open {
		t.Error("the output channel was not closed")
	}
	if row := r.st.row(s.ID()); row.CloseReason != "exited" || row.ClosedAt.IsZero() || !row.Privileged {
		t.Errorf("stored session = %+v", row)
	}
	if got := r.audit.actions(); got != "terminal_open,terminal_close" {
		t.Errorf("audit = %s", got)
	}

	// The recording is an asciicast file whose characters are whole.
	var cast bytes.Buffer
	if err := Export(context.Background(), r.st, r.box, s.ID(), &cast); err != nil {
		t.Fatalf("Export: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(cast.String()), "\n")
	var header map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil || header["version"] != float64(2) || header["width"] != float64(100) {
		t.Fatalf("header = %q, %v", lines[0], err)
	}
	var text strings.Builder
	for _, line := range lines[1:] {
		var event []any
		if err := json.Unmarshal([]byte(line), &event); err != nil || len(event) != 3 {
			t.Fatalf("event %q: %v", line, err)
		}
		if event[1] == "o" {
			fmt.Fprint(&text, event[2])
		}
	}
	if text.String() != want {
		t.Errorf("recorded output = %q, want %q", text.String(), want)
	}
	if strings.Contains(cast.String(), "ls") {
		t.Error("the keystrokes were recorded")
	}
	for _, chunk := range r.st.chunksOf(s.ID()) {
		if bytes.Contains(chunk.Sealed, []byte("hello")) {
			t.Error("a chunk is stored unencrypted")
		}
	}
}

func TestOpenRefusals(t *testing.T) {
	r := newRig(t, func(c *Config) { c.MaxPerServer = 1 })
	for name, req := range map[string]OpenRequest{
		"shell":     {ServerID: serverOne, Container: "web", Shell: "zsh", Actor: "admin"},
		"container": {ServerID: serverOne, Container: "../x", Shell: "sh", Actor: "admin"},
		"no user":   {ServerID: serverOne, Container: "web", Shell: "sh"},
		"size":      {ServerID: serverOne, Container: "web", Shell: "sh", Actor: "admin", Cols: 5000},
	} {
		if _, err := r.m.Open(context.Background(), req); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s: %v, want ErrInvalidRequest", name, err)
		}
	}
	if len(r.st.sessions) != 0 || len(r.agents.messages(serverOne)) != 0 {
		t.Error("a refused request reached the database or the agent")
	}

	first := r.open(t, request(serverOne, "web"), false)
	if _, err := r.m.Open(context.Background(), request(serverOne, "web")); !errors.Is(err, ErrSessionExists) {
		t.Errorf("second terminal of the same user and container: %v", err)
	}
	if _, err := r.m.Open(context.Background(), request(serverOne, "db")); !errors.Is(err, ErrTooManySessions) {
		t.Errorf("over the limit: %v", err)
	}
	// Another server has its own limit.
	other := r.open(t, request(serverTwo, "web"), false)
	first.Close(ReasonClosed)
	waitDone(t, first)
	again := r.open(t, request(serverOne, "db"), false)
	other.Close(ReasonClosed)
	again.Close(ReasonClosed)
}

func TestOpenFailures(t *testing.T) {
	t.Run("the agent refuses", func(t *testing.T) {
		r := newRig(t, nil)
		go func() {
			waitUntil(t, func() bool { return r.openedID(serverOne) != "" })
			r.m.TerminalOpened(serverOne, &agentv1.TerminalOpened{SessionId: r.openedID(serverOne), Error: "the container is not running"})
		}()
		_, err := r.m.Open(context.Background(), request(serverOne, "web"))
		var openErr *OpenError
		if !errors.As(err, &openErr) || openErr.Message != "the container is not running" {
			t.Fatalf("Open = %v", err)
		}
		// The slot is free again.
		go func() {
			time.Sleep(20 * time.Millisecond)
			r.m.TerminalOpened(serverOne, &agentv1.TerminalOpened{SessionId: r.openedID(serverOne), Error: "again"})
		}()
		if _, err := r.m.Open(context.Background(), request(serverOne, "web")); errors.Is(err, ErrSessionExists) || errors.Is(err, ErrTooManySessions) {
			t.Errorf("the failed session still counts: %v", err)
		}
	})

	t.Run("the agent is not connected", func(t *testing.T) {
		r := newRig(t, nil)
		r.agents.err = hub.ErrNotConnected
		if _, err := r.m.Open(context.Background(), request(serverOne, "web")); !errors.Is(err, hub.ErrNotConnected) {
			t.Errorf("Open = %v", err)
		}
	})

	t.Run("the agent never answers", func(t *testing.T) {
		r := newRig(t, func(c *Config) { c.OpenTimeout = 30 * time.Millisecond })
		_, err := r.m.Open(context.Background(), request(serverOne, "web"))
		var openErr *OpenError
		if !errors.As(err, &openErr) {
			t.Fatalf("Open = %v", err)
		}
		// The agent is told to drop a shell that may open late.
		sent := r.agents.messages(serverOne)
		if sent[len(sent)-1].GetTerminalClose() == nil {
			t.Errorf("the agent was not told to close: %v", sent)
		}
	})
}

func TestMessagesOfAnotherServerAreIgnored(t *testing.T) {
	r := newRig(t, nil)
	s := r.open(t, request(serverOne, "web"), false)

	// Server two guesses the id of a session of server one.
	r.m.TerminalOutput(serverTwo, &agentv1.TerminalOutput{SessionId: s.ID(), Data: []byte("injected")})
	r.m.TerminalClosed(serverTwo, &agentv1.TerminalClosed{SessionId: s.ID(), Reason: "exited"})
	r.m.AgentGone(serverTwo)

	select {
	case data := <-s.Output():
		t.Fatalf("output of another server reached the session: %q", data)
	case <-time.After(50 * time.Millisecond):
	}
	if s.Reason() != "" || r.m.Get(s.ID()) == nil {
		t.Error("another server ended the session")
	}
	s.Close(ReasonClosed)
	waitDone(t, s)
}

func TestSessionEndsWhenTheAgentIsGone(t *testing.T) {
	r := newRig(t, nil)
	s := r.open(t, request(serverOne, "web"), false)
	other := r.open(t, request(serverTwo, "web"), false)

	r.m.AgentGone(serverOne)
	waitDone(t, s)
	if s.Reason() != ReasonAgentOffline {
		t.Errorf("reason = %q", s.Reason())
	}
	if other.Reason() != "" {
		t.Error("the loss of one agent ended the session of another")
	}
	other.Close(ReasonClosed)
	waitDone(t, other)
}

func TestSlowBrowserEndsTheSession(t *testing.T) {
	r := newRig(t, func(c *Config) { c.PendingOutput = 2 })
	s := r.open(t, request(serverOne, "web"), false)
	for range 5 {
		r.m.TerminalOutput(serverOne, &agentv1.TerminalOutput{SessionId: s.ID(), Data: []byte("x")})
	}
	waitDone(t, s)
	if s.Reason() != ReasonSlowClient {
		t.Errorf("reason = %q", s.Reason())
	}
	sent := r.agents.messages(serverOne)
	if sent[len(sent)-1].GetTerminalClose() == nil {
		t.Error("the agent was not told to close the shell")
	}
}

func TestBusyAgentEndsTheSessionInsteadOfLosingKeystrokes(t *testing.T) {
	r := newRig(t, func(c *Config) { c.InputTimeout = 30 * time.Millisecond })
	s := r.open(t, request(serverOne, "web"), false)

	r.agents.setBlock(true)
	if err := s.Input(context.Background(), []byte("a")); err == nil {
		t.Fatal("Input succeeded with a stuck agent")
	}
	waitDone(t, s)
	if s.Reason() != ReasonAgentBusy {
		t.Errorf("reason = %q", s.Reason())
	}
	if err := s.Input(context.Background(), []byte("a")); !errors.Is(err, ErrClosed) {
		t.Errorf("Input after the end = %v", err)
	}
}

func TestInputLimits(t *testing.T) {
	r := newRig(t, nil)
	s := r.open(t, request(serverOne, "web"), false)
	if err := s.Input(context.Background(), make([]byte, maxInputBytes+1)); !errors.Is(err, ErrInputTooLarge) {
		t.Errorf("large input = %v", err)
	}
	if err := s.Resize(context.Background(), 0, 10); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("resize to zero = %v", err)
	}
	s.Close(ReasonClosed)
	waitDone(t, s)
}

func TestRecordingLimitAndFailureEndTheSession(t *testing.T) {
	t.Run("limit", func(t *testing.T) {
		r := newRig(t, func(c *Config) { c.MaxRecordingBytes = 400 })
		s := r.open(t, request(serverOne, "web"), false)
		for range 20 {
			r.m.TerminalOutput(serverOne, &agentv1.TerminalOutput{SessionId: s.ID(), Data: bytes.Repeat([]byte("y"), 50)})
		}
		waitDone(t, s)
		if s.Reason() != reasonRecordingLimit {
			t.Errorf("reason = %q", s.Reason())
		}
	})

	t.Run("database down", func(t *testing.T) {
		r := newRig(t, nil)
		s := r.open(t, request(serverOne, "web"), false)
		r.st.mu.Lock()
		r.st.failAdd = true
		r.st.mu.Unlock()
		r.m.TerminalOutput(serverOne, &agentv1.TerminalOutput{SessionId: s.ID(), Data: []byte("secret")})
		waitDone(t, s)
		if s.Reason() != reasonRecordingFailed {
			t.Errorf("reason = %q", s.Reason())
		}
	})
}

func TestIdleSessionIsEndedByTheCore(t *testing.T) {
	r := newRig(t, func(c *Config) { c.Idle = 40 * time.Millisecond })
	s := r.open(t, request(serverOne, "web"), false)
	waitDone(t, s)
	if s.Reason() != ReasonIdle {
		t.Errorf("reason = %q", s.Reason())
	}
}

func TestExportDetectsTampering(t *testing.T) {
	r := newRig(t, func(c *Config) { c.ChunkBytes = 64 })
	s := r.open(t, request(serverOne, "web"), false)
	for range 6 {
		r.m.TerminalOutput(serverOne, &agentv1.TerminalOutput{SessionId: s.ID(), Data: bytes.Repeat([]byte("z"), 60)})
		time.Sleep(25 * time.Millisecond)
	}
	s.Close(ReasonClosed)
	waitDone(t, s)

	original := r.st.chunksOf(s.ID())
	if len(original) < 3 {
		t.Fatalf("only %d chunks", len(original))
	}
	export := func() error { return Export(context.Background(), r.st, r.box, s.ID(), &bytes.Buffer{}) }
	if err := export(); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// A chunk dropped from the middle.
	r.st.setChunks(s.ID(), append(append([]store.TerminalChunk(nil), original[:1]...), original[2:]...))
	if err := export(); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("dropped chunk: %v", err)
	}
	// Two chunks swapped: each is valid, but not at that position.
	swapped := append([]store.TerminalChunk(nil), original...)
	swapped[1].Sealed, swapped[2].Sealed = swapped[2].Sealed, swapped[1].Sealed
	r.st.setChunks(s.ID(), swapped)
	if err := export(); err == nil {
		t.Error("swapped chunks were exported")
	}
	// A flipped byte.
	flipped := append([]store.TerminalChunk(nil), original...)
	flipped[1].Sealed = append([]byte(nil), original[1].Sealed...)
	flipped[1].Sealed[len(flipped[1].Sealed)-1] ^= 1
	r.st.setChunks(s.ID(), flipped)
	if err := export(); err == nil {
		t.Error("an altered chunk was exported")
	}
	if err := Export(context.Background(), r.st, r.box, "00000000-0000-4000-8000-ffffffffffff", &bytes.Buffer{}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown session: %v", err)
	}
}

func TestSplitIncomplete(t *testing.T) {
	tests := []struct {
		in, complete, rest string
	}{
		{"abc", "abc", ""},
		{"ab\xe2", "ab", "\xe2"},
		{"ab\xe2\x9c", "ab", "\xe2\x9c"},
		{"ab\xe2\x9c\x94", "ab\xe2\x9c\x94", ""},
		{"\xff", "\xff", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		complete, rest := splitIncomplete([]byte(tt.in))
		if string(complete) != tt.complete || string(rest) != tt.rest {
			t.Errorf("splitIncomplete(%q) = %q, %q", tt.in, complete, rest)
		}
	}
}

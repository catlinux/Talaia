// Package terminal runs the interactive shells the core opens in containers. A
// session is one shell with a terminal; the core names it, the agent routes the
// keystrokes and the output by that name and enforces its own limits, so a
// misbehaving core or browser cannot exhaust the host. The shell comes from a
// closed list and never from free text.
package terminal

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/catlinux/Talaia/agent/internal/jobs"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	// maxInputBytes is the most keystrokes one message may carry.
	maxInputBytes = 4 << 10
	// maxTerminalSize is the largest number of columns or rows.
	maxTerminalSize = 1000
	// pendingInput is how many input messages wait for a shell that does not read.
	pendingInput = 64
	// startTimeout bounds how long opening a session may take.
	startTimeout = 30 * time.Second
	// exitCodeTimeout bounds the wait for the exit code of a shell that ended.
	exitCodeTimeout = 2 * time.Second
)

// Reasons a session ends. The core shows them and audits them.
const (
	ReasonClosed       = "closed"
	ReasonExited       = "exited"
	ReasonIdle         = "idle"
	ReasonMaxDuration  = "max_duration"
	ReasonSlowShell    = "slow_shell"
	ReasonProtocol     = "protocol_error"
	ReasonDisconnected = "disconnected"
)

// sessionID is what the core generates: a random UUID.
var sessionID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Limits bound what the sessions of one agent may use.
type Limits struct {
	// MaxSessions is how many sessions may exist at once.
	MaxSessions int
	// Idle ends a session without input or output for this long.
	Idle time.Duration
	// MaxDuration ends a session that has lasted this long.
	MaxDuration time.Duration
	// Chunk is the most bytes of output sent in one message.
	Chunk int
	// BytesPerSecond limits the output of one session.
	BytesPerSecond int
	// Tick is how often idle time and duration are checked.
	Tick time.Duration
}

// DefaultLimits are the limits of a real agent.
func DefaultLimits() Limits {
	return Limits{
		MaxSessions:    4,
		Idle:           15 * time.Minute,
		MaxDuration:    4 * time.Hour,
		Chunk:          16 << 10,
		BytesPerSecond: 1 << 20,
		Tick:           5 * time.Second,
	}
}

// Manager runs the terminal sessions of the agent.
type Manager struct {
	backend Backend
	limits  Limits

	mu       sync.Mutex
	sessions map[string]*session
}

// New returns a Manager that starts shells with backend.
func New(backend Backend, limits Limits) *Manager {
	return &Manager{backend: backend, limits: limits, sessions: make(map[string]*session)}
}

// session is one shell.
type session struct {
	id     string
	cancel context.CancelFunc
	input  chan []byte
	start  time.Time
	last   atomic.Int64 // unix nanoseconds of the last input or output

	mu      sync.Mutex
	process Process // nil while the shell is starting
	reason  string  // why the session ended; empty while it lives
}

func (s *session) touch() { s.last.Store(time.Now().UnixNano()) }

// end ends the session for reason, unless it already ended for another one.
func (s *session) end(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reason != "" {
		return
	}
	s.reason = reason
	s.cancel()
	if s.process != nil {
		_ = s.process.Close()
	}
}

func (s *session) endReason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reason
}

func (s *session) current() Process {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.process
}

// Handle runs the terminal message msg, if it is one, and reports whether it was.
// It never blocks on Docker: opening and closing run in goroutines. emit is
// called from them, so it must be safe for concurrent use.
func (m *Manager) Handle(ctx context.Context, msg *agentv1.CoreMsg, emit func(*agentv1.AgentMsg)) bool {
	switch {
	case msg.GetTerminalOpen() != nil:
		m.open(ctx, msg.GetTerminalOpen(), emit)
	case msg.GetTerminalInput() != nil:
		m.input(msg.GetTerminalInput())
	case msg.GetTerminalResize() != nil:
		m.resize(ctx, msg.GetTerminalResize())
	case msg.GetTerminalClose() != nil:
		if s := m.find(msg.GetTerminalClose().GetSessionId()); s != nil {
			s.end(ReasonClosed)
		}
	default:
		return false
	}
	return true
}

// CloseAll ends every session, for example because the stream to the core is
// gone. It does not wait for them.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	all := make([]*session, 0, len(m.sessions))
	for _, s := range m.sessions {
		all = append(all, s)
	}
	m.mu.Unlock()

	for _, s := range all {
		s.end(ReasonDisconnected)
	}
}

// Count returns how many sessions exist.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

func (m *Manager) find(id string) *session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

func (m *Manager) open(ctx context.Context, request *agentv1.TerminalOpen, emit func(*agentv1.AgentMsg)) {
	id := request.GetSessionId()
	if !sessionID.MatchString(id) {
		slog.Warn("ignoring a terminal request with an invalid session id")
		return
	}

	refuse := func(reason string) {
		emit(opened(id, false, reason))
	}
	shell, known := shells[request.GetShell()]
	cols, rows := sizeOrDefault(request.GetCols(), 80), sizeOrDefault(request.GetRows(), 24)
	switch {
	case !known:
		refuse("the shell is not supported")
		return
	case !jobs.ValidContainerRef(request.GetContainer()):
		refuse("invalid container reference")
		return
	case cols > maxTerminalSize || rows > maxTerminalSize:
		refuse("invalid terminal size")
		return
	}

	sessionCtx, cancel := context.WithCancel(ctx)
	s := &session{id: id, cancel: cancel, input: make(chan []byte, pendingInput), start: time.Now()}
	s.touch()

	m.mu.Lock()
	switch {
	case m.sessions[id] != nil:
		m.mu.Unlock()
		cancel()
		refuse("the session already exists")
		return
	case len(m.sessions) >= m.limits.MaxSessions:
		m.mu.Unlock()
		cancel()
		refuse("too many terminal sessions on this server")
		return
	}
	m.sessions[id] = s
	m.mu.Unlock()

	go m.run(sessionCtx, s, request.GetContainer(), shell, cols, rows, emit)
}

// sizeOrDefault returns value, or def when it is 0.
func sizeOrDefault(value, def uint32) uint32 {
	if value == 0 {
		return def
	}
	return value
}

func (m *Manager) input(request *agentv1.TerminalInput) {
	s := m.find(request.GetSessionId())
	if s == nil {
		return
	}
	if len(request.GetData()) > maxInputBytes {
		s.end(ReasonProtocol)
		return
	}
	select {
	case s.input <- request.GetData():
	default:
		// The shell is not reading: do not stall the other sessions.
		s.end(ReasonSlowShell)
	}
}

func (m *Manager) resize(ctx context.Context, request *agentv1.TerminalResize) {
	s := m.find(request.GetSessionId())
	if s == nil {
		return
	}
	cols, rows := request.GetCols(), request.GetRows()
	if cols < 1 || cols > maxTerminalSize || rows < 1 || rows > maxTerminalSize {
		s.end(ReasonProtocol)
		return
	}
	process := s.current()
	if process == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(ctx, startTimeout)
		defer cancel()
		if err := process.Resize(ctx, cols, rows); err != nil {
			slog.Warn("resize terminal", "error", err)
		}
	}()
}

// run opens the shell and carries its output until the session ends.
func (m *Manager) run(ctx context.Context, s *session, container string, shell []string, cols, rows uint32, emit func(*agentv1.AgentMsg)) {
	defer m.forget(s.id)

	startCtx, cancelStart := context.WithTimeout(ctx, startTimeout)
	process, privileged, err := m.backend.Start(startCtx, container, shell, cols, rows)
	cancelStart()
	if err != nil {
		s.end(ReasonClosed)
		emit(opened(s.id, false, errorText(err)))
		return
	}

	s.mu.Lock()
	ended := s.reason != ""
	if !ended {
		s.process = process
	}
	s.mu.Unlock()
	if ended {
		// Closed while it was starting.
		_ = process.Close()
		emit(closed(s.id, s.endReason(), 0))
		return
	}

	slog.Info("terminal opened", "session", s.id, "container", container, "privileged", privileged)
	emit(opened(s.id, privileged, ""))

	go m.write(ctx, s, process)
	go m.watch(ctx, s)
	m.read(ctx, s, process, emit)

	s.end(ReasonExited)
	exitCode := 0
	if s.endReason() == ReasonExited {
		codeCtx, cancel := context.WithTimeout(context.Background(), exitCodeTimeout)
		if code, err := process.ExitCode(codeCtx); err == nil {
			exitCode = code
		}
		cancel()
	}
	slog.Info("terminal closed", "session", s.id, "reason", s.endReason())
	emit(closed(s.id, s.endReason(), exitCode))
}

// read sends the output of the shell in chunks, at the rate allowed. When the
// stream to the core is full, emit waits and so does the shell.
func (m *Manager) read(ctx context.Context, s *session, process Process, emit func(*agentv1.AgentMsg)) {
	buffer := make([]byte, m.limits.Chunk)
	for {
		n, err := process.Read(buffer)
		if n > 0 {
			s.touch()
			emit(&agentv1.AgentMsg{Msg: &agentv1.AgentMsg_TerminalOutput{TerminalOutput: &agentv1.TerminalOutput{
				SessionId: s.id, Data: append([]byte(nil), buffer[:n]...),
			}}})
			if m.limits.BytesPerSecond > 0 {
				timer := time.NewTimer(time.Duration(n) * time.Second / time.Duration(m.limits.BytesPerSecond))
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					return
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// write passes the keystrokes to the shell.
func (m *Manager) write(ctx context.Context, s *session, process Process) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-s.input:
			s.touch()
			if _, err := process.Write(data); err != nil {
				s.end(ReasonExited)
				return
			}
		}
	}
}

// watch ends a session that is idle or lasted too long.
func (m *Manager) watch(ctx context.Context, s *session) {
	ticker := time.NewTicker(m.limits.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			switch {
			case m.limits.MaxDuration > 0 && now.Sub(s.start) >= m.limits.MaxDuration:
				s.end(ReasonMaxDuration)
			case m.limits.Idle > 0 && now.Sub(time.Unix(0, s.last.Load())) >= m.limits.Idle:
				s.end(ReasonIdle)
			}
		}
	}
}

func opened(id string, privileged bool, errText string) *agentv1.AgentMsg {
	return &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_TerminalOpened{TerminalOpened: &agentv1.TerminalOpened{
		SessionId: id, Privileged: privileged, Error: errText,
	}}}
}

func closed(id, reason string, exitCode int) *agentv1.AgentMsg {
	return &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_TerminalClosed{TerminalClosed: &agentv1.TerminalClosed{
		SessionId: id, Reason: reason, ExitCode: int32(exitCode), //nolint:gosec // exit codes are small
	}}}
}

// errorText is an error as a message the core can store: valid UTF-8, short.
func errorText(err error) string {
	text := strings.ToValidUTF8(err.Error(), "�")
	if len(text) > 300 {
		text = strings.ToValidUTF8(text[:300], "")
	}
	if text == "" {
		return errors.New("could not open the terminal").Error()
	}
	return text
}

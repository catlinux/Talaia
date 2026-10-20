// Package terminal keeps the interactive shell sessions the panel opens in the
// containers of the servers. A session is routed by the pair (server, session
// id): the server is the one that authenticated the stream of the agent, so an
// agent can only feed its own sessions. Every session is recorded, sealed with
// the secrets key, and audited; a session that cannot be recorded does not run.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// Reasons a session ends, besides those the agent reports.
const (
	ReasonClosed       = "closed"
	ReasonOpenFailed   = "open_failed"
	ReasonAgentOffline = "agent_offline"
	ReasonAgentBusy    = "agent_busy"
	ReasonSlowClient   = "slow_client"
	ReasonIdle         = "idle"
	ReasonMaxDuration  = "max_duration"
	ReasonCoreRestart  = "core_restart"
)

// maxInputBytes is the most keystrokes one message may carry; the agent checks it
// again.
const maxInputBytes = 4 << 10

var (
	// ErrTooManySessions means the server already has the most sessions it may.
	ErrTooManySessions = errors.New("too many terminal sessions on this server")
	// ErrSessionExists means the user already has a terminal in that container.
	ErrSessionExists = errors.New("this user already has a terminal in this container")
	// ErrInvalidRequest means the container, shell or size is not acceptable.
	ErrInvalidRequest = errors.New("invalid terminal request")
	// ErrClosed means the session has ended.
	ErrClosed = errors.New("the terminal session is closed")
	// ErrInputTooLarge means a message of keystrokes is over the limit.
	ErrInputTooLarge = errors.New("the terminal input is too large")
)

// OpenError is a shell the agent could not start, with its explanation.
type OpenError struct{ Message string }

func (e *OpenError) Error() string { return "the terminal could not be opened: " + e.Message }

// containerRef matches a container id or name. The agent checks it again.
var containerRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// Store is the part of the store the sessions need.
type Store interface {
	CreateTerminalSession(ctx context.Context, serverID, container, shell, createdBy string) (store.TerminalSession, error)
	SetTerminalPrivileged(ctx context.Context, id string, privileged bool) error
	AppendTerminalChunk(ctx context.Context, id string, seq int, sealed []byte, plainBytes int) error
	CloseTerminalSession(ctx context.Context, id, reason string) error
}

// Dispatcher sends messages to the agent of a server, waiting for room.
type Dispatcher interface {
	DispatchWait(ctx context.Context, serverID string, msg *agentv1.CoreMsg) error
}

// Auditor records what people do.
type Auditor interface {
	AppendAudit(ctx context.Context, event store.AuditEvent) error
}

// Sealer encrypts the chunks of a recording, bound to their position.
type Sealer interface {
	SealWithContext(plaintext, bound []byte) []byte
}

// Config bounds the sessions.
type Config struct {
	// MaxPerServer is how many sessions one server may have at once.
	MaxPerServer int
	// Idle and MaxDuration end a session the agent did not end, as a second line
	// of defence.
	Idle        time.Duration
	MaxDuration time.Duration
	// MaxRecordingBytes bounds the recording of one session.
	MaxRecordingBytes int64
	// PendingOutput is how many output messages may wait for a slow browser.
	PendingOutput int
	// InputTimeout bounds the wait to hand keystrokes to a busy agent.
	InputTimeout time.Duration
	// OpenTimeout bounds the wait for the agent to start the shell.
	OpenTimeout time.Duration
	// ChunkBytes is the size at which a recording chunk is stored.
	ChunkBytes int
	// FlushEvery is how often the recording is stored at the latest.
	FlushEvery time.Duration
	// Tick is how often idle time and duration are checked.
	Tick time.Duration
}

// DefaultConfig is the configuration of a real core: the limits of the design.
func DefaultConfig() Config {
	return Config{
		MaxPerServer:      4,
		Idle:              16 * time.Minute, // the agent closes at 15
		MaxDuration:       4*time.Hour + time.Minute,
		MaxRecordingBytes: 50 << 20,
		PendingOutput:     64, // of up to 16 KiB: 1 MiB
		InputTimeout:      5 * time.Second,
		OpenTimeout:       30 * time.Second,
		ChunkBytes:        32 << 10,
		FlushEvery:        2 * time.Second,
		Tick:              10 * time.Second,
	}
}

// Manager keeps the live sessions.
type Manager struct {
	store      Store
	dispatcher Dispatcher
	audit      Auditor
	seal       Sealer
	cfg        Config
	now        func() time.Time

	mu       sync.Mutex
	sessions map[string]*Session
	// reserved counts the sessions being opened, so the limits hold while the
	// database and the agent are being waited for.
	reserved     map[string]int
	reservedKeys map[string]bool
}

// New returns a Manager.
func New(st Store, dispatcher Dispatcher, audit Auditor, seal Sealer, cfg Config) *Manager {
	return &Manager{
		store: st, dispatcher: dispatcher, audit: audit, seal: seal, cfg: cfg, now: time.Now,
		sessions: make(map[string]*Session), reserved: make(map[string]int), reservedKeys: make(map[string]bool),
	}
}

// OpenRequest is what the panel asks for.
type OpenRequest struct {
	ServerID  string
	Container string
	// Shell is "sh" or "bash".
	Shell string
	// Actor is the user; PanelSession the id of the session of the panel that
	// asked, which the WebSocket must present again.
	Actor        string
	PanelSession string
	IP           string
	Cols, Rows   uint32
}

// Session is one shell. Its output is read from Output; it ends when Done closes.
type Session struct {
	m            *Manager
	id           string
	serverID     string
	container    string
	shell        string
	actor        string
	panelSession string
	ip           string
	privileged   bool
	started      time.Time
	last         atomic.Int64

	out    chan []byte
	done   chan struct{}
	opened chan *agentv1.TerminalOpened
	rec    *recorder

	mu       sync.Mutex
	ended    bool
	reason   string
	exitCode int
}

// ID returns the session id the agent and the WebSocket use.
func (s *Session) ID() string { return s.id }

// ServerID returns the server the shell runs on.
func (s *Session) ServerID() string { return s.serverID }

// Actor returns the user who opened the session.
func (s *Session) Actor() string { return s.actor }

// PanelSession returns the id of the panel session that opened it.
func (s *Session) PanelSession() string { return s.panelSession }

// Privileged reports that the container is as good as root on the host.
func (s *Session) Privileged() bool { return s.privileged }

// Output carries what the shell prints. It is closed when the session ends.
func (s *Session) Output() <-chan []byte { return s.out }

// Done is closed once the session has ended and its recording is stored.
func (s *Session) Done() <-chan struct{} { return s.done }

// Reason says why the session ended; empty while it lives.
func (s *Session) Reason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reason
}

// ExitCode is the exit code of the shell when the agent reported one.
func (s *Session) ExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exitCode
}

func (s *Session) touch() { s.last.Store(s.m.now().UnixNano()) }

// Get returns a live session.
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// Open starts a shell in a container and returns once it runs.
func (m *Manager) Open(ctx context.Context, req OpenRequest) (*Session, error) {
	if req.Cols == 0 {
		req.Cols = 80
	}
	if req.Rows == 0 {
		req.Rows = 24
	}
	shell, ok := map[string]agentv1.TerminalShell{"sh": agentv1.TerminalShell_TERMINAL_SHELL_SH, "bash": agentv1.TerminalShell_TERMINAL_SHELL_BASH}[req.Shell]
	if !ok || !containerRef.MatchString(req.Container) || req.ServerID == "" || req.Actor == "" ||
		req.Cols > 1000 || req.Rows > 1000 {
		return nil, ErrInvalidRequest
	}

	key := req.ServerID + "|" + req.Actor + "|" + req.Container
	if err := m.reserve(req.ServerID, key); err != nil {
		return nil, err
	}
	defer m.release(req.ServerID, key)

	// The row comes first: without a place to record, nothing starts.
	row, err := m.store.CreateTerminalSession(ctx, req.ServerID, req.Container, req.Shell, req.Actor)
	if err != nil {
		return nil, fmt.Errorf("the session cannot be recorded: %w", err)
	}

	now := m.now()
	s := &Session{
		m: m, id: row.ID, serverID: req.ServerID, container: req.Container, shell: req.Shell,
		actor: req.Actor, panelSession: req.PanelSession, ip: req.IP, started: now,
		out: make(chan []byte, m.cfg.PendingOutput), done: make(chan struct{}), opened: make(chan *agentv1.TerminalOpened, 1),
		rec: newRecorder(row.ID, m.seal, m.store, m.cfg.ChunkBytes, m.cfg.MaxRecordingBytes, now, req.Container, req.Shell, req.Cols, req.Rows),
	}
	s.touch()

	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()

	open := &agentv1.CoreMsg{Cmd: &agentv1.CoreMsg_TerminalOpen{TerminalOpen: &agentv1.TerminalOpen{
		SessionId: s.id, Container: req.Container, Shell: shell, Cols: req.Cols, Rows: req.Rows,
	}}}
	dispatchCtx, cancel := context.WithTimeout(ctx, m.cfg.InputTimeout)
	err = m.dispatcher.DispatchWait(dispatchCtx, req.ServerID, open)
	cancel()
	if err != nil {
		s.finish(ReasonOpenFailed, 0, false)
		<-s.done
		return nil, fmt.Errorf("the agent did not receive the request: %w", err)
	}

	timer := time.NewTimer(m.cfg.OpenTimeout)
	defer timer.Stop()
	select {
	case answer := <-s.opened:
		if answer.GetError() != "" {
			s.finish(ReasonOpenFailed, 0, false)
			<-s.done
			return nil, &OpenError{Message: answer.GetError()}
		}
		s.privileged = answer.GetPrivileged()
	case <-timer.C:
		s.finish(ReasonOpenFailed, 0, true)
		<-s.done
		return nil, &OpenError{Message: "the agent did not answer in time"}
	case <-ctx.Done():
		s.finish(ReasonOpenFailed, 0, true)
		<-s.done
		return nil, ctx.Err()
	case <-s.done:
		// The agent went away while the shell was starting.
		return nil, &OpenError{Message: "the agent disconnected"}
	}

	if s.privileged {
		if err := m.store.SetTerminalPrivileged(ctx, s.id, true); err != nil {
			slog.Warn("record that a terminal is privileged", "session", s.id, "error", err)
		}
	}
	m.auditEvent(ctx, "terminal_open", s, map[string]any{"privileged": s.privileged, "shell": s.shell})

	recCtx, stopRecording := context.WithCancel(context.Background())
	go func() {
		<-s.done
		stopRecording()
	}()
	go s.rec.run(recCtx, m.cfg.FlushEvery, func(err error) {
		slog.Error("store the recording of a terminal", "session", s.id, "error", err)
		s.finish(reasonRecordingFailed, 0, true)
	})
	go s.watch()
	return s, nil
}

// reserve counts a session against the limits of its server and user.
func (m *Manager) reserve(serverID, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.reservedKeys[key] {
		return ErrSessionExists
	}
	count := m.reserved[serverID]
	for _, s := range m.sessions {
		if s.serverID == serverID {
			count++
		}
		if s.serverID+"|"+s.actor+"|"+s.container == key {
			return ErrSessionExists
		}
	}
	if count >= m.cfg.MaxPerServer {
		return ErrTooManySessions
	}
	m.reserved[serverID]++
	m.reservedKeys[key] = true
	return nil
}

func (m *Manager) release(serverID, key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reserved[serverID]--
	delete(m.reservedKeys, key)
}

// Input hands keystrokes to the shell. When the agent is busy it waits, which
// pushes the pressure back to the browser; if the wait runs out the session ends.
func (s *Session) Input(ctx context.Context, data []byte) error {
	if len(data) > maxInputBytes {
		return ErrInputTooLarge
	}
	if s.isEnded() {
		return ErrClosed
	}
	s.touch()
	msg := &agentv1.CoreMsg{Cmd: &agentv1.CoreMsg_TerminalInput{TerminalInput: &agentv1.TerminalInput{SessionId: s.id, Data: data}}}
	return s.send(ctx, msg)
}

// Resize changes the size of the terminal.
func (s *Session) Resize(ctx context.Context, cols, rows uint32) error {
	if cols < 1 || cols > 1000 || rows < 1 || rows > 1000 {
		return ErrInvalidRequest
	}
	if s.isEnded() {
		return ErrClosed
	}
	msg := &agentv1.CoreMsg{Cmd: &agentv1.CoreMsg_TerminalResize{TerminalResize: &agentv1.TerminalResize{SessionId: s.id, Cols: cols, Rows: rows}}}
	if err := s.send(ctx, msg); err != nil {
		return err
	}
	if reason := s.rec.resize(cols, rows, s.m.now()); reason != "" {
		s.finish(reason, 0, true)
	}
	return nil
}

func (s *Session) send(ctx context.Context, msg *agentv1.CoreMsg) error {
	ctx, cancel := context.WithTimeout(ctx, s.m.cfg.InputTimeout)
	defer cancel()
	if err := s.m.dispatcher.DispatchWait(ctx, s.serverID, msg); err != nil {
		reason := ReasonAgentBusy
		if errors.Is(err, hub.ErrNotConnected) {
			reason = ReasonAgentOffline
		}
		s.finish(reason, 0, false)
		return fmt.Errorf("the agent did not take the message: %w", err)
	}
	return nil
}

// Close ends the session at the request of the user.
func (s *Session) Close(reason string) {
	s.finish(reason, 0, true)
}

func (s *Session) isEnded() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
}

// deliver passes output of the agent to the browser and to the recording.
func (s *Session) deliver(data []byte) {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.touch()
	reason := s.rec.add(data, s.m.now())
	if reason == "" {
		select {
		case s.out <- append([]byte(nil), data...):
		default:
			reason = ReasonSlowClient
		}
	}
	s.mu.Unlock()

	if reason != "" {
		s.finish(reason, 0, true)
	}
}

// finish ends the session once. The slow part (the last chunk of the recording,
// the database and the audit log) runs in the background, so the loop that reads
// the stream of the agent never waits for it.
func (s *Session) finish(reason string, exitCode int, notifyAgent bool) {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	s.reason = reason
	s.exitCode = exitCode
	close(s.out)
	s.mu.Unlock()

	s.m.mu.Lock()
	delete(s.m.sessions, s.id)
	s.m.mu.Unlock()

	go s.teardown(reason, notifyAgent)
}

func (s *Session) teardown(reason string, notifyAgent bool) {
	defer close(s.done)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if notifyAgent {
		msg := &agentv1.CoreMsg{Cmd: &agentv1.CoreMsg_TerminalClose{TerminalClose: &agentv1.TerminalClose{SessionId: s.id}}}
		closeCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		_ = s.m.dispatcher.DispatchWait(closeCtx, s.serverID, msg)
		stop()
	}
	if err := s.rec.finish(ctx, s.m.now()); err != nil {
		slog.Error("store the end of a terminal recording", "session", s.id, "error", err)
	}
	if err := s.m.store.CloseTerminalSession(ctx, s.id, reason); err != nil {
		slog.Error("close a terminal session", "session", s.id, "error", err)
	}
	s.m.auditEvent(ctx, "terminal_close", s, map[string]any{
		"reason": reason, "duration_seconds": int(s.m.now().Sub(s.started).Seconds()),
	})
}

// watch ends a session that is idle or lasted too long, in case the agent does
// not.
func (s *Session) watch() {
	ticker := time.NewTicker(s.m.cfg.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			now := s.m.now()
			switch {
			case now.Sub(s.started) >= s.m.cfg.MaxDuration:
				s.finish(ReasonMaxDuration, 0, true)
			case now.Sub(time.Unix(0, s.last.Load())) >= s.m.cfg.Idle:
				s.finish(ReasonIdle, 0, true)
			}
		}
	}
}

func (m *Manager) auditEvent(ctx context.Context, action string, s *Session, extra map[string]any) {
	details := map[string]any{"server_id": s.serverID, "session_id": s.id, "container": s.container}
	for key, value := range extra {
		details[key] = value
	}
	if err := m.audit.AppendAudit(ctx, store.AuditEvent{Actor: s.actor, Action: action, IP: s.ip, Result: "ok", Details: details}); err != nil {
		slog.Error("append audit event", "action", action, "error", err)
	}
}

// CloseOrphans closes in the database the sessions an earlier run of the core
// left open.
func CloseOrphans(ctx context.Context, st interface {
	CloseOrphanTerminalSessions(ctx context.Context, reason string) (int64, error)
}) error {
	closed, err := st.CloseOrphanTerminalSessions(ctx, ReasonCoreRestart)
	if err != nil {
		return err
	}
	if closed > 0 {
		slog.Info("closed terminal sessions left open by a restart", "count", closed)
	}
	return nil
}

// TerminalOpened implements hub.TerminalEvents.
func (m *Manager) TerminalOpened(serverID string, msg *agentv1.TerminalOpened) {
	if s := m.owned(serverID, msg.GetSessionId()); s != nil {
		select {
		case s.opened <- msg:
		default:
		}
	}
}

// TerminalOutput implements hub.TerminalEvents.
func (m *Manager) TerminalOutput(serverID string, msg *agentv1.TerminalOutput) {
	if s := m.owned(serverID, msg.GetSessionId()); s != nil {
		s.deliver(msg.GetData())
	}
}

// TerminalClosed implements hub.TerminalEvents.
func (m *Manager) TerminalClosed(serverID string, msg *agentv1.TerminalClosed) {
	if s := m.owned(serverID, msg.GetSessionId()); s != nil {
		reason := msg.GetReason()
		if reason == "" {
			reason = ReasonClosed
		}
		s.finish(reason, int(msg.GetExitCode()), false)
	}
}

// AgentGone implements hub.TerminalEvents: the shells died with the stream.
func (m *Manager) AgentGone(serverID string) {
	m.mu.Lock()
	var gone []*Session
	for _, s := range m.sessions {
		if s.serverID == serverID {
			gone = append(gone, s)
		}
	}
	m.mu.Unlock()

	for _, s := range gone {
		s.finish(ReasonAgentOffline, 0, false)
	}
}

// owned finds a session by id, only if it belongs to serverID. A message about a
// session of another server is dropped: it cannot come from an honest agent.
func (m *Manager) owned(serverID, id string) *Session {
	s := m.Get(id)
	if s == nil {
		return nil
	}
	if s.serverID != serverID {
		slog.Warn("an agent sent a message for a terminal session of another server", "server_id", serverID)
		return nil
	}
	return s
}

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/catlinux/Talaia/core/internal/terminal"
)

const (
	// ticketTTL is how long the browser has to open the WebSocket after the
	// session was created.
	ticketTTL = 30 * time.Second
	// maxTickets bounds the tickets waiting to be redeemed.
	maxTickets = 64
	// handshakeTimeout bounds the wait for the ticket on a new WebSocket.
	handshakeTimeout = 10 * time.Second
	// writeTimeout bounds one write to the browser: a browser that does not read
	// loses its session instead of holding memory.
	writeTimeout = 10 * time.Second
	// maxSocketMessage bounds a message from the browser: keystrokes are at most
	// 4 KiB and the control messages are tiny.
	maxSocketMessage = 8 << 10
	// defaultSessionCheck is how often the panel session of a terminal is checked.
	defaultSessionCheck = time.Minute
)

// WebSocket close codes, in the range the standard leaves to applications.
const (
	closeBadTicket       websocket.StatusCode = 4401
	closeSessionRevoked  websocket.StatusCode = 4403
	closeProtocolProblem websocket.StatusCode = websocket.StatusPolicyViolation
)

// TerminalService is the terminal sessions of the core.
type TerminalService interface {
	Open(ctx context.Context, req terminal.OpenRequest) (*terminal.Session, error)
	Get(id string) *terminal.Session
}

// PanelSessions checks that the panel session of a terminal is still valid.
type PanelSessions interface {
	GetSessionByTokenHash(ctx context.Context, tokenHash []byte) (store.Session, error)
	TouchSession(ctx context.Context, id string, now time.Time) error
}

// TOTPVerifier checks a six-digit code with the replay guard of the auth
// handlers, so a code cannot be reused between login, password change and host
// terminals.
type TOTPVerifier interface {
	VerifyCode(ctx context.Context, user store.User, code string) error
}

// TerminalDeps are the collaborators of the terminal endpoints.
type TerminalDeps struct {
	Agents    LiveAgents
	Terminals TerminalService
	Sessions  PanelSessions
	// TOTP verifies the fresh code of a host terminal. The router wires the
	// auth handler here.
	TOTP TOTPVerifier
	// Limiter is the shared TOTP and login rate limiter.
	Limiter *RateLimiter
	// Origins are the browser origins allowed to open a WebSocket.
	Origins []string
	// Now and CheckInterval can be replaced by the tests.
	Now           func() time.Time
	CheckInterval time.Duration
	// TicketTTL is how long a ticket lives; zero means ticketTTL.
	TicketTTL time.Duration

	tickets *ticketBook
}

// ticketBook holds the one-time tickets that let a browser attach to a session.
// It keeps only the hash of each ticket.
type ticketBook struct {
	ttl     time.Duration
	mu      sync.Mutex
	tickets map[[sha256.Size]byte]ticket
	now     func() time.Time
}

type ticket struct {
	terminalID   string
	panelSession string
	expires      time.Time
}

func newTicketBook(now func() time.Time, ttl time.Duration) *ticketBook {
	return &ticketBook{tickets: make(map[[sha256.Size]byte]ticket), now: now, ttl: ttl}
}

// issue creates a ticket for a session of the panel and arranges for the shell to
// be closed if nobody redeems it in time.
func (b *ticketBook) issue(s *terminal.Session) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	key := sha256.Sum256([]byte(token))

	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweep()
	if len(b.tickets) >= maxTickets {
		return "", errors.New("too many terminal tickets")
	}
	b.tickets[key] = ticket{terminalID: s.ID(), panelSession: s.PanelSession(), expires: b.now().Add(b.ttl)}

	time.AfterFunc(b.ttl+time.Second, func() {
		// A ticket that was never redeemed leaves a shell nobody can reach.
		b.mu.Lock()
		_, pending := b.tickets[key]
		delete(b.tickets, key)
		b.mu.Unlock()
		if pending {
			s.Close("ticket_expired")
		}
	})
	return token, nil
}

// redeem consumes a ticket. It only works once, before it expires, for the
// session it was issued for and from the panel session that asked for it.
func (b *ticketBook) redeem(token, terminalID, panelSession string) bool {
	key := sha256.Sum256([]byte(token))

	b.mu.Lock()
	defer b.mu.Unlock()
	found, ok := b.tickets[key]
	if !ok {
		return false
	}
	delete(b.tickets, key)
	return found.terminalID == terminalID && found.panelSession == panelSession && b.now().Before(found.expires)
}

func (b *ticketBook) sweep() {
	now := b.now()
	for key, found := range b.tickets {
		if now.After(found.expires) {
			delete(b.tickets, key)
		}
	}
}

// NewTerminalDeps fills in the defaults of the terminal dependencies.
func NewTerminalDeps(deps TerminalDeps) *TerminalDeps {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.CheckInterval <= 0 {
		deps.CheckInterval = defaultSessionCheck
	}
	if deps.TicketTTL <= 0 {
		deps.TicketTTL = ticketTTL
	}
	deps.tickets = newTicketBook(deps.Now, deps.TicketTTL)
	return &deps
}

type terminalOpenRequest struct {
	// Confirm must be true: the person has been told the session is recorded.
	Confirm bool   `json:"confirm"`
	Shell   string `json:"shell"`
	Cols    uint32 `json:"cols"`
	Rows    uint32 `json:"rows"`
}

type terminalOpenResponse struct {
	SessionID string `json:"session_id"`
	Ticket    string `json:"ticket"`
	// Privileged says the container is as good as root on the server.
	Privileged bool `json:"privileged"`
}

// NewTerminalOpenHandler returns POST
// /api/v1/servers/{serverID}/containers/{container}/terminal. It starts the shell
// and answers with the ticket the browser presents on the WebSocket.
func NewTerminalOpenHandler(deps *TerminalDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID, container := chi.URLParam(r, "serverID"), chi.URLParam(r, "container")

		var request terminalOpenRequest
		if err := decodeJSON(w, r, &request); err != nil || !containerRef.MatchString(container) {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if !request.Confirm {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}

		agent, connected := connectedAgent(deps.Agents, serverID)
		if !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}
		if !hasContainer(agent, container) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}

		session, ok := sessionFromContext(r.Context())
		if !ok || session.User.Username == "" {
			writeError(w, http.StatusUnauthorized, "session_required")
			return
		}

		opened, err := deps.Terminals.Open(r.Context(), terminal.OpenRequest{
			ServerID: serverID, Container: container, Shell: request.Shell,
			Actor: session.User.Username, PanelSession: session.ID, IP: clientIP(r),
			Cols: request.Cols, Rows: request.Rows,
		})
		if err != nil {
			status, code := terminalError(err)
			slog.Warn("open a terminal", "server_id", serverID, "container", container, "error", err)
			writeError(w, status, code)
			return
		}

		token, err := deps.tickets.issue(opened)
		if err != nil {
			opened.Close("open_failed")
			writeError(w, http.StatusServiceUnavailable, "agent_busy")
			return
		}
		writeJSON(w, http.StatusCreated, terminalOpenResponse{SessionID: opened.ID(), Ticket: token, Privileged: opened.Privileged()})
	})
}

// terminalError maps the failure of opening a terminal to a status and a code.
func terminalError(err error) (int, string) {
	var openErr *terminal.OpenError
	switch {
	case errors.Is(err, terminal.ErrInvalidRequest):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, terminal.ErrTooManySessions):
		return http.StatusConflict, "too_many_terminals"
	case errors.Is(err, terminal.ErrSessionExists):
		return http.StatusConflict, "terminal_exists"
	case errors.Is(err, hub.ErrNotConnected):
		return http.StatusConflict, "agent_offline"
	case errors.Is(err, hub.ErrAgentBusy):
		return http.StatusServiceUnavailable, "agent_busy"
	case errors.As(err, &openErr):
		return http.StatusBadGateway, "terminal_open_failed"
	default:
		return http.StatusInternalServerError, "internal"
	}
}

// socketControl is a text message from the browser.
type socketControl struct {
	Type   string `json:"type"`
	Ticket string `json:"ticket,omitempty"`
	Cols   uint32 `json:"cols,omitempty"`
	Rows   uint32 `json:"rows,omitempty"`
}

// NewTerminalSocketHandler returns GET /api/v1/terminal/{sessionID}/ws. The
// browser must come from an allowed origin, have a full panel session and present
// the one-time ticket in its first message. Binary messages are keystrokes and
// output; text messages are control messages.
func NewTerminalSocketHandler(deps *TerminalDeps) http.Handler {
	allowed := make(map[string]bool, len(deps.Origins))
	for _, origin := range deps.Origins {
		allowed[strings.ToLower(origin)] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Unlike a POST, a WebSocket handshake is a GET that a browser sends from
		// any page, with the cookie: the origin must be named, not only "same site".
		origin := r.Header.Get("Origin")
		if origin == "" || !allowed[strings.ToLower(origin)] {
			writeError(w, http.StatusForbidden, "forbidden_origin")
			return
		}

		id := chi.URLParam(r, "sessionID")
		session, ok := sessionFromContext(r.Context())
		cookie, cookieErr := r.Cookie(SessionCookieName)
		if !ok || cookieErr != nil {
			writeError(w, http.StatusUnauthorized, "session_required")
			return
		}
		live := deps.Terminals.Get(id)
		if live == nil {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}

		// The origin was checked above, so the library does not check it again.
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		conn.SetReadLimit(maxSocketMessage)

		if !readTicket(r.Context(), conn, deps, id, session.ID) {
			return
		}
		deps.pump(r.Context(), conn, live, session.ID, auth.HashToken(cookie.Value))
	})
}

// readTicket waits for the first message and redeems the ticket in it.
func readTicket(ctx context.Context, conn *websocket.Conn, deps *TerminalDeps, terminalID, panelSession string) bool {
	ctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	kind, data, err := conn.Read(ctx)
	var first socketControl
	if err != nil || kind != websocket.MessageText || json.Unmarshal(data, &first) != nil ||
		first.Type != "ticket" || !deps.tickets.redeem(first.Ticket, terminalID, panelSession) {
		_ = conn.Close(closeBadTicket, "ticket")
		return false
	}
	return true
}

// pump carries the session between the browser and the core until either ends.
func (deps *TerminalDeps) pump(ctx context.Context, conn *websocket.Conn, s *terminal.Session, panelSession string, tokenHash []byte) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// The browser's messages are read on their own goroutine.
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		deps.readBrowser(ctx, conn, s)
	}()

	check := time.NewTicker(deps.CheckInterval)
	defer check.Stop()

	for {
		select {
		case data, open := <-s.Output():
			if !open {
				deps.finish(ctx, conn, s)
				<-readDone
				return
			}
			writeCtx, stop := context.WithTimeout(ctx, writeTimeout)
			err := conn.Write(writeCtx, websocket.MessageBinary, data)
			stop()
			if err != nil {
				s.Close(terminal.ReasonSlowClient)
				return
			}

		case <-check.C:
			if !deps.panelSessionAlive(ctx, panelSession, tokenHash) {
				s.Close("session_revoked")
				_ = conn.Close(closeSessionRevoked, "session")
				return
			}

		case <-readDone:
			// The browser went away (or broke the protocol): the shell goes too.
			s.Close(terminal.ReasonClosed)
			return
		}
	}
}

// panelSessionAlive reports whether the panel session that opened the terminal
// is still valid, and keeps it from idling out while the terminal is in use.
func (deps *TerminalDeps) panelSessionAlive(ctx context.Context, panelSession string, tokenHash []byte) bool {
	current, err := deps.Sessions.GetSessionByTokenHash(ctx, tokenHash)
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		// A store failure is not a reason to cut a working terminal.
		slog.Warn("check the session of a terminal", "error", err)
		return true
	}
	if current.ID != panelSession || current.Stage != store.StageFull {
		return false
	}
	if err := deps.Sessions.TouchSession(ctx, current.ID, deps.Now()); err != nil {
		slog.Warn("touch the session of a terminal", "error", err)
	}
	return true
}

// readBrowser passes keystrokes and resizes to the shell until the browser stops.
func (deps *TerminalDeps) readBrowser(ctx context.Context, conn *websocket.Conn, s *terminal.Session) {
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			return
		}

		switch kind {
		case websocket.MessageBinary:
			if err := s.Input(ctx, data); err != nil {
				if errors.Is(err, terminal.ErrInputTooLarge) {
					_ = conn.Close(closeProtocolProblem, "input too large")
				}
				return
			}
		case websocket.MessageText:
			var control socketControl
			if json.Unmarshal(data, &control) != nil || control.Type != "resize" {
				_ = conn.Close(closeProtocolProblem, "unknown message")
				return
			}
			if err := s.Resize(ctx, control.Cols, control.Rows); err != nil {
				if errors.Is(err, terminal.ErrInvalidRequest) {
					_ = conn.Close(closeProtocolProblem, "invalid size")
				}
				return
			}
		}
	}
}

// finish tells the browser why the session ended and closes the socket.
func (deps *TerminalDeps) finish(ctx context.Context, conn *websocket.Conn, s *terminal.Session) {
	message, _ := json.Marshal(map[string]any{"type": "closed", "reason": s.Reason(), "exit_code": s.ExitCode()})
	writeCtx, stop := context.WithTimeout(ctx, writeTimeout)
	defer stop()
	_ = conn.Write(writeCtx, websocket.MessageText, message)
	_ = conn.Close(websocket.StatusNormalClosure, "")
}

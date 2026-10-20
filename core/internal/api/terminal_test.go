package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/terminal"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// fakeTerminalAgent records what the core sends to the agents.
type fakeTerminalAgent struct {
	mu   sync.Mutex
	sent []*agentv1.CoreMsg
}

func (f *fakeTerminalAgent) DispatchWait(_ context.Context, _ string, msg *agentv1.CoreMsg) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeTerminalAgent) messages() []*agentv1.CoreMsg {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*agentv1.CoreMsg(nil), f.sent...)
}

// openedSessionID returns the id of the last terminal the core asked to open.
func (f *fakeTerminalAgent) openedSessionID() string {
	id := ""
	for _, msg := range f.messages() {
		if open := msg.GetTerminalOpen(); open != nil {
			id = open.GetSessionId()
		}
	}
	return id
}

func (f *fakeTerminalAgent) kinds() []string {
	var kinds []string
	for _, msg := range f.messages() {
		switch {
		case msg.GetTerminalOpen() != nil:
			kinds = append(kinds, "open")
		case msg.GetTerminalInput() != nil:
			kinds = append(kinds, "input:"+string(msg.GetTerminalInput().GetData()))
		case msg.GetTerminalResize() != nil:
			kinds = append(kinds, "resize")
		case msg.GetTerminalClose() != nil:
			kinds = append(kinds, "close")
		}
	}
	return kinds
}

func terminalPath(serverID, container string) string {
	return "/api/v1/servers/" + serverID + "/containers/" + container + "/terminal"
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

// terminalRig is a panel with a signed-in administrator and a connected agent.
type terminalRig struct {
	panel    *testPanel
	serverID string
}

func newTerminalRig(t *testing.T, options ...panelOption) *terminalRig {
	t.Helper()
	panel := newTestPanel(t, options...)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)
	serverID := enrollTestServer(t, panel)
	panel.agents.set(agentWithContainers(serverID))
	// Sessions store their recording when they end: let them finish before the
	// store is closed.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		panel.terminals.Shutdown(ctx)
	})
	return &terminalRig{panel: panel, serverID: serverID}
}

// open asks for a terminal and plays the agent that starts the shell.
func (r *terminalRig) open(t *testing.T, client *http.Client, container string, body map[string]any) (*http.Response, string) {
	t.Helper()
	before := len(r.panel.terminalAgent.messages())
	type result struct{ response *http.Response }
	done := make(chan result, 1)
	go func() {
		done <- result{r.panel.do(t, client, http.MethodPost, terminalPath(r.serverID, container), body)}
	}()

	// Answer only when the core asked for a shell; a refusal answers by itself.
	id := ""
	var finished *http.Response
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && id == "" && finished == nil {
		select {
		case got := <-done:
			finished = got.response
		case <-time.After(5 * time.Millisecond):
			if len(r.panel.terminalAgent.messages()) > before {
				id = r.panel.terminalAgent.openedSessionID()
			}
		}
	}
	if finished != nil {
		return finished, id
	}
	if id != "" {
		r.panel.terminals.TerminalOpened(r.serverID, &agentv1.TerminalOpened{SessionId: id, Privileged: true})
	}
	return (<-done).response, id
}

func (r *terminalRig) openOK(t *testing.T) terminalOpenResponse {
	t.Helper()
	response, _ := r.open(t, r.panel.client, "web", map[string]any{"confirm": true, "shell": "sh", "cols": 90, "rows": 30})
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", response.StatusCode)
	}
	var opened terminalOpenResponse
	decodeBody(t, response, &opened)
	if opened.SessionID == "" || opened.Ticket == "" || !opened.Privileged {
		t.Fatalf("answer = %+v", opened)
	}
	return opened
}

func (r *terminalRig) dial(t *testing.T, client *http.Client, sessionID, origin string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return websocket.Dial(ctx, r.panel.server.URL+"/api/v1/terminal/"+sessionID+"/ws", &websocket.DialOptions{HTTPClient: client, HTTPHeader: header})
}

// attach opens the WebSocket and presents the ticket.
func (r *terminalRig) attach(t *testing.T, opened terminalOpenResponse) *websocket.Conn {
	t.Helper()
	conn, _, err := r.dial(t, r.panel.client, opened.SessionID, testOrigin)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	sendTicket(t, conn, opened.Ticket)
	return conn
}

func sendTicket(t *testing.T, conn *websocket.Conn, ticket string) {
	t.Helper()
	message, _ := json.Marshal(socketControl{Type: "ticket", Ticket: ticket})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, message); err != nil {
		t.Fatalf("send the ticket: %v", err)
	}
}

func read(t *testing.T, conn *websocket.Conn) (websocket.MessageType, []byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return conn.Read(ctx)
}

func TestOpenTerminalNeedsAFullSessionAndConfirmation(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	if got := panel.post(t, terminalPath("x", "web"), map[string]any{"confirm": true, "shell": "sh"}).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("without a session = %d, want 401", got)
	}

	r := newTerminalRig(t)
	for name, test := range map[string]struct {
		container string
		body      map[string]any
		want      int
	}{
		"no confirmation":   {"web", map[string]any{"shell": "sh"}, http.StatusBadRequest},
		"unknown shell":     {"web", map[string]any{"confirm": true, "shell": "zsh"}, http.StatusBadRequest},
		"unknown container": {"ghost", map[string]any{"confirm": true, "shell": "sh"}, http.StatusNotFound},
	} {
		response, id := r.open(t, r.panel.client, test.container, test.body)
		if response.StatusCode != test.want {
			t.Errorf("%s: status = %d, want %d", name, response.StatusCode, test.want)
		}
		if id != "" {
			t.Errorf("%s: the core asked the agent for a shell", name)
		}
	}

	r.panel.agents.set()
	response, _ := r.open(t, r.panel.client, "web", map[string]any{"confirm": true, "shell": "sh"})
	if response.StatusCode != http.StatusConflict {
		t.Errorf("offline agent = %d, want 409", response.StatusCode)
	}
}

func TestTerminalCarriesKeystrokesAndOutput(t *testing.T) {
	r := newTerminalRig(t)
	opened := r.openOK(t)
	conn := r.attach(t, opened)

	// The shell prints, the browser receives binary frames.
	r.panel.terminals.TerminalOutput(r.serverID, &agentv1.TerminalOutput{SessionId: opened.SessionID, Data: []byte("prompt$ ")})
	kind, data, err := read(t, conn)
	if err != nil || kind != websocket.MessageBinary || string(data) != "prompt$ " {
		t.Fatalf("output = %v %q %v", kind, data, err)
	}

	// Binary frames are keystrokes; a text frame resizes.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageBinary, []byte("ls\n")); err != nil {
		t.Fatalf("write keystrokes: %v", err)
	}
	resize, _ := json.Marshal(socketControl{Type: "resize", Cols: 120, Rows: 40})
	if err := conn.Write(ctx, websocket.MessageText, resize); err != nil {
		t.Fatalf("write resize: %v", err)
	}
	eventually(t, func() bool { return strings.Join(r.panel.terminalAgent.kinds(), ",") == "open,input:ls\n,resize" })

	// When the shell exits the browser is told why, and the socket closes.
	r.panel.terminals.TerminalClosed(r.serverID, &agentv1.TerminalClosed{SessionId: opened.SessionID, Reason: "exited", ExitCode: 2})
	kind, data, err = read(t, conn)
	if err != nil || kind != websocket.MessageText {
		t.Fatalf("closing message = %v %q %v", kind, data, err)
	}
	var closed struct {
		Type     string `json:"type"`
		Reason   string `json:"reason"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal(data, &closed); err != nil || closed.Type != "closed" || closed.Reason != "exited" || closed.ExitCode != 2 {
		t.Errorf("closing message = %s (%v)", data, err)
	}
	if _, _, err := read(t, conn); websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Errorf("socket ended with %v", err)
	}
}

func TestTerminalRefusesWhatIsNotAnAllowedBrowser(t *testing.T) {
	r := newTerminalRig(t)
	opened := r.openOK(t)

	// Another site, no origin, and a client without the session cookie.
	if _, response, err := r.dial(t, r.panel.client, opened.SessionID, "https://evil.example"); err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin: %v %v", response, err)
	}
	if _, response, err := r.dial(t, r.panel.client, opened.SessionID, ""); err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Errorf("no origin: %v %v", response, err)
	}
	if _, response, err := r.dial(t, r.panel.rawClient(), opened.SessionID, testOrigin); err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Errorf("no cookie: %v %v", response, err)
	}
	if _, response, err := r.dial(t, r.panel.client, "00000000-0000-4000-8000-000000000000", testOrigin); err == nil || response == nil || response.StatusCode != http.StatusNotFound {
		t.Errorf("unknown session: %v %v", response, err)
	}

	// None of that touched the shell: the right browser still gets in.
	conn := r.attach(t, opened)
	r.panel.terminals.TerminalOutput(r.serverID, &agentv1.TerminalOutput{SessionId: opened.SessionID, Data: []byte("ok")})
	if _, data, err := read(t, conn); err != nil || string(data) != "ok" {
		t.Errorf("after the refusals = %q, %v", data, err)
	}
}

func TestTicketIsSingleUseAndBoundToItsSession(t *testing.T) {
	r := newTerminalRig(t)
	opened := r.openOK(t)

	// A wrong ticket closes the socket with the ticket code and keeps the shell.
	conn, _, err := r.dial(t, r.panel.client, opened.SessionID, testOrigin)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	sendTicket(t, conn, "not-the-ticket")
	if _, _, err := read(t, conn); websocket.CloseStatus(err) != closeBadTicket {
		t.Errorf("wrong ticket ended with %v", err)
	}

	// The real ticket works once.
	good := r.attach(t, opened)
	r.panel.terminals.TerminalOutput(r.serverID, &agentv1.TerminalOutput{SessionId: opened.SessionID, Data: []byte("x")})
	if _, _, err := read(t, good); err != nil {
		t.Fatalf("first use: %v", err)
	}
	reuse, _, err := r.dial(t, r.panel.client, opened.SessionID, testOrigin)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	sendTicket(t, reuse, opened.Ticket)
	if _, _, err := read(t, reuse); websocket.CloseStatus(err) != closeBadTicket {
		t.Errorf("a used ticket ended with %v", err)
	}

	// Not a text message with a ticket: also refused.
	silent, _, err := r.dial(t, r.panel.client, opened.SessionID, testOrigin)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := silent.Write(context.Background(), websocket.MessageBinary, []byte("ls\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := read(t, silent); websocket.CloseStatus(err) != closeBadTicket {
		t.Errorf("keystrokes before the ticket ended with %v", err)
	}
}

func TestTicketRedemptionRules(t *testing.T) {
	now := time.Now()
	book := newTicketBook(func() time.Time { return now }, ticketTTL)
	add := func(token string, expires time.Time) {
		book.tickets[sha256.Sum256([]byte(token))] = ticket{terminalID: "term-1", panelSession: "panel-1", expires: expires}
	}

	add("good", now.Add(time.Second))
	if book.redeem("good", "term-2", "panel-1") {
		t.Error("a ticket worked for another terminal session")
	}
	// The failed attempt burned it: it is single use whoever tries.
	if book.redeem("good", "term-1", "panel-1") {
		t.Error("a ticket survived a failed redemption")
	}

	add("other-panel", now.Add(time.Second))
	if book.redeem("other-panel", "term-1", "panel-2") {
		t.Error("a ticket worked from another panel session")
	}

	add("late", now.Add(-time.Second))
	if book.redeem("late", "term-1", "panel-1") {
		t.Error("an expired ticket worked")
	}

	add("fine", now.Add(time.Second))
	if !book.redeem("fine", "term-1", "panel-1") || book.redeem("fine", "term-1", "panel-1") {
		t.Error("a valid ticket must work exactly once")
	}
	if book.redeem("", "term-1", "panel-1") || book.redeem("unknown", "term-1", "panel-1") {
		t.Error("an unknown ticket worked")
	}
}

func TestUnredeemedTicketClosesTheShell(t *testing.T) {
	r := newTerminalRig(t, withTicketTTL(50*time.Millisecond))
	opened := r.openOK(t)

	eventually(t, func() bool { return r.panel.terminals.Get(opened.SessionID) == nil })
	eventually(t, func() bool {
		kinds := r.panel.terminalAgent.kinds()
		return len(kinds) > 0 && kinds[len(kinds)-1] == "close"
	})
	// Too late: the ticket is gone.
	conn, _, err := r.dial(t, r.panel.client, opened.SessionID, testOrigin)
	if err == nil {
		sendTicket(t, conn, opened.Ticket)
		if _, _, err := read(t, conn); err == nil {
			t.Error("an expired ticket was accepted")
		}
	}
}

func TestClosingTheBrowserClosesTheShell(t *testing.T) {
	r := newTerminalRig(t)
	opened := r.openOK(t)
	conn := r.attach(t, opened)
	r.panel.terminals.TerminalOutput(r.serverID, &agentv1.TerminalOutput{SessionId: opened.SessionID, Data: []byte("x")})
	if _, _, err := read(t, conn); err != nil {
		t.Fatalf("read: %v", err)
	}

	_ = conn.Close(websocket.StatusNormalClosure, "")
	eventually(t, func() bool { return r.panel.terminals.Get(opened.SessionID) == nil })
	eventually(t, func() bool {
		kinds := r.panel.terminalAgent.kinds()
		return kinds[len(kinds)-1] == "close"
	})
}

func TestRevokedPanelSessionClosesTheTerminal(t *testing.T) {
	r := newTerminalRig(t)
	opened := r.openOK(t)
	conn := r.attach(t, opened)

	user, err := r.panel.store.GetUserByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.panel.store.RevokeUserSessions(context.Background(), user.ID, ""); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, _, err := read(t, conn); websocket.CloseStatus(err) != closeSessionRevoked {
		t.Errorf("revoked session ended with %v", err)
	}
	eventually(t, func() bool { return r.panel.terminals.Get(opened.SessionID) == nil })
}

func TestHugeKeystrokesCloseTheSocket(t *testing.T) {
	r := newTerminalRig(t)
	opened := r.openOK(t)
	conn := r.attach(t, opened)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Over the 4 KiB of a message but under the limit of the socket.
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 5000)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, err := read(t, conn); websocket.CloseStatus(err) != closeProtocolProblem {
		t.Errorf("huge input ended with %v", err)
	}
	eventually(t, func() bool { return r.panel.terminals.Get(opened.SessionID) == nil })
}

func TestTerminalErrorMapping(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
		code   string
	}{
		{terminal.ErrInvalidRequest, 400, "invalid_request"},
		{terminal.ErrTooManySessions, 409, "too_many_terminals"},
		{terminal.ErrSessionExists, 409, "terminal_exists"},
		{hub.ErrNotConnected, 409, "agent_offline"},
		{hub.ErrAgentBusy, 503, "agent_busy"},
		{&terminal.OpenError{Message: "x"}, 502, "terminal_open_failed"},
	} {
		if status, code := terminalError(test.err); status != test.status || code != test.code {
			t.Errorf("%v = %d %s, want %d %s", test.err, status, code, test.status, test.code)
		}
	}
}

// withTicketTTL shortens the life of the tickets.
func withTicketTTL(d time.Duration) panelOption {
	return func(options *testPanelOptions) { options.ticketTTL = d }
}

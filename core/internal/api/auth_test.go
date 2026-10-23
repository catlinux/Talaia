package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/catlinux/Talaia/core/internal/terminal"
)

// testOrigin is the panel origin the test server allows.
const testOrigin = "https://panel.example.com"

// testPassword is a password that satisfies the policy.
const testPassword = "correct horse battery staple"

// testStore opens the test database inside a schema of its own. The store
// package tests use the same database and this phase allows a single user, so a
// private schema keeps one suite from deleting the other's user.
func testStore(t *testing.T) *store.Store {
	t.Helper()

	databaseURL := os.Getenv("TALAIA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TALAIA_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	schema := fmt.Sprintf("test_api_%d", time.Now().UnixNano())

	bootstrap, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to the test database: %v", err)
	}
	if _, err := bootstrap.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		_ = bootstrap.Close(ctx)
		t.Fatalf("create the test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = bootstrap.Exec(cleanupCtx, `DROP SCHEMA `+schema+` CASCADE`)
		_ = bootstrap.Close(cleanupCtx)
	})

	st, err := store.Open(ctx, schemaURL(databaseURL, schema))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

// schemaURL returns the database URL with search_path pointed at schema. pgx
// passes unknown URL parameters to the server as runtime parameters.
func schemaURL(databaseURL, schema string) string {
	separator := "?"
	if strings.Contains(databaseURL, "?") {
		separator = "&"
	}
	return databaseURL + separator + "search_path=" + url.QueryEscape(schema)
}

// testClock is a clock the tests can move.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Now()}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// testPanel is the core with the panel API and the real store.
type testPanel struct {
	server *httptest.Server
	store  *store.Store
	clock  *testClock
	client *http.Client
	// agents and dispatcher stand for the connected agents of the hub.
	agents     *controlledAgents
	dispatcher *fakeDispatcher
	// terminals runs on the real store; terminalAgent stands for the agents it
	// talks to.
	terminals     *terminal.Manager
	terminalAgent *fakeTerminalAgent
	// box lets tests create recording fixtures.
	box *auth.SecretBox
}

// testPanelOptions tweak the test panel.
type testPanelOptions struct {
	limiter      *RateLimiter
	lockDuration time.Duration
	idleTimeout  time.Duration
	webDir       string
	ticketTTL    time.Duration
}

// panelOption tweaks the test panel.
type panelOption func(*testPanelOptions)

// withLimiter replaces the rate limiter of the panel.
func withLimiter(limiter *RateLimiter) panelOption {
	return func(options *testPanelOptions) { options.limiter = limiter }
}

// withLockDuration changes how long a user stays locked.
func withLockDuration(d time.Duration) panelOption {
	return func(options *testPanelOptions) { options.lockDuration = d }
}

// newTestPanel starts the core over TLS with the authentication API, the
// session middleware and the security middleware.
func newTestPanel(t *testing.T, options ...panelOption) *testPanel {
	t.Helper()

	settings := testPanelOptions{
		lockDuration: time.Minute,
		idleTimeout:  12 * time.Hour,
		webDir:       t.TempDir(),
	}
	for _, option := range options {
		option(&settings)
	}

	st := testStore(t)
	clock := newTestClock()
	if settings.limiter == nil {
		settings.limiter = NewRateLimiterWithClock(1000, time.Minute, clock.Now)
	}

	key, err := auth.LoadOrCreateKey(filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatalf("secrets key: %v", err)
	}
	box, err := auth.NewSecretBox(key)
	if err != nil {
		t.Fatalf("secret box: %v", err)
	}

	agents := &controlledAgents{}
	dispatcher := &fakeDispatcher{}
	terminalAgent := &fakeTerminalAgent{}
	terminalConfig := terminal.DefaultConfig()
	terminalConfig.OpenTimeout = 2 * time.Second
	terminalConfig.FlushEvery = 20 * time.Millisecond
	terminals := terminal.New(st, terminalAgent, st, box, terminalConfig)
	recordings := &RecordingDeps{Recordings: st, Opener: box, Servers: st, Audit: st}
	router := NewRouter(RouterDeps{
		DB:                 st,
		WebDir:             settings.webDir,
		Security:           SecurityConfig{TLS: true, Origins: []string{testOrigin}},
		Session:            SessionDeps{Store: st, Now: clock.Now, IdleTimeout: settings.idleTimeout},
		Servers:            st,
		Agents:             agents,
		ServerRemover:      st,
		ServerDisconnector: &fakeDisconnect{},
		ServerAudit:        st,
		AuditLog:           st,
		AgentUpdates: &AgentUpdateDeps{
			Agents:     agents,
			Dispatcher: dispatcher,
			Jobs:       st,
			Audit:      st,
			Servers:    st,
		},
		System: &SystemDeps{},
		Authz: NewAuthorizer(AuthorizerDeps{
			Bindings: st,
			Servers:  st,
			Agents:   agents,
			Audit:    st,
		}),
		Terminal: NewTerminalDeps(TerminalDeps{
			Agents: agents, Terminals: terminals, Sessions: st, Origins: []string{testOrigin},
			CheckInterval: 50 * time.Millisecond, TicketTTL: settings.ticketTTL,
		}),
		Recordings: recordings,
		Containers: &ContainerDeps{
			Agents:     agents,
			Dispatcher: dispatcher,
			Jobs:       st,
			Audit:      st,
			Servers:    st,
		},
		Enrollment: &EnrollmentTokenDeps{
			Tokens:        st,
			Audit:         st,
			CAFingerprint: testFingerprint,
			PublicURL:     "https://192.0.2.10:8443",
			ScriptSHA256:  testScriptHash,
		},
		Auth: AuthDeps{
			Store:        st,
			Box:          box,
			Limiter:      settings.limiter,
			Now:          clock.Now,
			LockDuration: settings.lockDuration,
		},
	})
	server := httptest.NewTLSServer(router)
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &testPanel{
		server:     server,
		store:      st,
		clock:      clock,
		client:     &http.Client{Transport: server.Client().Transport, Jar: jar},
		agents:     agents,
		dispatcher: dispatcher,

		terminals:     terminals,
		terminalAgent: terminalAgent,
		box:           box,
	}
}

// newClient returns a client with its own cookie jar.
func (p *testPanel) newClient(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	// httptest returns the same client every time, so build a new one: the jar
	// must not be shared between test clients.
	return &http.Client{Transport: p.server.Client().Transport, Jar: jar}
}

// rawClient returns a client that keeps no cookies.
func (p *testPanel) rawClient() *http.Client {
	return &http.Client{Transport: p.server.Client().Transport}
}

// do sends a request the way the panel does: JSON and the allowed origin.
func (p *testPanel) do(t *testing.T, client *http.Client, method, path string, body any) *http.Response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal the body of %s: %v", path, err)
		}
		reader = bytes.NewReader(payload)
	}

	request, err := http.NewRequest(method, p.server.URL+path, reader)
	if err != nil {
		t.Fatalf("build %s: %v", path, err)
	}
	if method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("Origin", testOrigin)

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func (p *testPanel) post(t *testing.T, path string, body any) *http.Response {
	t.Helper()
	return p.do(t, p.client, http.MethodPost, path, body)
}

// decodeBody decodes the JSON body of a response.
func decodeBody(t *testing.T, response *http.Response, target any) {
	t.Helper()

	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode the body of %s: %v", response.Request.URL.Path, err)
	}
}

// createUser creates the administrator.
func (p *testPanel) createUser(t *testing.T, username, password string) store.User {
	t.Helper()

	ctx := context.Background()
	id, err := p.store.CreateUser(ctx, username, auth.HashPassword(password))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	user, err := p.store.GetUser(ctx, id)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	return user
}

// login signs in with the password and returns the next step.
func (p *testPanel) login(t *testing.T, username, password string) string {
	t.Helper()

	response := p.post(t, "/api/v1/auth/login", map[string]string{"username": username, "password": password})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", response.StatusCode)
	}
	var body loginResponse
	decodeBody(t, response, &body)
	return body.Next
}

// code returns the TOTP code of the current step.
func (p *testPanel) code(secret []byte) string {
	return auth.Code(secret, auth.Step(p.clock.Now()))
}

// enableTOTP runs the first-access flow, leaves a full session and returns the
// secret and the recovery codes.
func (p *testPanel) enableTOTP(t *testing.T, username, password string) ([]byte, []string) {
	t.Helper()

	if next := p.login(t, username, password); next != "totp_setup" {
		t.Fatalf("next = %q, want totp_setup", next)
	}

	var setup totpSetupResponse
	decodeBody(t, p.post(t, "/api/v1/auth/totp/setup", nil), &setup)
	if !strings.HasPrefix(setup.URI, "otpauth://totp/Talaia:"+username+"?") {
		t.Errorf("uri = %q, want the Talaia provisioning URI", setup.URI)
	}
	secret, err := auth.DecodeBase32(setup.Secret)
	if err != nil {
		t.Fatalf("decode the secret: %v", err)
	}

	var confirm recoveryCodesResponse
	decodeBody(t, p.post(t, "/api/v1/auth/totp/confirm", map[string]string{"code": p.code(secret)}), &confirm)
	if len(confirm.RecoveryCodes) != recoveryCodeCount {
		t.Fatalf("recovery codes = %d, want %d", len(confirm.RecoveryCodes), recoveryCodeCount)
	}

	// The next code has to belong to a later step.
	p.clock.Advance(30 * time.Second)
	return secret, confirm.RecoveryCodes
}

// loginTOTP completes the second factor with a fresh code.
func (p *testPanel) loginTOTP(t *testing.T, secret []byte) {
	t.Helper()

	response := p.post(t, "/api/v1/auth/totp", map[string]string{"code": p.code(secret)})
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("totp status = %d, want 204", response.StatusCode)
	}
	p.clock.Advance(30 * time.Second)
}

// stage returns the stage of the session of a client, or "" without a session.
func (p *testPanel) stage(t *testing.T, client *http.Client) string {
	t.Helper()

	response := p.do(t, client, http.MethodGet, "/api/v1/auth/session", nil)
	if response.StatusCode != http.StatusOK {
		return ""
	}
	var body sessionResponse
	decodeBody(t, response, &body)
	return body.Stage
}

// logout signs a client out.
func (p *testPanel) logout(t *testing.T, client *http.Client) {
	t.Helper()

	if response := p.do(t, client, http.MethodPost, "/api/v1/auth/logout", nil); response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", response.StatusCode)
	}
}

// sessionCookieValue returns the session token a client holds.
func (p *testPanel) sessionCookieValue(t *testing.T, client *http.Client) string {
	t.Helper()

	parsed, err := url.Parse(p.server.URL)
	if err != nil {
		t.Fatalf("parse the server URL: %v", err)
	}
	if client.Jar == nil {
		return ""
	}
	for _, cookie := range client.Jar.Cookies(parsed) {
		if cookie.Name == SessionCookieName {
			return cookie.Value
		}
	}
	return ""
}

func TestFullAuthenticationFlow(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)

	// 1. The password opens a pending session and asks for the first access.
	response := panel.post(t, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", response.StatusCode)
	}
	var login loginResponse
	decodeBody(t, response, &login)
	if login.Next != "totp_setup" {
		t.Fatalf("next = %q, want totp_setup", login.Next)
	}
	pending := panel.sessionCookieValue(t, panel.client)
	if pending == "" {
		t.Fatal("the login did not set a session cookie")
	}
	if cache := response.Header.Get("Cache-Control"); cache != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cache)
	}

	// 2. The pending session knows its stage.
	if stage := panel.stage(t, panel.client); stage != store.StageMFAPending {
		t.Fatalf("stage = %q, want %q", stage, store.StageMFAPending)
	}

	// 3. Setup returns the secret and the provisioning URI.
	var setup totpSetupResponse
	decodeBody(t, panel.post(t, "/api/v1/auth/totp/setup", nil), &setup)
	if setup.Secret == "" || !strings.HasPrefix(setup.URI, "otpauth://totp/") {
		t.Fatalf("setup = %+v", setup)
	}
	secret, err := auth.DecodeBase32(setup.Secret)
	if err != nil {
		t.Fatalf("decode the secret: %v", err)
	}

	// 4. Confirming the code enables the second factor and returns the codes.
	var confirm recoveryCodesResponse
	decodeBody(t, panel.post(t, "/api/v1/auth/totp/confirm", map[string]string{"code": panel.code(secret)}), &confirm)
	if len(confirm.RecoveryCodes) != recoveryCodeCount {
		t.Fatalf("recovery codes = %d, want %d", len(confirm.RecoveryCodes), recoveryCodeCount)
	}

	// 5. The session is full and its token changed.
	full := panel.sessionCookieValue(t, panel.client)
	if full == "" {
		t.Fatal("the confirmation did not set a session cookie")
	}
	if full == pending {
		t.Error("the session token was not rotated when it became full")
	}
	if stage := panel.stage(t, panel.client); stage != store.StageFull {
		t.Fatalf("stage = %q, want %q", stage, store.StageFull)
	}

	// 6. The old pending token no longer works.
	request, err := http.NewRequest(http.MethodGet, panel.server.URL+"/api/v1/auth/session", nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: pending})
	stale, err := panel.rawClient().Do(request)
	if err != nil {
		t.Fatalf("send the old token: %v", err)
	}
	defer func() { _ = stale.Body.Close() }()
	if stale.StatusCode != http.StatusUnauthorized {
		t.Errorf("the old session token was accepted: %d", stale.StatusCode)
	}

	// 7. Logout revokes the session.
	panel.logout(t, panel.client)
	if stage := panel.stage(t, panel.client); stage != "" {
		t.Errorf("the session survived the logout: %q", stage)
	}
}

func TestSecondLoginUsesTOTP(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	secret, _ := panel.enableTOTP(t, "admin", testPassword)
	panel.logout(t, panel.client)

	if next := panel.login(t, "admin", testPassword); next != "totp" {
		t.Fatalf("next = %q, want totp", next)
	}

	// A code from far away is rejected and counts as a failed attempt.
	wrong := auth.Code(secret, auth.Step(panel.clock.Now())+100)
	if response := panel.post(t, "/api/v1/auth/totp", map[string]string{"code": wrong}); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong code status = %d, want 401", response.StatusCode)
	}

	// The right code completes the login.
	panel.loginTOTP(t, secret)
	if stage := panel.stage(t, panel.client); stage != store.StageFull {
		t.Fatalf("stage = %q, want %q", stage, store.StageFull)
	}
}

func TestTOTPCodeCannotBeReplayed(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	secret, _ := panel.enableTOTP(t, "admin", testPassword)
	panel.logout(t, panel.client)

	// Use a code, then try it again in another login.
	panel.login(t, "admin", testPassword)
	code := panel.code(secret)
	if response := panel.post(t, "/api/v1/auth/totp", map[string]string{"code": code}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("first use status = %d, want 204", response.StatusCode)
	}
	panel.logout(t, panel.client)

	panel.login(t, "admin", testPassword)
	if response := panel.post(t, "/api/v1/auth/totp", map[string]string{"code": code}); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the replayed code status = %d, want 401", response.StatusCode)
	}
}

func TestRecoveryCodesAreSingleUse(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	_, codes := panel.enableTOTP(t, "admin", testPassword)
	panel.logout(t, panel.client)

	// The first code completes a login.
	panel.login(t, "admin", testPassword)
	if response := panel.post(t, "/api/v1/auth/totp", map[string]string{"recovery_code": codes[0]}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("recovery code status = %d, want 204", response.StatusCode)
	}
	panel.logout(t, panel.client)

	// The same code is spent.
	panel.login(t, "admin", testPassword)
	if response := panel.post(t, "/api/v1/auth/totp", map[string]string{"recovery_code": codes[0]}); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the reused code status = %d, want 401", response.StatusCode)
	}

	// Another code works, however it is typed.
	typographical := strings.ToLower(strings.ReplaceAll(codes[1], "-", " "))
	if response := panel.post(t, "/api/v1/auth/totp", map[string]string{"recovery_code": typographical}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("lower-case recovery code status = %d, want 204", response.StatusCode)
	}
}

func TestWrongPasswordsLockTheUser(t *testing.T) {
	panel := newTestPanel(t, withLockDuration(10*time.Minute))
	panel.createUser(t, "admin", testPassword)

	login := func(password string) int {
		return panel.post(t, "/api/v1/auth/login", map[string]string{"username": "admin", "password": password}).StatusCode
	}

	for attempt := 1; attempt <= 5; attempt++ {
		if status := login("a wrong password"); status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", attempt, status)
		}
	}

	// Even the right password is refused while the lock lasts.
	if status := login(testPassword); status != http.StatusUnauthorized {
		t.Fatalf("the right password was accepted during the lock: %d", status)
	}

	// Once the lock expires it works again.
	panel.clock.Advance(11 * time.Minute)
	if status := login(testPassword); status != http.StatusOK {
		t.Fatalf("status after the lock = %d, want 200", status)
	}
}

func TestFiveWrongCodesRevokeThePendingSession(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	secret, _ := panel.enableTOTP(t, "admin", testPassword)
	panel.logout(t, panel.client)

	panel.login(t, "admin", testPassword)
	wrong := auth.Code(secret, auth.Step(panel.clock.Now())+100)
	for attempt := 1; attempt <= 5; attempt++ {
		if response := panel.post(t, "/api/v1/auth/totp", map[string]string{"code": wrong}); response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", attempt, response.StatusCode)
		}
	}

	// The pending session is gone, so even the right code cannot be used.
	panel.clock.Advance(30 * time.Second)
	if response := panel.post(t, "/api/v1/auth/totp", map[string]string{"code": panel.code(secret)}); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status after the session was revoked = %d, want 401", response.StatusCode)
	}
	if stage := panel.stage(t, panel.client); stage != "" {
		t.Errorf("the pending session survived: %q", stage)
	}
}

func TestIdleSessionsAreRejected(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	if stage := panel.stage(t, panel.client); stage != store.StageFull {
		t.Fatalf("stage = %q, want %q", stage, store.StageFull)
	}

	panel.clock.Advance(12 * time.Hour)
	if stage := panel.stage(t, panel.client); stage != "" {
		t.Errorf("an idle session was accepted: %q", stage)
	}
}

func TestPasswordChangeRevokesTheOtherSessions(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)
	secret, _ := panel.enableTOTP(t, "admin", testPassword)

	// A second full session for the same user.
	other := panel.newClient(t)
	_ = panel.do(t, other, http.MethodPost, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	if response := panel.do(t, other, http.MethodPost, "/api/v1/auth/totp", map[string]string{"code": panel.code(secret)}); response.StatusCode != http.StatusNoContent {
		t.Fatalf("second login status = %d, want 204", response.StatusCode)
	}
	panel.clock.Advance(30 * time.Second)

	// The wrong current password is refused.
	change := func(current, next, code string) int {
		return panel.post(t, "/api/v1/auth/password", passwordRequest{
			CurrentPassword: current,
			NewPassword:     next,
			Code:            code,
		}).StatusCode
	}
	code := panel.code(secret)
	if status := change("a wrong password", "another good password", code); status != http.StatusUnauthorized {
		t.Fatalf("wrong current password status = %d, want 401", status)
	}

	// A weak new password is refused before the code is spent.
	if status := change(testPassword, "short", code); status != http.StatusBadRequest {
		t.Fatalf("weak password status = %d, want 400", status)
	}

	// The real change consumes the code.
	if status := change(testPassword, "a much better password", code); status != http.StatusNoContent {
		t.Fatalf("password change status = %d, want 204", status)
	}

	// The current session survives, the other one is revoked.
	if stage := panel.stage(t, panel.client); stage != store.StageFull {
		t.Errorf("the current session was revoked: %q", stage)
	}
	if stage := panel.stage(t, other); stage != "" {
		t.Errorf("the other session survived the password change: %q", stage)
	}

	// The new password works and the old one does not.
	panel.logout(t, panel.client)
	if status := panel.post(t, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword}).StatusCode; status != http.StatusUnauthorized {
		t.Errorf("the old password was accepted: %d", status)
	}
	if status := panel.post(t, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "a much better password"}).StatusCode; status != http.StatusOK {
		t.Errorf("the new password was refused: %d", status)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	panel := newTestPanel(t, withLimiter(NewRateLimiterWithClock(2, time.Minute, time.Now)))
	panel.createUser(t, "admin", testPassword)

	for attempt := 1; attempt <= 2; attempt++ {
		if status := panel.post(t, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"}).StatusCode; status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", attempt, status)
		}
	}

	response := panel.post(t, "/api/v1/auth/login", map[string]string{"username": "admin", "password": testPassword})
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", response.StatusCode)
	}
	var body errorBody
	decodeBody(t, response, &body)
	if body.Error.Code != "rate_limited" {
		t.Errorf("code = %q, want rate_limited", body.Error.Code)
	}
}

func TestUnknownUserIsRejectedLikeAWrongPassword(t *testing.T) {
	panel := newTestPanel(t)

	response := panel.post(t, "/api/v1/auth/login", map[string]string{"username": "nobody", "password": testPassword})
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.StatusCode)
	}
	var body errorBody
	decodeBody(t, response, &body)
	if body.Error.Code != "invalid_credentials" {
		t.Errorf("code = %q, want invalid_credentials", body.Error.Code)
	}
	if cookie := panel.sessionCookieValue(t, panel.client); cookie != "" {
		t.Error("the unknown user got a session cookie")
	}
}

func TestAuditTrailOfTheFlow(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)

	actions := func() map[string]bool {
		ctx := context.Background()
		entries, err := panel.store.ListAudit(ctx, 100)
		if err != nil {
			t.Fatalf("list audit: %v", err)
		}
		found := make(map[string]bool, len(entries))
		for _, entry := range entries {
			if entry.Actor != "admin" {
				continue
			}
			found[entry.Action] = true
			if strings.Contains(fmt.Sprint(entry.Details), "correct horse") {
				t.Errorf("the audit details hold the password: %v", entry.Details)
			}
		}
		return found
	}

	panel.post(t, "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong password"})
	if !actions()["login_failure"] {
		t.Error("the failed login was not audited")
	}

	secret, _ := panel.enableTOTP(t, "admin", testPassword)
	if found := actions(); !found["login_success"] || !found["mfa_enabled"] {
		t.Errorf("audit actions = %v, want login_success and mfa_enabled", found)
	}

	panel.logout(t, panel.client)
	panel.login(t, "admin", testPassword)
	panel.post(t, "/api/v1/auth/totp", map[string]string{"code": auth.Code(secret, auth.Step(panel.clock.Now())+100)})
	if !actions()["mfa_failure"] {
		t.Error("the failed code was not audited")
	}
}

// pendingLifetime returns how long the pending session of the panel client lasts.
func (p *testPanel) pendingLifetime(t *testing.T) time.Duration {
	t.Helper()

	token := p.sessionCookieValue(t, p.client)
	session, err := p.store.GetSessionByTokenHash(context.Background(), auth.HashToken(token))
	if err != nil {
		t.Fatalf("get the session: %v", err)
	}
	return session.ExpiresAt.Sub(session.CreatedAt).Round(time.Minute)
}

func TestSetupSessionOutlivesTheVerificationSession(t *testing.T) {
	panel := newTestPanel(t)
	panel.createUser(t, "admin", testPassword)

	// First access: there is time to install the authenticator app.
	panel.login(t, "admin", testPassword)
	if got := panel.pendingLifetime(t); got != setupSessionTTL {
		t.Errorf("setup session lasts %s, want %s", got, setupSessionTTL)
	}

	// A user that already has the second factor only gets the short window.
	panel.enableTOTP(t, "admin", testPassword)
	panel.logout(t, panel.client)
	panel.login(t, "admin", testPassword)
	if got := panel.pendingLifetime(t); got != pendingSessionTTL {
		t.Errorf("verification session lasts %s, want %s", got, pendingSessionTTL)
	}
}

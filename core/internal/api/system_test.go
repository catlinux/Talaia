package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/updates"
)

const (
	currentSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	offeredSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pendingSHA = "cccccccccccccccccccccccccccccccccccccccc"
)

type fakeUpdateChecker struct {
	state        updates.State
	ok           bool
	offered      string
	refreshErr   error
	refreshCalls int
	onRefresh    func(*fakeUpdateChecker)
	deadline     time.Duration
}

func (f *fakeUpdateChecker) State() (updates.State, bool) { return f.state, f.ok }

func (f *fakeUpdateChecker) LatestGreen() (string, bool) {
	return f.offered, f.offered != ""
}

func (f *fakeUpdateChecker) Refresh(ctx context.Context) error {
	f.refreshCalls++
	if deadline, ok := ctx.Deadline(); ok {
		f.deadline = time.Until(deadline)
	}
	if f.onRefresh != nil {
		f.onRefresh(f)
	}
	return f.refreshErr
}

type fakeUpdateRequester struct {
	available bool
	status    updates.Status
	err       error
	target    string
	calls     int
}

func (f *fakeUpdateRequester) Request(target string) error {
	f.calls++
	f.target = target
	return f.err
}

func (f *fakeUpdateRequester) Status() updates.Status { return f.status }

func (f *fakeUpdateRequester) Available() bool { return f.available }

// newSystemPanel builds the panel with the system endpoints and the real store
// and authentication stack.
func newSystemPanel(t *testing.T, deps SystemDeps) *testPanel {
	t.Helper()

	st := testStore(t)
	clock := newTestClock()
	if deps.Audit == nil {
		deps.Audit = st
	}

	key, err := auth.LoadOrCreateKey(filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatalf("secrets key: %v", err)
	}
	box, err := auth.NewSecretBox(key)
	if err != nil {
		t.Fatalf("secret box: %v", err)
	}

	router := NewRouter(RouterDeps{
		DB:       st,
		WebDir:   t.TempDir(),
		Security: SecurityConfig{TLS: true, Origins: []string{testOrigin}},
		Session:  SessionDeps{Store: st, Now: clock.Now},
		System:   &deps,
		Auth: AuthDeps{
			Store:        st,
			Box:          box,
			Limiter:      NewRateLimiterWithClock(1000, time.Minute, clock.Now),
			Now:          clock.Now,
			LockDuration: time.Minute,
		},
	})
	server := httptest.NewTLSServer(router)
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &testPanel{
		server: server,
		store:  st,
		clock:  clock,
		client: &http.Client{Transport: server.Client().Transport, Jar: jar},
	}
}

// systemDeps returns fakes with a green update offered.
func systemDeps() (*fakeUpdateChecker, *fakeUpdateRequester, SystemDeps) {
	checker := &fakeUpdateChecker{
		ok:      true,
		offered: offeredSHA,
		state: updates.State{
			Current:         currentSHA,
			Latest:          offeredSHA,
			Pending:         []updates.Pending{{SHA: pendingSHA[:7], Subject: "Fix the panel", Author: "Ana"}},
			CI:              "green",
			CheckedAt:       time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
			Error:           "",
			UpdateAvailable: true,
		},
	}
	requester := &fakeUpdateRequester{
		available: true,
		status:    updates.Status{State: "succeeded", From: currentSHA, To: offeredSHA, Log: []string{"done"}},
	}
	return checker, requester, SystemDeps{
		Version:   "1.2.3",
		Commit:    currentSHA,
		Checker:   checker,
		Requester: requester,
	}
}

// errorCode decodes the stable code of an error answer.
func errorCode(t *testing.T, response *http.Response) string {
	t.Helper()
	var body errorBody
	decodeBody(t, response, &body)
	return body.Error.Code
}

func TestSystemVersionNeedsAFullSession(t *testing.T) {
	_, _, deps := systemDeps()
	panel := newSystemPanel(t, deps)

	if response := panel.do(t, panel.rawClient(), http.MethodGet, "/api/v1/system/version", nil); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want 401", response.StatusCode)
	}
	if response := panel.do(t, panel.rawClient(), http.MethodPost, "/api/v1/system/check",
		map[string]any{}); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated check status = %d, want 401", response.StatusCode)
	}
	if response := panel.do(t, panel.rawClient(), http.MethodPost, "/api/v1/system/update",
		map[string]any{"confirm": true, "target": offeredSHA}); response.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated update status = %d, want 401", response.StatusCode)
	}
}

func TestSystemVersion(t *testing.T) {
	checker, requester, deps := systemDeps()
	panel := newSystemPanel(t, deps)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	var body systemVersionResponse
	response := panel.get(t, "/api/v1/system/version")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	decodeBody(t, response, &body)
	if body.Version != "1.2.3" || body.Commit != currentSHA {
		t.Errorf("version and commit = %q, %q", body.Version, body.Commit)
	}
	if body.Update.Latest != offeredSHA || body.Update.CI != "green" || !body.Update.UpdateAvailable {
		t.Errorf("update = %+v", body.Update)
	}
	if len(body.Update.Pending) != 1 || body.Update.Pending[0].Subject != "Fix the panel" {
		t.Errorf("pending = %+v", body.Update.Pending)
	}
	if body.Status.State != "succeeded" {
		t.Errorf("status = %+v", body.Status)
	}

	// Without the host helper nothing can be applied.
	requester.available = false
	decodeBody(t, panel.get(t, "/api/v1/system/version"), &body)
	if body.Update.UpdateAvailable || body.Update.Error != "host_helper_missing" {
		t.Errorf("update without the helper = %+v", body.Update)
	}

	// Before the first check finishes there is nothing to report.
	checker.ok = false
	response = panel.get(t, "/api/v1/system/version")
	if response.StatusCode != http.StatusServiceUnavailable || errorCode(t, response) != "update_check_pending" {
		t.Errorf("status without data = %d", response.StatusCode)
	}
}

func TestSystemCheckRefreshesAndReturnsTheVersion(t *testing.T) {
	checker, _, deps := systemDeps()
	panel := newSystemPanel(t, deps)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	checked := time.Date(2026, 10, 21, 12, 5, 0, 0, time.UTC)
	checker.onRefresh = func(f *fakeUpdateChecker) {
		f.state.CheckedAt = checked
		f.state.Latest = pendingSHA
		f.state.Pending = []updates.Pending{{SHA: pendingSHA[:7], Subject: "A new commit", Author: "Bea"}}
		f.state.UpdateAvailable = true
	}

	response := panel.post(t, "/api/v1/system/check", map[string]any{})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var body systemVersionResponse
	decodeBody(t, response, &body)
	if body.Update.Latest != pendingSHA {
		t.Errorf("latest = %q", body.Update.Latest)
	}
	if len(body.Update.Pending) != 1 || body.Update.Pending[0].Subject != "A new commit" {
		t.Errorf("pending = %+v", body.Update.Pending)
	}
	if !body.Update.CheckedAt.Equal(checked) {
		t.Errorf("checked at = %s", body.Update.CheckedAt)
	}
	if checker.refreshCalls != 1 {
		t.Errorf("refresh calls = %d", checker.refreshCalls)
	}
	if checker.deadline <= 20*time.Second || checker.deadline > 25*time.Second {
		t.Errorf("refresh deadline = %s, want about 25s", checker.deadline)
	}

	// The check answers with the same body as the version endpoint.
	var version systemVersionResponse
	decodeBody(t, panel.get(t, "/api/v1/system/version"), &version)
	if version.Update.Latest != body.Update.Latest || len(version.Update.Pending) != len(body.Update.Pending) {
		t.Errorf("version = %+v, check = %+v", version.Update, body.Update)
	}

	// It is a read: no update request is audited.
	entries, err := panel.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	for _, entry := range entries {
		if entry.Action == "system_update_requested" {
			t.Errorf("the check audited an update request: %+v", entry)
		}
	}
}

func TestSystemCheckRateLimited(t *testing.T) {
	checker, _, deps := systemDeps()
	checker.refreshErr = updates.ErrRateLimited
	panel := newSystemPanel(t, deps)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	response := panel.post(t, "/api/v1/system/check", map[string]any{})
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if code := errorCode(t, response); code != "rate_limited" {
		t.Errorf("code = %q", code)
	}
	if checker.refreshCalls != 1 {
		t.Errorf("refresh calls = %d", checker.refreshCalls)
	}
}

func TestSystemCheckFailureIsInTheState(t *testing.T) {
	checker, _, deps := systemDeps()
	checker.refreshErr = errors.New("boom")
	checker.onRefresh = func(f *fakeUpdateChecker) {
		f.state.CheckedAt = time.Date(2026, 10, 21, 12, 10, 0, 0, time.UTC)
		f.state.Error = "unreachable"
	}
	panel := newSystemPanel(t, deps)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	response := panel.post(t, "/api/v1/system/check", map[string]any{})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var body systemVersionResponse
	decodeBody(t, response, &body)
	if body.Update.Error != "unreachable" {
		t.Errorf("update error = %q", body.Update.Error)
	}
	if checker.refreshCalls != 1 {
		t.Errorf("refresh calls = %d", checker.refreshCalls)
	}
}

func TestSystemCheckWaitsForTheFirstState(t *testing.T) {
	checker, _, deps := systemDeps()
	checker.ok = false
	panel := newSystemPanel(t, deps)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	response := panel.post(t, "/api/v1/system/check", map[string]any{})
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if code := errorCode(t, response); code != "update_check_pending" {
		t.Errorf("code = %q", code)
	}
	if checker.refreshCalls != 1 {
		t.Errorf("refresh calls = %d", checker.refreshCalls)
	}
}

func TestSystemUpdateNeedsTheOriginAndSessionForWrites(t *testing.T) {
	checker, requester, deps := systemDeps()
	panel := newSystemPanel(t, deps)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	// Cooked by the panel client, but without the origin or a JSON content type.
	request, err := http.NewRequest(http.MethodPost, panel.server.URL+"/api/v1/system/update",
		strings.NewReader(`{"confirm":true,"target":"`+offeredSHA+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	response := panel.doRequest(t, request)
	if code := errorCode(t, response); response.StatusCode != http.StatusForbidden || code != "forbidden_origin" {
		t.Errorf("status = %d, code = %s", response.StatusCode, code)
	}
	if requester.calls != 0 {
		t.Errorf("the requester was called %d times", requester.calls)
	}

	request, err = http.NewRequest(http.MethodPost, panel.server.URL+"/api/v1/system/check",
		strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	response = panel.doRequest(t, request)
	if code := errorCode(t, response); response.StatusCode != http.StatusForbidden || code != "forbidden_origin" {
		t.Errorf("check status = %d, code = %s", response.StatusCode, code)
	}
	if checker.refreshCalls != 0 {
		t.Errorf("the checker was refreshed %d times", checker.refreshCalls)
	}
}

func TestSystemUpdateValidation(t *testing.T) {
	checker, requester, deps := systemDeps()
	panel := newSystemPanel(t, deps)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	tests := []struct {
		name   string
		body   map[string]any
		status int
		code   string
	}{
		{name: "confirmation missing", body: map[string]any{"target": offeredSHA}, status: http.StatusBadRequest, code: "confirmation_required"},
		{name: "target not a sha", body: map[string]any{"confirm": true, "target": "abc"}, status: http.StatusBadRequest, code: "invalid_request"},
		{name: "target not offered", body: map[string]any{"confirm": true, "target": currentSHA}, status: http.StatusConflict, code: "target_not_offered"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := panel.post(t, "/api/v1/system/update", tt.body)
			code := errorCode(t, response)
			if response.StatusCode != tt.status || code != tt.code {
				t.Fatalf("status = %d, code = %s", response.StatusCode, code)
			}
		})
	}

	// The host helper refuses the request.
	for _, tt := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "already queued", err: updates.ErrRequestExists, status: http.StatusConflict, code: "update_in_progress"},
		{name: "running", err: updates.ErrUpdateInProgress, status: http.StatusConflict, code: "update_in_progress"},
		{name: "helper missing", err: updates.ErrHostHelperMissing, status: http.StatusServiceUnavailable, code: "host_helper_missing"},
		{name: "internal", err: errors.New("boom"), status: http.StatusInternalServerError, code: "internal"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requester.err = tt.err
			response := panel.post(t, "/api/v1/system/update", map[string]any{"confirm": true, "target": offeredSHA})
			code := errorCode(t, response)
			if response.StatusCode != tt.status || code != tt.code {
				t.Fatalf("status = %d, code = %s", response.StatusCode, code)
			}
		})
	}

	// Without a finished check there is nothing to accept.
	checker.ok = false
	response := panel.post(t, "/api/v1/system/update", map[string]any{"confirm": true, "target": offeredSHA})
	code := errorCode(t, response)
	if response.StatusCode != http.StatusServiceUnavailable || code != "update_check_pending" {
		t.Errorf("status = %d, code = %s", response.StatusCode, code)
	}
}

func TestSystemUpdateWritesTheRequestFile(t *testing.T) {
	_, _, deps := systemDeps()
	hostDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(hostDir, "requests"), 0o750); err != nil {
		t.Fatal(err)
	}
	deps.Requester = updates.NewRequester(hostDir)
	panel := newSystemPanel(t, deps)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	response := panel.post(t, "/api/v1/system/update", map[string]any{"confirm": true, "target": offeredSHA})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}

	path := filepath.Join(hostDir, "requests", "update.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the request file: %v", err)
	}
	if got := string(data); got != `{"target":"`+offeredSHA+`"}` {
		t.Fatalf("request file = %q", got)
	}

	entries, err := panel.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action == "system_update_requested" && entry.Actor == "admin" && entry.Details["target"] == offeredSHA {
			found = true
		}
	}
	if !found {
		t.Error("the update request was not audited")
	}
}

func TestSystemUpdateQueuesTheRequestAndAuditsIt(t *testing.T) {
	_, requester, deps := systemDeps()
	panel := newSystemPanel(t, deps)
	panel.createUser(t, "admin", testPassword)
	panel.enableTOTP(t, "admin", testPassword)

	response := panel.post(t, "/api/v1/system/update", map[string]any{"confirm": true, "target": offeredSHA})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var body systemUpdateResponse
	decodeBody(t, response, &body)
	if body.Target != offeredSHA {
		t.Errorf("target = %q", body.Target)
	}
	if requester.calls != 1 || requester.target != offeredSHA {
		t.Errorf("requester = %d calls, target %q", requester.calls, requester.target)
	}

	entries, err := panel.store.ListAudit(context.Background(), 20)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	found := false
	for _, entry := range entries {
		if entry.Action != "system_update_requested" {
			continue
		}
		if entry.Actor != "admin" || entry.Result != "requested" || entry.Details["target"] != offeredSHA {
			t.Errorf("audit entry = %+v", entry)
		}
		found = true
	}
	if !found {
		t.Error("the update request was not audited")
	}
}

// doRequest sends a request built by the test.
func (p *testPanel) doRequest(t *testing.T, request *http.Request) *http.Response {
	t.Helper()
	response, err := p.client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", request.Method, request.URL.Path, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

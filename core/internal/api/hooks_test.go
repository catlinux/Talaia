package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/store"
)

type fakeHookApps struct {
	app   store.App
	err   error
	calls int
}

func (f *fakeHookApps) GetApp(context.Context, string) (store.App, error) {
	f.calls++
	if f.err != nil {
		return store.App{}, f.err
	}
	return f.app, nil
}

type hookEnqueueCall struct {
	appID   string
	trigger string
	commit  string
}

type fakeHookEnqueue struct {
	calls []hookEnqueueCall
	err   error
}

func (f *fakeHookEnqueue) Enqueue(_ context.Context, appID, trigger, commit string) error {
	f.calls = append(f.calls, hookEnqueueCall{appID: appID, trigger: trigger, commit: commit})
	return f.err
}

type fakeHookAudit struct {
	events []store.AuditEvent
}

func (f *fakeHookAudit) AppendAudit(_ context.Context, event store.AuditEvent) error {
	f.events = append(f.events, event)
	return nil
}

type hookOption func(*HooksDeps)

type hookRig struct {
	handler http.Handler
	apps    *fakeHookApps
	enqueue *fakeHookEnqueue
	audit   *fakeHookAudit
	secret  []byte
	app     store.App
}

func hookApp() store.App {
	return store.App{
		ID: uuid.NewString(), ServerID: uuid.NewString(), Source: store.AppSourceGit,
		Branch: "main", AutoDeploy: true,
	}
}

func newHookRig(t *testing.T, app store.App, secret string, options ...hookOption) *hookRig {
	t.Helper()
	box, err := auth.NewSecretBox(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatalf("secret box: %v", err)
	}
	secretBytes := []byte(secret)
	if secret != "" {
		app.WebhookSecretEnc = box.Seal(secretBytes)
	}
	apps := &fakeHookApps{app: app}
	enqueue := &fakeHookEnqueue{}
	audit := &fakeHookAudit{}
	deps := HooksDeps{Apps: apps, Box: box, Deploy: enqueue, Audit: audit}
	for _, option := range options {
		option(&deps)
	}
	router := chi.NewRouter()
	router.Post("/api/v1/hooks/git/{appID}", NewGitHookHandler(deps).ServeHTTP)
	return &hookRig{handler: router, apps: apps, enqueue: enqueue, audit: audit, secret: secretBytes, app: app}
}

func (r *hookRig) do(appID string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/hooks/git/"+appID, bytes.NewReader(body))
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	r.handler.ServeHTTP(recorder, request)
	return recorder
}

func signatureFor(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (r *hookRig) signed(body []byte, event, delivery string) *httptest.ResponseRecorder {
	headers := map[string]string{
		"X-Hub-Signature-256": signatureFor(r.secret, body),
		"X-GitHub-Event":      event,
	}
	if delivery != "" {
		headers["X-GitHub-Delivery"] = delivery
	}
	return r.do(r.app.ID, body, headers)
}

func pushBody(ref, after string) []byte {
	return []byte(fmt.Sprintf(`{"ref":%q,"after":%q}`, ref, after))
}

func hookStatusCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return body.Status
}

func TestValidWebhookQueuesAndAudits(t *testing.T) {
	rig := newHookRig(t, hookApp(), "top-secret")
	commit := strings.Repeat("a", 40)
	body := pushBody("refs/heads/main", commit)

	response := rig.signed(body, "push", "delivery-1")
	if response.Code != http.StatusAccepted || hookStatusCode(t, response) != "queued" {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
	if len(rig.enqueue.calls) != 1 {
		t.Fatalf("enqueue calls = %+v", rig.enqueue.calls)
	}
	call := rig.enqueue.calls[0]
	if call.appID != rig.app.ID || call.trigger != "push" || call.commit != commit {
		t.Fatalf("enqueue call = %+v", call)
	}
	if len(rig.audit.events) != 1 {
		t.Fatalf("audit events = %+v", rig.audit.events)
	}
	event := rig.audit.events[0]
	if event.Action != "app_webhook" || event.Result != "success" || event.Scope["app_id"] != rig.app.ID {
		t.Fatalf("audit event = %+v", event)
	}
	encoded := fmt.Sprint(rig.audit.events)
	signature := signatureFor(rig.secret, body)
	if strings.Contains(encoded, "top-secret") || strings.Contains(encoded, signature) {
		t.Fatal("the audit log leaked a secret or signature")
	}
}

func TestWebhookRejectsBadSignatures(t *testing.T) {
	commit := strings.Repeat("a", 40)
	body := pushBody("refs/heads/main", commit)
	other := pushBody("refs/heads/main", strings.Repeat("b", 40))
	wellFormedWrong := "sha256=" + strings.Repeat("0", 64)

	tests := []struct {
		name    string
		secret  string
		headers map[string]string
	}{
		{name: "missing", secret: "secret", headers: map[string]string{"X-GitHub-Event": "push"}},
		{name: "malformed", secret: "secret", headers: map[string]string{
			"X-Hub-Signature-256": "sha256=not-hex", "X-GitHub-Event": "push",
		}},
		{name: "wrong signature", secret: "secret", headers: map[string]string{
			"X-Hub-Signature-256": wellFormedWrong, "X-GitHub-Event": "push",
		}},
		{name: "different body", secret: "secret", headers: map[string]string{
			"X-Hub-Signature-256": signatureFor([]byte("secret"), other), "X-GitHub-Event": "push",
		}},
		{name: "no secret", secret: "", headers: map[string]string{
			"X-Hub-Signature-256": wellFormedWrong, "X-GitHub-Event": "push",
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rig := newHookRig(t, hookApp(), test.secret)
			response := rig.do(rig.app.ID, body, test.headers)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (%s)", response.Code, response.Body.String())
			}
			if len(rig.enqueue.calls) != 0 {
				t.Fatal("a bad signature enqueued a deployment")
			}
			if len(rig.audit.events) != 1 || rig.audit.events[0].Result != "denied" {
				t.Fatalf("audit events = %+v", rig.audit.events)
			}
			if test.secret != "" && strings.Contains(fmt.Sprint(rig.audit.events), test.secret) {
				t.Fatal("the audit log leaked the secret")
			}
		})
	}
}

func TestWebhookUnknownAndMalformedApp(t *testing.T) {
	rig := newHookRig(t, hookApp(), "secret")
	rig.apps.err = store.ErrNotFound
	body := pushBody("refs/heads/main", strings.Repeat("a", 40))
	if response := rig.do(rig.app.ID, body, map[string]string{"X-Hub-Signature-256": signatureFor(rig.secret, body)}); response.Code != http.StatusNotFound {
		t.Fatalf("unknown app status = %d, want 404", response.Code)
	}
	if response := rig.do("not-a-uuid", body, nil); response.Code != http.StatusNotFound {
		t.Fatalf("malformed app id status = %d, want 404", response.Code)
	}
	if len(rig.enqueue.calls) != 0 {
		t.Fatal("an unknown app enqueued a deployment")
	}
}

func TestWebhookPingAndIgnoredEvents(t *testing.T) {
	app := hookApp()
	rig := newHookRig(t, app, "secret")
	ping := []byte(`{}`)
	if response := rig.signed(ping, "ping", "ping-1"); response.Code != http.StatusOK || hookStatusCode(t, response) != "pong" {
		t.Fatalf("ping = %d %s", response.Code, response.Body.String())
	}
	if len(rig.enqueue.calls) != 0 {
		t.Fatal("ping enqueued a deployment")
	}
	if response := rig.signed(ping, "issues", "issues-1"); response.Code != http.StatusAccepted || hookStatusCode(t, response) != "ignored" {
		t.Fatalf("other event = %d %s", response.Code, response.Body.String())
	}
	if len(rig.audit.events) != 2 || rig.audit.events[0].Result != "success" || rig.audit.events[1].Result != "ignored" {
		t.Fatalf("audit events = %+v", rig.audit.events)
	}
}

func TestWebhookIgnoresUnusablePushes(t *testing.T) {
	commit := strings.Repeat("a", 40)
	tests := []struct {
		name   string
		app    func(store.App) store.App
		body   []byte
		status string
	}{
		{
			name: "other branch",
			app:  func(app store.App) store.App { return app },
			body: pushBody("refs/heads/dev", commit), status: "ignored",
		},
		{
			name: "deleted",
			app:  func(app store.App) store.App { return app },
			body: []byte(`{"ref":"refs/heads/main","after":"` + commit + `","deleted":true}`), status: "ignored",
		},
		{
			name: "zero commit",
			app:  func(app store.App) store.App { return app },
			body: pushBody("refs/heads/main", strings.Repeat("0", 40)), status: "ignored",
		},
		{
			name: "non git",
			app: func(app store.App) store.App {
				app.Source = store.AppSourceImage
				return app
			},
			body: pushBody("refs/heads/main", commit), status: "ignored",
		},
		{
			name: "auto deploy off",
			app: func(app store.App) store.App {
				app.AutoDeploy = false
				return app
			},
			body: pushBody("refs/heads/main", commit), status: "ignored",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rig := newHookRig(t, test.app(hookApp()), "secret")
			response := rig.signed(test.body, "push", "delivery-"+test.name)
			if response.Code != http.StatusAccepted || hookStatusCode(t, response) != test.status {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
			if len(rig.enqueue.calls) != 0 {
				t.Fatal("an unusable push enqueued a deployment")
			}
		})
	}
}

func TestWebhookDuplicateDelivery(t *testing.T) {
	rig := newHookRig(t, hookApp(), "secret")
	body := pushBody("refs/heads/main", strings.Repeat("a", 40))
	first := rig.signed(body, "push", "same-delivery")
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d", first.Code)
	}
	second := rig.signed(body, "push", "same-delivery")
	if second.Code != http.StatusOK || hookStatusCode(t, second) != "duplicate" {
		t.Fatalf("second = %d %s", second.Code, second.Body.String())
	}
	if len(rig.enqueue.calls) != 1 {
		t.Fatalf("enqueue calls = %+v", rig.enqueue.calls)
	}
}

func TestWebhookBodyLimit(t *testing.T) {
	rig := newHookRig(t, hookApp(), "secret")
	body := bytes.Repeat([]byte("x"), maxHookBody+1)
	response := rig.signed(body, "push", "large")
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", response.Code)
	}
	if len(rig.enqueue.calls) != 0 {
		t.Fatal("an oversized body enqueued a deployment")
	}
}

func TestWebhookRateLimits(t *testing.T) {
	body := pushBody("refs/heads/main", strings.Repeat("a", 40))
	firstRequest := func(rig *hookRig, delivery string) int {
		return rig.signed(body, "push", delivery).Code
	}
	now := time.Now()
	t.Run("per IP", func(t *testing.T) {
		rig := newHookRig(t, hookApp(), "secret", func(deps *HooksDeps) {
			deps.IPLimiter = NewRateLimiterWithClock(1, time.Minute, func() time.Time { return now })
		})
		if code := firstRequest(rig, "one"); code != http.StatusAccepted {
			t.Fatalf("first status = %d", code)
		}
		if code := firstRequest(rig, "two"); code != http.StatusTooManyRequests {
			t.Fatalf("second status = %d, want 429", code)
		}
		if len(rig.enqueue.calls) != 1 {
			t.Fatalf("enqueue calls = %+v", rig.enqueue.calls)
		}
	})
	t.Run("per app", func(t *testing.T) {
		rig := newHookRig(t, hookApp(), "secret", func(deps *HooksDeps) {
			deps.AppLimiter = NewRateLimiterWithClock(1, time.Minute, func() time.Time { return now })
		})
		if code := firstRequest(rig, "one"); code != http.StatusAccepted {
			t.Fatalf("first status = %d", code)
		}
		if code := firstRequest(rig, "two"); code != http.StatusTooManyRequests {
			t.Fatalf("second status = %d, want 429", code)
		}
	})
}

func TestWebhookVerifiesSignatureBeforeParsingJSON(t *testing.T) {
	body := []byte("{")
	rig := newHookRig(t, hookApp(), "secret")
	if response := rig.signed(body, "push", "invalid-json"); response.Code != http.StatusBadRequest {
		t.Fatalf("valid signature with invalid JSON = %d, want 400", response.Code)
	}
	headers := map[string]string{
		"X-Hub-Signature-256": signatureFor([]byte("wrong"), body),
		"X-GitHub-Event":      "push",
	}
	if response := rig.do(rig.app.ID, body, headers); response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature with invalid JSON = %d, want 401", response.Code)
	}
	if len(rig.enqueue.calls) != 0 {
		t.Fatal("invalid JSON enqueued a deployment")
	}
}

func TestDeliveryCacheExpiresAndKeepsASizeLimit(t *testing.T) {
	now := time.Unix(1000, 0)
	cache := newDeliveryCache(time.Hour, 2)
	cache.now = func() time.Time { return now }
	if cache.has("a") {
		t.Fatal("a new delivery id was a duplicate")
	}
	cache.add("a")
	if !cache.has("a") {
		t.Fatal("a repeated delivery id was not a duplicate")
	}
	now = now.Add(2 * time.Hour)
	if cache.has("a") {
		t.Fatal("an expired delivery id was still cached")
	}
	now = now.Add(time.Minute)
	cache.add("b")
	now = now.Add(time.Minute)
	cache.add("c")
	now = now.Add(time.Minute)
	// The cap evicts the oldest entry to make room for d.
	cache.add("d")
	if cache.has("b") {
		t.Fatal("the oldest delivery id was not evicted")
	}
}

func TestWebhookRejectsAnInvalidCommit(t *testing.T) {
	rig := newHookRig(t, hookApp(), "secret")
	response := rig.signed(pushBody("refs/heads/main", "not-a-commit"), "push", "invalid-commit")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if len(rig.enqueue.calls) != 0 {
		t.Fatal("an invalid commit enqueued a deployment")
	}
}

func TestWebhookEnqueueFailure(t *testing.T) {
	rig := newHookRig(t, hookApp(), "secret")
	rig.enqueue.err = errors.New("database unavailable")
	response := rig.signed(pushBody("refs/heads/main", strings.Repeat("a", 40)), "push", "enqueue-failure")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	if len(rig.audit.events) != 1 || rig.audit.events[0].Result != "failed" {
		t.Fatalf("audit events = %+v", rig.audit.events)
	}

	// GitHub retries the same delivery: a failure on our side must not make it a duplicate.
	rig.enqueue.err = nil
	retry := rig.signed(pushBody("refs/heads/main", strings.Repeat("a", 40)), "push", "enqueue-failure")
	if retry.Code != http.StatusAccepted {
		t.Fatalf("retry status = %d, want 202", retry.Code)
	}
}

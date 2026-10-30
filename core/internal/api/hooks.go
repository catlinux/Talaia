package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/catlinux/Talaia/core/internal/store"
)

const (
	maxHookBody = 1 << 20

	hookIPLimit  = 60
	hookAppLimit = 30
	hookWindow   = time.Minute

	hookDeliveryTTL   = time.Hour
	hookDeliveryLimit = 4096
)

// AppGetter returns the app a webhook is addressed to.
type AppGetter interface {
	GetApp(ctx context.Context, id string) (store.App, error)
}

// SecretOpener decrypts the sealed app secrets.
type SecretOpener interface {
	Open(sealed []byte) ([]byte, error)
}

// DeployTrigger queues a deployment. The deploy pipeline implements it later.
type DeployTrigger interface {
	Enqueue(ctx context.Context, appID, trigger, commit string) error
}

// HooksDeps are the collaborators of the public Git webhook.
type HooksDeps struct {
	Apps       AppGetter
	Box        SecretOpener
	Deploy     DeployTrigger
	Audit      Auditor
	IPLimiter  *RateLimiter
	AppLimiter *RateLimiter
}

type gitHookHandler struct {
	deps       HooksDeps
	deliveries *deliveryCache
}

type hookPayload struct {
	Ref     string `json:"ref"`
	After   string `json:"after"`
	Deleted bool   `json:"deleted"`
}

// NewGitHookHandler returns POST /api/v1/hooks/git/{appID}. It is public and
// authenticates the request only with the app webhook signature.
func NewGitHookHandler(deps HooksDeps) http.Handler {
	if deps.IPLimiter == nil {
		deps.IPLimiter = NewRateLimiterWithClock(hookIPLimit, hookWindow, time.Now)
	}
	if deps.AppLimiter == nil {
		deps.AppLimiter = NewRateLimiterWithClock(hookAppLimit, hookWindow, time.Now)
	}
	return &gitHookHandler{
		deps:       deps,
		deliveries: newDeliveryCache(hookDeliveryTTL, hookDeliveryLimit),
	}
}

func (h *gitHookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Rate-limited requests are not audited: otherwise a noisy IP could flood
	// the append-only audit log with entries that say nothing new.
	if !h.deps.IPLimiter.Allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	rawID := chi.URLParam(r, "appID")
	id, err := uuid.Parse(rawID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if !h.deps.AppLimiter.Allow(id.String()) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	if h.deps.Apps == nil {
		slog.Error("app webhook has no app store")
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	app, err := h.deps.Apps.GetApp(r.Context(), id.String())
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		internal(w, "get webhook app", err)
		return
	}

	if len(app.WebhookSecretEnc) == 0 {
		h.audit(r, app, "denied", map[string]any{"reason": "no_secret"})
		writeError(w, http.StatusUnauthorized, "invalid_signature")
		return
	}
	if h.deps.Box == nil {
		slog.Error("app webhook has no secret box", "app_id", app.ID)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	secret, err := h.deps.Box.Open(app.WebhookSecretEnc)
	if err != nil {
		slog.Error("open app webhook secret", "app_id", app.ID, "error", err)
		h.audit(r, app, "denied", map[string]any{"reason": "secret_unavailable"})
		writeError(w, http.StatusUnauthorized, "invalid_signature")
		return
	}

	body, ok := readHookBody(w, r)
	if !ok {
		return
	}
	if !validHookSignature(r.Header.Get("X-Hub-Signature-256"), secret, body) {
		h.audit(r, app, "denied", map[string]any{"reason": "invalid_signature"})
		writeError(w, http.StatusUnauthorized, "invalid_signature")
		return
	}

	event := strings.ToLower(strings.TrimSpace(r.Header.Get("X-GitHub-Event")))
	delivery := r.Header.Get("X-GitHub-Delivery")
	if h.deliveries.has(delivery) {
		h.audit(r, app, "ignored", map[string]any{"event": event, "reason": "duplicate_delivery"})
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}

	if event == "ping" {
		h.audit(r, app, "success", map[string]any{"event": event})
		writeJSON(w, http.StatusOK, map[string]string{"status": "pong"})
		return
	}
	if event != "push" {
		h.audit(r, app, "ignored", map[string]any{"event": event, "reason": "event"})
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
		return
	}
	if app.Source != store.AppSourceGit || !app.AutoDeploy {
		h.audit(r, app, "ignored", map[string]any{"event": event, "reason": "not_deployable"})
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
		return
	}

	var payload hookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		h.audit(r, app, "ignored", map[string]any{"event": event, "reason": "invalid_payload"})
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if payload.Deleted || allZero(payload.After) {
		h.audit(r, app, "ignored", map[string]any{"event": event, "reason": "deleted_branch"})
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
		return
	}
	if !validWebhookCommit(payload.After) {
		h.audit(r, app, "ignored", map[string]any{"event": event, "reason": "invalid_commit"})
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}
	if payload.Ref != "refs/heads/"+app.Branch {
		h.audit(r, app, "ignored", map[string]any{"event": event, "reason": "branch"})
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
		return
	}
	if h.deps.Deploy == nil {
		slog.Error("app webhook has no deploy trigger", "app_id", app.ID)
		h.audit(r, app, "failed", map[string]any{"event": event, "reason": "no_trigger"})
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	if err := h.deps.Deploy.Enqueue(r.Context(), app.ID, "push", payload.After); err != nil {
		slog.Error("enqueue webhook deployment", "app_id", app.ID, "error", err)
		h.audit(r, app, "failed", map[string]any{"event": event, "reason": "enqueue"})
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	// Recorded only after a successful enqueue, so GitHub can retry a delivery
	// that failed on our side.
	h.deliveries.add(delivery)
	h.audit(r, app, "success", map[string]any{"event": event, "commit": payload.After})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func readHookBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxHookBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request_too_large")
			return nil, false
		}
		writeError(w, http.StatusBadRequest, "invalid_request")
		return nil, false
	}
	return body, true
}

func validHookSignature(header string, secret, body []byte) bool {
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	raw := header[len(prefix):]
	if len(raw) != sha256.Size*2 {
		return false
	}
	provided, err := hex.DecodeString(raw)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}

func validWebhookCommit(commit string) bool {
	if len(commit) != 40 {
		return false
	}
	for _, c := range commit {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func allZero(value string) bool {
	if value == "" {
		return false
	}
	for _, c := range value {
		if c != '0' {
			return false
		}
	}
	return true
}

func (h *gitHookHandler) audit(r *http.Request, app store.App, result string, details map[string]any) {
	if h.deps.Audit == nil {
		return
	}
	if details == nil {
		details = map[string]any{}
	}
	details["app_id"] = app.ID
	details["server_id"] = app.ServerID
	event := store.AuditEvent{
		Actor:   "github",
		Action:  "app_webhook",
		IP:      clientIP(r),
		Result:  result,
		Scope:   map[string]any{"app_id": app.ID, "server_id": app.ServerID},
		Details: details,
	}
	if err := h.deps.Audit.AppendAudit(r.Context(), event); err != nil {
		slog.Error("append app webhook audit", "app_id", app.ID, "error", err)
	}
}

type deliveryCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
	now     func() time.Time
	ttl     time.Duration
	limit   int
}

func newDeliveryCache(ttl time.Duration, limit int) *deliveryCache {
	return &deliveryCache{entries: make(map[string]time.Time), now: time.Now, ttl: ttl, limit: limit}
}

// has reports whether id was already received. An empty id is never cached, so
// requests without a delivery header keep working.
func (c *deliveryCache) has(id string) bool {
	if id == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expire()
	_, ok := c.entries[id]
	return ok
}

// add remembers id, evicting the entry closest to expiry when the cache is full.
func (c *deliveryCache) add(id string) {
	if id == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expire()
	if _, ok := c.entries[id]; !ok && len(c.entries) >= c.limit {
		var oldestKey string
		var oldest time.Time
		for key, expiry := range c.entries {
			if oldestKey == "" || expiry.Before(oldest) {
				oldestKey, oldest = key, expiry
			}
		}
		delete(c.entries, oldestKey)
	}
	c.entries[id] = c.now().Add(c.ttl)
}

func (c *deliveryCache) expire() {
	now := c.now()
	for key, expiry := range c.entries {
		if !now.Before(expiry) {
			delete(c.entries, key)
		}
	}
}

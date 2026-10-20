package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/catlinux/Talaia/core/internal/install"
	"github.com/catlinux/Talaia/core/internal/store"
)

// enrollmentTokenTTL is how long the token created from the panel stays valid.
const enrollmentTokenTTL = 15 * time.Minute

// serverName is what the panel accepts as the name of a new server.
var serverName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

// TokenCreator creates one-time enrollment tokens.
type TokenCreator interface {
	CreateEnrollmentToken(ctx context.Context, serverName string, ttl time.Duration) (string, error)
}

// Auditor appends audit events.
type Auditor interface {
	AppendAudit(ctx context.Context, event store.AuditEvent) error
}

// EnrollmentTokenDeps are the collaborators of POST /api/v1/servers/enrollment-tokens.
type EnrollmentTokenDeps struct {
	Tokens TokenCreator
	Audit  Auditor
	// CAFingerprint is the SHA-256 of the core CA, the trust anchor of the agent.
	CAFingerprint string
	// PublicURL is the address servers reach the core at; empty when unknown.
	PublicURL string
}

type enrollmentTokenRequest struct {
	Name string `json:"name"`
}

type enrollmentTokenResponse struct {
	Token         string `json:"token"`
	CAFingerprint string `json:"ca_fingerprint"`
	ExpiresIn     int    `json:"expires_in"`
	// Command is the line to run on the new server; empty without a public URL.
	Command string `json:"command"`
}

// NewEnrollmentTokenHandler returns POST /api/v1/servers/enrollment-tokens. It
// is mounted behind RequireFull, so only a full session reaches it. The token
// is shown once, so the response is never cached.
func NewEnrollmentTokenHandler(deps EnrollmentTokenDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request enrollmentTokenRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		name := strings.TrimSpace(request.Name)
		if !serverName.MatchString(name) {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}

		token, err := deps.Tokens.CreateEnrollmentToken(r.Context(), name, enrollmentTokenTTL)
		if errors.Is(err, store.ErrNameTaken) {
			writeError(w, http.StatusConflict, "name_taken")
			return
		}
		if err != nil {
			slog.Error("create enrollment token", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}

		actor := ""
		if session, ok := sessionFromContext(r.Context()); ok {
			actor = session.User.Username
		}
		if err := deps.Audit.AppendAudit(r.Context(), store.AuditEvent{
			Actor:   actor,
			Action:  "enrollment_token_created",
			IP:      clientIP(r),
			Result:  "success",
			Details: map[string]any{"server_name": name},
		}); err != nil {
			slog.Error("append audit event", "action", "enrollment_token_created", "error", err)
		}

		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusCreated, enrollmentTokenResponse{
			Token:         token,
			CAFingerprint: deps.CAFingerprint,
			ExpiresIn:     int(enrollmentTokenTTL / time.Second),
			Command:       install.Command(deps.PublicURL, token, deps.CAFingerprint),
		})
	})
}

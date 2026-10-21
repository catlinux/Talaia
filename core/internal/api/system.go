package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/catlinux/Talaia/core/internal/updates"
)

// updateTarget is the only shape accepted for an update target: a full commit
// SHA. Nothing else from the request reaches the host helper.
var updateTarget = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// UpdateChecker exposes the cached state of the update checks and can force a
// check now.
type UpdateChecker interface {
	State() (updates.State, bool)
	LatestGreen() (string, bool)
	Refresh(ctx context.Context) error
}

// UpdateRequester queues an update for the host helper and reads its status.
type UpdateRequester interface {
	Request(target string) error
	Status() updates.Status
	Available() bool
}

// SystemDeps are the collaborators of the system endpoints.
type SystemDeps struct {
	Version   string
	Commit    string
	Checker   UpdateChecker
	Requester UpdateRequester
	Audit     Auditor
}

type systemVersionResponse struct {
	Version string         `json:"version"`
	Commit  string         `json:"commit"`
	Update  updates.State  `json:"update"`
	Status  updates.Status `json:"status"`
}

// systemVersionBody builds the answer shared by GET /system/version and
// POST /system/check. The bool is false until the first check finishes.
func systemVersionBody(deps SystemDeps) (systemVersionResponse, bool) {
	state, ok := deps.Checker.State()
	if !ok {
		return systemVersionResponse{}, false
	}
	// Without the host helper the core cannot apply anything, but it still
	// reports what it knows about newer commits.
	if !deps.Requester.Available() {
		state.UpdateAvailable = false
		state.Error = "host_helper_missing"
	}
	return systemVersionResponse{
		Version: deps.Version,
		Commit:  deps.Commit,
		Update:  state,
		Status:  deps.Requester.Status(),
	}, true
}

// writeSystemVersion writes the version body, or 503 until there is a state.
func writeSystemVersion(w http.ResponseWriter, deps SystemDeps) {
	body, ok := systemVersionBody(deps)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "update_check_pending")
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// NewSystemVersionHandler returns GET /api/v1/system/version.
func NewSystemVersionHandler(deps SystemDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSystemVersion(w, deps)
	})
}

// NewSystemCheckHandler returns POST /api/v1/system/check. It asks GitHub now
// and then answers exactly like GET /system/version: a failed check is already
// recorded in update.error, so only the rate limit is answered directly.
func NewSystemCheckHandler(deps SystemDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		if err := deps.Checker.Refresh(ctx); errors.Is(err, updates.ErrRateLimited) {
			writeError(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		writeSystemVersion(w, deps)
	})
}

type systemUpdateRequest struct {
	Confirm bool   `json:"confirm"`
	Target  string `json:"target"`
}

type systemUpdateResponse struct {
	Target string `json:"target"`
}

// NewSystemUpdateHandler returns POST /api/v1/system/update. It only queues the
// update: the host helper performs it.
func NewSystemUpdateHandler(deps SystemDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request systemUpdateRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if !request.Confirm {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}

		target := strings.ToLower(strings.TrimSpace(request.Target))
		if !updateTarget.MatchString(target) {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if _, ok := deps.Checker.State(); !ok {
			writeError(w, http.StatusServiceUnavailable, "update_check_pending")
			return
		}
		offered, ok := deps.Checker.LatestGreen()
		if !ok || target != offered {
			writeError(w, http.StatusConflict, "target_not_offered")
			return
		}

		if err := deps.Requester.Request(target); err != nil {
			switch {
			case errors.Is(err, updates.ErrHostHelperMissing):
				writeError(w, http.StatusServiceUnavailable, "host_helper_missing")
			case errors.Is(err, updates.ErrUpdateInProgress), errors.Is(err, updates.ErrRequestExists):
				// A queued request is pending just like a running update.
				writeError(w, http.StatusConflict, "update_in_progress")
			case errors.Is(err, updates.ErrInvalidTarget):
				writeError(w, http.StatusBadRequest, "invalid_request")
			default:
				slog.Error("queue the update request", "error", err)
				writeError(w, http.StatusInternalServerError, "internal")
			}
			return
		}

		actor := ""
		if session, ok := sessionFromContext(r.Context()); ok {
			actor = session.User.Username
		}
		if actor == "" {
			writeError(w, http.StatusUnauthorized, "session_required")
			return
		}
		if err := deps.Audit.AppendAudit(r.Context(), store.AuditEvent{
			Actor:   actor,
			Action:  "system_update_requested",
			IP:      clientIP(r),
			Result:  "requested",
			Details: map[string]any{"target": target},
		}); err != nil {
			slog.Error("append audit event", "action", "system_update_requested", "error", err)
		}

		writeJSON(w, http.StatusAccepted, systemUpdateResponse{Target: target})
	})
}

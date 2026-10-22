package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/catlinux/Talaia/core/internal/terminal"
)

// hostTerminalOpenRequest is the body of POST /servers/{serverID}/terminal.
type hostTerminalOpenRequest struct {
	Confirm bool   `json:"confirm"`
	Code    string `json:"code"`
	Shell   string `json:"shell"`
	Cols    uint32 `json:"cols"`
	Rows    uint32 `json:"rows"`
}

// NewHostTerminalOpenHandler returns POST /api/v1/servers/{serverID}/terminal.
// It starts a recorded root shell on the server after a fresh TOTP code.
func NewHostTerminalOpenHandler(deps *TerminalDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")

		var request hostTerminalOpenRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if !request.Confirm {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}
		session, ok := sessionFromContext(r.Context())
		if !ok || session.User.Username == "" {
			writeError(w, http.StatusUnauthorized, "session_required")
			return
		}
		// The agent is checked before the code, so a refusal here does not use it up.
		agent, connected := connectedAgent(deps.Agents, serverID)
		if !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}
		if !agent.HostTerminal {
			writeError(w, http.StatusConflict, "host_terminal_disabled")
			return
		}

		if deps.Limiter != nil && !deps.Limiter.Allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "rate_limited")
			return
		}
		if deps.TOTP == nil {
			slog.Error("open a host terminal without a totp verifier", "server_id", serverID)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if err := deps.TOTP.VerifyCode(r.Context(), session.User, request.Code); err != nil {
			if errors.Is(err, errInvalidCode) {
				// A wrong code here means someone holds a full session and is
				// guessing the second factor: leave a trace, never the code.
				if deps.Audit != nil {
					if auditErr := deps.Audit.AppendAudit(r.Context(), store.AuditEvent{
						Actor: session.User.Username, Action: "host_terminal_denied", IP: clientIP(r),
						Result: "failure", Details: map[string]any{"server_id": serverID, "reason": "code_invalid"},
					}); auditErr != nil {
						slog.Error("append audit event", "action", "host_terminal_denied", "error", auditErr)
					}
				}
				writeError(w, http.StatusForbidden, "code_invalid")
				return
			}
			slog.Warn("verify the code of a host terminal", "user_id", session.User.ID, "error", err)
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}

		opened, err := deps.Terminals.Open(r.Context(), terminal.OpenRequest{
			ServerID: serverID, Host: true, Shell: request.Shell,
			Actor: session.User.Username, PanelSession: session.ID, IP: clientIP(r),
			Cols: request.Cols, Rows: request.Rows,
		})
		if err != nil {
			status, code := terminalError(err)
			slog.Warn("open a host terminal", "server_id", serverID, "error", err)
			writeError(w, status, code)
			return
		}

		token, err := deps.tickets.issue(opened)
		if err != nil {
			opened.Close("open_failed")
			writeError(w, http.StatusServiceUnavailable, "agent_busy")
			return
		}
		writeJSON(w, http.StatusCreated, terminalOpenResponse{
			SessionID: opened.ID(), Ticket: token, Privileged: true,
		})
	})
}

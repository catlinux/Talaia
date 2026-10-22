package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// Job kinds of the read-only apt status command. A refresh also runs
// apt-get update, so it gets a longer stale-job window.
const (
	aptStatusKind  = "apt_status"
	aptRefreshKind = "apt_refresh"
)

type aptStatusRequest struct {
	Refresh bool `json:"refresh"`
}

// NewAptStatusHandler returns POST /api/v1/servers/{serverID}/apt/status. The
// body is optional; an empty body means a cached status without refreshing the
// package lists. The package list itself is only in the job output.
func NewAptStatusHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")

		var request aptStatusRequest
		if err := decodeJSON(w, r, &request); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if _, connected := connectedAgent(deps.Agents, serverID); !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}

		kind := aptStatusKind
		if request.Refresh {
			kind = aptRefreshKind
		}
		submitJob(w, r, deps, serverID, kind, "apt_status",
			map[string]any{"refresh": request.Refresh},
			map[string]any{"refresh": request.Refresh},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd: &agentv1.CoreMsg_AptStatus{
						AptStatus: &agentv1.AptStatus{Refresh: request.Refresh},
					},
				}
			})
	})
}

package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/agentpkg"
	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// agentUpdateKind is the kind of the jobs that update an agent.
const agentUpdateKind = "agent_update"

// AgentPackages is the release the core can offer to its agents: the version of
// the packages and the directory where they are served from.
type AgentPackages struct {
	// UpgradeCommand is the console command that upgrades an agent by hand; empty
	// when the core has no public address.
	UpgradeCommand string
	Version        string
	DistDir        string
}

// AgentUpdatePackage is one package that can be sent to an agent.
type AgentUpdatePackage struct {
	Version string
	Arch    string
	SHA256  string
}

// LatestVersion returns the core version when it is a comparable x.y.z, or an
// empty string otherwise.
func (p AgentPackages) LatestVersion() string {
	if !agentpkg.ValidVersion(p.Version) {
		return ""
	}
	return p.Version
}

// ForAgent returns the package to offer to the agent, if its version is newer
// and the package for its architecture exists.
func (p AgentPackages) ForAgent(agent hub.AgentInfo) (AgentUpdatePackage, bool) {
	if !agentpkg.SupportsSelfUpdate(agent.AgentVersion) {
		return AgentUpdatePackage{}, false
	}
	return p.offer(agent)
}

// ManualUpdate reports whether a newer package exists for an agent too old to
// understand the update order: it must be upgraded once with the command.
func (p AgentPackages) ManualUpdate(agent hub.AgentInfo) bool {
	if agentpkg.SupportsSelfUpdate(agent.AgentVersion) {
		return false
	}
	_, ok := p.offer(agent)
	return ok
}

func (p AgentPackages) offer(agent hub.AgentInfo) (AgentUpdatePackage, bool) {
	if !agentpkg.ValidVersion(p.Version) {
		return AgentUpdatePackage{}, false
	}
	arch, ok := agentpkg.NormalizeArch(agent.Arch)
	if !ok {
		return AgentUpdatePackage{}, false
	}
	if !agentpkg.Newer(p.Version, agent.AgentVersion) {
		return AgentUpdatePackage{}, false
	}
	sha, err := agentpkg.Info(p.DistDir, arch)
	if err != nil {
		return AgentUpdatePackage{}, false
	}
	return AgentUpdatePackage{Version: p.Version, Arch: arch, SHA256: sha}, true
}

// AgentUpdateJobStore creates the update job and checks for one already in
// flight. The store implements it; the tests use a fake.
type AgentUpdateJobStore interface {
	CreateJob(ctx context.Context, serverID, kind string, params map[string]any, createdBy string) (store.Job, error)
	UpdateJobState(ctx context.Context, id, serverID, state, message, output string) (bool, error)
	HasActiveJob(ctx context.Context, serverID, kind string) (bool, error)
}

// AgentUpdateServers reads the enrolled servers for the update endpoints.
type AgentUpdateServers interface {
	GetServer(ctx context.Context, id string) (store.Server, error)
	ListServers(ctx context.Context) ([]store.Server, error)
}

// AgentUpdateDeps are the collaborators of the agent update endpoints.
type AgentUpdateDeps struct {
	Agents     LiveAgents
	Dispatcher CommandDispatcher
	Jobs       AgentUpdateJobStore
	Audit      Auditor
	Servers    AgentUpdateServers
	Packages   AgentPackages
}

type agentUpdateRequest struct {
	Confirm bool `json:"confirm"`
}

// NewAgentUpdateHandler returns POST /api/v1/servers/{serverID}/agent/update.
// The command only starts the installation; the agent reports the final result
// when it reconnects with the new version.
func NewAgentUpdateHandler(deps AgentUpdateDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request agentUpdateRequest
		if err := decodeJSON(w, r, &request); err != nil || !request.Confirm {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}

		serverID := chi.URLParam(r, "serverID")
		server, err := deps.Servers.GetServer(r.Context(), serverID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			slog.Error("get server to update its agent", "server_id", serverID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if !server.RemovedAt.IsZero() {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}

		agent, connected := connectedAgent(deps.Agents, serverID)
		if !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}
		update, ok := deps.Packages.ForAgent(agent)
		if !ok {
			writeError(w, http.StatusConflict, "no_update")
			return
		}
		active, err := deps.Jobs.HasActiveJob(r.Context(), serverID, agentUpdateKind)
		if err != nil {
			slog.Error("check active agent update", "server_id", serverID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if active {
			writeError(w, http.StatusConflict, "update_in_progress")
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

		jobID, err := queueAgentUpdate(r, deps, server, agent, update, actor)
		if err != nil {
			writeAgentUpdateQueueError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, jobCreatedResponse{JobID: jobID})
	})
}

type agentUpdateSkipped struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type agentUpdateAllResponse struct {
	Queued  []string             `json:"queued"`
	Skipped []agentUpdateSkipped `json:"skipped"`
}

// NewAgentUpdateAllHandler returns POST /api/v1/agents/update-all. It queues one
// update per connected server that has one available and reports the servers it
// skipped and why.
func NewAgentUpdateAllHandler(deps AgentUpdateDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request agentUpdateRequest
		if err := decodeJSON(w, r, &request); err != nil || !request.Confirm {
			writeError(w, http.StatusBadRequest, "confirmation_required")
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

		servers, err := deps.Servers.ListServers(r.Context())
		if err != nil {
			slog.Error("list servers to update their agents", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		byServerID := make(map[string]hub.AgentInfo)
		for _, agent := range deps.Agents.Connected() {
			byServerID[agent.ServerID] = agent
		}

		response := agentUpdateAllResponse{Queued: []string{}, Skipped: []agentUpdateSkipped{}}
		for _, server := range servers {
			if !server.RemovedAt.IsZero() {
				continue
			}
			agent, connected := byServerID[server.ID]
			if !connected {
				response.Skipped = append(response.Skipped, agentUpdateSkipped{ID: server.ID, Reason: "agent_offline"})
				continue
			}
			update, ok := deps.Packages.ForAgent(agent)
			if !ok {
				response.Skipped = append(response.Skipped, agentUpdateSkipped{ID: server.ID, Reason: "no_update"})
				continue
			}
			active, err := deps.Jobs.HasActiveJob(r.Context(), server.ID, agentUpdateKind)
			if err != nil {
				slog.Error("check active agent update", "server_id", server.ID, "error", err)
				response.Skipped = append(response.Skipped, agentUpdateSkipped{ID: server.ID, Reason: "internal"})
				continue
			}
			if active {
				response.Skipped = append(response.Skipped, agentUpdateSkipped{ID: server.ID, Reason: "update_in_progress"})
				continue
			}
			jobID, err := queueAgentUpdate(r, deps, server, agent, update, actor)
			if err != nil {
				response.Skipped = append(response.Skipped, agentUpdateSkipped{ID: server.ID, Reason: agentUpdateReason(err)})
				continue
			}
			slog.Info("agent update queued", "server_id", server.ID, "job_id", jobID)
			response.Queued = append(response.Queued, server.ID)
		}

		writeJSON(w, http.StatusAccepted, response)
	})
}

// updateQueueError carries the API reason of a failed queue.
type updateQueueError struct {
	reason string
	err    error
}

func (e *updateQueueError) Error() string { return e.err.Error() }
func (e *updateQueueError) Unwrap() error { return e.err }

// queueAgentUpdate creates the job, sends the typed command and audits the
// request. Success is observed later, when the agent reconnects with the new
// version; the job remains pending until the agent reports its start.
func queueAgentUpdate(r *http.Request, deps AgentUpdateDeps, server store.Server, agent hub.AgentInfo, update AgentUpdatePackage, actor string) (string, error) {
	job, err := deps.Jobs.CreateJob(r.Context(), server.ID, agentUpdateKind, map[string]any{
		"version":      update.Version,
		"sha256":       update.SHA256,
		"from_version": agent.AgentVersion,
		"arch":         update.Arch,
	}, actor)
	if err != nil {
		return "", &updateQueueError{reason: "internal", err: err}
	}

	command := &agentv1.CoreMsg{
		JobId: job.ID,
		Cmd: &agentv1.CoreMsg_AgentUpdate{AgentUpdate: &agentv1.AgentUpdate{
			Version: update.Version,
			Sha256:  update.SHA256,
		}},
	}
	if err := deps.Dispatcher.Dispatch(server.ID, command); err != nil {
		_, _ = deps.Jobs.UpdateJobState(r.Context(), job.ID, server.ID, store.JobFailed, err.Error(), "")
		reason := "agent_busy"
		if errors.Is(err, hub.ErrNotConnected) {
			reason = "agent_offline"
		}
		return "", &updateQueueError{reason: reason, err: err}
	}

	if deps.Audit != nil {
		if err := deps.Audit.AppendAudit(r.Context(), store.AuditEvent{
			Actor:  actor,
			Action: "agent_update",
			IP:     clientIP(r),
			Result: "requested",
			Details: auditDetails(server.ID, job.ID, map[string]any{
				"server_name":  server.Name,
				"from_version": agent.AgentVersion,
				"to_version":   update.Version,
				"sha256":       update.SHA256,
			}),
		}); err != nil {
			slog.Error("append audit event", "action", "agent_update", "error", err)
		}
	}
	return job.ID, nil
}

// agentUpdateReason returns the skipped reason of a queue error.
func agentUpdateReason(err error) string {
	var queueErr *updateQueueError
	if errors.As(err, &queueErr) {
		return queueErr.reason
	}
	return "internal"
}

// writeAgentUpdateQueueError maps a queue failure to the response of the single
// update endpoint.
func writeAgentUpdateQueueError(w http.ResponseWriter, err error) {
	reason := agentUpdateReason(err)
	switch reason {
	case "agent_offline":
		writeError(w, http.StatusConflict, "agent_offline")
	case "agent_busy":
		writeError(w, http.StatusServiceUnavailable, "agent_busy")
	default:
		slog.Error("queue agent update", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
	}
}

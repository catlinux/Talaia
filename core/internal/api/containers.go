package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// containerActionKind is the kind of the jobs created by container actions.
const containerActionKind = "container_action"

// containerLogsKind is the kind of the jobs that read container logs. They get
// their own kind because live logs create one every few seconds and the core
// purges them once they have finished (internal/jobs).
const containerLogsKind = "container_logs"

// jobTimeout is how long an unfinished job of a kind may live before it is
// reported as failed, for example because the agent disconnected without
// answering. It must outlast the timeout the agent gives the same work, or a
// slow job that succeeds would already be closed as failed.
func jobTimeout(kind string) time.Duration {
	const margin = 30 * time.Second
	switch kind {
	case stackActionKind:
		return 10*time.Minute + margin
	case resourcesRemoveKind:
		return 5*time.Minute + margin
	case stackWriteKind:
		return 2*time.Minute + margin
	case aptStatusKind:
		return 2*time.Minute + margin
	case aptRefreshKind:
		return 10*time.Minute + margin
	case aptUpgradeKind:
		return 60*time.Minute + margin
	case hostRebootKind:
		return time.Minute + margin
	case serviceActionKind:
		return 90*time.Second + margin
	default:
		return 90*time.Second + margin
	}
}

// containerRef matches a container id or name. The agent checks it again.
var containerRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// containerActions maps the action names of the API to the typed commands.
var containerActions = map[string]agentv1.ContainerActionType{
	"start":   agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_START,
	"stop":    agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_STOP,
	"restart": agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_RESTART,
	"remove":  agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_REMOVE,
}

// CommandDispatcher queues a typed command for the agent of a server.
type CommandDispatcher interface {
	Dispatch(serverID string, msg *agentv1.CoreMsg) error
}

// JobStore is the part of the store the container API needs.
type JobStore interface {
	CreateJob(ctx context.Context, serverID, kind string, params map[string]any, createdBy string) (store.Job, error)
	GetJob(ctx context.Context, id string) (store.Job, error)
	UpdateJobState(ctx context.Context, id, serverID, state, message, output string) (bool, error)
	FailStaleJob(ctx context.Context, id string, maxAge time.Duration) (bool, error)
	HasActiveJobWithin(ctx context.Context, serverID, kind string, maxAge time.Duration) (bool, error)
}

// ContainerDeps are the collaborators of the container endpoints.
type ContainerDeps struct {
	Agents     LiveAgents
	Dispatcher CommandDispatcher
	Jobs       JobStore
	Audit      Auditor
}

type containerEntry struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Image          string `json:"image"`
	State          string `json:"state"`
	Status         string `json:"status"`
	ComposeProject string `json:"compose_project"`
	ComposeService string `json:"compose_service"`
	// Use is the last sample of the container; absent when it is not running or
	// the sample is too old to be shown.
	Use *containerUse `json:"use,omitempty"`
}

// containerUse is the resource use of one running container.
type containerUse struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemUsedBytes  uint64  `json:"mem_used_bytes"`
	MemLimitBytes uint64  `json:"mem_limit_bytes"`
	NetRxBytes    uint64  `json:"net_rx_bytes"`
	NetTxBytes    uint64  `json:"net_tx_bytes"`
}

// statsFreshness is how old a sample may be and still be shown.
const statsFreshness = 60 * time.Second

type containersResponse struct {
	// Connected is false when the agent of the server has no open stream; the
	// list is then empty.
	Connected       bool             `json:"connected"`
	DockerAvailable bool             `json:"docker_available"`
	Containers      []containerEntry `json:"containers"`
}

// NewContainersHandler returns GET /api/v1/servers/{serverID}/containers.
func NewContainersHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent, ok := connectedAgent(deps.Agents, chi.URLParam(r, "serverID"))

		response := containersResponse{Containers: []containerEntry{}}
		if ok {
			response.Connected = true
			response.DockerAvailable = agent.DockerAvailable
			fresh := !agent.StatsAt.IsZero() && time.Since(agent.StatsAt) < statsFreshness
			for _, container := range agent.ContainerList {
				entry := containerEntry{
					ID:             container.GetId(),
					Name:           container.GetName(),
					Image:          container.GetImage(),
					State:          container.GetState(),
					Status:         container.GetStatus(),
					ComposeProject: container.GetComposeProject(),
					ComposeService: container.GetComposeService(),
				}
				if stat, ok := agent.ContainerStats[container.GetId()]; ok && fresh && container.GetState() == "running" {
					entry.Use = &containerUse{
						CPUPercent:    stat.GetCpuPercent(),
						MemUsedBytes:  stat.GetMemUsedBytes(),
						MemLimitBytes: stat.GetMemLimitBytes(),
						NetRxBytes:    stat.GetNetRxBytes(),
						NetTxBytes:    stat.GetNetTxBytes(),
					}
				}
				response.Containers = append(response.Containers, entry)
			}
		}

		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, response)
	})
}

type containerActionRequest struct {
	Action string `json:"action"`
	// Confirm must be true for the actions that destroy something.
	Confirm bool `json:"confirm"`
}

type jobCreatedResponse struct {
	JobID string `json:"job_id"`
}

// NewContainerActionHandler returns POST
// /api/v1/servers/{serverID}/containers/{container}/actions. It creates a job,
// sends the typed command to the agent and answers 202 with the job id.
func NewContainerActionHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")
		reference := chi.URLParam(r, "container")

		var request containerActionRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		action, known := containerActions[request.Action]
		if !known || !containerRef.MatchString(reference) {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		// Removing is the one action that destroys something: it needs the
		// confirmation the interface collects.
		if action == agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_REMOVE && !request.Confirm {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}

		agent, connected := connectedAgent(deps.Agents, serverID)
		if !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}
		// Only containers the agent reported can be acted on.
		if !hasContainer(agent, reference) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}

		submitJob(w, r, deps, serverID, containerActionKind, "container_"+request.Action,
			map[string]any{"container": reference, "action": request.Action},
			map[string]any{"container": reference},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd: &agentv1.CoreMsg_ContainerAction{
						ContainerAction: &agentv1.ContainerAction{Container: reference, Action: action},
					},
				}
			})
	})
}

type jobResponse struct {
	ID         string         `json:"id"`
	ServerID   string         `json:"server_id"`
	Kind       string         `json:"kind"`
	Params     map[string]any `json:"params"`
	State      string         `json:"state"`
	Message    string         `json:"message"`
	Output     string         `json:"output"`
	CreatedBy  string         `json:"created_by"`
	CreatedAt  time.Time      `json:"created_at"`
	FinishedAt *time.Time     `json:"finished_at"`
}

// NewJobHandler returns GET /api/v1/jobs/{jobID}.
func NewJobHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "jobID")

		job, err := deps.Jobs.GetJob(r.Context(), id)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			slog.Error("get job", "job_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}

		// A job nobody finished is closed here, so the interface never waits for
		// an agent that is gone.
		if job.State == store.JobPending || job.State == store.JobRunning {
			changed, err := deps.Jobs.FailStaleJob(r.Context(), id, jobTimeout(job.Kind))
			if err != nil {
				slog.Warn("fail stale job", "job_id", id, "error", err)
			}
			if changed {
				if job, err = deps.Jobs.GetJob(r.Context(), id); err != nil {
					slog.Error("get job", "job_id", id, "error", err)
					writeError(w, http.StatusInternalServerError, "internal")
					return
				}
			}
		}

		response := jobResponse{
			ID: job.ID, ServerID: job.ServerID, Kind: job.Kind, Params: job.Params,
			State: job.State, Message: job.Message, Output: job.Output, CreatedBy: job.CreatedBy, CreatedAt: job.CreatedAt,
		}
		if !job.FinishedAt.IsZero() {
			finished := job.FinishedAt
			response.FinishedAt = &finished
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, response)
	})
}

// JobRecorder stores the events the agents report about their jobs. It is the
// hub's JobEvents sink.
type JobRecorder struct {
	Jobs JobStore
}

// RecordJobEvent moves the job forward. The store only accepts the event when
// the job belongs to the server that reported it.
func (j JobRecorder) RecordJobEvent(ctx context.Context, serverID string, event *agentv1.JobEvent) {
	var state string
	switch event.GetState() {
	case agentv1.JobState_JOB_STATE_RUNNING:
		state = store.JobRunning
	case agentv1.JobState_JOB_STATE_SUCCEEDED:
		state = store.JobSucceeded
	case agentv1.JobState_JOB_STATE_FAILED:
		state = store.JobFailed
	case agentv1.JobState_JOB_STATE_UNSPECIFIED:
		return
	default:
		return
	}

	// PostgreSQL refuses text that is not valid UTF-8, and a cut can split a
	// character, so the event would be lost.
	message := strings.ToValidUTF8(strings.TrimSpace(event.GetMessage()), "�")
	if len(message) > 500 {
		message = strings.ToValidUTF8(message[:500], "")
	}
	if _, err := j.Jobs.UpdateJobState(ctx, event.GetJobId(), serverID, state, message, event.GetOutput()); err != nil {
		slog.Warn("record job event", "job_id", event.GetJobId(), "error", err)
	}
}

// connectedAgent finds the connected agent of a server.
func connectedAgent(agents LiveAgents, serverID string) (hub.AgentInfo, bool) {
	if serverID == "" {
		return hub.AgentInfo{}, false
	}
	for _, agent := range agents.Connected() {
		if agent.ServerID == serverID {
			return agent, true
		}
	}
	return hub.AgentInfo{}, false
}

// hasContainer reports whether the agent listed a container with this id (full
// or short) or name.
func hasContainer(agent hub.AgentInfo, reference string) bool {
	for _, container := range agent.ContainerList {
		id := container.GetId()
		// The agent reports the short id; the full one starts with it.
		if container.GetName() == reference || id == reference ||
			(id != "" && len(reference) > len(id) && strings.HasPrefix(reference, id)) {
			return true
		}
	}
	return false
}

// submitJob creates a job of the given kind, sends its command to the agent and
// answers 202 with the job id. The caller has already validated the request and
// checked that the target exists on the server. params are stored with the job;
// details go to the audit log next to the server and job ids, so they must never
// hold secrets or file contents.
func submitJob(w http.ResponseWriter, r *http.Request, deps ContainerDeps, serverID, kind, auditAction string, params, details map[string]any, command func(jobID string) *agentv1.CoreMsg) {
	actor := ""
	if session, ok := sessionFromContext(r.Context()); ok {
		actor = session.User.Username
	}
	if actor == "" {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}

	job, err := deps.Jobs.CreateJob(r.Context(), serverID, kind, params, actor)
	if err != nil {
		slog.Error("create job", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}

	if err := deps.Dispatcher.Dispatch(serverID, command(job.ID)); err != nil {
		// The command never left: close the job instead of leaving it pending.
		_, _ = deps.Jobs.UpdateJobState(r.Context(), job.ID, serverID, store.JobFailed, err.Error(), "")
		status, code := http.StatusServiceUnavailable, "agent_busy"
		if errors.Is(err, hub.ErrNotConnected) {
			status, code = http.StatusConflict, "agent_offline"
		}
		writeError(w, status, code)
		return
	}

	if err := deps.Audit.AppendAudit(r.Context(), store.AuditEvent{
		Actor:   actor,
		Action:  auditAction,
		IP:      clientIP(r),
		Result:  "requested",
		Details: auditDetails(serverID, job.ID, details),
	}); err != nil {
		slog.Error("append audit event", "action", auditAction, "error", err)
	}

	writeJSON(w, http.StatusAccepted, jobCreatedResponse{JobID: job.ID})
}

type containerLogsRequest struct {
	// Tail is how many lines from the end; 0 asks for the default.
	Tail int `json:"tail"`
	// Since is an optional RFC 3339 timestamp: only lines after it are returned.
	Since string `json:"since"`
	// Timestamps asks for the timestamp prefix of each line, for live logs.
	Timestamps bool `json:"timestamps"`
}

// maxLogTail is the most lines one request may ask for.
const maxLogTail = 1000

// NewContainerLogsHandler returns POST
// /api/v1/servers/{serverID}/containers/{container}/logs. The lines come back
// in the output of the job it creates. The log text is never written to the
// audit log: only the fact that it was read.
func NewContainerLogsHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")
		reference := chi.URLParam(r, "container")

		var request containerLogsRequest
		since := ""
		valid := decodeJSON(w, r, &request) == nil && request.Tail >= 0 && request.Tail <= maxLogTail &&
			containerRef.MatchString(reference)
		if valid {
			since = strings.TrimSpace(request.Since)
			// The timestamp is optional, but an unparsable one is a bad request
			// and never reaches the agent.
			if len(request.Since) > 40 {
				valid = false
			} else if since != "" {
				if _, err := time.Parse(time.RFC3339, since); err != nil {
					valid = false
				}
			}
		}
		if !valid {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}

		agent, connected := connectedAgent(deps.Agents, serverID)
		if !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}
		if !hasContainer(agent, reference) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}

		submitJob(w, r, deps, serverID, containerLogsKind, "container_logs",
			map[string]any{"container": reference, "tail": request.Tail},
			map[string]any{"container": reference},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd: &agentv1.CoreMsg_ReadLogs{
						ReadLogs: &agentv1.ReadLogs{
							Container:  reference,
							Tail:       uint32(request.Tail),
							Since:      since,
							Timestamps: request.Timestamps,
						},
					},
				}
			})
	})
}

// auditDetails joins the common ids with the details of one request.
func auditDetails(serverID, jobID string, details map[string]any) map[string]any {
	merged := map[string]any{"server_id": serverID, "job_id": jobID}
	for key, value := range details {
		merged[key] = value
	}
	return merged
}

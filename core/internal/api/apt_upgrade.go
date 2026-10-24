package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// Job kinds of the apt write operations.
const (
	aptUpgradeKind = "apt_upgrade"
	hostRebootKind = "host_reboot"
)

// aptUpgradeModes maps the request name to the typed command mode. The core
// never sends package names: the agent computes them.
var aptUpgradeModes = map[string]agentv1.AptUpgradeMode{
	"security": agentv1.AptUpgradeMode_APT_UPGRADE_MODE_SECURITY,
	"all":      agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL,
}

type aptUpgradeRequest struct {
	Mode    string `json:"mode"`
	Confirm bool   `json:"confirm"`
}

type hostRebootRequest struct {
	// Confirm must be the hostname the connected agent reports.
	Confirm string `json:"confirm"`
}

// NewAptUpgradeHandler returns POST /api/v1/servers/{serverID}/apt/upgrade. The
// request only chooses a mode; the agent works out the package list itself.
func NewAptUpgradeHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")

		var request aptUpgradeRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		mode, known := aptUpgradeModes[request.Mode]
		if !known {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if !request.Confirm {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}
		if _, connected := connectedAgent(deps.Agents, serverID); !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}
		active, err := activeAptOperation(r.Context(), deps.Jobs, serverID)
		if err != nil {
			slog.Error("check active apt operation", "server_id", serverID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if active {
			writeError(w, http.StatusConflict, "operation_in_progress")
			return
		}

		submitJob(w, r, deps, serverID, aptUpgradeKind, "apt_upgrade",
			map[string]any{"mode": request.Mode},
			map[string]any{"mode": request.Mode},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd: &agentv1.CoreMsg_AptUpgrade{
						AptUpgrade: &agentv1.AptUpgrade{Mode: mode},
					},
				}
			})
	})
}

// NewHostRebootHandler returns POST /api/v1/servers/{serverID}/reboot. The
// confirmation must be the hostname the connected agent reports, so a command
// cannot reboot another machine.
func NewHostRebootHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")

		var request hostRebootRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}
		if strings.TrimSpace(request.Confirm) == "" {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}
		agent, connected := connectedAgent(deps.Agents, serverID)
		if !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}
		hostname := strings.TrimSpace(agent.Hostname)
		if hostname == "" || !strings.EqualFold(strings.TrimSpace(request.Confirm), hostname) {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}
		active, err := activeAptOperation(r.Context(), deps.Jobs, serverID)
		if err != nil {
			slog.Error("check active apt operation", "server_id", serverID, "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}
		if active {
			writeError(w, http.StatusConflict, "operation_in_progress")
			return
		}

		submitJobWithSuccess(w, r, deps, serverID, hostRebootKind, "host_reboot",
			map[string]any{"hostname": hostname},
			map[string]any{"hostname": hostname},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd: &agentv1.CoreMsg_HostReboot{
						HostReboot: &agentv1.HostReboot{Hostname: hostname},
					},
				}
			}, func() {
				if deps.Alerts != nil {
					deps.Alerts.SilenceServer(r.Context(), serverID, 10*time.Minute, "reboot")
				}
			})
	})
}

// activeAptOperation reports whether the server has an unfinished apt upgrade
// or reboot that can still finish. The age windows match jobTimeout, so a job
// the agent abandoned does not block a new attempt forever.
func activeAptOperation(ctx context.Context, jobs JobStore, serverID string) (bool, error) {
	for _, kind := range []string{aptUpgradeKind, hostRebootKind} {
		active, err := jobs.HasActiveJobWithin(ctx, serverID, kind, jobTimeout(kind))
		if err != nil {
			return false, fmt.Errorf("check active %s job: %w", kind, err)
		}
		if active {
			return true, nil
		}
	}
	return false, nil
}

package api

import (
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/hub"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// serviceActionKind is the job kind of the typed service actions.
const serviceActionKind = "service_action"

var serviceUnitRE = regexp.MustCompile(`^[A-Za-z0-9@._:-]{1,250}\.service$`)

var serviceActions = map[string]agentv1.ServiceActionType{
	"start":   agentv1.ServiceActionType_SERVICE_ACTION_TYPE_START,
	"stop":    agentv1.ServiceActionType_SERVICE_ACTION_TYPE_STOP,
	"restart": agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RESTART,
	"reload":  agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RELOAD,
	"enable":  agentv1.ServiceActionType_SERVICE_ACTION_TYPE_ENABLE,
	"disable": agentv1.ServiceActionType_SERVICE_ACTION_TYPE_DISABLE,
}

// serviceEntry is one systemd unit as the panel receives it.
type serviceEntry struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	LoadState     string `json:"load_state"`
	ActiveState   string `json:"active_state"`
	SubState      string `json:"sub_state"`
	UnitFileState string `json:"unit_file_state"`
}

type servicesResponse struct {
	Services []serviceEntry `json:"services"`
}

type serviceActionRequest struct {
	Action string `json:"action"`
	// Confirm must be true for the actions that can stop the service.
	Confirm bool `json:"confirm"`
}

// NewServicesHandler returns GET /api/v1/servers/{serverID}/services. The list
// is what the connected agent reported in its last inventory, sorted by name.
func NewServicesHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent, connected := connectedAgent(deps.Agents, chi.URLParam(r, "serverID"))
		if !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}

		services := make([]serviceEntry, 0, len(agent.ServiceList))
		for _, service := range agent.ServiceList {
			services = append(services, serviceEntry{
				Name:          service.GetName(),
				Description:   service.GetDescription(),
				LoadState:     service.GetLoadState(),
				ActiveState:   service.GetActiveState(),
				SubState:      service.GetSubState(),
				UnitFileState: service.GetUnitFileState(),
			})
		}
		sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })

		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, servicesResponse{Services: services})
	})
}

// NewServiceActionHandler returns POST
// /api/v1/servers/{serverID}/services/{unit}/actions. It only accepts a unit
// the agent reported and sends a typed action, never a command line.
func NewServiceActionHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")
		unit := chi.URLParam(r, "unit")

		var request serviceActionRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if !validServiceUnit(unit) {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		action, known := serviceActions[request.Action]
		if !known {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if serviceNeedsConfirmation(action) && !request.Confirm {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}

		agent, connected := connectedAgent(deps.Agents, serverID)
		if !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}
		if unit == "talaia-agent.service" &&
			(action == agentv1.ServiceActionType_SERVICE_ACTION_TYPE_STOP ||
				action == agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RESTART ||
				action == agentv1.ServiceActionType_SERVICE_ACTION_TYPE_DISABLE) {
			writeError(w, http.StatusBadRequest, "self_management")
			return
		}
		if !hasService(agent, unit) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}

		submitJob(w, r, deps, serverID, serviceActionKind, "service_"+request.Action,
			map[string]any{"unit": unit, "action": request.Action},
			map[string]any{"unit": unit},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd: &agentv1.CoreMsg_ServiceAction{
						ServiceAction: &agentv1.ServiceAction{Unit: unit, Action: action},
					},
				}
			})
	})
}

func validServiceUnit(unit string) bool {
	return !strings.HasPrefix(unit, "-") && serviceUnitRE.MatchString(unit)
}

func serviceNeedsConfirmation(action agentv1.ServiceActionType) bool {
	switch action {
	case agentv1.ServiceActionType_SERVICE_ACTION_TYPE_STOP,
		agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RESTART,
		agentv1.ServiceActionType_SERVICE_ACTION_TYPE_DISABLE:
		return true
	default:
		return false
	}
}

func hasService(agent hub.AgentInfo, unit string) bool {
	for _, service := range agent.ServiceList {
		if service.GetName() == unit {
			return true
		}
	}
	return false
}

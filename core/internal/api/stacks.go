package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/hub"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// Job kinds of the stack commands.
const (
	stackAdoptKind  = "stack_adopt"
	stackWriteKind  = "stack_write"
	stackReadKind   = "stack_read"
	stackActionKind = "stack_action"
)

const (
	// maxComposeBytes and maxEnvBytes bound what the panel may send for a stack.
	maxComposeBytes = 256 << 10
	maxEnvBytes     = 64 << 10
)

// stackName matches the name of a managed stack. The agent checks it again.
var stackName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// stackActions maps the action names of the API to the typed commands.
var stackActions = map[string]agentv1.StackActionType{
	"up":      agentv1.StackActionType_STACK_ACTION_TYPE_UP,
	"down":    agentv1.StackActionType_STACK_ACTION_TYPE_DOWN,
	"pull":    agentv1.StackActionType_STACK_ACTION_TYPE_PULL,
	"restart": agentv1.StackActionType_STACK_ACTION_TYPE_RESTART,
	"remove":  agentv1.StackActionType_STACK_ACTION_TYPE_REMOVE,
	"forget":  agentv1.StackActionType_STACK_ACTION_TYPE_FORGET,
}

// destructiveStackActions need an explicit confirmation: they stop a service or
// delete the files of the stack.
var destructiveStackActions = map[string]bool{"down": true, "remove": true}

type stacksResponse struct {
	Connected bool     `json:"connected"`
	Stacks    []string `json:"stacks"`
	// Adopted lists the managed stacks that were adopted from a running project.
	Adopted []string `json:"adopted"`
	// Broken lists the adopted stacks whose files are gone from the host.
	Broken []string `json:"broken"`
	// Adoptable lists the running Compose projects Talaia does not manage yet.
	Adoptable []string `json:"adoptable"`
}

// NewStacksHandler returns GET /api/v1/servers/{serverID}/stacks: the names of
// the stacks Talaia manages on the server.
func NewStacksHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent, ok := connectedAgent(deps.Agents, chi.URLParam(r, "serverID"))

		response := stacksResponse{
			Connected: ok,
			Stacks:    []string{},
			Adopted:   []string{},
			Broken:    []string{},
			Adoptable: []string{},
		}
		if ok {
			response.Stacks = append(response.Stacks, agent.ManagedStacks...)
			response.Adopted = append(response.Adopted, agent.AdoptedStacks...)
			response.Broken = append(response.Broken, agent.BrokenStacks...)
			response.Adoptable = append(response.Adoptable, adoptableProjects(agent)...)
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, response)
	})
}

type stackWriteRequest struct {
	Compose string `json:"compose"`
	Env     string `json:"env"`
}

// NewStackWriteHandler returns PUT /api/v1/servers/{serverID}/stacks/{name}. The
// agent validates the project with docker compose before it replaces the files;
// the answer of the job says whether it accepted them. Neither the files nor the
// environment reach the audit log, only their hashes.
func NewStackWriteHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")
		name := chi.URLParam(r, "name")

		var request stackWriteRequest
		if err := decodeJSON(w, r, &request); err != nil || !stackName.MatchString(name) ||
			strings.TrimSpace(request.Compose) == "" ||
			len(request.Compose) > maxComposeBytes || len(request.Env) > maxEnvBytes {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		agent, connected := connectedAgent(deps.Agents, serverID)
		if !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}
		// A new stack named like a running project Talaia does not manage would
		// take that project over: "up --remove-orphans" removes its containers.
		// That project is adopted instead.
		if slices.Contains(adoptableProjects(agent), name) {
			writeError(w, http.StatusConflict, "name_taken")
			return
		}

		submitJob(w, r, deps, serverID, stackWriteKind, "stack_write",
			map[string]any{"name": name},
			map[string]any{"stack": name, "compose_sha256": hashOf(request.Compose), "env_sha256": hashOf(request.Env)},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd: &agentv1.CoreMsg_StackWrite{StackWrite: &agentv1.StackWrite{
						Name: name, ComposeYaml: request.Compose, Env: request.Env,
					}},
				}
			})
	})
}

// NewStackReadHandler returns POST /api/v1/servers/{serverID}/stacks/{name}/read.
// The files come back as the JSON output of the job. The environment may hold
// secrets, so reading it is audited.
func NewStackReadHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID, name := chi.URLParam(r, "serverID"), chi.URLParam(r, "name")
		if !stackName.MatchString(name) {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if !managedStack(w, deps, serverID, name) {
			return
		}

		submitJob(w, r, deps, serverID, stackReadKind, "stack_read",
			map[string]any{"name": name},
			map[string]any{"stack": name},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd:   &agentv1.CoreMsg_StackRead{StackRead: &agentv1.StackRead{Name: name}},
				}
			})
	})
}

type stackActionRequest struct {
	Action string `json:"action"`
	// Confirm must be true for the actions that stop a service or delete files.
	Confirm bool `json:"confirm"`
}

// NewStackActionHandler returns POST
// /api/v1/servers/{serverID}/stacks/{name}/actions.
func NewStackActionHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID, name := chi.URLParam(r, "serverID"), chi.URLParam(r, "name")

		var request stackActionRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		action, known := stackActions[request.Action]
		if !known || !stackName.MatchString(name) {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if destructiveStackActions[request.Action] && !request.Confirm {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}
		if !managedStack(w, deps, serverID, name) {
			return
		}
		// An adopted stack is forgotten, never removed; a stack of Talaia is
		// removed, never forgotten.
		if agent, ok := connectedAgent(deps.Agents, serverID); ok {
			adopted := slices.Contains(agent.AdoptedStacks, name)
			if (request.Action == "remove" && adopted) || (request.Action == "forget" && !adopted) {
				writeError(w, http.StatusBadRequest, "invalid_request")
				return
			}
		}

		submitJob(w, r, deps, serverID, stackActionKind, "stack_"+request.Action,
			map[string]any{"name": name, "action": request.Action},
			map[string]any{"stack": name},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd: &agentv1.CoreMsg_StackAction{
						StackAction: &agentv1.StackAction{Name: name, Action: action},
					},
				}
			})
	})
}

// managedStack answers the error itself and returns false unless the agent of
// the server is connected and lists the stack.
func managedStack(w http.ResponseWriter, deps ContainerDeps, serverID, name string) bool {
	agent, connected := connectedAgent(deps.Agents, serverID)
	if !connected {
		writeError(w, http.StatusConflict, "agent_offline")
		return false
	}
	if !slices.Contains(agent.ManagedStacks, name) {
		writeError(w, http.StatusNotFound, "not_found")
		return false
	}
	return true
}

// hashOf returns the SHA-256 of a text in hexadecimal, or "" for an empty one.
func hashOf(text string) string {
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// adoptableProjects returns the Compose projects the agent runs that Talaia does
// not manage, sorted and without repeats, as the containers report them.
func adoptableProjects(agent hub.AgentInfo) []string {
	var names []string
	for _, container := range agent.ContainerList {
		name := container.GetComposeProject()
		if name == "" || !stackName.MatchString(name) || slices.Contains(agent.ManagedStacks, name) || slices.Contains(names, name) {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// NewStackAdoptHandler returns POST /api/v1/servers/{serverID}/stacks/{name}/adopt.
// The agent records where the running project lives; the core sends only its
// name, never a path, and only for a project the agent itself reported.
func NewStackAdoptHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID, name := chi.URLParam(r, "serverID"), chi.URLParam(r, "name")
		if !stackName.MatchString(name) {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		agent, connected := connectedAgent(deps.Agents, serverID)
		if !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}
		if slices.Contains(agent.ManagedStacks, name) {
			writeError(w, http.StatusConflict, "name_taken")
			return
		}
		if !slices.Contains(adoptableProjects(agent), name) {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}

		submitJob(w, r, deps, serverID, stackAdoptKind, "stack_adopt",
			map[string]any{"name": name},
			map[string]any{"stack": name},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd:   &agentv1.CoreMsg_StackAdopt{StackAdopt: &agentv1.StackAdopt{Name: name}},
				}
			})
	})
}

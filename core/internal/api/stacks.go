package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/rbac"
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
		effective, hasEffective := effectiveFromRequest(r)
		if !hasEffective {
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, response)
			return
		}
		base, err := targetFromRequest(r, targetServer)
		if err != nil {
			internal(w, "build stack list target", err)
			return
		}
		if ok {
			for _, name := range agent.ManagedStacks {
				if !stackVisible(effective, base, name) {
					continue
				}
				response.Stacks = append(response.Stacks, name)
			}
			for _, name := range agent.AdoptedStacks {
				if !stackVisible(effective, base, name) {
					continue
				}
				response.Adopted = append(response.Adopted, name)
			}
			for _, name := range agent.BrokenStacks {
				if !stackVisible(effective, base, name) {
					continue
				}
				response.Broken = append(response.Broken, name)
			}
			for _, name := range adoptableProjects(agent) {
				if !stackVisible(effective, base, name) {
					continue
				}
				response.Adoptable = append(response.Adoptable, name)
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, response)
	})
}

func stackVisible(effective rbac.Effective, base rbac.Target, name string) bool {
	base.Stack = name
	return effective.Can(rbac.PermStackView, base)
}

// AllStack is one stack in GET /api/v1/stacks.
type AllStack struct {
	ServerID   string `json:"server_id"`
	ServerName string `json:"server_name"`
	Name       string `json:"name"`
	// State is "managed", "adopted" or "broken".
	State string `json:"state"`
}

type allStacksResponse struct {
	Stacks []AllStack `json:"stacks"`
}

// NewAllStacksHandler returns GET /api/v1/stacks. It lists the stacks of every
// connected server the caller can see. The handler filters with the effective
// permissions resolved by requireRoute.
func NewAllStacksHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		effective, hasEffective := effectiveFromRequest(r)
		response := allStacksResponse{Stacks: []AllStack{}}
		if !hasEffective {
			writeJSON(w, http.StatusOK, response)
			return
		}

		names := map[string]string{}
		groups := map[string]string{}
		if deps.Servers != nil {
			servers, err := deps.Servers.ListServers(r.Context())
			if err != nil {
				internal(w, "list servers for stacks", err)
				return
			}
			for _, server := range servers {
				names[server.ID] = server.Name
				groups[server.ID] = server.GroupID
			}
		}

		seen := map[string]bool{}
		for _, agent := range connectedAgents(deps.Agents) {
			serverID := agent.ServerID
			serverName := names[serverID]
			if serverID == "" {
				serverID = devServerPrefix + agent.Hostname
				serverName = agent.Hostname
			}
			if serverName == "" {
				serverName = agent.Hostname
			}
			if !effective.CanSeeServer(serverID, groups[serverID]) {
				continue
			}
			for name, state := range stackStates(agent) {
				base := rbac.Target{ServerID: serverID, GroupID: groups[serverID], Stack: name}
				if !effective.Can(rbac.PermStackView, base) {
					continue
				}
				key := serverID + "\x00" + name
				if seen[key] {
					continue
				}
				seen[key] = true
				response.Stacks = append(response.Stacks, AllStack{
					ServerID: serverID, ServerName: serverName, Name: name, State: state,
				})
			}
		}

		sort.Slice(response.Stacks, func(i, j int) bool {
			if response.Stacks[i].ServerName != response.Stacks[j].ServerName {
				return response.Stacks[i].ServerName < response.Stacks[j].ServerName
			}
			if response.Stacks[i].ServerID != response.Stacks[j].ServerID {
				return response.Stacks[i].ServerID < response.Stacks[j].ServerID
			}
			return response.Stacks[i].Name < response.Stacks[j].Name
		})
		writeJSON(w, http.StatusOK, response)
	})
}

func connectedAgents(agents LiveAgents) []hub.AgentInfo {
	if agents == nil {
		return nil
	}
	return agents.Connected()
}

func stackStates(agent hub.AgentInfo) map[string]string {
	states := map[string]string{}
	for _, name := range agent.ManagedStacks {
		states[name] = "managed"
	}
	for _, name := range agent.AdoptedStacks {
		states[name] = "adopted"
	}
	for _, name := range agent.BrokenStacks {
		states[name] = "broken"
	}
	return states
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
		// Removing or forgetting a stack changes what is deployed, like editing
		// its compose file, so stack.deploy alone is not enough.
		if request.Action == "remove" || request.Action == "forget" {
			target, err := targetFromRequest(r, targetStack)
			if err != nil {
				internal(w, "build stack target", err)
				return
			}
			if !authorize(r, rbac.PermStackEdit, target) {
				denyRequest(w, r, rbac.PermStackEdit, target, http.StatusForbidden, "forbidden")
				return
			}
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

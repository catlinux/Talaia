package api

import (
	"net/http"

	"github.com/catlinux/Talaia/core/internal/rbac"
)

// MeUser is the user block of GET /api/v1/me.
type MeUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// MePermissions is the permission view of GET /api/v1/me.
//
// Organization contains the permissions that apply to an empty Target.
// Servers maps each visible server id to the permissions that apply to the
// server target. Stacks and Containers map a server id to a name and the
// permissions that apply to that stack or container target. The JSON field
// names are part of the panel contract.
type MePermissions struct {
	Organization []rbac.Permission                       `json:"organization"`
	Servers      map[string][]rbac.Permission            `json:"servers"`
	Stacks       map[string]map[string][]rbac.Permission `json:"stacks"`
	Containers   map[string]map[string][]rbac.Permission `json:"containers"`
}

// MeResponse is the body of GET /api/v1/me. Bindings is the resolved summary
// used by the panel to explain effective access; permissions contains the
// concrete checks it uses to hide controls.
type MeResponse struct {
	User        MeUser        `json:"user"`
	Bindings    rbac.Summary  `json:"bindings"`
	Permissions MePermissions `json:"permissions"`
}

// MeDeps are the collaborators of GET /api/v1/me.
type MeDeps struct {
	Servers ServerLister
	Agents  LiveAgents
}

// NewMeHandler returns GET /api/v1/me. It is registered as permAny and relies
// on the effective permissions resolved by requireRoute.
func NewMeHandler(deps MeDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := sessionFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "session_required")
			return
		}
		effective, ok := effectiveFromRequest(r)
		if !ok {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}

		response := MeResponse{
			User: MeUser{
				ID:          session.User.ID,
				Username:    session.User.Username,
				DisplayName: session.User.DisplayName,
				Email:       session.User.Email,
			},
			Bindings: effective.Summary(),
			Permissions: MePermissions{
				Organization: permissionsFor(effective, rbac.Target{}),
				Servers:      map[string][]rbac.Permission{},
				Stacks:       map[string]map[string][]rbac.Permission{},
				Containers:   map[string]map[string][]rbac.Permission{},
			},
		}

		if deps.Servers == nil {
			writeJSON(w, http.StatusOK, response)
			return
		}
		servers, err := deps.Servers.ListServers(r.Context())
		if err != nil {
			internal(w, "list servers for me", err)
			return
		}

		for _, server := range servers {
			if !effective.CanSeeServer(server.ID, server.GroupID) {
				continue
			}
			serverTarget := rbac.Target{ServerID: server.ID, GroupID: server.GroupID}
			response.Permissions.Servers[server.ID] = permissionsFor(effective, serverTarget)

			agent, connected := connectedAgent(deps.Agents, server.ID)
			if !connected {
				continue
			}
			stackNames := map[string]bool{}
			for _, name := range agent.ManagedStacks {
				stackNames[name] = true
			}
			for _, name := range agent.AdoptedStacks {
				stackNames[name] = true
			}
			for _, name := range agent.BrokenStacks {
				stackNames[name] = true
			}
			for name := range stackNames {
				target := serverTarget
				target.Stack = name
				if !effective.Can(rbac.PermStackView, target) {
					continue
				}
				if response.Permissions.Stacks[server.ID] == nil {
					response.Permissions.Stacks[server.ID] = map[string][]rbac.Permission{}
				}
				response.Permissions.Stacks[server.ID][name] = permissionsFor(effective, target)
			}

			for _, container := range agent.ContainerList {
				name := container.GetName()
				if name == "" {
					name = container.GetId()
				}
				target := serverTarget
				target.Stack = container.GetComposeProject()
				target.Container = name
				if !effective.Can(rbac.PermContainerView, target) {
					target.Container = container.GetId()
					if !effective.Can(rbac.PermContainerView, target) {
						continue
					}
				}
				if response.Permissions.Containers[server.ID] == nil {
					response.Permissions.Containers[server.ID] = map[string][]rbac.Permission{}
				}
				response.Permissions.Containers[server.ID][name] = permissionsFor(effective, target)
			}
		}

		writeJSON(w, http.StatusOK, response)
	})
}

func permissionsFor(effective rbac.Effective, target rbac.Target) []rbac.Permission {
	permissions := make([]rbac.Permission, 0, len(rbac.AllPermissions))
	for _, permission := range rbac.AllPermissions {
		if effective.Can(permission, target) {
			permissions = append(permissions, permission)
		}
	}
	return permissions
}

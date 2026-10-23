// Package api exposes the talaia-core HTTP API and serves the web panel.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// pingTimeout bounds the dependency check made by the health endpoint.
const pingTimeout = 2 * time.Second

// Pinger reports whether a dependency is reachable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// RouterDeps are the collaborators of the core router.
type RouterDeps struct {
	DB       Pinger
	Auth     AuthDeps
	Session  SessionDeps
	Security SecurityConfig
	WebDir   string
	// Servers and Agents feed GET /api/v1/servers.
	Servers ServerLister
	Agents  LiveAgents
	// ServerRemover enables DELETE /api/v1/servers/{serverID}; nil leaves it out.
	ServerRemover ServerRemover
	// ServerDisconnector closes the live agent of a server being removed; nil
	// skips it.
	ServerDisconnector ServerDisconnector
	// ServerAudit records server administration events; nil skips them.
	ServerAudit Auditor
	// AuditLog reads the audit log for GET /api/v1/audit; nil answers 500.
	AuditLog AuditReader
	// AgentPackages describes the agent packages the core can offer in GET
	// /api/v1/servers and in the update endpoints.
	AgentPackages AgentPackages
	// AgentUpdates enables the agent update endpoints; nil leaves them out.
	AgentUpdates *AgentUpdateDeps
	// Enrollment enables POST /api/v1/servers/enrollment-tokens; nil leaves it
	// out (the insecure development mode has no CA to enroll against).
	Enrollment *EnrollmentTokenDeps
	// Containers enables the container and job endpoints; nil leaves them out.
	Containers *ContainerDeps
	// Terminal enables the terminal endpoints; nil leaves them out. Build it with
	// NewTerminalDeps.
	Terminal *TerminalDeps
	// System enables the version and update endpoints; nil leaves them out.
	System *SystemDeps
	// Authz enforces the permissions of the protected API. Nil fails closed.
	Authz *Authorizer
}

// NewRouter returns the HTTP handler of the core: the health endpoint, the
// authentication API, the protected API routes and the static panel.
func NewRouter(deps RouterDeps) http.Handler {
	r := chi.NewRouter()
	r.Use(securityHeaders(deps.Security))

	r.Get("/healthz", health(deps.DB))

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(Sessions(deps.Session))
		r.Use(securityOrigin(deps.Security))
		// The first allowed browser origin is the configured public base URL.
		publicURL := ""
		if len(deps.Security.Origins) > 0 {
			publicURL = deps.Security.Origins[0]
		}
		oidcSettings := newOIDCSettingsHandler(OIDCSettingsDeps{
			Store: deps.Auth.Store, Box: deps.Auth.Box, Audit: deps.Auth.Store, PublicURL: publicURL,
		})
		authHandler := newAuthHandler(deps.Auth)
		authHandler.oidc = oidcSettings
		r.Mount("/auth", authHandler.routes())

		// Every other /api/v1 route requires a session that passed both
		// factors. Later tasks add their endpoints to this group.
		r.Group(func(r chi.Router) {
			r.Use(RequireFull)
			serverDeps := ServerDeps{
				Servers:    deps.Servers,
				Agents:     deps.Agents,
				Remover:    deps.ServerRemover,
				Disconnect: deps.ServerDisconnector,
				Audit:      deps.ServerAudit,
				Packages:   deps.AgentPackages,
			}
			registerRoute(r, deps.Authz, http.MethodGet, "/servers", NewServersHandler(serverDeps))
			registerRoute(r, deps.Authz, http.MethodGet, "/me", NewMeHandler(MeDeps{
				Servers: deps.Servers,
				Agents:  deps.Agents,
			}))
			registerRoute(r, deps.Authz, http.MethodGet, "/audit", NewAuditHandler(AuditDeps{Entries: deps.AuditLog}))
			registerRoute(r, deps.Authz, http.MethodGet, "/settings/oidc", http.HandlerFunc(oidcSettings.getSettings))
			registerRoute(r, deps.Authz, http.MethodPut, "/settings/oidc", http.HandlerFunc(oidcSettings.updateSettings))
			userAdmin := newUserAdminHandler(UserAdminDeps{
				Store: deps.Auth.Store, Audit: deps.Auth.Store, TOTP: authHandler,
				Limiter: authHandler.deps.Limiter, Now: authHandler.now,
			})
			registerRoute(r, deps.Authz, http.MethodGet, "/users", http.HandlerFunc(userAdmin.listUsers))
			registerRoute(r, deps.Authz, http.MethodPost, "/users/invites", http.HandlerFunc(userAdmin.createInvite))
			registerRoute(r, deps.Authz, http.MethodGet, "/users/invites", http.HandlerFunc(userAdmin.listInvites))
			registerRoute(r, deps.Authz, http.MethodDelete, "/users/invites/{inviteID}", http.HandlerFunc(userAdmin.revokeInvite))
			registerRoute(r, deps.Authz, http.MethodGet, "/users/{userID}/bindings", http.HandlerFunc(userAdmin.listUserBindings))
			registerRoute(r, deps.Authz, http.MethodPost, "/users/{userID}/bindings", http.HandlerFunc(userAdmin.createBinding))
			registerRoute(r, deps.Authz, http.MethodPatch, "/users/{userID}", http.HandlerFunc(userAdmin.updateUser))
			registerRoute(r, deps.Authz, http.MethodPost, "/users/{userID}/disable", http.HandlerFunc(userAdmin.disableUser))
			registerRoute(r, deps.Authz, http.MethodPost, "/users/{userID}/enable", http.HandlerFunc(userAdmin.enableUser))
			registerRoute(r, deps.Authz, http.MethodPatch, "/bindings/{bindingID}", http.HandlerFunc(userAdmin.updateBinding))
			registerRoute(r, deps.Authz, http.MethodDelete, "/bindings/{bindingID}", http.HandlerFunc(userAdmin.deleteBinding))
			registerRoute(r, deps.Authz, http.MethodPost, "/apikeys", http.HandlerFunc(userAdmin.createAPIKey))
			registerRoute(r, deps.Authz, http.MethodGet, "/apikeys", http.HandlerFunc(userAdmin.listAPIKeys))
			registerRoute(r, deps.Authz, http.MethodDelete, "/apikeys/{id}", http.HandlerFunc(userAdmin.revokeAPIKey))
			registerRoute(r, deps.Authz, http.MethodGet, "/groups", http.HandlerFunc(userAdmin.listGroups))
			registerRoute(r, deps.Authz, http.MethodPost, "/groups", http.HandlerFunc(userAdmin.createGroup))
			registerRoute(r, deps.Authz, http.MethodPatch, "/groups/{groupID}", http.HandlerFunc(userAdmin.updateGroup))
			registerRoute(r, deps.Authz, http.MethodDelete, "/groups/{groupID}", http.HandlerFunc(userAdmin.deleteGroup))
			registerRoute(r, deps.Authz, http.MethodPut, "/servers/{serverID}/group", http.HandlerFunc(userAdmin.setServerGroup))
			registerRoute(r, deps.Authz, http.MethodGet, "/teams", http.HandlerFunc(userAdmin.listTeams))
			registerRoute(r, deps.Authz, http.MethodPost, "/teams", http.HandlerFunc(userAdmin.createTeam))
			registerRoute(r, deps.Authz, http.MethodGet, "/teams/{teamID}", http.HandlerFunc(userAdmin.getTeam))
			registerRoute(r, deps.Authz, http.MethodPatch, "/teams/{teamID}", http.HandlerFunc(userAdmin.updateTeam))
			registerRoute(r, deps.Authz, http.MethodDelete, "/teams/{teamID}", http.HandlerFunc(userAdmin.deleteTeam))
			registerRoute(r, deps.Authz, http.MethodPut, "/teams/{teamID}/members/{userID}", http.HandlerFunc(userAdmin.addTeamMember))
			registerRoute(r, deps.Authz, http.MethodDelete, "/teams/{teamID}/members/{userID}", http.HandlerFunc(userAdmin.removeTeamMember))
			registerRoute(r, deps.Authz, http.MethodGet, "/teams/{teamID}/bindings", http.HandlerFunc(userAdmin.listTeamBindings))
			registerRoute(r, deps.Authz, http.MethodPost, "/teams/{teamID}/bindings", http.HandlerFunc(userAdmin.createTeamBinding))
			if deps.AgentUpdates != nil {
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/agent/update",
					NewAgentUpdateHandler(*deps.AgentUpdates))
				registerRoute(r, deps.Authz, http.MethodPost, "/agents/update-all",
					NewAgentUpdateAllHandler(*deps.AgentUpdates))
			}
			if deps.ServerRemover != nil {
				registerRoute(r, deps.Authz, http.MethodDelete, "/servers/{serverID}",
					NewServerRemoveHandler(serverDeps))
			}
			if deps.Containers != nil {
				registerRoute(r, deps.Authz, http.MethodGet, "/servers/{serverID}/containers",
					NewContainersHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/containers/{container}/actions",
					NewContainerActionHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/containers/{container}/logs",
					NewContainerLogsHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/journal",
					NewJournalHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/apt/status",
					NewAptStatusHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/apt/upgrade",
					NewAptUpgradeHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/reboot",
					NewHostRebootHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodGet, "/servers/{serverID}/services",
					NewServicesHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/services/{unit}/actions",
					NewServiceActionHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodGet, "/jobs/{jobID}",
					NewJobHandler(*deps.Containers))
				if deps.Terminal != nil {
					deps.Terminal.TOTP = authHandler
					deps.Terminal.Limiter = authHandler.deps.Limiter
					deps.Terminal.Audit = authHandler.deps.Store
					registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/containers/{container}/terminal",
						NewTerminalOpenHandler(deps.Terminal))
					registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/terminal",
						NewHostTerminalOpenHandler(deps.Terminal))
					registerRoute(r, deps.Authz, http.MethodGet, "/terminal/{sessionID}/ws",
						NewTerminalSocketHandler(deps.Terminal))
				}
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/resources/list",
					NewResourcesListHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/resources/remove",
					NewResourcesRemoveHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodGet, "/stacks",
					NewAllStacksHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodGet, "/servers/{serverID}/stacks",
					NewStacksHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPut, "/servers/{serverID}/stacks/{name}",
					NewStackWriteHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/stacks/{name}/read",
					NewStackReadHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/stacks/{name}/adopt",
					NewStackAdoptHandler(*deps.Containers))
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/{serverID}/stacks/{name}/actions",
					NewStackActionHandler(*deps.Containers))
			}
			if deps.Enrollment != nil {
				registerRoute(r, deps.Authz, http.MethodPost, "/servers/enrollment-tokens",
					NewEnrollmentTokenHandler(*deps.Enrollment))
			}
			if deps.System != nil {
				registerRoute(r, deps.Authz, http.MethodGet, "/system/version",
					NewSystemVersionHandler(*deps.System))
				registerRoute(r, deps.Authz, http.MethodPost, "/system/check",
					NewSystemCheckHandler(*deps.System))
				registerRoute(r, deps.Authz, http.MethodPost, "/system/update",
					NewSystemUpdateHandler(*deps.System))
			}
		})
	})

	r.NotFound(Static(deps.WebDir).ServeHTTP)
	return r
}

// NewHandler returns a handler with the health endpoint only.
func NewHandler(db Pinger) http.Handler {
	mux := chi.NewRouter()
	mux.Get("/healthz", health(db))
	return mux
}

// health returns the health endpoint.
func health(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
		defer cancel()

		status := http.StatusOK
		body := healthResponse{Status: "ok"}
		if err := db.Ping(ctx); err != nil {
			status = http.StatusServiceUnavailable
			body = healthResponse{Status: "unavailable"}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
}

type healthResponse struct {
	Status string `json:"status"`
}

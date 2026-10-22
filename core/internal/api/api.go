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
}

// NewRouter returns the HTTP handler of the core: the health endpoint, the
// authentication API, the protected API routes and the static panel.
func NewRouter(deps RouterDeps) http.Handler {
	r := chi.NewRouter()
	r.Use(Security(deps.Security))

	r.Get("/healthz", health(deps.DB))

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(Sessions(deps.Session))
		r.Mount("/auth", NewAuthHandler(deps.Auth))

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
			r.Get("/servers", NewServersHandler(serverDeps).ServeHTTP)
			if deps.AgentUpdates != nil {
				r.Post("/servers/{serverID}/agent/update", NewAgentUpdateHandler(*deps.AgentUpdates).ServeHTTP)
				r.Post("/agents/update-all", NewAgentUpdateAllHandler(*deps.AgentUpdates).ServeHTTP)
			}
			if deps.ServerRemover != nil {
				r.Delete("/servers/{serverID}", NewServerRemoveHandler(serverDeps).ServeHTTP)
			}
			if deps.Containers != nil {
				r.Get("/servers/{serverID}/containers", NewContainersHandler(*deps.Containers).ServeHTTP)
				r.Post("/servers/{serverID}/containers/{container}/actions", NewContainerActionHandler(*deps.Containers).ServeHTTP)
				r.Post("/servers/{serverID}/containers/{container}/logs", NewContainerLogsHandler(*deps.Containers).ServeHTTP)
				r.Post("/servers/{serverID}/apt/status", NewAptStatusHandler(*deps.Containers).ServeHTTP)
				r.Get("/jobs/{jobID}", NewJobHandler(*deps.Containers).ServeHTTP)
				if deps.Terminal != nil {
					r.Post("/servers/{serverID}/containers/{container}/terminal", NewTerminalOpenHandler(deps.Terminal).ServeHTTP)
					r.Get("/terminal/{sessionID}/ws", NewTerminalSocketHandler(deps.Terminal).ServeHTTP)
				}
				r.Post("/servers/{serverID}/resources/list", NewResourcesListHandler(*deps.Containers).ServeHTTP)
				r.Post("/servers/{serverID}/resources/remove", NewResourcesRemoveHandler(*deps.Containers).ServeHTTP)
				r.Get("/servers/{serverID}/stacks", NewStacksHandler(*deps.Containers).ServeHTTP)
				r.Put("/servers/{serverID}/stacks/{name}", NewStackWriteHandler(*deps.Containers).ServeHTTP)
				r.Post("/servers/{serverID}/stacks/{name}/read", NewStackReadHandler(*deps.Containers).ServeHTTP)
				r.Post("/servers/{serverID}/stacks/{name}/adopt", NewStackAdoptHandler(*deps.Containers).ServeHTTP)
				r.Post("/servers/{serverID}/stacks/{name}/actions", NewStackActionHandler(*deps.Containers).ServeHTTP)
			}
			if deps.Enrollment != nil {
				r.Post("/servers/enrollment-tokens", NewEnrollmentTokenHandler(*deps.Enrollment).ServeHTTP)
			}
			if deps.System != nil {
				r.Get("/system/version", NewSystemVersionHandler(*deps.System).ServeHTTP)
				r.Post("/system/check", NewSystemCheckHandler(*deps.System).ServeHTTP)
				r.Post("/system/update", NewSystemUpdateHandler(*deps.System).ServeHTTP)
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

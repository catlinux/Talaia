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

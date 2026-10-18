// Package api exposes the talaia-core HTTP API.
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

// NewHandler returns the HTTP handler of the core API.
func NewHandler(db Pinger) http.Handler {
	h := &handler{db: db}

	r := chi.NewRouter()
	r.Get("/healthz", h.healthz)
	return r
}

type handler struct {
	db Pinger
}

type healthResponse struct {
	Status string `json:"status"`
}

func (h *handler) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
	defer cancel()

	status := http.StatusOK
	body := healthResponse{Status: "ok"}
	if err := h.db.Ping(ctx); err != nil {
		status = http.StatusServiceUnavailable
		body = healthResponse{Status: "unavailable"}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

package hub

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/catlinux/Talaia/core/internal/store"
)

// ServerStore tells the hub which servers may connect.
type ServerStore interface {
	GetServer(ctx context.Context, id string) (store.Server, error)
	TouchServer(ctx context.Context, id string) error
}

// contextKey is the type of the context keys of this package.
type contextKey struct{ name string }

// serverIDKey carries the server id of an authenticated connection.
var serverIDKey = contextKey{"server-id"}

func withServerID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, serverIDKey, id)
}

// serverIDFromContext returns the server id of an authenticated connection.
func serverIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(serverIDKey).(string)
	return id, ok && id != ""
}

// identityFromRequest returns the server id of a connection, taken from the
// verified client certificate (its common name).
func identityFromRequest(r *http.Request) (string, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return "", errors.New("no client certificate")
	}

	commonName := strings.TrimSpace(r.TLS.PeerCertificates[0].Subject.CommonName)
	if commonName == "" {
		return "", errors.New("the client certificate has no common name")
	}
	return commonName, nil
}

// authenticate wraps the hub handler with certificate authentication: it
// requires a verified client certificate, checks that the server exists and is
// not revoked, and carries the server id in the request context.
func authenticate(servers ServerStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID, err := identityFromRequest(r)
		if err != nil {
			http.Error(w, "client certificate required", http.StatusUnauthorized)
			return
		}

		server, err := servers.GetServer(r.Context(), serverID)
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err != nil {
			slog.Error("check server", "server_id", serverID, "error", err)
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if !server.RevokedAt.IsZero() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(withServerID(r.Context(), serverID)))
	})
}

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

// connection is the identity of an authenticated stream: the server id and the
// serial of the certificate it presented.
type connection struct {
	serverID string
	serial   string
}

// connectionKey carries the identity of an authenticated connection.
var connectionKey = contextKey{"connection"}

func withConnection(ctx context.Context, serverID, serial string) context.Context {
	return context.WithValue(ctx, connectionKey, connection{serverID: serverID, serial: serial})
}

// connectionFromContext returns the identity of an authenticated connection.
func connectionFromContext(ctx context.Context) (connection, bool) {
	conn, ok := ctx.Value(connectionKey).(connection)
	return conn, ok && conn.serverID != ""
}

// serverIDFromContext returns the server id of an authenticated connection.
func serverIDFromContext(ctx context.Context) (string, bool) {
	conn, ok := connectionFromContext(ctx)
	return conn.serverID, ok
}

// identityFromRequest returns the server id and the certificate serial of a
// connection, taken from the verified client certificate. The serial is
// formatted like ca.SignClientCert does, so both can be compared as strings.
func identityFromRequest(r *http.Request) (serverID, serial string, err error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return "", "", errors.New("no client certificate")
	}

	cert := r.TLS.PeerCertificates[0]
	serverID = strings.TrimSpace(cert.Subject.CommonName)
	if serverID == "" {
		return "", "", errors.New("the client certificate has no common name")
	}
	if cert.SerialNumber != nil {
		serial = cert.SerialNumber.Text(16)
	}
	return serverID, serial, nil
}

// authenticate wraps the hub handler with certificate authentication: it
// requires a verified client certificate of an enrolled, not revoked server
// whose certificate serial is still accepted, and carries the identity of the
// connection in the request context.
func authenticate(servers ServerStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID, serial, err := identityFromRequest(r)
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
		if !server.RevokedAt.IsZero() || !server.AcceptsSerial(serial) {
			slog.Warn("connection rejected", "server_id", serverID, "serial", serial, "revoked", !server.RevokedAt.IsZero())
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(withConnection(r.Context(), serverID, serial)))
	})
}

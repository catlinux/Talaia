package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/catlinux/Talaia/core/internal/ca"
	"github.com/catlinux/Talaia/core/internal/store"
)

// clientCertValidity is how long the client certificate of an agent is valid.
const clientCertValidity = 90 * 24 * time.Hour

// maxBodyBytes limits the body of the enrollment requests.
const maxBodyBytes = 16 << 10

// EnrollmentStore enrolls servers and finds them.
type EnrollmentStore interface {
	// EnrollServer consumes the token, creates the server and stores the
	// certificate in a single transaction.
	EnrollServer(ctx context.Context, token string, sign store.SignFunc) (serverID string, certPEM []byte, err error)
	GetServer(ctx context.Context, id string) (store.Server, error)
	// RotateServerCertificate replaces the certificate, keeping the previous
	// serial acceptable.
	RotateServerCertificate(ctx context.Context, id, newSerial string, notAfter time.Time) error
}

// CertificateSigner signs the client certificates of the agents.
type CertificateSigner interface {
	SignClientCert(csrPEM []byte, serverID string, validity time.Duration) ([]byte, time.Time, string, error)
}

// CertificateProvider serves the CA certificate.
type CertificateProvider interface {
	CertPEM() []byte
}

// EnrollmentDeps are the collaborators of the enrollment endpoints.
type EnrollmentDeps struct {
	Store   EnrollmentStore
	Signer  CertificateSigner
	CA      CertificateProvider
	Limiter *RateLimiter
}

// NewEnrollmentHandler returns the handler of GET /ca.pem, POST /enroll and
// POST /renew. The health endpoint is mounted separately, and insecure_dev mode
// does not mount the enrollment endpoints at all.
func NewEnrollmentHandler(deps EnrollmentDeps) http.Handler {
	h := &enrollHandler{deps: deps}

	r := chi.NewRouter()
	r.Get("/ca.pem", h.caPEM)
	r.Post("/enroll", h.enroll)
	r.Post("/renew", h.renew)
	return r
}

type enrollHandler struct {
	deps EnrollmentDeps
}

func (h *enrollHandler) caPEM(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/x-pem-file")
	_, _ = w.Write(h.deps.CA.CertPEM())
}

type enrollRequest struct {
	Token    string `json:"token"`
	CSRPEM   string `json:"csr_pem"`
	Hostname string `json:"hostname"`
}

type enrollResponse struct {
	ServerID string `json:"server_id"`
	CertPEM  string `json:"cert_pem"`
	CAPEM    string `json:"ca_pem"`
}

func (h *enrollHandler) enroll(w http.ResponseWriter, r *http.Request) {
	if h.deps.Limiter != nil && !h.deps.Limiter.Allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	var request enrollRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	// The csr is validated before the token is consumed, so a malformed request
	// does not burn the one-time token.
	if err := ca.ValidateCSR([]byte(request.CSRPEM)); err != nil {
		slog.Debug("invalid csr", "error", err)
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	// Consuming the token, creating the server and storing the certificate
	// happen in a single transaction: if anything fails the rollback leaves the
	// one-time token usable and no orphan server behind.
	serverID, certPEM, err := h.deps.Store.EnrollServer(r.Context(), request.Token,
		func(serverID string) ([]byte, string, time.Time, error) {
			certPEM, notAfter, serial, err := h.deps.Signer.SignClientCert([]byte(request.CSRPEM), serverID, clientCertValidity)
			return certPEM, serial, notAfter, err
		})
	if err != nil {
		// Unknown, used and expired tokens are all reported the same way.
		if errors.Is(err, store.ErrInvalidToken) {
			writeError(w, http.StatusUnauthorized, "invalid_token")
			return
		}
		slog.Error("enroll server", "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}

	slog.Info("server enrolled", "server_id", serverID, "hostname", request.Hostname)
	writeJSON(w, http.StatusOK, enrollResponse{
		ServerID: serverID,
		CertPEM:  string(certPEM),
		CAPEM:    string(h.deps.CA.CertPEM()),
	})
}

type renewRequest struct {
	CSRPEM string `json:"csr_pem"`
}

type renewResponse struct {
	CertPEM string `json:"cert_pem"`
}

func (h *enrollHandler) renew(w http.ResponseWriter, r *http.Request) {
	// The identity comes from the client certificate, never from the body. A
	// missing certificate, an unknown server, a revoked server and a serial
	// that is neither the current one nor the previous one are all forbidden.
	serverID, presentedSerial, ok := clientIdentity(r)
	if !ok {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	server, err := h.deps.Store.GetServer(r.Context(), serverID)
	if err != nil {
		slog.Warn("renewal rejected", "server_id", serverID, "error", err)
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if !server.RevokedAt.IsZero() || !server.AcceptsSerial(presentedSerial) {
		slog.Warn("renewal rejected", "server_id", serverID, "serial", presentedSerial, "revoked", !server.RevokedAt.IsZero())
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var request renewRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if err := ca.ValidateCSR([]byte(request.CSRPEM)); err != nil {
		slog.Debug("invalid csr", "server_id", serverID, "error", err)
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	certPEM, notAfter, serial, err := h.deps.Signer.SignClientCert([]byte(request.CSRPEM), serverID, clientCertValidity)
	if err != nil {
		slog.Error("renew client certificate", "server_id", serverID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}
	// The serial that was current becomes the previous one, so the agent keeps
	// working until it stores the new certificate.
	if err := h.deps.Store.RotateServerCertificate(r.Context(), serverID, serial, notAfter); err != nil {
		slog.Error("store renewed certificate", "server_id", serverID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal")
		return
	}

	slog.Info("server certificate renewed", "server_id", serverID)
	writeJSON(w, http.StatusOK, renewResponse{CertPEM: string(certPEM)})
}

// clientIdentity returns the server id and the certificate serial of a
// connection, taken from the verified client certificate. The serial is
// formatted like ca.SignClientCert does, so both can be compared as strings.
func clientIdentity(r *http.Request) (serverID, serial string, ok bool) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return "", "", false
	}

	cert := r.TLS.PeerCertificates[0]
	serverID = strings.TrimSpace(cert.Subject.CommonName)
	if serverID == "" {
		return "", "", false
	}
	if cert.SerialNumber != nil {
		serial = cert.SerialNumber.Text(16)
	}
	return serverID, serial, true
}

// errorBody is the JSON shape of every API error. The panel translates the
// stable code; the core never sends sentences.
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code string `json:"code"`
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// decodeJSON reads a JSON body of at most maxBodyBytes.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(target)
}

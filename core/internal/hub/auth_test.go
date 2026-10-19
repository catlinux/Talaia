package hub

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

// certWithCommonName builds a client certificate. The serial is hexadecimal, so
// it compares like ca.SignClientCert serials do.
func certWithCommonName(commonName string, serial int64) *x509.Certificate {
	certificate := &x509.Certificate{Subject: pkix.Name{CommonName: commonName}}
	if serial != 0 {
		certificate.SerialNumber = big.NewInt(serial)
	}
	return certificate
}

func requestWithCertificate(commonName string, serial int64) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/talaia.agent.v1.AgentHub/Connect", nil)
	if commonName == "" {
		return request
	}
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{certWithCommonName(commonName, serial)}}
	return request
}

func TestIdentityFromRequest(t *testing.T) {
	tests := []struct {
		name       string
		request    *http.Request
		wantID     string
		wantSerial string
		wantErr    bool
	}{
		{name: "no certificate", request: requestWithCertificate("", 0), wantErr: true},
		{name: "empty common name", request: requestWithCertificate(" ", 0xaa), wantErr: true},
		{name: "certificate", request: requestWithCertificate("server-1", 0xaa), wantID: "server-1", wantSerial: "aa"},
		{name: "certificate without serial", request: requestWithCertificate("server-1", 0), wantID: "server-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverID, serial, err := identityFromRequest(tt.request)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if serverID != tt.wantID || serial != tt.wantSerial {
				t.Fatalf("identity = %q/%q, want %q/%q", serverID, serial, tt.wantID, tt.wantSerial)
			}
		})
	}
}

func TestAuthenticateRejectsWithoutACertificate(t *testing.T) {
	handler := authenticate(newFakeServerStore(enrolledServer("server-1", "aa")), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the hub handler must not run")
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestWithCertificate("", 0))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestAuthenticateChecksTheServerAndTheSerial(t *testing.T) {
	tests := []struct {
		name    string
		servers *fakeServerStore
		serial  int64
		want    int
	}{
		{
			name:    "unknown server",
			servers: newFakeServerStore(),
			serial:  0xaa,
			want:    http.StatusForbidden,
		},
		{
			name:    "revoked server",
			servers: newFakeServerStore(store.Server{ID: "server-1", CertSerial: "aa", RevokedAt: time.Now()}),
			serial:  0xaa,
			want:    http.StatusForbidden,
		},
		{
			name:    "serial that is neither current nor previous",
			servers: newFakeServerStore(store.Server{ID: "server-1", CertSerial: "bb", PrevCertSerial: "cc"}),
			serial:  0xaa,
			want:    http.StatusForbidden,
		},
		{
			name:    "certificate without serial",
			servers: newFakeServerStore(enrolledServer("server-1", "aa")),
			serial:  0,
			want:    http.StatusForbidden,
		},
		{
			name:    "current serial",
			servers: newFakeServerStore(enrolledServer("server-1", "aa")),
			serial:  0xaa,
			want:    http.StatusOK,
		},
		{
			name: "previous serial",
			servers: newFakeServerStore(store.Server{
				ID: "server-1", CertSerial: "bb", PrevCertSerial: "aa",
			}),
			serial: 0xaa,
			want:   http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := authenticate(tt.servers, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, requestWithCertificate("server-1", tt.serial))

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.want)
			}
		})
	}
}

func TestAuthenticatePassesTheConnectionToTheHandler(t *testing.T) {
	servers := newFakeServerStore(enrolledServer("server-1", "aa"))
	var gotID, gotSerial string

	handler := authenticate(servers, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID, _ = serverIDFromContext(r.Context())
		conn, ok := connectionFromContext(r.Context())
		if !ok {
			t.Error("the connection is not in the context")
		}
		gotSerial = conn.serial
		w.WriteHeader(http.StatusOK)
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestWithCertificate("server-1", 0xaa))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if gotID != "server-1" || gotSerial != "aa" {
		t.Fatalf("the handler saw %q/%q, want server-1/aa", gotID, gotSerial)
	}
}

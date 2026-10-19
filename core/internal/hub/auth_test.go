package hub

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

func certWithCommonName(commonName string) *x509.Certificate {
	return &x509.Certificate{Subject: pkix.Name{CommonName: commonName}}
}

func requestWithCertificate(commonName string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/talaia.agent.v1.AgentHub/Connect", nil)
	if commonName == "" {
		return request
	}
	request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{certWithCommonName(commonName)}}
	return request
}

func TestIdentityFromRequest(t *testing.T) {
	tests := []struct {
		name    string
		request *http.Request
		want    string
		wantErr bool
	}{
		{name: "no certificate", request: requestWithCertificate(""), wantErr: true},
		{name: "empty common name", request: requestWithCertificate(" "), wantErr: true},
		{name: "certificate", request: requestWithCertificate("server-1"), want: "server-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := identityFromRequest(tt.request)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("identity = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthenticateRejectsWithoutACertificate(t *testing.T) {
	handler := authenticate(newFakeServerStore(store.Server{ID: "server-1"}), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the hub handler must not run")
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestWithCertificate(""))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

func TestAuthenticateChecksTheServer(t *testing.T) {
	tests := []struct {
		name    string
		servers *fakeServerStore
		want    int
	}{
		{
			name:    "unknown server",
			servers: newFakeServerStore(),
			want:    http.StatusForbidden,
		},
		{
			name:    "revoked server",
			servers: newFakeServerStore(store.Server{ID: "server-1", RevokedAt: time.Now()}),
			want:    http.StatusForbidden,
		},
		{
			name:    "enrolled server",
			servers: newFakeServerStore(store.Server{ID: "server-1"}),
			want:    http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := authenticate(tt.servers, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, requestWithCertificate("server-1"))

			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.want)
			}
		})
	}
}

func TestAuthenticatePassesTheServerIDToTheHandler(t *testing.T) {
	servers := newFakeServerStore(store.Server{ID: "server-1"})
	var got string

	handler := authenticate(servers, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = serverIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, requestWithCertificate("server-1"))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got != "server-1" {
		t.Fatalf("the handler saw %q, want server-1", got)
	}
}

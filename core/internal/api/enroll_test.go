package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

const testServerID = "11111111-1111-1111-1111-111111111111"

type fakeTokens struct {
	tokens map[string]string
	err    error
	used   []string
}

func (f *fakeTokens) ConsumeEnrollmentToken(_ context.Context, token string) (string, error) {
	f.used = append(f.used, token)
	if f.err != nil {
		return "", f.err
	}
	name, ok := f.tokens[token]
	if !ok {
		return "", store.ErrInvalidToken
	}
	return name, nil
}

type fakeServers struct {
	nextID  string
	created []string
	servers map[string]store.Server
	serial  string
	err     error
}

func newFakeServers(servers ...store.Server) *fakeServers {
	f := &fakeServers{nextID: testServerID, servers: make(map[string]store.Server)}
	for _, server := range servers {
		f.servers[server.ID] = server
	}
	return f
}

func (f *fakeServers) CreateServer(_ context.Context, name string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.created = append(f.created, name)
	f.servers[f.nextID] = store.Server{ID: f.nextID, Name: name}
	return f.nextID, nil
}

func (f *fakeServers) GetServer(_ context.Context, id string) (store.Server, error) {
	server, ok := f.servers[id]
	if !ok {
		return store.Server{}, store.ErrNotFound
	}
	return server, nil
}

func (f *fakeServers) SetServerCertificate(_ context.Context, _, serial string, _ time.Time) error {
	f.serial = serial
	return nil
}

type fakeSigner struct {
	err error
}

func (f fakeSigner) SignClientCert(_ []byte, serverID string, _ time.Duration) ([]byte, time.Time, string, error) {
	if f.err != nil {
		return nil, time.Time{}, "", f.err
	}
	return []byte("cert-for-" + serverID), time.Now().Add(time.Hour), "serial-1", nil
}

type fakeCA struct{}

func (fakeCA) CertPEM() []byte {
	return []byte("ca-pem")
}

// testCSR returns a valid ECDSA P-256 certificate request in PEM.
func testCSR(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

func enrollBody(token, csr string) string {
	return fmt.Sprintf(`{"token":%s,"csr_pem":%s,"hostname":"vps-web-01"}`, strconv.Quote(token), strconv.Quote(csr))
}

func post(t *testing.T, handler http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func postRenew(t *testing.T, handler http.Handler, commonName, csr string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/renew", strings.NewReader(fmt.Sprintf(`{"csr_pem":%s}`, strconv.Quote(csr))))
	if commonName != "" {
		request.TLS = &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{{Subject: pkix.Name{CommonName: commonName}}},
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestCAPEM(t *testing.T) {
	handler := NewEnrollmentHandler(EnrollmentDeps{CA: fakeCA{}})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ca.pem", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); body != "ca-pem" {
		t.Errorf("body = %q", body)
	}
}

func TestEnrollValidatesTheCSRBeforeTheToken(t *testing.T) {
	tokens := &fakeTokens{tokens: map[string]string{"good": "vps-web-01"}}
	handler := NewEnrollmentHandler(EnrollmentDeps{
		Tokens:  tokens,
		Servers: newFakeServers(),
		Signer:  fakeSigner{},
		CA:      fakeCA{},
		Limiter: NewRateLimiter(),
	})

	recorder := post(t, handler, "/enroll", enrollBody("good", "not a csr"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"invalid_request"`) {
		t.Errorf("body = %q", recorder.Body.String())
	}
	if len(tokens.used) != 0 {
		t.Errorf("the token was consumed: %v", tokens.used)
	}
}

func TestEnrollInvalidToken(t *testing.T) {
	tokens := &fakeTokens{tokens: map[string]string{}}
	servers := newFakeServers()
	handler := NewEnrollmentHandler(EnrollmentDeps{Tokens: tokens, Servers: servers, Signer: fakeSigner{}, CA: fakeCA{}})

	recorder := post(t, handler, "/enroll", enrollBody("unknown", testCSR(t)))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"invalid_token"`) {
		t.Errorf("body = %q", recorder.Body.String())
	}
	if len(servers.created) != 0 {
		t.Errorf("a server was created: %v", servers.created)
	}
}

func TestEnrollHappyPath(t *testing.T) {
	tokens := &fakeTokens{tokens: map[string]string{"good": "vps-web-01"}}
	servers := newFakeServers()
	handler := NewEnrollmentHandler(EnrollmentDeps{Tokens: tokens, Servers: servers, Signer: fakeSigner{}, CA: fakeCA{}})

	recorder := post(t, handler, "/enroll", enrollBody("good", testCSR(t)))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`"server_id":"` + testServerID + `"`,
		`"cert_pem":"cert-for-` + testServerID + `"`,
		`"ca_pem":"ca-pem"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body %q does not contain %q", body, want)
		}
	}
	if len(servers.created) != 1 || servers.created[0] != "vps-web-01" {
		t.Errorf("servers created = %v, want the token's server name", servers.created)
	}
	if servers.serial != "serial-1" {
		t.Errorf("stored serial = %q", servers.serial)
	}
}

func TestEnrollRejectsHugeBodies(t *testing.T) {
	tokens := &fakeTokens{tokens: map[string]string{"good": "vps-web-01"}}
	handler := NewEnrollmentHandler(EnrollmentDeps{Tokens: tokens, Servers: newFakeServers(), Signer: fakeSigner{}, CA: fakeCA{}})

	recorder := post(t, handler, "/enroll", enrollBody("good", strings.Repeat("a", 32<<10)))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if len(tokens.used) != 0 {
		t.Errorf("the token was consumed: %v", tokens.used)
	}
}

func TestEnrollMalformedJSON(t *testing.T) {
	handler := NewEnrollmentHandler(EnrollmentDeps{Tokens: &fakeTokens{}, Servers: newFakeServers(), Signer: fakeSigner{}, CA: fakeCA{}})

	if recorder := post(t, handler, "/enroll", "{"); recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestEnrollRateLimited(t *testing.T) {
	handler := NewEnrollmentHandler(EnrollmentDeps{
		Tokens:  &fakeTokens{tokens: map[string]string{}},
		Servers: newFakeServers(),
		Signer:  fakeSigner{},
		CA:      fakeCA{},
		Limiter: NewRateLimiterWithClock(3, time.Minute, time.Now),
	})

	for i := 0; i < 3; i++ {
		if recorder := post(t, handler, "/enroll", enrollBody("x", "bad")); recorder.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was limited too early", i+1)
		}
	}

	recorder := post(t, handler, "/enroll", enrollBody("x", "bad"))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"rate_limited"`) {
		t.Errorf("body = %q", recorder.Body.String())
	}
}

func TestEnrollServerError(t *testing.T) {
	servers := newFakeServers()
	servers.err = errors.New("database down")
	handler := NewEnrollmentHandler(EnrollmentDeps{
		Tokens:  &fakeTokens{tokens: map[string]string{"good": "vps-web-01"}},
		Servers: servers,
		Signer:  fakeSigner{},
		CA:      fakeCA{},
	})

	if recorder := post(t, handler, "/enroll", enrollBody("good", testCSR(t))); recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestRenewRequiresACertificate(t *testing.T) {
	handler := NewEnrollmentHandler(EnrollmentDeps{Servers: newFakeServers(), Signer: fakeSigner{}, CA: fakeCA{}})

	recorder := postRenew(t, handler, "", testCSR(t))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"forbidden"`) {
		t.Errorf("body = %q", recorder.Body.String())
	}
}

func TestRenewUnknownOrRevokedServer(t *testing.T) {
	revokedAt := time.Now()
	servers := newFakeServers(
		store.Server{ID: "revoked-server", Name: "vps-1", RevokedAt: revokedAt},
	)
	handler := NewEnrollmentHandler(EnrollmentDeps{Servers: servers, Signer: fakeSigner{}, CA: fakeCA{}})

	tests := []struct {
		name string
		cn   string
	}{
		{name: "unknown", cn: "unknown-server"},
		{name: "revoked", cn: "revoked-server"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := postRenew(t, handler, tt.cn, testCSR(t))
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", recorder.Code)
			}
		})
	}
}

func TestRenewHappyPath(t *testing.T) {
	servers := newFakeServers(store.Server{ID: "server-1", Name: "vps-1"})
	handler := NewEnrollmentHandler(EnrollmentDeps{Servers: servers, Signer: fakeSigner{}, CA: fakeCA{}})

	recorder := postRenew(t, handler, "server-1", testCSR(t))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"cert_pem":"cert-for-server-1"`) {
		t.Errorf("body = %q", recorder.Body.String())
	}
	if servers.serial != "serial-1" {
		t.Errorf("stored serial = %q", servers.serial)
	}
}

func TestRenewInvalidCSR(t *testing.T) {
	handler := NewEnrollmentHandler(EnrollmentDeps{
		Servers: newFakeServers(store.Server{ID: "server-1", Name: "vps-1"}),
		Signer:  fakeSigner{},
		CA:      fakeCA{},
	})

	if recorder := postRenew(t, handler, "server-1", "garbage"); recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

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
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

const testServerID = "11111111-1111-1111-1111-111111111111"

// fakeStore is an in-memory EnrollmentStore that mimics the transaction: the
// token is only spent when the signature succeeds.
type fakeStore struct {
	tokens  map[string]string
	servers map[string]store.Server
	nextID  string

	err error

	attempted []string
	rotated   []string
}

func newFakeStore(servers ...store.Server) *fakeStore {
	f := &fakeStore{
		nextID:  testServerID,
		tokens:  make(map[string]string),
		servers: make(map[string]store.Server),
	}
	for _, server := range servers {
		f.servers[server.ID] = server
	}
	return f
}

func (f *fakeStore) EnrollServer(_ context.Context, token string, sign store.SignFunc) (string, []byte, error) {
	f.attempted = append(f.attempted, token)
	if f.err != nil {
		return "", nil, f.err
	}

	name, ok := f.tokens[token]
	if !ok {
		return "", nil, store.ErrInvalidToken
	}

	certPEM, serial, notAfter, err := sign(f.nextID)
	if err != nil {
		// The real store rolls the transaction back, so the token stays usable.
		return "", nil, err
	}

	delete(f.tokens, token)
	f.servers[f.nextID] = store.Server{ID: f.nextID, Name: name, CertSerial: serial, CertNotAfter: notAfter}
	return f.nextID, certPEM, nil
}

func (f *fakeStore) GetServer(_ context.Context, id string) (store.Server, error) {
	server, ok := f.servers[id]
	if !ok {
		return store.Server{}, store.ErrNotFound
	}
	return server, nil
}

func (f *fakeStore) RotateServerCertificate(_ context.Context, id, newSerial string, notAfter time.Time) error {
	server, ok := f.servers[id]
	if !ok {
		return store.ErrNotFound
	}
	server.PrevCertSerial = server.CertSerial
	server.CertSerial = newSerial
	server.CertNotAfter = notAfter
	f.servers[id] = server
	f.rotated = append(f.rotated, id)
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

// postRenew sends a renewal request. An empty commonName sends no client
// certificate at all; a non-empty serialNumber becomes the serial of the
// presented certificate.
func postRenew(t *testing.T, handler http.Handler, commonName, serialNumber, csr string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/renew", strings.NewReader(fmt.Sprintf(`{"csr_pem":%s}`, strconv.Quote(csr))))
	if commonName != "" {
		certificate := &x509.Certificate{Subject: pkix.Name{CommonName: commonName}}
		if serialNumber != "" {
			number, ok := new(big.Int).SetString(serialNumber, 16)
			if !ok {
				t.Fatalf("invalid serial %q", serialNumber)
			}
			certificate.SerialNumber = number
		}
		request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{certificate}}
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
	fake := newFakeStore()
	handler := NewEnrollmentHandler(EnrollmentDeps{Store: fake, Signer: fakeSigner{}, CA: fakeCA{}, Limiter: NewRateLimiter()})

	recorder := post(t, handler, "/enroll", enrollBody("good", "not a csr"))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"invalid_request"`) {
		t.Errorf("body = %q", recorder.Body.String())
	}
	if len(fake.attempted) != 0 {
		t.Errorf("the token was consumed: %v", fake.attempted)
	}
}

func TestEnrollInvalidToken(t *testing.T) {
	fake := newFakeStore()
	handler := NewEnrollmentHandler(EnrollmentDeps{Store: fake, Signer: fakeSigner{}, CA: fakeCA{}})

	recorder := post(t, handler, "/enroll", enrollBody("unknown", testCSR(t)))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"invalid_token"`) {
		t.Errorf("body = %q", recorder.Body.String())
	}
}

func TestEnrollHappyPath(t *testing.T) {
	fake := newFakeStore()
	fake.tokens["good"] = "vps-web-01"
	handler := NewEnrollmentHandler(EnrollmentDeps{Store: fake, Signer: fakeSigner{}, CA: fakeCA{}})

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

	server, err := fake.GetServer(context.Background(), testServerID)
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if server.Name != "vps-web-01" || server.CertSerial != "serial-1" {
		t.Errorf("server = %+v, want the token's name and the issued serial", server)
	}
	if _, ok := fake.tokens["good"]; ok {
		t.Error("the token was not consumed")
	}
}

func TestEnrollSigningFailureKeepsTheToken(t *testing.T) {
	fake := newFakeStore()
	fake.tokens["good"] = "vps-web-01"
	handler := NewEnrollmentHandler(EnrollmentDeps{
		Store:  fake,
		Signer: fakeSigner{err: errors.New("ca is down")},
		CA:     fakeCA{},
	})

	if recorder := post(t, handler, "/enroll", enrollBody("good", testCSR(t))); recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if _, ok := fake.tokens["good"]; !ok {
		t.Error("a failed enrollment must not spend the token")
	}
}

func TestEnrollRejectsHugeBodies(t *testing.T) {
	fake := newFakeStore()
	handler := NewEnrollmentHandler(EnrollmentDeps{Store: fake, Signer: fakeSigner{}, CA: fakeCA{}})

	recorder := post(t, handler, "/enroll", enrollBody("good", strings.Repeat("a", 32<<10)))

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	if len(fake.attempted) != 0 {
		t.Errorf("the token was consumed: %v", fake.attempted)
	}
}

func TestEnrollMalformedJSON(t *testing.T) {
	handler := NewEnrollmentHandler(EnrollmentDeps{Store: newFakeStore(), Signer: fakeSigner{}, CA: fakeCA{}})

	if recorder := post(t, handler, "/enroll", "{"); recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestEnrollRateLimited(t *testing.T) {
	handler := NewEnrollmentHandler(EnrollmentDeps{
		Store:   newFakeStore(),
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

func TestEnrollStoreError(t *testing.T) {
	fake := newFakeStore()
	fake.err = errors.New("database down")
	handler := NewEnrollmentHandler(EnrollmentDeps{Store: fake, Signer: fakeSigner{}, CA: fakeCA{}})

	if recorder := post(t, handler, "/enroll", enrollBody("good", testCSR(t))); recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
}

func TestRenewRequiresACertificate(t *testing.T) {
	handler := NewEnrollmentHandler(EnrollmentDeps{Store: newFakeStore(), Signer: fakeSigner{}, CA: fakeCA{}})

	recorder := postRenew(t, handler, "", "", testCSR(t))

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"code":"forbidden"`) {
		t.Errorf("body = %q", recorder.Body.String())
	}
}

func TestRenewUnknownRevokedOrForeignSerial(t *testing.T) {
	servers := newFakeStore(
		store.Server{ID: "revoked-server", Name: "vps-1", CertSerial: "aa", RevokedAt: time.Now()},
		store.Server{ID: "server-1", Name: "vps-1", CertSerial: "aa"},
	)
	handler := NewEnrollmentHandler(EnrollmentDeps{Store: servers, Signer: fakeSigner{}, CA: fakeCA{}})

	tests := []struct {
		name   string
		cn     string
		serial string
	}{
		{name: "unknown server", cn: "unknown-server", serial: "aa"},
		{name: "revoked server", cn: "revoked-server", serial: "aa"},
		{name: "serial that is not the current one", cn: "server-1", serial: "bb"},
		{name: "certificate without serial", cn: "server-1", serial: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := postRenew(t, handler, tt.cn, tt.serial, testCSR(t))
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", recorder.Code)
			}
		})
	}
	if len(servers.rotated) != 0 {
		t.Errorf("a rejected renewal rotated the certificate: %v", servers.rotated)
	}
}

func TestRenewHappyPath(t *testing.T) {
	servers := newFakeStore(store.Server{ID: "server-1", Name: "vps-1", CertSerial: "aa"})
	handler := NewEnrollmentHandler(EnrollmentDeps{Store: servers, Signer: fakeSigner{}, CA: fakeCA{}})

	recorder := postRenew(t, handler, "server-1", "aa", testCSR(t))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"cert_pem":"cert-for-server-1"`) {
		t.Errorf("body = %q", recorder.Body.String())
	}

	server, err := servers.GetServer(context.Background(), "server-1")
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	// The serial that was current becomes the previous one, so the agent keeps
	// working until it stores the new certificate.
	if server.CertSerial != "serial-1" || server.PrevCertSerial != "aa" {
		t.Errorf("serials = %q/%q, want serial-1/aa", server.CertSerial, server.PrevCertSerial)
	}
}

func TestRenewAcceptsThePreviousSerial(t *testing.T) {
	servers := newFakeStore(store.Server{ID: "server-1", Name: "vps-1", CertSerial: "bb", PrevCertSerial: "aa"})
	handler := NewEnrollmentHandler(EnrollmentDeps{Store: servers, Signer: fakeSigner{}, CA: fakeCA{}})

	// The agent missed the new certificate and still presents the old one.
	recorder := postRenew(t, handler, "server-1", "aa", testCSR(t))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
}

func TestRenewInvalidCSR(t *testing.T) {
	handler := NewEnrollmentHandler(EnrollmentDeps{
		Store:  newFakeStore(store.Server{ID: "server-1", Name: "vps-1", CertSerial: "aa"}),
		Signer: fakeSigner{},
		CA:     fakeCA{},
	})

	if recorder := postRenew(t, handler, "server-1", "aa", "garbage"); recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

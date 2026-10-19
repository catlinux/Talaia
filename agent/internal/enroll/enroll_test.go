package enroll

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// testCore is a throwaway core for the enrollment tests. It follows the same
// rules as the real one: ECDSA P-256 only, the CSR subject is ignored and the
// common name of the client certificate is the server id.
type testCore struct {
	issuer      *testIssuer
	serverURL   string
	serverID    string
	token       string
	fingerprint string

	issueWrongKey bool

	mu      sync.Mutex
	enrolls int
	renews  int
}

func newTestCore(t *testing.T) *testCore {
	t.Helper()

	issuer := newTestIssuer(t)
	core := &testCore{
		issuer:      issuer,
		serverID:    "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		token:       "tl_enr_test",
		fingerprint: fmt.Sprintf("%x", sha256.Sum256(issuer.cert.Raw)),
	}

	serverCert, err := issuer.issueServer(net.ParseIP("127.0.0.1"))
	if err != nil {
		t.Fatalf("issue server certificate: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ca.pem", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(issuer.certPEM)
	})
	mux.HandleFunc("/enroll", core.enroll)
	mux.HandleFunc("/renew", core.renew)

	server := httptest.NewUnstartedServer(mux)
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    poolWith(issuer.cert),
		Certificates: []tls.Certificate{serverCert},
		NextProtos:   []string{"h2", "http/1.1"},
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	core.serverURL = server.URL
	return core
}

func (c *testCore) enroll(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Token    string `json:"token"`
		CSRPEM   string `json:"csr_pem"`
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	c.mu.Lock()
	c.enrolls++
	wrongKey := c.issueWrongKey
	c.mu.Unlock()

	if request.Token != c.token {
		writeTestJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{"code": "invalid_token"},
		})
		return
	}

	csr, err := parseTestCSR(request.CSRPEM)
	if err != nil {
		http.Error(w, "bad csr", http.StatusBadRequest)
		return
	}

	publicKey := csr.PublicKey
	if wrongKey {
		other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			http.Error(w, "internal", http.StatusInternalServerError)
			return
		}
		publicKey = &other.PublicKey
	}

	certPEM, err := c.issuer.issue(c.serverID, publicKey, x509.ExtKeyUsageClientAuth,
		time.Now().Add(-time.Minute), time.Now().Add(90*24*time.Hour))
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	writeTestJSON(w, http.StatusOK, map[string]string{
		"server_id": c.serverID,
		"cert_pem":  string(certPEM),
		"ca_pem":    string(c.issuer.certPEM),
	})
}

func (c *testCore) renew(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	serverID := r.TLS.PeerCertificates[0].Subject.CommonName

	var request struct {
		CSRPEM string `json:"csr_pem"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	csr, err := parseTestCSR(request.CSRPEM)
	if err != nil {
		http.Error(w, "bad csr", http.StatusBadRequest)
		return
	}

	c.mu.Lock()
	c.renews++
	c.mu.Unlock()

	certPEM, err := c.issuer.issue(serverID, csr.PublicKey, x509.ExtKeyUsageClientAuth,
		time.Now().Add(-time.Minute), time.Now().Add(90*24*time.Hour))
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}

	writeTestJSON(w, http.StatusOK, map[string]string{"cert_pem": string(certPEM)})
}

func (c *testCore) counters() (enrolls, renews int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enrolls, c.renews
}

func parseTestCSR(csrPEM string) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil {
		return nil, errors.New("the csr is not PEM encoded")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, err
	}
	publicKey, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P256() {
		return nil, errors.New("the csr must contain an ECDSA P-256 key")
	}
	return csr, nil
}

func writeTestJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (c *testCore) options(dir, tokenFile string) Options {
	return Options{
		CoreURL:       c.serverURL,
		CAFingerprint: c.fingerprint,
		Token:         c.token,
		Hostname:      "vps-web-01",
		CertDir:       dir,
		TokenFile:     tokenFile,
	}
}

func writeTokenFile(t *testing.T, token string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "enroll.token")
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	return path
}

func TestEnrollHappyPath(t *testing.T) {
	core := newTestCore(t)
	dir := filepath.Join(t.TempDir(), "certs")
	tokenFile := writeTokenFile(t, core.token)

	identity, err := Enroll(context.Background(), core.options(dir, tokenFile))
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}

	if identity.ServerID() != core.serverID {
		t.Errorf("server id = %q, want %q", identity.ServerID(), core.serverID)
	}
	for _, name := range []string{identityFileName, caFileName} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	// The key and the certificate live together in one file.
	for _, name := range []string{legacyKeyFileName, legacyCertFileName} {
		requireNoFile(t, dir, name)
	}
	if _, err := os.Stat(tokenFile); !errors.Is(err, os.ErrNotExist) {
		t.Error("the token file must be removed after enrolling")
	}
	if _, err := LoadIdentity(dir); err != nil {
		t.Errorf("the stored identity does not load: %v", err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	for _, tt := range []struct {
		path string
		mode os.FileMode
	}{
		{filepath.Join(dir, identityFileName), identityMode},
		{filepath.Join(dir, caFileName), caMode},
		{dir, dirMode},
	} {
		info, err := os.Stat(tt.path)
		if err != nil {
			t.Fatalf("stat %s: %v", tt.path, err)
		}
		if info.Mode().Perm() != tt.mode {
			t.Errorf("%s mode = %o, want %o", tt.path, info.Mode().Perm(), tt.mode)
		}
	}
}

func TestEnrollWrongFingerprint(t *testing.T) {
	core := newTestCore(t)
	dir := filepath.Join(t.TempDir(), "certs")
	tokenFile := writeTokenFile(t, core.token)

	options := core.options(dir, tokenFile)
	options.CAFingerprint = strings.Repeat("00", 32)

	if _, err := Enroll(context.Background(), options); !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("error = %v, want ErrFingerprintMismatch", err)
	}
	if !IsPermanent(ErrFingerprintMismatch) {
		t.Error("a fingerprint mismatch must be permanent")
	}
	requireNoIdentity(t, dir)
	if _, err := os.Stat(tokenFile); err != nil {
		t.Error("the token file must be kept on a fingerprint mismatch")
	}
	if enrolls, _ := core.counters(); enrolls != 0 {
		t.Errorf("the core was asked to enroll %d times", enrolls)
	}
}

func TestEnrollInvalidToken(t *testing.T) {
	core := newTestCore(t)
	dir := filepath.Join(t.TempDir(), "certs")
	tokenFile := writeTokenFile(t, core.token)

	options := core.options(dir, tokenFile)
	options.Token = "wrong-token"

	if _, err := Enroll(context.Background(), options); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("error = %v, want ErrInvalidToken", err)
	}
	if !IsPermanent(ErrInvalidToken) {
		t.Error("an invalid token must be permanent")
	}
	requireNoIdentity(t, dir)
	if _, err := os.Stat(tokenFile); err != nil {
		t.Error("the token file must be kept with an invalid token")
	}
}

func TestEnrollRejectsACertificateForAnotherKey(t *testing.T) {
	core := newTestCore(t)
	core.issueWrongKey = true
	dir := filepath.Join(t.TempDir(), "certs")
	tokenFile := writeTokenFile(t, core.token)

	_, err := Enroll(context.Background(), core.options(dir, tokenFile))
	if err == nil || !strings.Contains(err.Error(), "does not match the generated key") {
		t.Fatalf("error = %v, want the key mismatch", err)
	}
	requireNoIdentity(t, dir)
	if _, err := os.Stat(tokenFile); err != nil {
		t.Error("the token file must be kept when the certificate does not match the key")
	}
}

func TestRenewReplacesKeyAndCertificate(t *testing.T) {
	core := newTestCore(t)
	dir := filepath.Join(t.TempDir(), "certs")
	tokenFile := writeTokenFile(t, core.token)

	identity, err := Enroll(context.Background(), core.options(dir, tokenFile))
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}

	oldIdentity, err := os.ReadFile(filepath.Join(dir, identityFileName))
	if err != nil {
		t.Fatalf("read %s: %v", identityFileName, err)
	}
	oldCA, err := os.ReadFile(filepath.Join(dir, caFileName))
	if err != nil {
		t.Fatalf("read %s: %v", caFileName, err)
	}

	if err := Renew(context.Background(), identity, core.serverURL); err != nil {
		t.Fatalf("renew: %v", err)
	}

	// The renewal rewrites the identity file only: the CA certificate is not
	// touched.
	newIdentity, err := os.ReadFile(filepath.Join(dir, identityFileName))
	if err != nil {
		t.Fatalf("read the renewed identity: %v", err)
	}
	if bytes.Equal(oldIdentity, newIdentity) {
		t.Error("the identity file was not replaced")
	}
	newCA, err := os.ReadFile(filepath.Join(dir, caFileName))
	if err != nil {
		t.Fatalf("read %s: %v", caFileName, err)
	}
	if !bytes.Equal(oldCA, newCA) {
		t.Error("the renewal must not rewrite the ca certificate")
	}

	if _, renews := core.counters(); renews != 1 {
		t.Errorf("renewals = %d, want 1", renews)
	}
	if identity.ServerID() != core.serverID {
		t.Errorf("server id = %q, want %q", identity.ServerID(), core.serverID)
	}

	// The certificate in the file is the one the identity presents.
	certificate, err := identity.TLSConfig("localhost").GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil {
		t.Fatalf("get client certificate: %v", err)
	}
	if len(certificate.Certificate) == 0 {
		t.Fatal("the TLS config does not return a certificate")
	}
	stored := certificateInPEM(t, newIdentity)
	if !bytes.Equal(certificate.Certificate[0], stored.Raw) {
		t.Error("the identity does not use the renewed certificate")
	}

	// The renewed identity loads from disk as well.
	reloaded, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("load the renewed identity: %v", err)
	}
	if reloaded.ServerID() != core.serverID {
		t.Errorf("reloaded server id = %q, want %q", reloaded.ServerID(), core.serverID)
	}
}

// certificateInPEM returns the certificate of a PEM document that also holds a
// private key.
func certificateInPEM(t *testing.T, pemData []byte) *x509.Certificate {
	t.Helper()

	for block, rest := pem.Decode(pemData); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("parse certificate: %v", err)
		}
		return cert
	}
	t.Fatal("the file holds no certificate")
	return nil
}

// requireNoIdentity fails when any identity file was written.
func requireNoIdentity(t *testing.T, dir string) {
	t.Helper()

	for _, name := range []string{identityFileName, legacyKeyFileName, legacyCertFileName} {
		requireNoFile(t, dir, name)
	}
}

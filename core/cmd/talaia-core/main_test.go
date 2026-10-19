package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/catlinux/Talaia/core/internal/ca"
	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
	"github.com/catlinux/Talaia/proto/gen/go/agentv1connect"
)

// testCore is the core running over TLS against the test database.
type testCore struct {
	server *httptest.Server
	store  *store.Store
	ca     *ca.CA
	hub    *hub.Hub
}

func newTestCore(t *testing.T) *testCore {
	t.Helper()

	databaseURL := os.Getenv("TALAIA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TALAIA_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	st, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	authority, err := ca.LoadOrCreate(filepath.Join(t.TempDir(), "ca"))
	if err != nil {
		t.Fatalf("load ca: %v", err)
	}

	h := hub.New(st)
	certificate, err := serverCertificate(authority, []string{"127.0.0.1", "localhost"})
	if err != nil {
		t.Fatalf("server certificate: %v", err)
	}

	server := httptest.NewUnstartedServer(newTLSRouterWithHub(st, authority, h))
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		ClientAuth:   tls.VerifyClientCertIfGiven,
		ClientCAs:    authority.Pool(),
		Certificates: []tls.Certificate{certificate},
		NextProtos:   []string{"h2", "http/1.1"},
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	return &testCore{server: server, store: st, ca: authority, hub: h}
}

// client returns an HTTPS client that trusts the CA and, when not nil, presents
// the given client certificate.
func (c *testCore) client(t *testing.T, certificate *tls.Certificate) *http.Client {
	t.Helper()

	config := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: c.ca.Pool()}
	if certificate != nil {
		config.Certificates = []tls.Certificate{*certificate}
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: config, ForceAttemptHTTP2: true}}
}

// hubClient returns a connect client for the agent hub.
func (c *testCore) hubClient(t *testing.T, certificate *tls.Certificate) agentv1connect.AgentHubClient {
	t.Helper()

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:   tls.VersionTLS13,
			RootCAs:      c.ca.Pool(),
			Certificates: []tls.Certificate{*certificate},
		},
		ForceAttemptHTTP2: true,
	}
	return agentv1connect.NewAgentHubClient(connect.NewClient(
		connecthttp.NewTransport(&http.Client{Transport: transport}, c.server.URL),
	))
}

type enrollResult struct {
	status int
	body   struct {
		ServerID string `json:"server_id"`
		CertPEM  string `json:"cert_pem"`
		CAPEM    string `json:"ca_pem"`
	}
}

func (c *testCore) enroll(t *testing.T, client *http.Client, token, csr, hostname string) enrollResult {
	t.Helper()

	payload, err := json.Marshal(map[string]string{"token": token, "csr_pem": csr, "hostname": hostname})
	if err != nil {
		t.Fatalf("marshal enroll body: %v", err)
	}

	response, err := client.Post(c.server.URL+"/enroll", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post enroll: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	var result enrollResult
	result.status = response.StatusCode
	_ = json.NewDecoder(response.Body).Decode(&result.body)
	return result
}

func (c *testCore) postHub(t *testing.T, client *http.Client) int {
	t.Helper()

	response, err := client.Post(c.server.URL+agentv1connect.AgentHubConnectProcedure, "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post hub: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode
}

func (c *testCore) renew(t *testing.T, client *http.Client, csr string) (int, string) {
	t.Helper()

	payload, err := json.Marshal(map[string]string{"csr_pem": csr})
	if err != nil {
		t.Fatalf("marshal renew body: %v", err)
	}

	response, err := client.Post(c.server.URL+"/renew", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("post renew: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	var body struct {
		CertPEM string `json:"cert_pem"`
	}
	_ = json.NewDecoder(response.Body).Decode(&body)
	return response.StatusCode, body.CertPEM
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within the timeout")
}

func uniqueName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test-%d", time.Now().UnixNano())
}

// cleanupRows deletes the rows the test created, by server name.
func cleanupRows(t *testing.T, names ...string) {
	t.Helper()

	databaseURL := os.Getenv("TALAIA_TEST_DATABASE_URL")
	t.Cleanup(func() {
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			t.Errorf("cleanup: open database: %v", err)
			return
		}
		defer pool.Close()
		for _, name := range names {
			_, _ = pool.Exec(ctx, `DELETE FROM enrollment_token WHERE server_name = $1`, name)
			_, _ = pool.Exec(ctx, `DELETE FROM server WHERE name = $1`, name)
		}
	})
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func csrFor(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()

	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "ignored"},
		DNSNames: []string{"attacker.example.com"},
	}, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

func keyPair(t *testing.T, certPEM string, key *ecdsa.PrivateKey) tls.Certificate {
	t.Helper()

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pair, err := tls.X509KeyPair([]byte(certPEM), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	if err != nil {
		t.Fatalf("key pair: %v", err)
	}
	return pair
}

func parseCertPEM(t *testing.T, certPEM string) *x509.Certificate {
	t.Helper()

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatal("the certificate is not PEM encoded")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

func fingerprintOfPEM(t *testing.T, certPEM []byte) string {
	t.Helper()

	cert := parseCertPEM(t, string(certPEM))
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// enrolledServer is the result of enrolling one server.
type enrolledServer struct {
	serverID    string
	token       string
	key         *ecdsa.PrivateKey
	certificate tls.Certificate
}

// enrollServer creates a token and enrolls a server with it.
func (core *testCore) enrollServer(t *testing.T, name string) enrolledServer {
	t.Helper()

	token, err := core.store.CreateEnrollmentToken(context.Background(), name, 15*time.Minute)
	if err != nil {
		t.Fatalf("create enrollment token: %v", err)
	}

	key := newKey(t)
	result := core.enroll(t, core.client(t, nil), token, csrFor(t, key), name)
	if result.status != http.StatusOK {
		t.Fatalf("enroll status = %d, want 200", result.status)
	}
	if result.body.ServerID == "" {
		t.Fatal("the enroll response has no server id")
	}
	if result.body.CAPEM == "" {
		t.Fatal("the enroll response has no CA certificate")
	}

	return enrolledServer{
		serverID:    result.body.ServerID,
		token:       token,
		key:         key,
		certificate: keyPair(t, result.body.CertPEM, key),
	}
}

// inventoryMessage is the first message of every agent stream.
func inventoryMessage(hostname string) *agentv1.AgentMsg {
	return &agentv1.AgentMsg{
		Msg: &agentv1.AgentMsg_Inventory{Inventory: &agentv1.Inventory{Hostname: hostname, AgentVersion: "test"}},
	}
}

// heartbeatMessage keeps an open stream alive.
func heartbeatMessage(seq uint64) *agentv1.AgentMsg {
	return &agentv1.AgentMsg{
		Msg: &agentv1.AgentMsg_Heartbeat{Heartbeat: &agentv1.Heartbeat{Seq: seq}},
	}
}

// sendInventory opens the hub stream and sends one inventory message.
func (core *testCore) sendInventory(t *testing.T, client agentv1connect.AgentHubClient, hostname string) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	stream, err := client.Connect(ctx)
	if err != nil {
		return err
	}
	t.Cleanup(func() { _ = stream.Close() })

	return stream.Send(inventoryMessage(hostname))
}

// streamError opens the hub stream, sends an inventory and returns the first
// error of the connection, from either opening, sending or receiving.
func (core *testCore) streamError(t *testing.T, client agentv1connect.AgentHubClient, hostname string) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.Connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()

	if err := stream.Send(inventoryMessage(hostname)); err != nil {
		return err
	}
	_, err = stream.Receive()
	return err
}

func TestEnrollAndConnectOverMTLS(t *testing.T) {
	core := newTestCore(t)
	ctx := context.Background()
	name := uniqueName(t)
	cleanupRows(t, name)

	// GET /ca.pem serves the CA certificate the fingerprint pins.
	caResponse, err := core.client(t, nil).Get(core.server.URL + "/ca.pem")
	if err != nil {
		t.Fatalf("get ca.pem: %v", err)
	}
	caPEM, err := io.ReadAll(caResponse.Body)
	_ = caResponse.Body.Close()
	if err != nil {
		t.Fatalf("read ca.pem: %v", err)
	}
	if caResponse.StatusCode != http.StatusOK {
		t.Fatalf("ca.pem status = %d, want 200", caResponse.StatusCode)
	}
	if got := fingerprintOfPEM(t, caPEM); got != core.ca.Fingerprint() {
		t.Errorf("fingerprint = %s, want %s", got, core.ca.Fingerprint())
	}

	enrolled := core.enrollServer(t, name)

	server, err := core.store.GetServer(ctx, enrolled.serverID)
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if server.Name != name {
		t.Errorf("server name = %q, want %q", server.Name, name)
	}
	if server.CertSerial == "" || server.CertNotAfter.IsZero() {
		t.Error("the certificate was not recorded")
	}

	// The agent connects over mTLS with the issued certificate and appears in
	// the registry keyed by server id.
	client := core.hubClient(t, &enrolled.certificate)
	if err := core.sendInventory(t, client, "vps-web-01"); err != nil {
		t.Fatalf("send inventory: %v", err)
	}
	waitFor(t, func() bool {
		agents := core.hub.Connected()
		return len(agents) == 1 && agents[0].ServerID == enrolled.serverID && agents[0].Hostname == "vps-web-01"
	})

	// The token was consumed: replaying it is rejected.
	replay := core.enroll(t, core.client(t, nil), enrolled.token, csrFor(t, newKey(t)), name)
	if replay.status != http.StatusUnauthorized {
		t.Fatalf("replay status = %d, want 401", replay.status)
	}
}

func TestEnrollGarbageCSRDoesNotConsumeTheToken(t *testing.T) {
	core := newTestCore(t)
	name := uniqueName(t)
	cleanupRows(t, name)

	token, err := core.store.CreateEnrollmentToken(context.Background(), name, 15*time.Minute)
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	client := core.client(t, nil)

	if result := core.enroll(t, client, token, "not a csr", name); result.status != http.StatusBadRequest {
		t.Fatalf("garbage csr status = %d, want 400", result.status)
	}

	// The token is still valid.
	if result := core.enroll(t, client, token, csrFor(t, newKey(t)), name); result.status != http.StatusOK {
		t.Fatalf("status = %d, want 200: the token was burned", result.status)
	}

	// And it is only good once.
	if result := core.enroll(t, client, token, csrFor(t, newKey(t)), name); result.status != http.StatusUnauthorized {
		t.Fatalf("replay status = %d, want 401", result.status)
	}
}

func TestHubRequiresACertificateAndRejectsRevokedServers(t *testing.T) {
	core := newTestCore(t)
	ctx := context.Background()
	name := uniqueName(t)
	cleanupRows(t, name)

	enrolled := core.enrollServer(t, name)

	if status := core.postHub(t, core.client(t, nil)); status != http.StatusUnauthorized {
		t.Fatalf("without a certificate: %d, want 401", status)
	}

	status := core.postHub(t, core.client(t, &enrolled.certificate))
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		t.Fatalf("the enrolled server was rejected: %d", status)
	}

	if err := core.store.RevokeServer(ctx, enrolled.serverID); err != nil {
		t.Fatalf("revoke server: %v", err)
	}
	if status := core.postHub(t, core.client(t, &enrolled.certificate)); status != http.StatusForbidden {
		t.Fatalf("the revoked server was accepted: %d", status)
	}

	// Reconnecting over mTLS fails as well.
	client := core.hubClient(t, &enrolled.certificate)
	if err := core.streamError(t, client, "vps-web-01"); err == nil {
		t.Fatal("the revoked server could open a stream")
	}
}

func TestRevocationClosesALiveStream(t *testing.T) {
	core := newTestCore(t)
	ctx := context.Background()
	name := uniqueName(t)
	cleanupRows(t, name)

	// Revocation is checked on every heartbeat tick, not only on connecting.
	core.hub.CheckInterval = 20 * time.Millisecond

	enrolled := core.enrollServer(t, name)
	client := core.hubClient(t, &enrolled.certificate)

	streamCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := client.Connect(streamCtx)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = stream.Close() }()

	if err := stream.Send(inventoryMessage("vps-web-01")); err != nil {
		t.Fatalf("send inventory: %v", err)
	}
	waitFor(t, func() bool { return len(core.hub.Connected()) == 1 })

	if err := core.store.RevokeServer(ctx, enrolled.serverID); err != nil {
		t.Fatalf("revoke server: %v", err)
	}

	// The next heartbeat is past the check interval, so it triggers the check.
	time.Sleep(50 * time.Millisecond)
	if err := stream.Send(heartbeatMessage(1)); err != nil {
		t.Fatalf("send heartbeat: %v", err)
	}

	_, err = stream.Receive()
	if err == nil {
		t.Fatal("the revoked stream stayed open")
	}
	var connectErr *connect.Error
	if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodePermissionDenied {
		t.Fatalf("stream error = %v, want PermissionDenied", err)
	}
	waitFor(t, func() bool { return len(core.hub.Connected()) == 0 })
}

func TestRenewCertificate(t *testing.T) {
	core := newTestCore(t)
	name := uniqueName(t)
	cleanupRows(t, name)

	enrolled := core.enrollServer(t, name)

	if status, _ := core.renew(t, core.client(t, nil), csrFor(t, newKey(t))); status != http.StatusForbidden {
		t.Fatalf("without a certificate: %d, want 403", status)
	}

	// The identity comes from the certificate: another key in the csr does not
	// change the server id.
	renewedKey := newKey(t)
	status, certPEM := core.renew(t, core.client(t, &enrolled.certificate), csrFor(t, renewedKey))
	if status != http.StatusOK {
		t.Fatalf("renew status = %d, want 200", status)
	}

	renewed := parseCertPEM(t, certPEM)
	if renewed.Subject.CommonName != enrolled.serverID {
		t.Errorf("subject = %q, want %q", renewed.Subject.CommonName, enrolled.serverID)
	}
	renewedPublicKey, ok := renewed.PublicKey.(*ecdsa.PublicKey)
	if !ok || !renewedPublicKey.Equal(&renewedKey.PublicKey) {
		t.Error("the renewed certificate does not carry the key of the new csr")
	}
	if _, err := renewed.Verify(x509.VerifyOptions{
		Roots:       core.ca.Pool(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		CurrentTime: time.Now(),
	}); err != nil {
		t.Errorf("the renewed certificate does not verify: %v", err)
	}

	// The serial of the certificate that was current became the previous one.
	server, err := core.store.GetServer(context.Background(), enrolled.serverID)
	if err != nil {
		t.Fatalf("get server: %v", err)
	}
	if server.CertSerial == "" || server.PrevCertSerial == "" || server.CertSerial == server.PrevCertSerial {
		t.Errorf("serials = %q/%q, want a new current serial and the old one as previous", server.CertSerial, server.PrevCertSerial)
	}

	// A renewal that presents the previous certificate is accepted, because the
	// agent may not have stored the new one yet...
	if status, _ := core.renew(t, core.client(t, &enrolled.certificate), csrFor(t, newKey(t))); status != http.StatusOK {
		t.Fatalf("renew with the previous certificate: %d, want 200", status)
	}

	// ...but only while it is the previous one: one more rotation retires it.
	if status, _ := core.renew(t, core.client(t, &enrolled.certificate), csrFor(t, newKey(t))); status != http.StatusForbidden {
		t.Fatalf("renew with a retired serial: %d, want 403", status)
	}
}

func TestEnrollRateLimited(t *testing.T) {
	core := newTestCore(t)
	client := core.client(t, nil)

	for i := 0; i < 10; i++ {
		if result := core.enroll(t, client, "invalid", "not a csr", "vps-web-01"); result.status == http.StatusTooManyRequests {
			t.Fatalf("request %d was limited too early", i+1)
		}
	}

	if result := core.enroll(t, client, "invalid", "not a csr", "vps-web-01"); result.status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", result.status)
	}
}

func TestClientCertificateFromAnotherCAIsRejected(t *testing.T) {
	core := newTestCore(t)
	name := uniqueName(t)
	cleanupRows(t, name)

	enrolled := core.enrollServer(t, name)

	otherCA, err := ca.LoadOrCreate(filepath.Join(t.TempDir(), "other-ca"))
	if err != nil {
		t.Fatalf("load other ca: %v", err)
	}
	otherKey := newKey(t)
	otherCertPEM, _, _, err := otherCA.SignClientCert([]byte(csrFor(t, otherKey)), enrolled.serverID, time.Hour)
	if err != nil {
		t.Fatalf("sign certificate with the other ca: %v", err)
	}
	otherCertificate := keyPair(t, string(otherCertPEM), otherKey)

	response, err := core.client(t, &otherCertificate).Post(
		core.server.URL+agentv1connect.AgentHubConnectProcedure, "application/json", strings.NewReader("{}"))
	if err == nil {
		_ = response.Body.Close()
		t.Fatalf("the handshake must fail, got status %d", response.StatusCode)
	}
}

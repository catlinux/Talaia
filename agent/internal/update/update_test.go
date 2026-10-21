package update

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/catlinux/Talaia/agent/internal/collect"
	"github.com/catlinux/Talaia/agent/internal/enroll"
)

func TestStrictlyNewer(t *testing.T) {
	tests := []struct {
		name    string
		running string
		offered string
		want    bool
	}{
		{name: "patch", running: "1.2.3", offered: "1.2.4", want: true},
		{name: "minor", running: "1.2.3", offered: "1.3.0", want: true},
		{name: "major", running: "1.2.3", offered: "2.0.0", want: true},
		{name: "numeric not lexical", running: "1.9.9", offered: "1.10.0", want: true},
		{name: "equal", running: "1.2.3", offered: "1.2.3", want: false},
		{name: "older patch", running: "1.2.3", offered: "1.2.2", want: false},
		{name: "older minor", running: "1.10.0", offered: "1.9.9", want: false},
		{name: "dev running", running: "dev", offered: "1.0.0", want: true},
		{name: "non-semver running", running: "1.2", offered: "1.0.0", want: true},
		{name: "invalid offered", running: "1.2.3", offered: "1.2", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strictlyNewer(tt.offered, tt.running); got != tt.want {
				t.Errorf("strictlyNewer(%q, %q) = %t, want %t", tt.offered, tt.running, got, tt.want)
			}
		})
	}
}

func TestValidateVersion(t *testing.T) {
	tests := []struct {
		version string
		valid   bool
	}{
		{version: "0.0.0", valid: true},
		{version: "1.2.3", valid: true},
		{version: "1.2", valid: false},
		{version: "1.2.3.4", valid: false},
		{version: "01.2.3", valid: false},
		{version: "1.02.3", valid: false},
		{version: "1.2.03", valid: false},
		{version: "v1.2.3", valid: false},
		{version: "1.2.3-rc1", valid: false},
		{version: "1.2.-3", valid: false},
		{version: "", valid: false},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			err := validateVersion(tt.version)
			if (err == nil) != tt.valid {
				t.Errorf("validateVersion(%q) = %v, want valid=%t", tt.version, err, tt.valid)
			}
		})
	}
}

func TestPackageNames(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		want := "talaia-agent_" + arch + ".deb"
		if got, err := remotePackageName(arch); err != nil || got != want {
			t.Errorf("remotePackageName(%q) = %q, %v", arch, got, err)
		}
		local := "talaia-agent_1.2.3.deb"
		if got := localPackageName("1.2.3"); got != local {
			t.Errorf("localPackageName(1.2.3) = %q, want %q", got, local)
		}
	}
	if _, err := remotePackageName("386"); err == nil {
		t.Error("an unsupported architecture was accepted")
	}
}

func TestNewUsesTheAgentDefaults(t *testing.T) {
	updater := New(Options{})
	if updater.arch != runtime.GOARCH {
		t.Errorf("architecture = %q, want %q", updater.arch, runtime.GOARCH)
	}
	if updater.limit != maxPackageSize {
		t.Errorf("limit = %d, want %d", updater.limit, maxPackageSize)
	}
	if updater.currentVersion != collect.Version() {
		t.Errorf("current version = %q, want %q", updater.currentVersion, collect.Version())
	}
	if updater.dir != DefaultDir {
		t.Errorf("directory = %q, want %q", updater.dir, DefaultDir)
	}
}

func TestUpdateRejectsBadOrders(t *testing.T) {
	const validSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tests := []struct {
		name    string
		version string
		sha     string
		want    string
	}{
		{name: "invalid version", version: "1.2", sha: validSHA, want: "invalid agent version"},
		{name: "short sha", version: "1.2.4", sha: "abc", want: "SHA-256"},
		{name: "non-hex sha", version: "1.2.4", sha: strings.Repeat("g", 64), want: "SHA-256"},
		{name: "equal version", version: "1.2.3", sha: validSHA, want: "not newer"},
		{name: "older version", version: "1.2.2", sha: validSHA, want: "not newer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			runner := &fakeRunner{}
			updater := New(Options{
				CoreURL:        "https://panel.example.com",
				Dir:            dir,
				Runner:         runner,
				CurrentVersion: "1.2.3",
			})
			if _, err := updater.Update(context.Background(), tt.version, tt.sha); err == nil ||
				!strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Update() error = %v, want it to contain %q", err, tt.want)
			}
			if calls := runner.recorded(); len(calls) != 0 {
				t.Errorf("runner was called: %v", calls)
			}
			assertNoTemporaryFiles(t, dir)
		})
	}
}

func TestUpdateRejectsUnsupportedArchitecture(t *testing.T) {
	runner := &fakeRunner{}
	updater := New(Options{
		CoreURL:        "https://panel.example.com",
		Dir:            t.TempDir(),
		Runner:         runner,
		CurrentVersion: "1.2.3",
	})
	updater.arch = "386"

	if _, err := updater.Update(context.Background(), "1.2.4", strings.Repeat("a", 64)); err == nil ||
		!strings.Contains(err.Error(), "unsupported agent architecture") {
		t.Fatalf("Update() error = %v, want an unsupported architecture error", err)
	}
	if calls := runner.recorded(); len(calls) != 0 {
		t.Errorf("runner was called: %v", calls)
	}
}

func TestUpdateRequiresAnIdentity(t *testing.T) {
	updater := New(Options{
		CoreURL:        "https://panel.example.com",
		Dir:            t.TempDir(),
		Runner:         &fakeRunner{},
		CurrentVersion: "1.2.3",
	})
	updater.arch = "amd64"
	if _, err := updater.Update(context.Background(), "1.2.4", strings.Repeat("a", 64)); err == nil ||
		!strings.Contains(err.Error(), "identity") {
		t.Fatalf("Update() error = %v, want an identity error", err)
	}
}

func TestUpdateHappyPath(t *testing.T) {
	ca := newTestCA(t)
	identity := newTestIdentity(t, ca)
	body := []byte("the new agent package")
	sha := sha256Hex(body)
	server := newTestServer(t, ca, http.StatusOK, body)

	dir := t.TempDir()
	oldPackage := []byte("the installed agent package")
	if err := os.WriteFile(filepath.Join(dir, "talaia-agent_1.2.3.deb"), oldPackage, 0o600); err != nil {
		t.Fatalf("write old package: %v", err)
	}
	runner := &fakeRunner{}
	updater := newTestUpdater(server.server.URL, identity, dir, "1.2.3", runner)

	message, err := updater.Update(context.Background(), "1.2.4", sha)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if want := "update to 1.2.4 started; the agent will restart"; message != want {
		t.Errorf("message = %q, want %q", message, want)
	}
	if server.requests.Load() != 1 {
		t.Errorf("requests = %d, want 1", server.requests.Load())
	}

	target := filepath.Join(dir, "talaia-agent_1.2.4.deb")
	if got, err := os.ReadFile(target); err != nil || string(got) != string(body) {
		t.Errorf("new package = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "previous.deb")); err != nil || string(got) != string(oldPackage) {
		t.Errorf("previous package = %q, %v", got, err)
	}
	assertNoTemporaryFiles(t, dir)
	assertModes(t, dir, target)

	call := runner.recorded()
	if len(call) != 1 {
		t.Fatalf("runner calls = %v, want 1", call)
	}
	if call[0].name != "systemd-run" {
		t.Errorf("runner name = %q, want systemd-run", call[0].name)
	}
	wantArgs := []string{
		"--unit=talaia-agent-upgrade", "--collect", "--no-block",
		"apt-get", "install", "-y", "--reinstall", target,
	}
	if !slices.Equal(call[0].args, wantArgs) {
		t.Errorf("runner args = %v, want %v", call[0].args, wantArgs)
	}
}

func TestUpdateAllowsTheDevVersion(t *testing.T) {
	ca := newTestCA(t)
	identity := newTestIdentity(t, ca)
	body := []byte("dev agent package")
	server := newTestServer(t, ca, http.StatusOK, body)
	dir := filepath.Join(t.TempDir(), "update")
	runner := &fakeRunner{}
	updater := newTestUpdater(server.server.URL, identity, dir, "dev", runner)

	if _, err := updater.Update(context.Background(), "1.2.4", sha256Hex(body)); err != nil {
		t.Fatalf("Update() with a dev running version = %v", err)
	}
	target := filepath.Join(dir, "talaia-agent_1.2.4.deb")
	if _, err := os.Stat(target); err != nil {
		t.Errorf("package not saved: %v", err)
	}
	assertModes(t, dir, target)
}

func TestUpdateHashMismatchLeavesNoPackage(t *testing.T) {
	ca := newTestCA(t)
	identity := newTestIdentity(t, ca)
	server := newTestServer(t, ca, http.StatusOK, []byte("the wrong package"))
	dir := t.TempDir()
	runner := &fakeRunner{}
	updater := newTestUpdater(server.server.URL, identity, dir, "1.2.3", runner)

	if _, err := updater.Update(context.Background(), "1.2.4", sha256Hex([]byte("the expected package"))); err == nil ||
		!strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("Update() error = %v, want a SHA-256 error", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "talaia-agent_1.2.4.deb")); !os.IsNotExist(err) {
		t.Errorf("the bad package was left in the directory: %v", err)
	}
	assertNoTemporaryFiles(t, dir)
	if calls := runner.recorded(); len(calls) != 0 {
		t.Errorf("runner was called: %v", calls)
	}
}

func TestUpdateRejectsAnOversizedPackage(t *testing.T) {
	ca := newTestCA(t)
	identity := newTestIdentity(t, ca)
	body := []byte("0123456789")
	server := newTestServer(t, ca, http.StatusOK, body)
	dir := t.TempDir()
	runner := &fakeRunner{}
	updater := newTestUpdater(server.server.URL, identity, dir, "1.2.3", runner)
	updater.limit = 8

	if _, err := updater.Update(context.Background(), "1.2.4", sha256Hex(body)); err == nil ||
		!strings.Contains(err.Error(), "larger than") {
		t.Fatalf("Update() error = %v, want a size error", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "talaia-agent_1.2.4.deb")); !os.IsNotExist(err) {
		t.Errorf("the oversized package was left in the directory: %v", err)
	}
	assertNoTemporaryFiles(t, dir)
	if calls := runner.recorded(); len(calls) != 0 {
		t.Errorf("runner was called: %v", calls)
	}
}

func TestUpdateRequiresHTTPStatusOK(t *testing.T) {
	ca := newTestCA(t)
	identity := newTestIdentity(t, ca)
	server := newTestServer(t, ca, http.StatusInternalServerError, []byte("oops"))
	dir := t.TempDir()
	runner := &fakeRunner{}
	updater := newTestUpdater(server.server.URL, identity, dir, "1.2.3", runner)

	if _, err := updater.Update(context.Background(), "1.2.4", strings.Repeat("a", 64)); err == nil ||
		!strings.Contains(err.Error(), "HTTP status") {
		t.Fatalf("Update() error = %v, want an HTTP status error", err)
	}
	assertNoTemporaryFiles(t, dir)
	if calls := runner.recorded(); len(calls) != 0 {
		t.Errorf("runner was called: %v", calls)
	}
}

func TestUpdateDoesNotKeepAPreviousPackageWhenNoneExists(t *testing.T) {
	ca := newTestCA(t)
	identity := newTestIdentity(t, ca)
	body := []byte("first agent package")
	server := newTestServer(t, ca, http.StatusOK, body)
	dir := t.TempDir()
	updater := newTestUpdater(server.server.URL, identity, dir, "1.2.3", &fakeRunner{})

	if _, err := updater.Update(context.Background(), "1.2.4", sha256Hex(body)); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "previous.deb")); !os.IsNotExist(err) {
		t.Errorf("previous.deb was created without an old package: %v", err)
	}
}

func newTestUpdater(coreURL string, identity *enroll.Identity, dir, currentVersion string, runner Runner) *Updater {
	updater := New(Options{
		CoreURL:        coreURL,
		Identity:       identity,
		Dir:            dir,
		Runner:         runner,
		CurrentVersion: currentVersion,
	})
	// The test server and the assertions use amd64 on every host.
	updater.arch = "amd64"
	return updater
}

func assertNoTemporaryFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".*"))
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("temporary files left behind: %v", matches)
	}
}

func assertModes(t *testing.T, dir, packagePath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat update directory: %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %o, want 700", dirInfo.Mode().Perm())
	}
	packageInfo, err := os.Stat(packagePath)
	if err != nil {
		t.Fatalf("stat package: %v", err)
	}
	if packageInfo.Mode().Perm() != 0o600 {
		t.Errorf("package mode = %o, want 600", packageInfo.Mode().Perm())
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type runnerCall struct {
	name string
	args []string
}

type fakeRunner struct {
	mu    sync.Mutex
	calls []runnerCall
	err   error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, runnerCall{name: name, args: slices.Clone(args)})
	return f.err
}

func (f *fakeRunner) recorded() []runnerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

type testCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
	serial  atomic.Int64
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test ca"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create ca certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse ca certificate: %v", err)
	}
	return &testCA{
		cert:    cert,
		key:     key,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

func (c *testCA) issue(t *testing.T, commonName string, publicKey any, usage x509.ExtKeyUsage, ips ...net.IP) []byte {
	t.Helper()
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(c.serial.Add(1)),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, c.cert, publicKey, c.key)
	if err != nil {
		t.Fatalf("issue certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func (c *testCA) pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(c.cert)
	return pool
}

func (c *testCA) issueServer(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	certPEM := c.issue(t, "test core", &key.PublicKey, x509.ExtKeyUsageServerAuth, net.ParseIP("127.0.0.1"))
	block, _ := pem.Decode(certPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse server certificate: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{leaf.Raw}, PrivateKey: key, Leaf: leaf}
}

func (c *testCA) issueClient(t *testing.T, serverID string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	certPEM = c.issue(t, serverID, &key.PublicKey, x509.ExtKeyUsageClientAuth)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal client key: %v", err)
	}
	return certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func newTestIdentity(t *testing.T, ca *testCA) *enroll.Identity {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "certs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create cert directory: %v", err)
	}
	certPEM, keyPEM := ca.issueClient(t, "server-1")
	identityPEM := append(append([]byte(nil), keyPEM...), certPEM...)
	if err := os.WriteFile(filepath.Join(dir, "agent.pem"), identityPEM, 0o600); err != nil {
		t.Fatalf("write identity: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), ca.certPEM, 0o644); err != nil {
		t.Fatalf("write ca: %v", err)
	}
	identity, err := enroll.LoadIdentity(dir)
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	return identity
}

type testServer struct {
	server   *httptest.Server
	requests atomic.Int32
}

func newTestServer(t *testing.T, ca *testCA, status int, body []byte) *testServer {
	t.Helper()
	result := &testServer{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result.requests.Add(1)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/agent/talaia-agent_amd64.deb" {
			t.Errorf("path = %q, want /agent/talaia-agent_amd64.deb", r.URL.Path)
		}
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			t.Error("the download did not present the agent client certificate")
		} else if got := r.TLS.PeerCertificates[0].Subject.CommonName; got != "server-1" {
			t.Errorf("client certificate common name = %q, want server-1", got)
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			if _, err := w.Write(body); err != nil {
				t.Errorf("write response: %v", err)
			}
		}
	})
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    ca.pool(),
		Certificates: []tls.Certificate{ca.issueServer(t)},
		NextProtos:   []string{"http/1.1"},
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	result.server = server
	return result
}

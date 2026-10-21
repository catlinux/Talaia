package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/registration"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testCertificate returns a self-signed certificate and its key in PEM.
func testCertificate(t *testing.T, serial int64, notAfter time.Time) ([]byte, []byte) {
	t.Helper()
	return testCertificateFor(t, serial, notAfter, "panel.example.com")
}

func testCertificateFor(t *testing.T, serial int64, notAfter time.Time, names ...string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: names[0]},
		DNSNames:              names,
		NotBefore:             notAfter.Add(-90 * 24 * time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("encode key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

// writeStoredCertificate writes a certificate where the manager looks for it
// with the default directory.
func writeStoredCertificate(t *testing.T, dir string, certPEM, keyPEM []byte) {
	t.Helper()
	acmeDir := defaultStorage(t, dir)
	if err := os.MkdirAll(acmeDir, directoryMode); err != nil {
		t.Fatalf("create the ACME directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(acmeDir, certificateName), certPEM, privateFileMode); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	if err := os.WriteFile(filepath.Join(acmeDir, privateKeyName), keyPEM, privateFileMode); err != nil {
		t.Fatalf("write key: %v", err)
	}
}

func checkMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", path, got, want)
	}
}

type fakeIssuer struct {
	mu          sync.Mutex
	calls       int
	lastDomains []string
	certPEM     []byte
	keyPEM      []byte
	err         error
}

func (f *fakeIssuer) Obtain(_ context.Context, domains []string) (Certificate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastDomains = append([]string(nil), domains...)
	if f.err != nil {
		return Certificate{}, f.err
	}
	return Certificate{CertificatePEM: f.certPEM, PrivateKeyPEM: f.keyPEM}, nil
}

func (f *fakeIssuer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func testOptions(t *testing.T, issuer Issuer) Options {
	t.Helper()
	return Options{
		Provider: "ovh",
		Email:    "ops@example.com",
		Domains:  []string{"Panel.Example.com.", "192.0.2.10"},
		DataDir:  t.TempDir(),
		Logger:   testLogger(),
		now:      func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) },
		issuer:   issuer,
	}
}

func TestNewValidation(t *testing.T) {
	valid := func() Options {
		return Options{
			Provider: "ovh",
			Email:    "ops@example.com",
			Domains:  []string{"panel.example.com"},
			DataDir:  t.TempDir(),
			Logger:   testLogger(),
			issuer:   &fakeIssuer{},
		}
	}
	notAfter := time.Now().Add(90 * 24 * time.Hour)
	certPEM, keyPEM := testCertificate(t, 1, notAfter)
	fallback, err := parseCertificate(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		change  func(*Options)
		wantErr string
	}{
		{name: "unsupported provider", change: func(o *Options) { o.Provider = "cloudflare" }, wantErr: "unsupported ACME DNS provider"},
		{name: "only IP addresses", change: func(o *Options) { o.Domains = []string{"192.0.2.10"} }, wantErr: "at least one DNS name"},
		{name: "missing data dir", change: func(o *Options) { o.DataDir = "" }, wantErr: "data directory"},
		{name: "invalid directory", change: func(o *Options) { o.DirectoryURL = "not a url" }, wantErr: "invalid ACME directory URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := valid()
			tt.change(&opts)
			if _, err := New(opts, *fallback); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}

	if _, err := New(valid(), *fallback); err != nil {
		t.Fatalf("valid options: %v", err)
	}
}

func TestGetCertificateFallsBack(t *testing.T) {
	certPEM, keyPEM := testCertificate(t, 1, time.Now().Add(365*24*time.Hour))
	fallback, err := parseCertificate(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := New(testOptions(t, &fakeIssuer{}), *fallback)
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !current.Leaf.PublicKey.(*ecdsa.PublicKey).Equal(fallback.Leaf.PublicKey.(*ecdsa.PublicKey)) {
		t.Fatal("GetCertificate did not return the fallback certificate")
	}
}

func TestNewUsesStoredCertificate(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	certPEM, keyPEM := testCertificate(t, 7, now.Add(60*24*time.Hour))
	fallbackPEM, fallbackKey := testCertificate(t, 1, now.Add(365*24*time.Hour))
	fallback, err := parseCertificate(fallbackPEM, fallbackKey)
	if err != nil {
		t.Fatal(err)
	}

	opts := testOptions(t, &fakeIssuer{})
	opts.now = func() time.Time { return now }
	writeStoredCertificate(t, opts.DataDir, certPEM, keyPEM)

	manager, err := New(opts, *fallback)
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if current.Leaf.SerialNumber.Int64() != 7 {
		t.Fatalf("serial = %d, want the stored certificate", current.Leaf.SerialNumber)
	}
}

func TestExpiredStoredCertificateIsIgnored(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	certPEM, keyPEM := testCertificate(t, 7, now.Add(-time.Hour))
	fallbackPEM, fallbackKey := testCertificate(t, 1, now.Add(365*24*time.Hour))
	fallback, err := parseCertificate(fallbackPEM, fallbackKey)
	if err != nil {
		t.Fatal(err)
	}

	opts := testOptions(t, &fakeIssuer{})
	opts.now = func() time.Time { return now }
	writeStoredCertificate(t, opts.DataDir, certPEM, keyPEM)

	manager, err := New(opts, *fallback)
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !current.Leaf.PublicKey.(*ecdsa.PublicKey).Equal(fallback.Leaf.PublicKey.(*ecdsa.PublicKey)) {
		t.Fatal("the expired certificate was loaded")
	}
}

func TestStoredCertificateForOtherDomainsIsIgnored(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	certPEM, keyPEM := testCertificateFor(t, 7, now.Add(60*24*time.Hour), "other.example.com")
	fallbackPEM, fallbackKey := testCertificate(t, 1, now.Add(365*24*time.Hour))
	fallback, err := parseCertificate(fallbackPEM, fallbackKey)
	if err != nil {
		t.Fatal(err)
	}

	opts := testOptions(t, &fakeIssuer{})
	opts.now = func() time.Time { return now }
	writeStoredCertificate(t, opts.DataDir, certPEM, keyPEM)

	manager, err := New(opts, *fallback)
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if current.Leaf.SerialNumber.Int64() != 1 {
		t.Fatalf("serial = %d, want the fallback certificate", current.Leaf.SerialNumber)
	}
}

func TestRenewObtainsAndStores(t *testing.T) {
	opts := testOptions(t, nil)
	now := opts.now()
	certPEM, keyPEM := testCertificate(t, 9, now.Add(90*24*time.Hour))
	issuer := &fakeIssuer{certPEM: certPEM, keyPEM: keyPEM}
	opts.issuer = issuer
	fallbackPEM, fallbackKey := testCertificate(t, 1, now.Add(365*24*time.Hour))
	fallback, err := parseCertificate(fallbackPEM, fallbackKey)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := New(opts, *fallback)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := manager.renew(context.Background())
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if !renewed {
		t.Fatal("expected a certificate request")
	}
	if issuer.count() != 1 {
		t.Fatalf("obtain calls = %d, want 1", issuer.count())
	}
	if got := issuer.lastDomains; len(got) != 1 || got[0] != "panel.example.com" {
		t.Fatalf("domains = %v, want only the DNS name", got)
	}
	current, err := manager.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if current.Leaf.SerialNumber.Int64() != 9 {
		t.Fatalf("serial = %d, want the new certificate", current.Leaf.SerialNumber)
	}

	acmeDir := defaultStorage(t, opts.DataDir)
	checkMode(t, acmeDir, directoryMode)
	checkMode(t, filepath.Join(acmeDir, certificateName), privateFileMode)
	checkMode(t, filepath.Join(acmeDir, privateKeyName), privateFileMode)
	if _, err := parseCertificate(
		mustRead(t, filepath.Join(acmeDir, certificateName)),
		mustRead(t, filepath.Join(acmeDir, privateKeyName)),
	); err != nil {
		t.Fatalf("stored certificate: %v", err)
	}
}

func TestRenewSkipsFreshCertificate(t *testing.T) {
	opts := testOptions(t, nil)
	now := opts.now()
	certPEM, keyPEM := testCertificate(t, 7, now.Add(60*24*time.Hour))
	writeStoredCertificate(t, opts.DataDir, certPEM, keyPEM)

	issuer := &fakeIssuer{err: context.DeadlineExceeded}
	opts.issuer = issuer
	fallbackPEM, fallbackKey := testCertificate(t, 1, now.Add(365*24*time.Hour))
	fallback, err := parseCertificate(fallbackPEM, fallbackKey)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := New(opts, *fallback)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := manager.renew(context.Background())
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if renewed || issuer.count() != 0 {
		t.Fatalf("renewed = %t, calls = %d; the certificate was still fresh", renewed, issuer.count())
	}
}

func TestRenewNearExpiry(t *testing.T) {
	opts := testOptions(t, nil)
	now := opts.now()
	oldPEM, oldKey := testCertificate(t, 7, now.Add(10*24*time.Hour))
	writeStoredCertificate(t, opts.DataDir, oldPEM, oldKey)
	newPEM, newKey := testCertificate(t, 9, now.Add(90*24*time.Hour))
	issuer := &fakeIssuer{certPEM: newPEM, keyPEM: newKey}
	opts.issuer = issuer
	fallbackPEM, fallbackKey := testCertificate(t, 1, now.Add(365*24*time.Hour))
	fallback, err := parseCertificate(fallbackPEM, fallbackKey)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := New(opts, *fallback)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := manager.renew(context.Background())
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	current, err := manager.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !renewed || issuer.count() != 1 || current.Leaf.SerialNumber.Int64() != 9 {
		t.Fatalf("renewed = %t, calls = %d, serial = %d", renewed, issuer.count(), current.Leaf.SerialNumber)
	}
}

func TestRenewFailureKeepsTheCurrentCertificate(t *testing.T) {
	opts := testOptions(t, nil)
	now := opts.now()
	oldPEM, oldKey := testCertificate(t, 7, now.Add(10*24*time.Hour))
	writeStoredCertificate(t, opts.DataDir, oldPEM, oldKey)
	opts.issuer = &fakeIssuer{err: context.DeadlineExceeded}
	fallbackPEM, fallbackKey := testCertificate(t, 1, now.Add(365*24*time.Hour))
	fallback, err := parseCertificate(fallbackPEM, fallbackKey)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := New(opts, *fallback)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := manager.renew(context.Background())
	if err == nil {
		t.Fatal("expected the renewal error")
	}
	if renewed {
		t.Fatal("a failed renewal reported success")
	}
	current, err := manager.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if current.Leaf.SerialNumber.Int64() != 7 {
		t.Fatalf("serial = %d, want the previous certificate", current.Leaf.SerialNumber)
	}
}

func TestStartObtainsInTheBackground(t *testing.T) {
	opts := testOptions(t, nil)
	now := opts.now()
	certPEM, keyPEM := testCertificate(t, 9, now.Add(90*24*time.Hour))
	issuer := &fakeIssuer{certPEM: certPEM, keyPEM: keyPEM}
	opts.issuer = issuer
	fallbackPEM, fallbackKey := testCertificate(t, 1, now.Add(365*24*time.Hour))
	fallback, err := parseCertificate(fallbackPEM, fallbackKey)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := New(opts, *fallback)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for {
		current, err := manager.GetCertificate(nil)
		if err != nil {
			t.Fatal(err)
		}
		if current.Leaf.SerialNumber.Int64() == 9 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the background manager did not obtain a certificate")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if issuer.count() != 1 {
		t.Fatalf("obtain calls = %d, want 1", issuer.count())
	}
}

func TestDNSNames(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   []string
	}{
		{name: "cleans names", values: []string{"Panel.Example.com.", " panel.example.com ", "B.example.com"}, want: []string{"panel.example.com", "b.example.com"}},
		{name: "drops addresses", values: []string{"192.0.2.10", "2001:db8::1", "panel.example.com"}, want: []string{"panel.example.com"}},
		{name: "drops empties", values: []string{"", "  "}, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DNSNames(tt.values)
			if len(got) != len(tt.want) {
				t.Fatalf("DNSNames() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("DNSNames() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestProviders(t *testing.T) {
	if !SupportedProvider("ovh") {
		t.Fatal("ovh is not a supported provider")
	}
	if SupportedProvider("cloudflare") {
		t.Fatal("cloudflare is not implemented yet")
	}
	if got := Providers(); len(got) != 2 || got[0] != ProviderDelegation || got[1] != "ovh" {
		t.Fatalf("Providers() = %v, want [delegation ovh]", got)
	}
}

func TestAccountKeyIsReused(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), directoryName)
	first := &legoIssuer{email: "ops@example.com", dataDir: dataDir}
	account1, err := first.loadAccount()
	if err != nil {
		t.Fatalf("load the account: %v", err)
	}
	checkMode(t, dataDir, directoryMode)
	checkMode(t, filepath.Join(dataDir, accountKeyName), privateFileMode)

	second := &legoIssuer{email: "ops@example.com", dataDir: dataDir}
	account2, err := second.loadAccount()
	if err != nil {
		t.Fatalf("reload the account: %v", err)
	}
	key1, ok := account1.key.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("account key is %T, want ECDSA", account1.key)
	}
	key2, ok := account2.key.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("account key is %T, want ECDSA", account2.key)
	}
	if !key1.PublicKey.Equal(&key2.PublicKey) {
		t.Fatal("the account key was generated again")
	}
}

func TestRegistrationRoundTrip(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), directoryName)
	if err := os.MkdirAll(dataDir, directoryMode); err != nil {
		t.Fatal(err)
	}
	issuer := &legoIssuer{dataDir: dataDir}
	registrationURL := "https://acme.example/account/1"
	if err := issuer.saveRegistration(&registration.Resource{URI: registrationURL}); err != nil {
		t.Fatalf("save: %v", err)
	}
	checkMode(t, filepath.Join(dataDir, accountName), privateFileMode)

	reg, err := readRegistration(filepath.Join(dataDir, accountName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if reg.URI != registrationURL {
		t.Fatalf("URI = %q, want %q", reg.URI, registrationURL)
	}

	if err := os.WriteFile(filepath.Join(dataDir, accountName), []byte(`{}`), privateFileMode); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegistration(filepath.Join(dataDir, accountName)); err == nil {
		t.Fatal("a registration without URL was accepted")
	}
}

// discardProvider answers the DNS challenge without publishing anything. The
// integration test uses it against pebble with PEBBLE_VA_ALWAYS_VALID=1.
type discardProvider struct{}

func (discardProvider) Present(string, string, string) error { return nil }
func (discardProvider) CleanUp(string, string, string) error { return nil }

// TestObtainFromARealACMEServer runs the lego path against an ACME server.
// Start a pebble server with PEBBLE_VA_ALWAYS_VALID=1 and point
// TALAIA_TEST_ACME_DIRECTORY to its directory URL (and TALAIA_TEST_ACME_CA_CERT
// to the PEM of its test CA when it uses a self-signed certificate).
func TestObtainFromARealACMEServer(t *testing.T) {
	directoryURL := strings.TrimSpace(os.Getenv("TALAIA_TEST_ACME_DIRECTORY"))
	if directoryURL == "" {
		t.Skip("set TALAIA_TEST_ACME_DIRECTORY to the directory URL of a pebble server started with PEBBLE_VA_ALWAYS_VALID=1")
	}
	if caFile := strings.TrimSpace(os.Getenv("TALAIA_TEST_ACME_CA_CERT")); caFile != "" {
		t.Setenv("LEGO_CA_CERTIFICATES", caFile)
	}

	certPEM, keyPEM := testCertificate(t, 1, time.Now().Add(365*24*time.Hour))
	fallback, err := parseCertificate(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Options{
		Provider:     "ovh",
		Email:        "acme@example.com",
		DirectoryURL: directoryURL,
		Domains:      []string{"panel.example.com"},
		DataDir:      t.TempDir(),
		Logger:       testLogger(),
		newProvider:  func() (challenge.Provider, error) { return discardProvider{}, nil },
		challengeOptions: []dns01.ChallengeOption{
			dns01.DisableAuthoritativeNssPropagationRequirement(),
		},
	}, *fallback)
	if err != nil {
		t.Fatal(err)
	}

	renewed, err := manager.renew(context.Background())
	if err != nil {
		t.Fatalf("obtain: %v", err)
	}
	if !renewed {
		t.Fatal("expected a certificate")
	}
	current, err := manager.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	if current.Leaf.SerialNumber.Cmp(fallback.Leaf.SerialNumber) == 0 {
		t.Fatal("the manager still serves the fallback certificate")
	}
	for _, name := range []string{accountKeyName, accountName, certificateName, privateKeyName} {
		if _, err := os.Stat(filepath.Join(manager.dataDir, name)); err != nil {
			t.Errorf("the %s file was not stored: %v", name, err)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func TestGetCertificateMatchesTheServerName(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	certPEM, keyPEM := testCertificate(t, 7, now.Add(60*24*time.Hour))
	fallbackPEM, fallbackKey := testCertificate(t, 1, now.Add(365*24*time.Hour))
	fallback, err := parseCertificate(fallbackPEM, fallbackKey)
	if err != nil {
		t.Fatal(err)
	}
	opts := testOptions(t, &fakeIssuer{})
	opts.now = func() time.Time { return now }
	writeStoredCertificate(t, opts.DataDir, certPEM, keyPEM)
	manager, err := New(opts, *fallback)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		serverName string
		want       int64
	}{
		{serverName: "panel.example.com", want: 7},
		{serverName: "", want: 1},
		{serverName: "other.example.com", want: 1},
	} {
		current, err := manager.GetCertificate(&tls.ClientHelloInfo{ServerName: tt.serverName})
		if err != nil {
			t.Fatal(err)
		}
		if got := current.Leaf.SerialNumber.Int64(); got != tt.want {
			t.Errorf("server name %q: serial = %d, want %d", tt.serverName, got, tt.want)
		}
	}
}

// defaultStorage is the storage directory of the default ACME directory.
func defaultStorage(t *testing.T, dataDir string) string {
	t.Helper()
	directory, err := url.Parse(DefaultDirectoryURL)
	if err != nil {
		t.Fatal(err)
	}
	return storageDir(dataDir, directory)
}

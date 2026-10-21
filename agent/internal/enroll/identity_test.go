package enroll

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testIssuer is a throwaway CA for the tests.
type testIssuer struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
	serial  atomic.Int64
}

func newTestIssuer(t *testing.T) *testIssuer {
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

	return &testIssuer{
		cert:    cert,
		key:     key,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

func (i *testIssuer) nextSerial() *big.Int {
	return big.NewInt(i.serial.Add(1))
}

// issue signs a certificate for publicKey.
func (i *testIssuer) issue(commonName string, publicKey any, usage x509.ExtKeyUsage, notBefore, notAfter time.Time) ([]byte, error) {
	template := &x509.Certificate{
		SerialNumber: i.nextSerial(),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, i.cert, publicKey, i.key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// issueServer signs the certificate of the test core.
func (i *testIssuer) issueServer(ips ...net.IP) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: i.nextSerial(),
		Subject:      pkix.Name{CommonName: "test core"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, i.cert, &key.PublicKey, i.key)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, nil
}

// clientFiles issues a client certificate and its private key for serverID.
func (i *testIssuer) clientFiles(t *testing.T, serverID string, notBefore, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	certPEM, err = i.issue(serverID, &key.PublicKey, x509.ExtKeyUsageClientAuth, notBefore, notAfter)
	if err != nil {
		t.Fatalf("issue certificate: %v", err)
	}
	keyPEM, err = marshalKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return certPEM, keyPEM
}

// writeFiles writes the given files into dir, creating it.
func writeFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()

	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, identityMode); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// requireNoFile fails when the file exists.
func requireNoFile(t *testing.T, dir, name string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s must not exist", filepath.Join(dir, name))
	}
}

// writeTestIdentity writes an identity in the current layout and loads it.
func writeTestIdentity(t *testing.T, dir string, issuer *testIssuer, serverID string, notBefore, notAfter time.Time) *Identity {
	t.Helper()

	certPEM, keyPEM := issuer.clientFiles(t, serverID, notBefore, notAfter)
	writeFiles(t, dir, map[string][]byte{
		identityFileName: joinIdentity(keyPEM, certPEM),
		caFileName:       issuer.certPEM,
	})

	identity, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	return identity
}

// writeLegacyIdentity writes an identity in the first layout, key and
// certificate apart.
func writeLegacyIdentity(t *testing.T, dir string, issuer *testIssuer, serverID string, notBefore, notAfter time.Time) {
	t.Helper()

	certPEM, keyPEM := issuer.clientFiles(t, serverID, notBefore, notAfter)
	writeFiles(t, dir, map[string][]byte{
		legacyKeyFileName:  keyPEM,
		legacyCertFileName: certPEM,
		caFileName:         issuer.certPEM,
	})
}

func TestLoadIdentityNotEnrolled(t *testing.T) {
	if _, err := LoadIdentity(t.TempDir()); !errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("error = %v, want ErrNotEnrolled", err)
	}
}

func TestIdentitySaveLoadRoundTrip(t *testing.T) {
	issuer := newTestIssuer(t)
	dir := filepath.Join(t.TempDir(), "certs")
	now := time.Now()
	identity := writeTestIdentity(t, dir, issuer, "server-1", now, now.Add(24*time.Hour))

	// A renewal replaces the file: the key and the certificate come from it, in
	// this order.
	certPEM, keyPEM := issuer.clientFiles(t, "server-2", now, now.Add(48*time.Hour))
	if err := identity.save(keyPEM, certPEM); err != nil {
		t.Fatalf("save: %v", err)
	}

	reloaded, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	if reloaded.ServerID() != "server-2" {
		t.Errorf("server id = %q, want server-2", reloaded.ServerID())
	}

	stored, err := os.ReadFile(filepath.Join(dir, identityFileName))
	if err != nil {
		t.Fatalf("read %s: %v", identityFileName, err)
	}
	if !bytes.HasPrefix(stored, bytes.TrimRight(keyPEM, "\r\n")) {
		t.Error("the identity file must start with the private key")
	}
	if !bytes.HasPrefix(stored, []byte("-----BEGIN PRIVATE KEY-----")) {
		t.Error("the key must be a PKCS#8 PRIVATE KEY block")
	}
	if !bytes.HasSuffix(stored, certPEM) {
		t.Error("the identity file must end with the certificate")
	}
}

func TestLoadIdentityMigratesLegacyFiles(t *testing.T) {
	issuer := newTestIssuer(t)
	dir := filepath.Join(t.TempDir(), "certs")
	now := time.Now()
	writeLegacyIdentity(t, dir, issuer, "server-1", now, now.Add(24*time.Hour))

	identity, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	if identity.ServerID() != "server-1" {
		t.Errorf("server id = %q, want server-1", identity.ServerID())
	}

	if _, err := os.Stat(filepath.Join(dir, identityFileName)); err != nil {
		t.Errorf("%s was not written: %v", identityFileName, err)
	}
	for _, name := range []string{legacyKeyFileName, legacyCertFileName} {
		requireNoFile(t, dir, name)
	}

	// The migrated identity loads again, now from the single file.
	if _, err := LoadIdentity(dir); err != nil {
		t.Fatalf("load the migrated identity: %v", err)
	}
}

func TestLoadIdentityDoesNotMigrateABrokenLegacyPair(t *testing.T) {
	issuer := newTestIssuer(t)
	dir := filepath.Join(t.TempDir(), "certs")
	now := time.Now()
	writeLegacyIdentity(t, dir, issuer, "server-1", now, now.Add(24*time.Hour))

	// The key belongs to another identity.
	_, otherKeyPEM := issuer.clientFiles(t, "server-2", now, now.Add(24*time.Hour))
	if err := os.WriteFile(filepath.Join(dir, legacyKeyFileName), otherKeyPEM, identityMode); err != nil {
		t.Fatalf("write %s: %v", legacyKeyFileName, err)
	}

	_, err := LoadIdentity(dir)
	if err == nil || errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("error = %v, want an error that is not ErrNotEnrolled", err)
	}
	if !strings.Contains(err.Error(), legacyKeyFileName) {
		t.Errorf("error = %v, must name %s", err, legacyKeyFileName)
	}

	// Nothing was migrated and the legacy files are still there.
	requireNoFile(t, dir, identityFileName)
	for _, name := range []string{legacyKeyFileName, legacyCertFileName} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("the legacy %s was removed: %v", name, err)
		}
	}
}

func TestLoadIdentityIncompleteLegacyPair(t *testing.T) {
	issuer := newTestIssuer(t)
	dir := filepath.Join(t.TempDir(), "certs")
	now := time.Now()
	_, keyPEM := issuer.clientFiles(t, "server-1", now, now.Add(24*time.Hour))
	writeFiles(t, dir, map[string][]byte{legacyKeyFileName: keyPEM, caFileName: issuer.certPEM})

	_, err := LoadIdentity(dir)
	if err == nil || errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("error = %v, want an error that is not ErrNotEnrolled", err)
	}
	if !strings.Contains(err.Error(), legacyCertFileName) {
		t.Errorf("error = %v, must name %s", err, legacyCertFileName)
	}
}

func TestLoadIdentityCorruptIdentityFile(t *testing.T) {
	issuer := newTestIssuer(t)
	dir := filepath.Join(t.TempDir(), "certs")
	writeFiles(t, dir, map[string][]byte{
		identityFileName: []byte("this is not a certificate\n"),
		caFileName:       issuer.certPEM,
	})

	_, err := LoadIdentity(dir)
	if err == nil || errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("error = %v, want an error that is not ErrNotEnrolled", err)
	}
	if !strings.Contains(err.Error(), identityFileName) {
		t.Errorf("error = %v, must name %s", err, identityFileName)
	}
}

func TestLoadIdentityRejectsAKeyThatDoesNotMatch(t *testing.T) {
	issuer := newTestIssuer(t)
	dir := filepath.Join(t.TempDir(), "certs")
	now := time.Now()
	certPEM, _ := issuer.clientFiles(t, "server-1", now, now.Add(24*time.Hour))
	_, otherKeyPEM := issuer.clientFiles(t, "server-2", now, now.Add(24*time.Hour))
	writeFiles(t, dir, map[string][]byte{
		identityFileName: joinIdentity(otherKeyPEM, certPEM),
		caFileName:       issuer.certPEM,
	})

	_, err := LoadIdentity(dir)
	if err == nil || errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("error = %v, want an error that is not ErrNotEnrolled", err)
	}
	if !strings.Contains(err.Error(), identityFileName) {
		t.Errorf("error = %v, must name %s", err, identityFileName)
	}
}

func TestIdentityAccessors(t *testing.T) {
	issuer := newTestIssuer(t)
	now := time.Now()
	identity := writeTestIdentity(t, t.TempDir(), issuer, "server-1", now.Add(-time.Hour), now.Add(24*time.Hour))

	if identity.ServerID() != "server-1" {
		t.Errorf("server id = %q", identity.ServerID())
	}
	if identity.NotAfter().IsZero() {
		t.Error("not after is zero")
	}
	if len(identity.CAPEM()) == 0 {
		t.Error("the CA certificate is empty")
	}
	if identity.CAPool() == nil {
		t.Error("the CA pool is nil")
	}

	config := identity.TLSConfig("core.example.com")
	if config.MinVersion != tls.VersionTLS13 {
		t.Errorf("min version = %x, want TLS 1.3", config.MinVersion)
	}
	if config.ServerName != "core.example.com" {
		t.Errorf("server name = %q", config.ServerName)
	}

	certificate, err := config.GetClientCertificate(&tls.CertificateRequestInfo{})
	if err != nil {
		t.Fatalf("get client certificate: %v", err)
	}
	if len(certificate.Certificate) == 0 {
		t.Fatal("the TLS config does not return a certificate")
	}
}

func TestNeedsRenewal(t *testing.T) {
	issuer := newTestIssuer(t)
	now := time.Now()
	// A 90-day certificate needs renewal when less than a third is left.
	identity := writeTestIdentity(t, t.TempDir(), issuer, "server-1", now, now.Add(90*24*time.Hour))

	if identity.NeedsRenewal(now) {
		t.Error("a fresh certificate must not need renewal")
	}
	if identity.NeedsRenewal(now.Add(59 * 24 * time.Hour)) {
		t.Error("more than a third left must not need renewal")
	}
	if !identity.NeedsRenewal(now.Add(61 * 24 * time.Hour)) {
		t.Error("less than a third left must need renewal")
	}
	if !identity.NeedsRenewal(now.Add(100 * 24 * time.Hour)) {
		t.Error("an expired certificate must need renewal")
	}
}

func TestForgetAndCAFingerprint(t *testing.T) {
	dir := t.TempDir()
	issuer := newTestIssuer(t)
	now := time.Now()
	identity := writeTestIdentity(t, dir, issuer, "srv-1", now.Add(-time.Hour), now.Add(time.Hour))

	sum := sha256.Sum256(issuer.cert.Raw)
	if got, want := identity.CAFingerprint(), hex.EncodeToString(sum[:]); got != want {
		t.Errorf("CAFingerprint() = %q, want %q", got, want)
	}

	if err := Forget(dir); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if _, err := LoadIdentity(dir); !errors.Is(err, ErrNotEnrolled) {
		t.Errorf("LoadIdentity after Forget = %v, want ErrNotEnrolled", err)
	}
	if err := Forget(dir); err != nil {
		t.Errorf("Forget on an empty directory: %v", err)
	}
}

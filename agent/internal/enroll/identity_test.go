package enroll

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
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

// writeTestIdentity writes the identity files and loads the identity.
func writeTestIdentity(t *testing.T, dir string, issuer *testIssuer, serverID string, notBefore, notAfter time.Time) *Identity {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	certPEM, err := issuer.issue(serverID, &key.PublicKey, x509.ExtKeyUsageClientAuth, notBefore, notAfter)
	if err != nil {
		t.Fatalf("issue certificate: %v", err)
	}
	keyPEM, err := marshalKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	if err := os.MkdirAll(dir, dirMode); err != nil {
		t.Fatalf("create dir: %v", err)
	}
	files := map[string][]byte{keyFileName: keyPEM, certFileName: certPEM, caFileName: issuer.certPEM}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, keyMode); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	identity, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	return identity
}

func TestLoadIdentityNotEnrolled(t *testing.T) {
	if _, err := LoadIdentity(t.TempDir()); !errors.Is(err, ErrNotEnrolled) {
		t.Fatalf("error = %v, want ErrNotEnrolled", err)
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

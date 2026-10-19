package ca

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func newCA(t *testing.T) *CA {
	t.Helper()

	authority, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	return authority
}

func caPool(t *testing.T, authority *CA) *x509.CertPool {
	t.Helper()

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(authority.CertPEM()) {
		t.Fatal("could not add the CA certificate to the pool")
	}
	return pool
}

func parseCert(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()

	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("certificate is not PEM encoded")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

func requestCSR(t *testing.T, key any) []byte {
	t.Helper()

	template := &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "ignored-subject"},
		DNSNames: []string{"attacker.example.com"},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

func TestLoadOrCreateRoundTrip(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(first.Fingerprint()) != 64 {
		t.Errorf("fingerprint = %q, want 64 hex characters", first.Fingerprint())
	}
	if !first.cert.IsCA || !first.cert.MaxPathLenZero {
		t.Errorf("ca certificate is not a leaf CA: %+v", first.cert)
	}

	second, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if second.Fingerprint() != first.Fingerprint() {
		t.Errorf("fingerprint changed between loads: %q != %q", second.Fingerprint(), first.Fingerprint())
	}
	if !bytes.Equal(second.CertPEM(), first.CertPEM()) {
		t.Error("certificate changed between loads")
	}

	if runtime.GOOS == "windows" {
		return
	}
	for _, tt := range []struct {
		name string
		mode os.FileMode
	}{{"ca.key", keyMode}, {"ca.crt", certMode}, {".", dirMode}} {
		info, err := os.Stat(filepath.Join(dir, tt.name))
		if err != nil {
			t.Fatalf("stat %s: %v", tt.name, err)
		}
		if info.Mode().Perm() != tt.mode {
			t.Errorf("%s mode = %o, want %o", tt.name, info.Mode().Perm(), tt.mode)
		}
	}
}

func TestLoadOrCreateIncomplete(t *testing.T) {
	tests := []struct {
		name   string
		remove string
	}{
		{name: "missing key", remove: keyFileName},
		{name: "missing certificate", remove: certFileName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := LoadOrCreate(dir); err != nil {
				t.Fatalf("create: %v", err)
			}
			if err := os.Remove(filepath.Join(dir, tt.remove)); err != nil {
				t.Fatalf("remove %s: %v", tt.remove, err)
			}

			if _, err := LoadOrCreate(dir); err == nil {
				t.Fatal("expected an error for an incomplete CA")
			}
		})
	}
}

func TestLoadOrCreateKeyMismatch(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()
	if _, err := LoadOrCreate(firstDir); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := LoadOrCreate(secondDir); err != nil {
		t.Fatalf("create second: %v", err)
	}

	otherKey, err := os.ReadFile(filepath.Join(secondDir, keyFileName))
	if err != nil {
		t.Fatalf("read other key: %v", err)
	}
	if err := os.WriteFile(filepath.Join(firstDir, keyFileName), otherKey, keyMode); err != nil {
		t.Fatalf("write key: %v", err)
	}

	if _, err := LoadOrCreate(firstDir); err == nil {
		t.Fatal("expected an error when the key does not match the certificate")
	}
}

func TestSignClientCert(t *testing.T) {
	authority := newCA(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	notAfter := time.Now().Add(90 * 24 * time.Hour)
	certPEM, gotNotAfter, serial, err := authority.SignClientCert(requestCSR(t, key), "server-1", 90*24*time.Hour)
	if err != nil {
		t.Fatalf("SignClientCert: %v", err)
	}
	if serial == "" {
		t.Error("serial is empty")
	}
	if gotNotAfter.Before(notAfter.Add(-time.Minute)) {
		t.Errorf("notAfter = %s, want about %s", gotNotAfter, notAfter)
	}

	cert := parseCert(t, certPEM)
	if cert.Subject.CommonName != "server-1" {
		t.Errorf("subject = %q, want server-1", cert.Subject.CommonName)
	}
	if len(cert.DNSNames) != 0 || len(cert.IPAddresses) != 0 {
		t.Errorf("SANs were copied from the csr: %v %v", cert.DNSNames, cert.IPAddresses)
	}

	now := time.Now()
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:       caPool(t, authority),
		CurrentTime: now,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("verify client certificate: %v", err)
	}
	if _, err := cert.Verify(x509.VerifyOptions{
		Roots:       caPool(t, authority),
		CurrentTime: now,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err == nil {
		t.Error("a client certificate must not verify for server authentication")
	}
}

func TestSignClientCertRejectsRSAKey(t *testing.T) {
	authority := newCA(t)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	if _, _, _, err := authority.SignClientCert(requestCSR(t, key), "server-1", time.Hour); err == nil {
		t.Fatal("expected an error for a csr with an RSA key")
	}
}

func TestSignClientCertRejectsTamperedCSR(t *testing.T) {
	authority := newCA(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	csr := requestCSR(t, key)
	block, _ := pem.Decode(csr)
	if block == nil {
		t.Fatal("csr is not PEM encoded")
	}
	// Flip the last byte of the signature, which is the last element of the DER.
	tampered := append([]byte(nil), block.Bytes...)
	tampered[len(tampered)-1] ^= 0xff
	tamperedPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: tampered})

	if _, _, _, err := authority.SignClientCert(tamperedPEM, "server-1", time.Hour); err == nil {
		t.Fatal("expected an error for a csr with a broken signature")
	}
}

func TestSignClientCertRejectsGarbage(t *testing.T) {
	authority := newCA(t)

	if _, _, _, err := authority.SignClientCert([]byte("not a pem"), "server-1", time.Hour); err == nil {
		t.Fatal("expected an error for a csr that is not PEM")
	}
}

func TestSignServerCert(t *testing.T) {
	authority := newCA(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	certPEM, err := authority.SignServerCert([]string{"core.example.com", "192.0.2.10"}, &key.PublicKey, 365*24*time.Hour)
	if err != nil {
		t.Fatalf("SignServerCert: %v", err)
	}

	cert := parseCert(t, certPEM)
	if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "core.example.com" {
		t.Errorf("DNS names = %v", cert.DNSNames)
	}
	if len(cert.IPAddresses) != 1 || !cert.IPAddresses[0].Equal(net.ParseIP("192.0.2.10")) {
		t.Errorf("IP addresses = %v", cert.IPAddresses)
	}

	options := x509.VerifyOptions{Roots: caPool(t, authority), CurrentTime: time.Now()}
	for _, host := range []string{"core.example.com", "192.0.2.10"} {
		options.DNSName = host
		if _, err := cert.Verify(options); err != nil {
			t.Errorf("verify for %s: %v", host, err)
		}
	}
	options.DNSName = "other.example.com"
	if _, err := cert.Verify(options); err == nil {
		t.Error("the certificate must not verify for a host that is not in the SANs")
	}

	if _, err := authority.SignServerCert(nil, &key.PublicKey, time.Hour); err == nil {
		t.Error("expected an error without hosts")
	}
}

package proxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func TestStatusReportsCaddyCertificates(t *testing.T) {
	manager, root := newTestManager(t, fakePorts{}, &fakeStacks{}, &fakeAdmin{})
	if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	if _, err := manager.Apply(context.Background(), []*agentv1.ProxyRoute{
		{Hostname: "app.example.com", UpstreamService: "app", UpstreamPort: 3000},
	}); err != nil {
		t.Fatal(err)
	}
	writeCertificate(t, root, "app.example.com", expires)

	status := manager.Status(context.Background())
	if len(status.GetCertificates()) != 1 {
		t.Fatalf("certificates = %v", status.GetCertificates())
	}
	cert := status.GetCertificates()[0]
	if cert.GetHostname() != "app.example.com" || cert.GetState() != "valid" {
		t.Errorf("certificate = %v", cert)
	}
	if cert.GetExpiresAt().AsTime().Unix() != expires.Unix() {
		t.Errorf("expiry = %v, want %v", cert.GetExpiresAt().AsTime(), expires)
	}
}

func TestStatusReportsExpiredCertificates(t *testing.T) {
	manager, root := newTestManager(t, fakePorts{}, &fakeStacks{}, &fakeAdmin{})
	if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), []*agentv1.ProxyRoute{
		{Hostname: "old.example.com", UpstreamService: "app", UpstreamPort: 3000},
	}); err != nil {
		t.Fatal(err)
	}
	writeCertificate(t, root, "old.example.com", time.Now().Add(-time.Hour))

	status := manager.Status(context.Background())
	if len(status.GetCertificates()) != 1 || status.GetCertificates()[0].GetState() != "failed" {
		t.Errorf("certificates = %v", status.GetCertificates())
	}
}

func TestStatusReportsPendingCertificates(t *testing.T) {
	manager, _ := newTestManager(t, fakePorts{}, &fakeStacks{}, &fakeAdmin{})
	if _, err := manager.Install(context.Background(), agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Apply(context.Background(), []*agentv1.ProxyRoute{
		{Hostname: "new.example.com", UpstreamService: "app", UpstreamPort: 3000},
	}); err != nil {
		t.Fatal(err)
	}

	status := manager.Status(context.Background())
	if len(status.GetCertificates()) != 1 || status.GetCertificates()[0].GetState() != "pending" {
		t.Errorf("certificates = %v", status.GetCertificates())
	}
	if status.GetCertificates()[0].GetExpiresAt() != nil {
		t.Errorf("a pending certificate has an expiry: %v", status.GetCertificates()[0])
	}
}

func writeCertificate(t *testing.T, root, hostname string, notAfter time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: hostname},
		DNSNames:              []string{hostname},
		NotBefore:             notAfter.Add(-48 * time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	dir := filepath.Join(root, "data", "caddy", "certificates", "test")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, hostname+".crt"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

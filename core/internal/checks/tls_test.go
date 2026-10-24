package checks

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

func TestTLSChecksWithExpiringCertificate(t *testing.T) {
	now := time.Now()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(72 * time.Hour),
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
	)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
	server.StartTLS()
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	addr := net.JoinHostPort("localhost", port)
	roots := x509.NewCertPool()
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(leaf)
	runner := New(Options{Store: &testStore{private: true}, RootCAs: roots})
	for _, check := range []store.AlertCheck{
		{ID: "tls-trusted", Kind: "tls", Target: addr},
		{ID: "https-trusted", Kind: "http", Target: "https://" + addr, Config: json.RawMessage(`{}`)},
	} {
		result := runner.RunOnce(context.Background(), check)
		if !result.OK || result.CertNotAfter == nil || absDuration(result.CertNotAfter.Sub(now.Add(72*time.Hour))) > time.Minute {
			t.Errorf("trusted %s result=%+v", check.Kind, result)
		}
	}
	untrusted := New(Options{Store: &testStore{private: true}})
	for _, check := range []store.AlertCheck{
		{ID: "tls-untrusted", Kind: "tls", Target: addr},
		{ID: "https-untrusted", Kind: "http", Target: "https://" + addr, Config: json.RawMessage(`{}`)},
	} {
		result := untrusted.RunOnce(context.Background(), check)
		if result.OK || result.Error != "tls: certificate verification failed" || result.CertNotAfter == nil || absDuration(result.CertNotAfter.Sub(now.Add(72*time.Hour))) > time.Minute {
			t.Errorf("untrusted %s result=%+v", check.Kind, result)
		}
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func TestPrivateTargetsBlockedForAllKinds(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	addr := server.Listener.Addr().String()
	runner := New(Options{Store: &testStore{private: false}})
	for _, check := range []store.AlertCheck{
		{ID: "http", Kind: "http", Target: "http://" + addr, Config: json.RawMessage(`{}`)},
		{ID: "tcp", Kind: "tcp", Target: addr},
		{ID: "tls", Kind: "tls", Target: addr},
	} {
		result := runner.RunOnce(context.Background(), check)
		if result.OK || result.Error != "blocked address" {
			t.Errorf("%s result=%+v", check.Kind, result)
		}
	}
}

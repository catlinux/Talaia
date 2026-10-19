package enroll

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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
	"net/url"
	"os"
	"strings"
	"time"
)

// ErrFingerprintMismatch means the CA served by the core is not the one the
// installer pinned. It is permanent: trusting another certificate is not a
// legitimate recovery.
var ErrFingerprintMismatch = errors.New("the core CA fingerprint does not match")

// ErrInvalidToken means the one-time enrollment token is unknown, used or
// expired. It is permanent.
var ErrInvalidToken = errors.New("invalid enrollment token")

const (
	// requestTimeout bounds each HTTP request of the enrollment.
	requestTimeout = 30 * time.Second
	// maxResponseBytes limits the responses of the core.
	maxResponseBytes = 64 << 10
)

// Options are the inputs of Enroll.
type Options struct {
	CoreURL       string
	CAFingerprint string
	Token         string
	Hostname      string
	CertDir       string
	// TokenFile is removed after a successful enrollment.
	TokenFile string
	// HTTPClientFactory returns the client of the enrollment request. It
	// defaults to a client that verifies the core against the fetched CA; the
	// tests use it.
	HTTPClientFactory func(*tls.Config) *http.Client
}

// IsPermanent reports whether an enrollment error must not be retried.
func IsPermanent(err error) bool {
	return errors.Is(err, ErrFingerprintMismatch) || errors.Is(err, ErrInvalidToken)
}

// Enroll registers the agent with the core and stores its identity in CertDir.
func Enroll(ctx context.Context, options Options) (*Identity, error) {
	base, err := parseCoreURL(options.CoreURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(options.CertDir) == "" {
		return nil, errors.New("cert dir is required")
	}

	// 1. Fetch the CA certificate and compare it with the pinned fingerprint.
	caPEM, caCert, err := fetchCA(ctx, base, options.CAFingerprint)
	if err != nil {
		return nil, err
	}

	// 2. Generate the key and the certificate request.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate key: %w", err)
	}
	csrPEM, err := createCSR(key, options.Hostname)
	if err != nil {
		return nil, err
	}

	// 3. Enroll over TLS verified against the fetched CA.
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: base.Hostname(),
		RootCAs:    poolWith(caCert),
	}
	client := clientFactory(options)(tlsConfig)

	payload, err := json.Marshal(enrollRequest{
		Token:    options.Token,
		CSRPEM:   string(csrPEM),
		Hostname: options.Hostname,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal enrollment request: %w", err)
	}

	status, body, err := post(ctx, client, base, "/enroll", payload)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, ErrInvalidToken
	default:
		return nil, fmt.Errorf("the core answered %d to the enrollment request", status)
	}

	var response enrollResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse enrollment response: %w", err)
	}

	// 4. The certificate must belong to the CA, to our key and to the server id.
	leaf, err := verifyIssuedCertificate(response.CertPEM, caCert, &key.PublicKey, response.ServerID)
	if err != nil {
		return nil, err
	}

	// 5. Persist the identity.
	keyPEM, err := marshalKey(key)
	if err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair([]byte(response.CertPEM), keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load the issued certificate: %w", err)
	}
	pair.Leaf = leaf

	identity := &Identity{
		dir:      options.CertDir,
		cert:     pair,
		caPEM:    caPEM,
		serverID: leaf.Subject.CommonName,
		notAfter: leaf.NotAfter,
	}
	if err := identity.saveAll(keyPEM, []byte(response.CertPEM), caPEM); err != nil {
		return nil, err
	}

	// 6. Only now the token is spent: remove it so it is never reused.
	if options.TokenFile != "" {
		if err := os.Remove(options.TokenFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove the enrollment token file: %w", err)
		}
	}

	return identity, nil
}

// Renew requests a new certificate for the identity and replaces the key and
// the certificate atomically, with a new key every time.
func Renew(ctx context.Context, identity *Identity, coreURL string) error {
	base, err := parseCoreURL(coreURL)
	if err != nil {
		return err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	csrPEM, err := createCSR(key, identity.ServerID())
	if err != nil {
		return err
	}

	client := &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			TLSClientConfig:   identity.TLSConfig(base.Hostname()),
			ForceAttemptHTTP2: true,
		},
	}

	payload, err := json.Marshal(renewRequest{CSRPEM: string(csrPEM)})
	if err != nil {
		return fmt.Errorf("marshal renewal request: %w", err)
	}

	status, body, err := post(ctx, client, base, "/renew", payload)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("the core answered %d to the renewal request", status)
	}

	var response renewResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("parse renewal response: %w", err)
	}

	caCert, err := identity.caCertificate()
	if err != nil {
		return err
	}
	leaf, err := verifyIssuedCertificate(response.CertPEM, caCert, &key.PublicKey, identity.ServerID())
	if err != nil {
		return err
	}

	keyPEM, err := marshalKey(key)
	if err != nil {
		return err
	}
	pair, err := tls.X509KeyPair([]byte(response.CertPEM), keyPEM)
	if err != nil {
		return fmt.Errorf("load the renewed certificate: %w", err)
	}
	pair.Leaf = leaf

	if err := identity.save(keyPEM, []byte(response.CertPEM)); err != nil {
		return err
	}
	identity.update(pair, leaf)
	return nil
}

type enrollRequest struct {
	Token    string `json:"token"`
	CSRPEM   string `json:"csr_pem"`
	Hostname string `json:"hostname"`
}

type enrollResponse struct {
	ServerID string `json:"server_id"`
	CertPEM  string `json:"cert_pem"`
	CAPEM    string `json:"ca_pem"`
}

type renewRequest struct {
	CSRPEM string `json:"csr_pem"`
}

type renewResponse struct {
	CertPEM string `json:"cert_pem"`
}

// fetchCA downloads /ca.pem and checks it against the pinned fingerprint.
func fetchCA(ctx context.Context, base *url.URL, expected string) ([]byte, *x509.Certificate, error) {
	expectedBytes, err := hex.DecodeString(strings.TrimSpace(expected))
	if err != nil || len(expectedBytes) != sha256.Size {
		return nil, nil, fmt.Errorf("%w: invalid fingerprint %q", ErrFingerprintMismatch, expected)
	}

	// This is the only request that skips certificate verification: the
	// fingerprint is the trust anchor, and it is checked below with a
	// constant-time comparison.
	client := &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: true, // verified by fingerprint below
		}},
	}

	status, body, err := get(ctx, client, base, "/ca.pem")
	if err != nil {
		return nil, nil, err
	}
	if status != http.StatusOK {
		return nil, nil, fmt.Errorf("the core answered %d to /ca.pem", status)
	}

	block, _ := pem.Decode(body)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, nil, errors.New("the core did not return a PEM certificate in /ca.pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse the core CA certificate: %w", err)
	}
	if !cert.IsCA {
		return nil, nil, errors.New("the certificate served by the core is not a CA")
	}

	sum := sha256.Sum256(cert.Raw)
	if subtle.ConstantTimeCompare(sum[:], expectedBytes) != 1 {
		return nil, nil, ErrFingerprintMismatch
	}

	return body, cert, nil
}

// verifyIssuedCertificate checks that certPEM is a client certificate of caCert
// for publicKey and with the given server id.
func verifyIssuedCertificate(certPEM string, caCert *x509.Certificate, publicKey *ecdsa.PublicKey, serverID string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("the core returned a certificate that is not PEM encoded")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse the issued certificate: %w", err)
	}

	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     poolWith(caCert),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return nil, fmt.Errorf("the issued certificate does not verify against the core CA: %w", err)
	}

	issued, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || !issued.Equal(publicKey) {
		return nil, errors.New("the issued certificate does not match the generated key")
	}

	if serverID == "" || leaf.Subject.CommonName != serverID {
		return nil, fmt.Errorf("the issued certificate has CN %q, want the server id %q", leaf.Subject.CommonName, serverID)
	}

	return leaf, nil
}

func (i *Identity) caCertificate() (*x509.Certificate, error) {
	block, _ := pem.Decode(i.caPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("ca.crt does not contain a PEM certificate")
	}
	return x509.ParseCertificate(block.Bytes)
}

func createCSR(key *ecdsa.PrivateKey, hostname string) ([]byte, error) {
	template := &x509.CertificateRequest{Subject: pkix.Name{CommonName: hostname}}
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		return nil, fmt.Errorf("create csr: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), nil
}

func marshalKey(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

func poolWith(cert *x509.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return pool
}

func clientFactory(options Options) func(*tls.Config) *http.Client {
	if options.HTTPClientFactory != nil {
		return options.HTTPClientFactory
	}
	return func(config *tls.Config) *http.Client {
		return &http.Client{
			Timeout:   requestTimeout,
			Transport: &http.Transport{TLSClientConfig: config, ForceAttemptHTTP2: true},
		}
	}
}

func parseCoreURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("core_url is required")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("core_url is not a valid URL: %w", err)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("core_url must be an absolute URL, got %q", raw)
	}
	return parsed, nil
}

// endpoint builds the URL of an endpoint, dropping any path in core_url.
func endpoint(base *url.URL, path string) string {
	reference := *base
	reference.Path = path
	reference.RawQuery = ""
	return reference.String()
}

func get(ctx context.Context, client *http.Client, base *url.URL, path string) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint(base, path), nil)
	if err != nil {
		return 0, nil, fmt.Errorf("create the request of %s: %w", path, err)
	}

	return do(client, request, path)
}

func post(ctx context.Context, client *http.Client, base *url.URL, path string, payload []byte) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint(base, path), bytes.NewReader(payload))
	if err != nil {
		return 0, nil, fmt.Errorf("create the request of %s: %w", path, err)
	}
	request.Header.Set("Content-Type", "application/json")

	return do(client, request, path)
}

func do(client *http.Client, request *http.Request, path string) (int, []byte, error) {
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("request %s: %w", path, err)
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("read the response of %s: %w", path, err)
	}
	return response.StatusCode, body, nil
}

// Package ca manages the internal certificate authority of talaia-core: it
// creates the root certificate on first use, signs the certificates of the core
// server and of the enrolled agents, and reports the CA fingerprint that the
// install command pins on the agent.
package ca

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// certFileName and keyFileName are the files of the CA inside its directory.
	certFileName = "ca.crt"
	keyFileName  = "ca.key"

	// caValidity is how long the root certificate is valid.
	caValidity = 10 * 365 * 24 * time.Hour
	// caCommonName is the subject of the root certificate.
	caCommonName = "Talaia internal CA"

	dirMode     = 0o700
	certMode    = 0o644
	keyMode     = 0o600
	serialBytes = 128
)

// CA is the internal certificate authority.
type CA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
}

// LoadOrCreate loads the CA from dir, creating the directory (0700) and an
// ECDSA P-256 root certificate on first use. It fails when only one of the two
// files exists or when the key does not match the certificate.
func LoadOrCreate(dir string) (*CA, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("ca directory is required")
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("create ca directory %s: %w", dir, err)
	}
	// MkdirAll keeps the mode of an existing directory: tighten it, the
	// directory holds the CA private key.
	if err := os.Chmod(dir, dirMode); err != nil {
		return nil, fmt.Errorf("set mode of ca directory %s: %w", dir, err)
	}

	certPath := filepath.Join(dir, certFileName)
	keyPath := filepath.Join(dir, keyFileName)

	certPEM, certErr := os.ReadFile(certPath)
	keyPEM, keyErr := os.ReadFile(keyPath)

	certMissing := errors.Is(certErr, os.ErrNotExist)
	keyMissing := errors.Is(keyErr, os.ErrNotExist)

	switch {
	case certErr == nil && keyErr == nil:
		return load(certPEM, keyPEM)
	case certMissing && keyMissing:
		return create(certPath, keyPath)
	default:
		if certErr != nil && !certMissing {
			return nil, fmt.Errorf("read %s: %w", certPath, certErr)
		}
		if keyErr != nil && !keyMissing {
			return nil, fmt.Errorf("read %s: %w", keyPath, keyErr)
		}
		return nil, fmt.Errorf("the CA in %s is incomplete: %s and %s must both exist", dir, certFileName, keyFileName)
	}
}

// Fingerprint returns the hex SHA-256 of the DER certificate. It is the value
// the agent pins to avoid trusting the first connection blindly.
func (c *CA) Fingerprint() string {
	sum := sha256.Sum256(c.cert.Raw)
	return hex.EncodeToString(sum[:])
}

// CertPEM returns the PEM of the CA certificate.
func (c *CA) CertPEM() []byte {
	return c.certPEM
}

// Pool returns a certificate pool holding the CA certificate, for TLS.
func (c *CA) Pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(c.cert)
	return pool
}

// ValidateCSR checks that the bytes are a PEM certificate request with a valid
// signature and an ECDSA P-256 public key.
func ValidateCSR(csrPEM []byte) error {
	_, err := parseCSR(csrPEM)
	return err
}

// parseCSR decodes and validates a certificate request.
func parseCSR(csrPEM []byte) (*x509.CertificateRequest, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, errors.New("csr is not PEM encoded")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse csr: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("csr signature: %w", err)
	}

	publicKey, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || publicKey.Curve != elliptic.P256() {
		return nil, errors.New("the csr must contain an ECDSA P-256 public key")
	}
	return csr, nil
}

// SignServerCert signs the TLS certificate of the core for the given hosts
// (DNS names or IP addresses) and public key.
func (c *CA) SignServerCert(hosts []string, pub crypto.PublicKey, validity time.Duration) ([]byte, error) {
	if validity <= 0 {
		return nil, errors.New("validity must be positive")
	}

	dnsNames, ips := splitHosts(hosts)
	if len(dnsNames) == 0 && len(ips) == 0 {
		return nil, errors.New("at least one host is required")
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	var commonName string
	if len(dnsNames) > 0 {
		commonName = dnsNames[0]
	} else {
		commonName = ips[0].String()
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
	}

	return c.sign(template, pub)
}

// SignClientCert signs the certificate of a server from its CSR. The subject
// and the SANs of the CSR are ignored: the identity is serverID. The CSR must
// carry an ECDSA P-256 public key and a valid signature.
func (c *CA) SignClientCert(csrPEM []byte, serverID string, validity time.Duration) ([]byte, time.Time, string, error) {
	if strings.TrimSpace(serverID) == "" {
		return nil, time.Time{}, "", errors.New("server id is required")
	}
	if validity <= 0 {
		return nil, time.Time{}, "", errors.New("validity must be positive")
	}

	csr, err := parseCSR(csrPEM)
	if err != nil {
		return nil, time.Time{}, "", err
	}

	publicKey, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, time.Time{}, "", errors.New("the csr must contain an ECDSA P-256 public key")
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, time.Time{}, "", err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: serverID},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certPEM, err := c.sign(template, publicKey)
	if err != nil {
		return nil, time.Time{}, "", err
	}

	return certPEM, now.Add(validity), serial.Text(16), nil
}

func (c *CA) sign(template *x509.Certificate, pub crypto.PublicKey) ([]byte, error) {
	der, err := x509.CreateCertificate(rand.Reader, template, c.cert, pub, c.key)
	if err != nil {
		return nil, fmt.Errorf("sign certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// create generates the root certificate and writes ca.crt and ca.key.
func create(certPath, keyPath string) (*CA, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ca key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: caCommonName},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(caValidity),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create ca certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("parse ca certificate: %w", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal ca key: %w", err)
	}

	if err := writeFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), keyMode); err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := writeFile(certPath, certPEM, certMode); err != nil {
		return nil, err
	}

	return &CA{cert: cert, key: key, certPEM: certPEM}, nil
}

// load parses an existing ca.crt and ca.key and checks that they belong
// together.
func load(certPEM, keyPEM []byte) (*CA, error) {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil || certBlock.Type != "CERTIFICATE" {
		return nil, errors.New("ca.crt does not contain a PEM certificate")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse ca certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("ca.key does not contain a PEM private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse ca key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("ca.key is not an ECDSA private key")
	}

	if !samePublicKey(&key.PublicKey, cert.PublicKey) {
		return nil, errors.New("ca.key does not match ca.crt")
	}

	return &CA{cert: cert, key: key, certPEM: certPEM}, nil
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	// os.WriteFile does not change the mode of an existing file.
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("set mode of %s: %w", path, err)
	}
	return nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), serialBytes)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	return serial, nil
}

// splitHosts separates the configured hosts into DNS names and IP addresses.
func splitHosts(hosts []string) (dnsNames []string, ips []net.IP) {
	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if ip := net.ParseIP(host); ip != nil {
			ips = append(ips, ip)
			continue
		}
		dnsNames = append(dnsNames, host)
	}
	return dnsNames, ips
}

func samePublicKey(a *ecdsa.PublicKey, b crypto.PublicKey) bool {
	other, ok := b.(*ecdsa.PublicKey)
	if !ok {
		return false
	}
	return a.Equal(other)
}

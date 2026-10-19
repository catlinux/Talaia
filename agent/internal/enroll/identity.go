// Package enroll implements the enrollment of the agent with the core and the
// storage of its TLS identity.
package enroll

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrNotEnrolled is returned when the identity files are not there yet.
var ErrNotEnrolled = errors.New("not enrolled")

const (
	// keyFileName, certFileName and caFileName are the identity files.
	keyFileName  = "agent.key"
	certFileName = "agent.crt"
	caFileName   = "ca.crt"

	dirMode  os.FileMode = 0o700
	keyMode  os.FileMode = 0o600
	certMode os.FileMode = 0o644
)

// Identity is the TLS identity of the agent: its key, its client certificate
// and the CA certificate of the core.
type Identity struct {
	dir string

	mu       sync.RWMutex
	cert     tls.Certificate
	caPEM    []byte
	serverID string
	notAfter time.Time
}

// LoadIdentity reads the identity from dir. It returns ErrNotEnrolled when the
// files are missing.
func LoadIdentity(dir string) (*Identity, error) {
	keyPEM, err := os.ReadFile(filepath.Join(dir, keyFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("read agent key: %w", err)
	}

	certPEM, err := os.ReadFile(filepath.Join(dir, certFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("read agent certificate: %w", err)
	}

	caPEM, err := os.ReadFile(filepath.Join(dir, caFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("read ca certificate: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("%s does not contain a certificate", filepath.Join(dir, caFileName))
	}

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("load agent certificate: %w", err)
	}
	leaf, err := leafOf(pair)
	if err != nil {
		return nil, err
	}

	return &Identity{
		dir:      dir,
		cert:     pair,
		caPEM:    caPEM,
		serverID: leaf.Subject.CommonName,
		notAfter: leaf.NotAfter,
	}, nil
}

// ServerID returns the server id of the identity, the common name of the
// certificate.
func (i *Identity) ServerID() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.serverID
}

// NotAfter returns the expiry of the current certificate.
func (i *Identity) NotAfter() time.Time {
	i.mu.RLock()
	defer i.mu.RUnlock()
	return i.notAfter
}

// NeedsRenewal reports whether less than a third of the certificate lifetime
// remains at now.
func (i *Identity) NeedsRenewal(now time.Time) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()

	leaf := i.cert.Leaf
	if leaf == nil {
		return true
	}
	lifetime := leaf.NotAfter.Sub(leaf.NotBefore)
	return leaf.NotAfter.Sub(now) < lifetime/3
}

// TLSConfig returns the TLS configuration for the core. The client certificate
// is read on every handshake, so a renewal takes effect on the next connection
// without restarting the agent.
func (i *Identity) TLSConfig(serverName string) *tls.Config {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(i.caPEM)

	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: serverName,
		RootCAs:    pool,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			i.mu.RLock()
			defer i.mu.RUnlock()
			certificate := i.cert
			return &certificate, nil
		},
	}
}

// CAPool returns a pool with the CA certificate of the core.
func (i *Identity) CAPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(i.caPEM)
	return pool
}

// CAPEM returns the CA certificate of the core.
func (i *Identity) CAPEM() []byte {
	return i.caPEM
}

// update replaces the in-memory certificate after a renewal.
func (i *Identity) update(pair tls.Certificate, leaf *x509.Certificate) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.cert = pair
	i.serverID = leaf.Subject.CommonName
	i.notAfter = leaf.NotAfter
}

// save writes the key and the certificate atomically.
func (i *Identity) save(keyPEM, certPEM []byte) error {
	if err := ensureDir(i.dir); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(i.dir, keyFileName), keyPEM, keyMode); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(i.dir, certFileName), certPEM, certMode)
}

// saveAll writes the key, the certificate and the CA certificate.
func (i *Identity) saveAll(keyPEM, certPEM, caPEM []byte) error {
	if err := ensureDir(i.dir); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(i.dir, caFileName), caPEM, certMode); err != nil {
		return err
	}
	return i.save(keyPEM, certPEM)
}

func leafOf(pair tls.Certificate) (*x509.Certificate, error) {
	if pair.Leaf != nil {
		return pair.Leaf, nil
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("the certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse agent certificate: %w", err)
	}
	return leaf, nil
}

func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create cert directory %s: %w", dir, err)
	}
	// MkdirAll keeps the mode of an existing directory: tighten it, the
	// directory holds the agent private key.
	if err := os.Chmod(dir, dirMode); err != nil {
		return fmt.Errorf("set mode of cert directory %s: %w", dir, err)
	}
	return nil
}

// writeFileAtomic writes data through a temporary file in the same directory,
// so a crash never leaves a half-written file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()

	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set mode of %s: %w", name, err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}

	return nil
}

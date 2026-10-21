// Package enroll implements the enrollment of the agent with the core and the
// storage of its TLS identity.
package enroll

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ErrNotEnrolled is returned when the identity is not there yet.
var ErrNotEnrolled = errors.New("not enrolled")

const (
	// identityFileName holds the private key block followed by the certificate
	// block. One file means one atomic rename, so a crash can never leave a key
	// and a certificate that do not match.
	identityFileName = "agent.pem"
	// caFileName holds the CA certificate of the core, written at enrollment.
	caFileName = "ca.crt"
	// legacyKeyFileName and legacyCertFileName are the separate files of the
	// first layout. They are only read to migrate them into identityFileName.
	legacyKeyFileName  = "agent.key"
	legacyCertFileName = "agent.crt"

	dirMode      os.FileMode = 0o700
	identityMode os.FileMode = 0o600
	caMode       os.FileMode = 0o644
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
// identity is not there yet. A file that does not parse, or whose key does not
// match its certificate, is an error naming the file: it is a broken identity,
// not a missing one, and re-enrolling is not the answer.
func LoadIdentity(dir string) (*Identity, error) {
	caPath := filepath.Join(dir, caFileName)
	caPEM, err := os.ReadFile(caPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", caPath, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("%s does not contain a certificate", caPath)
	}

	identityPath := filepath.Join(dir, identityFileName)
	identityPEM, err := os.ReadFile(identityPath)
	switch {
	case err == nil:
		pair, err := keyPair(identityPath, identityPEM, identityPEM)
		if err != nil {
			return nil, err
		}
		return newIdentity(dir, pair, caPEM), nil

	case errors.Is(err, os.ErrNotExist):
		return loadLegacyIdentity(dir, caPEM)

	default:
		return nil, fmt.Errorf("read %s: %w", identityPath, err)
	}
}

// loadLegacyIdentity loads an identity stored as separate files and migrates it
// to the single identity file. A pair that does not load correctly is left
// untouched.
func loadLegacyIdentity(dir string, caPEM []byte) (*Identity, error) {
	keyPath := filepath.Join(dir, legacyKeyFileName)
	certPath := filepath.Join(dir, legacyCertFileName)

	keyPEM, keyErr := os.ReadFile(keyPath)
	certPEM, certErr := os.ReadFile(certPath)
	if errors.Is(keyErr, os.ErrNotExist) && errors.Is(certErr, os.ErrNotExist) {
		return nil, ErrNotEnrolled
	}
	if keyErr != nil {
		return nil, fmt.Errorf("read %s: %w", keyPath, keyErr)
	}
	if certErr != nil {
		return nil, fmt.Errorf("read %s: %w", certPath, certErr)
	}

	pair, err := keyPair(keyPath+" and "+certPath, certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	if err := migrateLegacy(dir, keyPEM, certPEM); err != nil {
		return nil, err
	}
	return newIdentity(dir, pair, caPEM), nil
}

// migrateLegacy writes the single identity file and then removes the files it
// replaces.
func migrateLegacy(dir string, keyPEM, certPEM []byte) error {
	if err := writeFileAtomic(filepath.Join(dir, identityFileName), joinIdentity(keyPEM, certPEM), identityMode); err != nil {
		return err
	}

	for _, name := range []string{legacyKeyFileName, legacyCertFileName} {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove the legacy %s: %w", path, err)
		}
	}
	return nil
}

// keyPair parses a certificate and its private key. Both arguments may be the
// same document, which is how the identity file stores them together.
func keyPair(name string, certPEM, keyPEM []byte) (tls.Certificate, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("%s: %w", name, err)
	}

	leaf, err := leafOf(pair)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("%s: %w", name, err)
	}
	pair.Leaf = leaf
	return pair, nil
}

// joinIdentity returns the private key block followed by the certificate block.
func joinIdentity(keyPEM, certPEM []byte) []byte {
	joined := make([]byte, 0, len(keyPEM)+len(certPEM)+1)
	joined = append(joined, bytes.TrimRight(keyPEM, "\r\n")...)
	joined = append(joined, '\n')
	return append(joined, certPEM...)
}

func newIdentity(dir string, pair tls.Certificate, caPEM []byte) *Identity {
	return &Identity{
		dir:      dir,
		cert:     pair,
		caPEM:    caPEM,
		serverID: pair.Leaf.Subject.CommonName,
		notAfter: pair.Leaf.NotAfter,
	}
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

// save writes the identity atomically: the key and the certificate go to the
// same file, so a single rename replaces both.
func (i *Identity) save(keyPEM, certPEM []byte) error {
	if err := ensureDir(i.dir); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(i.dir, identityFileName), joinIdentity(keyPEM, certPEM), identityMode)
}

// saveAll writes the identity and the CA certificate. The CA is only written
// when enrolling, it never changes.
func (i *Identity) saveAll(keyPEM, certPEM, caPEM []byte) error {
	if err := ensureDir(i.dir); err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(i.dir, caFileName), caPEM, caMode); err != nil {
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
		return nil, fmt.Errorf("parse certificate: %w", err)
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

// CAFingerprint returns the SHA-256 of the stored CA certificate, as lower-case
// hexadecimal, or "" when the CA cannot be read.
func (i *Identity) CAFingerprint() string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	ca, err := i.caCertificate()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(ca.Raw)
	return hex.EncodeToString(sum[:])
}

// Forget deletes the stored identity files in dir, so the next start enrolls
// again. It is for an agent that is pointed at another core with a new token.
func Forget(dir string) error {
	for _, name := range []string{identityFileName, caFileName, legacyKeyFileName, legacyCertFileName} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", name, err)
		}
	}
	return nil
}

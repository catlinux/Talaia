// Package acme obtains and renews the panel certificate over ACME DNS-01, so
// browsers trust it without installing Talaia's own CA.
package acme

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/ovh"
	"github.com/go-acme/lego/v4/registration"
)

const (
	// DefaultDirectoryURL is the Let's Encrypt production directory.
	DefaultDirectoryURL = lego.LEDirectoryProduction
	// RenewBefore is how long before its expiry a certificate is renewed.
	RenewBefore = 30 * 24 * time.Hour
	// CheckInterval is how often the manager checks the certificate.
	CheckInterval = 12 * time.Hour

	defaultRetry    = time.Minute
	maxRetry        = time.Hour
	dnsTimeout      = 2 * time.Minute
	directoryName   = "acme"
	accountKeyName  = "account.key"
	accountName     = "account.json"
	certificateName = "certificate.pem"
	privateKeyName  = "certificate.key"
	directoryMode   = 0o700
	privateFileMode = 0o600
)

// providerConstructors maps a configured provider name to its lego
// constructor. Adding a provider is one line here plus its lego import.
var providerConstructors = map[string]func() (challenge.Provider, error){
	"ovh": func() (challenge.Provider, error) { return ovh.NewDNSProvider() },
}

// Providers returns the supported DNS provider names, sorted.
func Providers() []string {
	names := make([]string, 0, len(providerConstructors))
	for name := range providerConstructors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// SupportedProvider reports whether the DNS provider is implemented.
func SupportedProvider(name string) bool {
	_, ok := providerConstructors[name]
	return ok
}

// DNSNames returns the values that can be covered by an ACME certificate:
// trimmed, lower case, without duplicates and without IP addresses, which no
// certificate authority issues certificates for.
func DNSNames(values []string) []string {
	seen := make(map[string]bool, len(values))
	names := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
		if name == "" || net.ParseIP(strings.Trim(name, "[]")) != nil {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// Options configures a Manager.
type Options struct {
	// Provider is the DNS provider that answers the DNS-01 challenge.
	Provider string
	// Email is the contact address of the ACME account.
	Email string
	// DirectoryURL is the ACME directory. Empty means Let's Encrypt production;
	// set it to the staging endpoint or a test server when needed.
	DirectoryURL string
	// Domains are the DNS names of the certificate. IP addresses are ignored.
	Domains []string
	// DataDir is the directory of the core; the account and the certificate
	// live under its acme/ subdirectory.
	DataDir string
	// Logger defaults to slog.Default().
	Logger *slog.Logger

	// issuer, now, newProvider and challengeOptions are hooks for the tests.
	issuer           Issuer
	now              func() time.Time
	newProvider      func() (challenge.Provider, error)
	challengeOptions []dns01.ChallengeOption
}

// Certificate is a PEM encoded certificate, with its chain, and its private
// key.
type Certificate struct {
	CertificatePEM []byte
	PrivateKeyPEM  []byte
}

// Issuer obtains a certificate for the given domains. lego implements it; the
// unit tests replace it so the manager logic runs without network.
type Issuer interface {
	Obtain(ctx context.Context, domains []string) (Certificate, error)
}

// Manager keeps the panel certificate current. Until ACME has produced one it
// hands out the certificate of Talaia's own CA, so the panel starts at once
// even when the ACME server or the DNS provider is slow.
type Manager struct {
	fallback      tls.Certificate
	issuer        Issuer
	logger        *slog.Logger
	now           func() time.Time
	domains       []string
	dataDir       string
	checkInterval time.Duration
	retryAfter    time.Duration
	maxRetry      time.Duration

	mu      sync.Mutex
	current atomic.Pointer[tls.Certificate]
}

// New creates a manager and loads the stored certificate when it is still
// valid. It does not contact the ACME server; Start does that in the
// background.
func New(opts Options, fallback tls.Certificate) (*Manager, error) {
	provider := strings.ToLower(strings.TrimSpace(opts.Provider))
	if !SupportedProvider(provider) {
		return nil, fmt.Errorf("unsupported ACME DNS provider %q (supported: %s)", opts.Provider, strings.Join(Providers(), ", "))
	}
	domains := DNSNames(opts.Domains)
	if len(domains) == 0 {
		return nil, errors.New("ACME needs at least one DNS name")
	}
	if strings.TrimSpace(opts.DataDir) == "" {
		return nil, errors.New("a data directory is required")
	}
	directoryURL := strings.TrimSpace(opts.DirectoryURL)
	if directoryURL == "" {
		directoryURL = DefaultDirectoryURL
	}
	parsed, err := url.Parse(directoryURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid ACME directory URL %q", directoryURL)
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.now
	if now == nil {
		now = time.Now
	}

	manager := &Manager{
		fallback:      fallback,
		logger:        logger,
		now:           now,
		domains:       domains,
		dataDir:       filepath.Join(opts.DataDir, directoryName),
		checkInterval: CheckInterval,
		retryAfter:    defaultRetry,
		maxRetry:      maxRetry,
	}
	if opts.issuer != nil {
		manager.issuer = opts.issuer
	} else {
		manager.issuer = &legoIssuer{
			provider:         provider,
			email:            opts.Email,
			directoryURL:     directoryURL,
			dataDir:          manager.dataDir,
			newProvider:      opts.newProvider,
			challengeOptions: opts.challengeOptions,
		}
	}

	if err := manager.loadStored(); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			logger.Warn("ignoring the stored ACME certificate", "error", err)
		}
	} else {
		logger.Info("using the stored ACME certificate", "not_after", manager.current.Load().Leaf.NotAfter, "domains", domains)
	}
	logger.Info("ACME enabled", "provider", provider, "directory", directoryURL, "domains", domains)
	return manager, nil
}

// Start loads or renews the certificate in the background and keeps checking
// it every 12 hours. It returns immediately and stops with ctx.
func (m *Manager) Start(ctx context.Context) {
	go m.run(ctx)
}

// GetCertificate is the tls.Config callback of the panel. It never returns
// nil: while ACME has no certificate it returns the one of the own CA, and so
// it does for a name the ACME certificate does not cover. Browsers send no
// server name when the panel is opened by IP address, which only the own CA
// certificate is valid for.
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	current := m.current.Load()
	if current == nil {
		return &m.fallback, nil
	}
	if hello != nil && (hello.ServerName == "" || current.Leaf.VerifyHostname(hello.ServerName) != nil) {
		return &m.fallback, nil
	}
	return current, nil
}

func (m *Manager) run(ctx context.Context) {
	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	backoff := m.retryAfter
	for {
		_, err := m.renew(ctx)
		if err != nil {
			m.logger.Warn("ACME certificate renewal failed; keeping the current certificate", "error", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < m.maxRetry {
				backoff = min(backoff*2, m.maxRetry)
			}
			continue
		}
		backoff = m.retryAfter

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// renew obtains a certificate when there is none yet or the current one
// expires within RenewBefore. It returns whether it obtained one.
func (m *Manager) renew(ctx context.Context) (bool, error) {
	if current := m.current.Load(); current != nil && !m.needsRenewal(current) {
		return false, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Another request may have renewed it while this one waited for the lock.
	if current := m.current.Load(); current != nil && !m.needsRenewal(current) {
		return false, nil
	}

	m.logger.Info("requesting an ACME certificate", "domains", m.domains)
	issued, err := m.issuer.Obtain(ctx, m.domains)
	if err != nil {
		return false, err
	}
	cert, err := parseCertificate(issued.CertificatePEM, issued.PrivateKeyPEM)
	if err != nil {
		return false, err
	}
	if err := m.save(issued); err != nil {
		// A valid certificate in memory is better than none: keep it and ask
		// again after a restart.
		m.logger.Warn("could not store the ACME certificate", "error", err)
	}
	m.current.Store(cert)
	m.logger.Info("ACME certificate ready", "not_after", cert.Leaf.NotAfter, "domains", m.domains)
	return true, nil
}

func (m *Manager) needsRenewal(cert *tls.Certificate) bool {
	return cert.Leaf == nil || !m.now().Add(RenewBefore).Before(cert.Leaf.NotAfter)
}

func (m *Manager) loadStored() error {
	certPEM, err := os.ReadFile(filepath.Join(m.dataDir, certificateName))
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(filepath.Join(m.dataDir, privateKeyName))
	if err != nil {
		return err
	}
	cert, err := parseCertificate(certPEM, keyPEM)
	if err != nil {
		return err
	}
	for _, domain := range m.domains {
		if err := cert.Leaf.VerifyHostname(domain); err != nil {
			return fmt.Errorf("the stored certificate is not valid for %s", domain)
		}
	}
	if !m.now().Before(cert.Leaf.NotAfter) {
		return fmt.Errorf("the stored certificate expired on %s", cert.Leaf.NotAfter.Format(time.RFC3339))
	}
	m.current.Store(cert)
	return nil
}

func (m *Manager) save(issued Certificate) error {
	if err := os.MkdirAll(m.dataDir, directoryMode); err != nil {
		return fmt.Errorf("create the ACME directory: %w", err)
	}
	if err := writeFile(filepath.Join(m.dataDir, certificateName), issued.CertificatePEM); err != nil {
		return fmt.Errorf("store the certificate: %w", err)
	}
	if err := writeFile(filepath.Join(m.dataDir, privateKeyName), issued.PrivateKeyPEM); err != nil {
		return fmt.Errorf("store the private key: %w", err)
	}
	return nil
}

func parseCertificate(certPEM, keyPEM []byte) (*tls.Certificate, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parse the certificate and its key: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("the certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("parse the leaf certificate: %w", err)
	}
	pair.Leaf = leaf
	return &pair, nil
}

// writeFile replaces path atomically with data, readable only by the owner.
func writeFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() { _ = os.Remove(name) }()
	if err := file.Chmod(privateFileMode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// legoIssuer is the only code that talks to lego. It keeps the account in
// memory, so the key and the registration are read and written once.
type legoIssuer struct {
	provider         string
	email            string
	directoryURL     string
	dataDir          string
	newProvider      func() (challenge.Provider, error)
	challengeOptions []dns01.ChallengeOption

	mu      sync.Mutex
	account *account
}

// Obtain registers the account when needed and requests a certificate. lego
// has no context support, so an in-flight request runs to completion.
func (i *legoIssuer) Obtain(ctx context.Context, domains []string) (Certificate, error) {
	if err := ctx.Err(); err != nil {
		return Certificate{}, err
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	acct, err := i.loadAccount()
	if err != nil {
		return Certificate{}, err
	}

	client, err := newClient(acct, i.directoryURL)
	if err != nil {
		return Certificate{}, err
	}
	provider, err := i.dnsProvider()
	if err != nil {
		return Certificate{}, err
	}
	options := append([]dns01.ChallengeOption{dns01.AddDNSTimeout(dnsTimeout)}, i.challengeOptions...)
	if err := client.Challenge.SetDNS01Provider(provider, options...); err != nil {
		return Certificate{}, fmt.Errorf("set the DNS-01 provider: %w", err)
	}

	if acct.registration == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return Certificate{}, fmt.Errorf("register the ACME account: %w", err)
		}
		acct.registration = reg
		if err := i.saveRegistration(reg); err != nil {
			return Certificate{}, err
		}
	}

	resource, err := client.Certificate.Obtain(certificate.ObtainRequest{Domains: domains, Bundle: true})
	if err != nil {
		return Certificate{}, fmt.Errorf("obtain the certificate: %w", err)
	}
	return Certificate{CertificatePEM: resource.Certificate, PrivateKeyPEM: resource.PrivateKey}, nil
}

func newClient(user registration.User, directoryURL string) (*lego.Client, error) {
	cfg := lego.NewConfig(user)
	cfg.CADirURL = directoryURL
	client, err := lego.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create the ACME client: %w", err)
	}
	return client, nil
}

func (i *legoIssuer) dnsProvider() (challenge.Provider, error) {
	if i.newProvider != nil {
		return i.newProvider()
	}
	constructor, ok := providerConstructors[i.provider]
	if !ok {
		return nil, fmt.Errorf("unsupported ACME DNS provider %q", i.provider)
	}
	provider, err := constructor()
	if err != nil {
		return nil, fmt.Errorf("create the %s DNS provider: %w", i.provider, err)
	}
	return provider, nil
}

func (i *legoIssuer) loadAccount() (*account, error) {
	if i.account != nil {
		return i.account, nil
	}
	if err := os.MkdirAll(i.dataDir, directoryMode); err != nil {
		return nil, fmt.Errorf("create the ACME directory: %w", err)
	}
	key, created, err := i.loadOrCreateAccountKey()
	if err != nil {
		return nil, err
	}
	acct := &account{email: i.email, key: key}
	if !created {
		reg, err := readRegistration(filepath.Join(i.dataDir, accountName))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		acct.registration = reg
	}
	i.account = acct
	return acct, nil
}

func (i *legoIssuer) loadOrCreateAccountKey() (crypto.PrivateKey, bool, error) {
	path := filepath.Join(i.dataDir, accountKeyName)
	data, err := os.ReadFile(path)
	if err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, false, fmt.Errorf("the ACME account key %s is not PEM encoded", path)
		}
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, false, fmt.Errorf("parse the ACME account key: %w", err)
		}
		return key, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, false, fmt.Errorf("generate the ACME account key: %w", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, false, fmt.Errorf("encode the ACME account key: %w", err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	if err := writeFile(path, encoded); err != nil {
		return nil, false, fmt.Errorf("store the ACME account key: %w", err)
	}
	return key, true, nil
}

func (i *legoIssuer) saveRegistration(reg *registration.Resource) error {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the ACME registration: %w", err)
	}
	if err := writeFile(filepath.Join(i.dataDir, accountName), data); err != nil {
		return fmt.Errorf("store the ACME registration: %w", err)
	}
	return nil
}

func readRegistration(path string) (*registration.Resource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var reg registration.Resource
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse the ACME registration: %w", err)
	}
	if reg.URI == "" {
		return nil, errors.New("the stored ACME registration has no URL")
	}
	return &reg, nil
}

// account is the registration.User lego expects.
type account struct {
	email        string
	key          crypto.PrivateKey
	registration *registration.Resource
}

func (a *account) GetEmail() string                        { return a.email }
func (a *account) GetRegistration() *registration.Resource { return a.registration }
func (a *account) GetPrivateKey() crypto.PrivateKey        { return a.key }

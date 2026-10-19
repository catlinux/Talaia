// Command talaia-core is the Talaia control plane server.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/catlinux/Talaia/core/internal/api"
	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/ca"
	"github.com/catlinux/Talaia/core/internal/config"
	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
)

// version is the version placeholder, overridden at build time.
var version = "dev"

const (
	shutdownTimeout = 10 * time.Second
	// serverCertValidity is how long the in-memory TLS certificate of the core
	// is valid.
	serverCertValidity = 365 * 24 * time.Hour
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches the subcommands and returns the exit code: 0 on success, 1 on
// a runtime error and 2 on a usage error.
func run(args []string, stdout, stderr io.Writer) int {
	command, rest := splitCommand(args)

	switch command {
	case "", "serve":
		if err := serve(); err != nil {
			slog.Error("talaia-core stopped", "error", err)
			return 1
		}
		return 0
	case "token":
		return runToken(rest, stdout, stderr)
	case "server":
		return runServer(rest, stdout, stderr)
	case "user":
		return runUser(rest, os.Stdin, stdout, stderr)
	case "session":
		return runSession(rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", command)
		printUsage(stderr)
		return 2
	}
}

// splitCommand returns the subcommand and its arguments. With no arguments the
// subcommand is empty, which means serve.
func splitCommand(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	return args[0], args[1:]
}

// panelOptions are the settings of the authentication API and the web panel,
// shared by the TLS and the insecure development modes.
type panelOptions struct {
	Box          *auth.SecretBox
	WebDir       string
	Origins      []string
	TLS          bool
	Limiter      *api.RateLimiter
	Now          func() time.Time
	LockDuration time.Duration
}

// newCoreRouter returns the handler of the health endpoint, the authentication
// API and the panel.
func newCoreRouter(st *store.Store, options panelOptions) http.Handler {
	return api.NewRouter(api.RouterDeps{
		DB:       st,
		WebDir:   options.WebDir,
		Security: api.SecurityConfig{TLS: options.TLS, Origins: options.Origins},
		Session:  api.SessionDeps{Store: st, Now: options.Now},
		Auth: api.AuthDeps{
			Store:        st,
			Box:          options.Box,
			Limiter:      options.Limiter,
			Now:          options.Now,
			LockDuration: options.LockDuration,
		},
	})
}

// newPlainRouter serves the health endpoint, the authentication API and the
// panel over plaintext HTTP/2, plus the agent stream. It is only used in
// insecure development mode, and it does not mount the enrollment endpoints.
func newPlainRouter(st *store.Store, options panelOptions) http.Handler {
	mux := http.NewServeMux()

	path, hubHandler := hub.NewInsecure().Handler()
	mux.Handle(path, hubHandler)
	mux.Handle("/", newCoreRouter(st, options))

	return mux
}

// newTLSRouter serves the health endpoint, the authentication API, the panel,
// the enrollment endpoints and the agent hub, which requires a client
// certificate.
func newTLSRouter(st *store.Store, authority *ca.CA, options panelOptions) http.Handler {
	return newTLSRouterWithHub(st, authority, hub.New(st), options)
}

// newTLSRouterWithHub is newTLSRouter with the hub injected, for the tests.
func newTLSRouterWithHub(st *store.Store, authority *ca.CA, h *hub.Hub, options panelOptions) http.Handler {
	enrollment := api.NewEnrollmentHandler(api.EnrollmentDeps{
		Store:   st,
		Signer:  authority,
		CA:      authority,
		Limiter: api.NewRateLimiter(),
	})

	mux := http.NewServeMux()

	path, hubHandler := h.Handler()
	mux.Handle(path, hubHandler)
	mux.Handle("/ca.pem", enrollment)
	mux.Handle("/enroll", enrollment)
	mux.Handle("/renew", enrollment)
	mux.Handle("/", newCoreRouter(st, options))

	return mux
}

// serverCertificate generates the in-memory ECDSA key and the server
// certificate of the core, valid for the configured hostnames.
func serverCertificate(authority *ca.CA, hosts []string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate server key: %w", err)
	}

	certPEM, err := authority.SignServerCert(hosts, &key.PublicKey, serverCertValidity)
	if err != nil {
		return tls.Certificate{}, err
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return tls.Certificate{}, errors.New("the signed server certificate is not PEM encoded")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("parse server certificate: %w", err)
	}

	return tls.Certificate{Certificate: [][]byte{leaf.Raw}, PrivateKey: key, Leaf: leaf}, nil
}

// newProtocols enables plaintext HTTP/2 next to HTTP/1.1, which the agent
// stream needs in insecure development mode.
func newProtocols() *http.Protocols {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return &protocols
}

// newTLSProtocols enables HTTP/1.1 and HTTP/2 over TLS.
func newTLSProtocols() *http.Protocols {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(true)
	return &protocols
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.Level()}))
	slog.SetDefault(logger)
	logger.Info("starting talaia-core", "version", version, "listen", cfg.Listen, "insecure_dev", cfg.InsecureDev)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	if err := st.Migrate(ctx); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		ReadHeaderTimeout: 5 * time.Second,
		// No ReadTimeout/WriteTimeout: they would cut the long-lived agent stream.
		// Per-request limits for the REST API come with its handlers.
		IdleTimeout: 60 * time.Second,
	}

	// The TOTP secrets are sealed with this key, created on first use.
	key, err := auth.LoadOrCreateKey(filepath.Join(cfg.DataDir, "secrets.key"))
	if err != nil {
		return fmt.Errorf("load the secrets key: %w", err)
	}
	box, err := auth.NewSecretBox(key)
	if err != nil {
		return fmt.Errorf("create the secret box: %w", err)
	}

	panel := panelOptions{
		Box:     box,
		WebDir:  cfg.WebDir,
		Origins: cfg.AllowedOrigins,
		TLS:     !cfg.InsecureDev,
		Limiter: api.NewAuthRateLimiter(),
	}

	tlsEnabled := !cfg.InsecureDev
	if tlsEnabled {
		if err := cfg.ValidateCertificateHosts(); err != nil {
			return err
		}

		authority, err := ca.LoadOrCreate(caDirectory(cfg))
		if err != nil {
			return err
		}
		certificate, err := serverCertificate(authority, cfg.PublicHostnames)
		if err != nil {
			return err
		}

		srv.Handler = newTLSRouter(st, authority, panel)
		srv.Protocols = newTLSProtocols()
		srv.TLSConfig = &tls.Config{
			MinVersion:   tls.VersionTLS13,
			ClientAuth:   tls.VerifyClientCertIfGiven,
			ClientCAs:    authority.Pool(),
			Certificates: []tls.Certificate{certificate},
			NextProtos:   []string{"h2", "http/1.1"},
		}
	} else {
		logger.Warn("insecure development mode: no TLS and no agent authentication")
		srv.Handler = newPlainRouter(st, panel)
		srv.Protocols = newProtocols()
	}

	serveErr := make(chan error, 1)
	go func() {
		if tlsEnabled {
			logger.Info("https server listening", "address", cfg.Listen)
			serveErr <- srv.ListenAndServeTLS("", "")
			return
		}
		logger.Info("http server listening (plaintext)", "address", cfg.Listen)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	return nil
}

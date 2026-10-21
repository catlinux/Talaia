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

	"github.com/catlinux/Talaia/core/internal/acme"
	"github.com/catlinux/Talaia/core/internal/api"
	"github.com/catlinux/Talaia/core/internal/auth"
	"github.com/catlinux/Talaia/core/internal/ca"
	"github.com/catlinux/Talaia/core/internal/config"
	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/install"
	"github.com/catlinux/Talaia/core/internal/jobs"
	"github.com/catlinux/Talaia/core/internal/store"
	"github.com/catlinux/Talaia/core/internal/terminal"
	"github.com/catlinux/Talaia/core/internal/updates"
)

// version is the version placeholder, overridden at build time.
var version = "dev"

// commit is the git commit placeholder, overridden at build time. An empty
// value means the build does not know its commit.
var commit = ""

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
	case "terminal":
		return runTerminal(rest, stdout, stderr)
	case "dns":
		return runDNS(rest, stderr)
	case "acme":
		return runACME(rest, stdout, stderr)
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
	Box     *auth.SecretBox
	WebDir  string
	DistDir string
	// Enrollment enables creating enrollment tokens from the panel.
	Enrollment *api.EnrollmentTokenDeps
	// Containers enables the container and job API.
	Containers     *api.ContainerDeps
	Terminal       *api.TerminalDeps
	System         *api.SystemDeps
	Origins        []string
	TLS            bool
	Limiter        *api.RateLimiter
	Now            func() time.Time
	LockDuration   time.Duration
	DeviceMFAHours int
}

// newCoreRouter returns the handler of the health endpoint, the authentication
// API, the protected API and the panel. agents is the hub the agents connect to,
// so the API sees their live data.
func newCoreRouter(st *store.Store, agents api.LiveAgents, options panelOptions) http.Handler {
	disconnector, _ := agents.(api.ServerDisconnector)
	return api.NewRouter(api.RouterDeps{
		DB:                 st,
		WebDir:             options.WebDir,
		Security:           api.SecurityConfig{TLS: options.TLS, Origins: options.Origins},
		Session:            api.SessionDeps{Store: st, Now: options.Now},
		Servers:            st,
		Agents:             agents,
		ServerRemover:      st,
		ServerDisconnector: disconnector,
		ServerAudit:        st,
		Enrollment:         options.Enrollment,
		Containers:         options.Containers,
		Terminal:           options.Terminal,
		System:             options.System,
		Auth: api.AuthDeps{
			Store:          st,
			Box:            options.Box,
			Limiter:        options.Limiter,
			Now:            options.Now,
			LockDuration:   options.LockDuration,
			DeviceMFAHours: options.DeviceMFAHours,
		},
	})
}

// newPlainRouter serves the health endpoint, the authentication API and the
// panel over plaintext HTTP/2, plus the agent stream. It is only used in
// insecure development mode, and it does not mount the enrollment endpoints.
func newPlainRouter(st *store.Store, h *hub.Hub, options panelOptions) http.Handler {
	mux := http.NewServeMux()

	path, hubHandler := h.Handler()
	mux.Handle(path, hubHandler)
	mux.Handle("/", newCoreRouter(st, h, options))

	return mux
}

// newPanelRouter serves the health endpoint, the authentication API, the
// protected API and the panel. It has no agent route.
func newPanelRouter(st *store.Store, h *hub.Hub, options panelOptions) http.Handler {
	return newCoreRouter(st, h, options)
}

// newAgentRouter serves what agents and the installer use: the agent stream,
// which requires a client certificate, the enrollment endpoints, the CA and the
// downloads. It has no panel or API route; anything else is a 404.
func newAgentRouter(st *store.Store, authority *ca.CA, h *hub.Hub, distDir string) http.Handler {
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

	dist := api.NewDistHandler(distDir)
	mux.Handle("/install.sh", dist)
	mux.Handle("/agent/", dist)

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
	logger.Info("starting talaia-core", "version", version, "commit", commit, "panel_listen", cfg.PanelListen, "agent_listen", cfg.AgentListen, "insecure_dev", cfg.InsecureDev)

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

	newServer := func(addr string) *http.Server {
		return &http.Server{
			Addr:              addr,
			ReadHeaderTimeout: 5 * time.Second,
			// No ReadTimeout/WriteTimeout: they would cut the long-lived agent stream.
			// Per-request limits for the REST API come with its handlers.
			IdleTimeout: 60 * time.Second,
		}
	}
	srv := newServer(cfg.PanelListen)
	// agentSrv only exists with TLS; the insecure mode serves everything on srv.
	var agentSrv *http.Server

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
		Box:            box,
		WebDir:         cfg.WebDir,
		DistDir:        cfg.DistDir,
		Origins:        cfg.AllowedOrigins,
		TLS:            !cfg.InsecureDev,
		Limiter:        api.NewAuthRateLimiter(),
		DeviceMFAHours: cfg.DeviceMFAHours,
	}

	var terminals *terminal.Manager
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

		panel.Enrollment = &api.EnrollmentTokenDeps{
			Tokens:        st,
			Audit:         st,
			CAFingerprint: authority.Fingerprint(),
			PublicURL:     cfg.AgentURL(),
			ScriptSHA256:  install.ScriptSHA256(cfg.DistDir),
		}
		agentHub := hub.New(st)
		agentHub.SetJobEvents(api.JobRecorder{Jobs: st})
		panel.Containers = &api.ContainerDeps{Agents: agentHub, Dispatcher: agentHub, Jobs: st, Audit: st}

		// Terminal sessions: the ones an earlier run left open are closed first.
		if err := terminal.CloseOrphans(context.Background(), st); err != nil {
			return fmt.Errorf("close the terminal sessions left open: %w", err)
		}
		terminals = terminal.New(st, agentHub, st, box, terminal.DefaultConfig())
		agentHub.SetTerminals(terminals)
		panel.Terminal = api.NewTerminalDeps(api.TerminalDeps{Agents: agentHub, Terminals: terminals, Sessions: st, Origins: cfg.AllowedOrigins})

		checker, err := updates.New(updates.Options{
			Repo:          cfg.UpdateRepo,
			APIURL:        cfg.UpdateAPIURL,
			CurrentCommit: commit,
			Version:       version,
			Interval:      cfg.UpdateCheckInterval,
			Logger:        logger,
		})
		if err != nil {
			return fmt.Errorf("configure update checks: %w", err)
		}
		checker.Start(ctx)
		panel.System = &api.SystemDeps{
			Version:   version,
			Commit:    commit,
			Checker:   checker,
			Requester: updates.NewRequester(cfg.HostDir),
			Audit:     st,
		}

		purgeCtx, stopPurge := context.WithCancel(context.Background())
		defer stopPurge()
		go terminal.PurgeLoop(purgeCtx, st, cfg.TerminalRecordingDays, 24*time.Hour)
		go jobs.PurgeLoop(purgeCtx, st, jobs.LogRetention, jobs.PurgeInterval)
		srv.Handler = newPanelRouter(st, agentHub, panel)
		srv.Protocols = newTLSProtocols()
		// Browsers are never asked for a client certificate. Until ACME has a
		// certificate ready, GetCertificate returns the one of the own CA.
		panelCertificate := func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return &certificate, nil
		}
		if cfg.ACMEProvider != "" {
			manager, err := acme.New(acme.Options{
				Provider:        cfg.ACMEProvider,
				Email:           cfg.ACMEEmail,
				DirectoryURL:    cfg.ACMEDirectoryURL,
				Domains:         cfg.ACMEDomains(),
				DataDir:         cfg.DataDir,
				ChallengeDir:    cfg.ACMEChallengeDir,
				DNSCheckAddress: cfg.ACMEDNSCheck,
				Logger:          logger,
			}, certificate)
			if err != nil {
				return fmt.Errorf("configure ACME: %w", err)
			}
			manager.Start(ctx)
			panelCertificate = manager.GetCertificate
		}
		srv.TLSConfig = &tls.Config{
			MinVersion:     tls.VersionTLS13,
			GetCertificate: panelCertificate,
			NextProtos:     []string{"h2", "http/1.1"},
		}

		// Agents always get the certificate of the own CA, so a certificate the
		// panel may use (a public one) never lets anyone impersonate the core. The
		// stream and /renew check the client certificate themselves; /enroll,
		// /ca.pem and the downloads must work before an agent has one.
		agentSrv = newServer(cfg.AgentListen)
		agentSrv.Handler = newAgentRouter(st, authority, agentHub, cfg.DistDir)
		agentSrv.Protocols = newTLSProtocols()
		agentSrv.TLSConfig = &tls.Config{
			MinVersion:   tls.VersionTLS13,
			ClientAuth:   tls.VerifyClientCertIfGiven,
			ClientCAs:    authority.Pool(),
			Certificates: []tls.Certificate{certificate},
			NextProtos:   []string{"h2", "http/1.1"},
		}
	} else {
		logger.Warn("insecure development mode: no TLS and no agent authentication")
		srv.Handler = newPlainRouter(st, hub.NewInsecure(), panel)
		srv.Protocols = newProtocols()
	}

	serveErr := make(chan error, 2)
	go func() {
		if tlsEnabled {
			logger.Info("https panel listening", "address", cfg.PanelListen)
			serveErr <- srv.ListenAndServeTLS("", "")
			return
		}
		logger.Info("http server listening (plaintext)", "address", cfg.PanelListen)
		serveErr <- srv.ListenAndServe()
	}()
	if agentSrv != nil {
		go func() {
			logger.Info("https agent endpoint listening", "address", cfg.AgentListen)
			serveErr <- agentSrv.ListenAndServeTLS("", "")
		}()
	}

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
	if terminals != nil {
		terminals.Shutdown(shutdownCtx)
	}
	if agentSrv != nil {
		if err := agentSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shut down the agent HTTP server: %w", err)
		}
	}
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	return nil
}

// Command talaia-core is the Talaia control plane server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/catlinux/Talaia/core/internal/api"
	"github.com/catlinux/Talaia/core/internal/config"
	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
)

// version is the version placeholder, overridden at build time.
var version = "dev"

const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("talaia-core stopped", "error", err)
		os.Exit(1)
	}
}

// newRouter mounts the agent hub next to the existing API router, so /healthz
// and the agent stream are served by the same listener.
func newRouter(st *store.Store) http.Handler {
	mux := http.NewServeMux()

	path, hubHandler := hub.New().Handler()
	mux.Handle(path, hubHandler)
	mux.Handle("/", api.NewHandler(st))

	return mux
}

// newProtocols enables plaintext HTTP/2 next to HTTP/1.1, which the agent
// stream needs until phase 1 adds mTLS.
func newProtocols() *http.Protocols {
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return &protocols
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.Level()}))
	slog.SetDefault(logger)
	logger.Info("starting talaia-core", "version", version, "listen", cfg.Listen)

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
		Addr: cfg.Listen,
		// Phase 0 runs the agent stream over plaintext HTTP/2 with no TLS
		// and no enrollment; phase 1 replaces this with mTLS. The hub path is
		// mounted next to the existing API so /healthz keeps working.
		Handler:           newRouter(st),
		Protocols:         newProtocols(),
		ReadHeaderTimeout: 5 * time.Second,
		// No ReadTimeout/WriteTimeout: they would cut the long-lived agent stream.
		// Per-request limits for the REST API come with its handlers.
		IdleTimeout: 60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "address", cfg.Listen)
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

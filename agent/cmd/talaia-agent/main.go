// Command talaia-agent is the Talaia per-server agent.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/catlinux/Talaia/agent/internal/collect"
	"github.com/catlinux/Talaia/agent/internal/config"
	"github.com/catlinux/Talaia/agent/internal/enroll"
	"github.com/catlinux/Talaia/agent/internal/link"
)

// Enrollment retry bounds: exponential backoff from two seconds up to a minute.
const (
	enrollBackoffBase = 2 * time.Second
	enrollBackoffMax  = 60 * time.Second
)

// exitConfig is the exit status of the failures only an operator can fix, the
// EX_CONFIG status of sysexits.h. The systemd unit sets
// RestartPreventExitStatus=78, so the agent is not restarted for them.
const exitConfig = 78

// errNotEnrolled is the message of an agent that has neither an identity nor a
// token to get one: retrying cannot help.
var errNotEnrolled = errors.New("not enrolled: no certificate and no enrollment token")

func main() {
	if err := run(); err != nil {
		slog.Error("talaia-agent stopped", "error", err)
		os.Exit(exitStatus(err))
	}
}

// exitStatus returns the status of a failure: EX_CONFIG for the errors that no
// restart can fix, 1 otherwise.
func exitStatus(err error) int {
	var configErr configError
	if errors.As(err, &configErr) {
		return exitConfig
	}
	return 1
}

// configError marks a failure that needs an operator: a bad configuration, a
// wrong CA fingerprint, an invalid token or nothing to enroll with.
type configError struct{ err error }

func (e configError) Error() string { return e.err.Error() }
func (e configError) Unwrap() error { return e.err }

// permanent marks err so that main exits with EX_CONFIG.
func permanent(err error) error { return configError{err} }

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return permanent(fmt.Errorf("load configuration: %w", err))
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.Level()}))
	slog.SetDefault(logger)
	logger.Info("starting talaia-agent", "version", collect.Version(), "core_url", cfg.CoreURL)

	if cfg.InsecureDev {
		logger.Warn("insecure development mode: no TLS")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var identity *enroll.Identity
	if !cfg.InsecureDev {
		identity, err = loadOrEnroll(ctx, cfg, logger)
		if err != nil {
			return err
		}
	}

	if err := link.Run(ctx, cfg, collect.New(), identity); err != nil {
		return err
	}
	logger.Info("talaia-agent stopped")
	return nil
}

// loadOrEnroll loads the stored identity or, when there is none, enrols the
// agent with the one-time token. It never logs the token.
func loadOrEnroll(ctx context.Context, cfg config.Config, logger *slog.Logger) (*enroll.Identity, error) {
	identity, err := enroll.LoadIdentity(cfg.CertDir)
	if err == nil {
		logger.Info("loaded the agent identity", "server_id", identity.ServerID(), "not_after", identity.NotAfter())
		return identity, nil
	}
	if !errors.Is(err, enroll.ErrNotEnrolled) {
		return nil, fmt.Errorf("load identity: %w", err)
	}

	token, err := os.ReadFile(cfg.EnrollTokenFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, permanent(errNotEnrolled)
	}
	if err != nil {
		return nil, fmt.Errorf("read the enrollment token: %w", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("read the hostname: %w", err)
	}

	options := enroll.Options{
		CoreURL:       cfg.CoreURL,
		CAFingerprint: cfg.CAFingerprint,
		Token:         strings.TrimSpace(string(token)),
		Hostname:      hostname,
		CertDir:       cfg.CertDir,
		TokenFile:     cfg.EnrollTokenFile,
	}

	backoff := enrollBackoffBase
	for {
		identity, err := enroll.Enroll(ctx, options)
		if err == nil {
			logger.Info("enrolled with the core", "server_id", identity.ServerID(), "not_after", identity.NotAfter())
			return identity, nil
		}
		if enroll.IsPermanent(err) {
			return nil, permanent(fmt.Errorf("enroll: %w", err))
		}

		logger.Warn("enrollment failed, retrying", "error", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, enrollBackoffMax)
	}
}

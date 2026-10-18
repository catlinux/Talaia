// Command talaia-agent is the Talaia per-server agent.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/catlinux/Talaia/agent/internal/collect"
	"github.com/catlinux/Talaia/agent/internal/config"
	"github.com/catlinux/Talaia/agent/internal/link"
)

func main() {
	if err := run(); err != nil {
		slog.Error("talaia-agent stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.Level()}))
	slog.SetDefault(logger)
	logger.Info("starting talaia-agent", "version", collect.Version(), "core_url", cfg.CoreURL)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := link.Run(ctx, cfg, collect.New()); err != nil {
		return err
	}
	logger.Info("talaia-agent stopped")
	return nil
}

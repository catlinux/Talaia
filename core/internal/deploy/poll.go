// Package deploy holds the deployment pipeline shared by the core.
package deploy

import (
	"context"
	"log/slog"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

// DefaultPollInterval is how often the core asks the branch of a poll-enabled
// app for a new commit.
const DefaultPollInterval = 2 * time.Minute

// Prober returns the commit a branch points at.
type Prober interface {
	Probe(ctx context.Context, app store.App) (string, error)
}

// Enqueuer queues one deployment of an app.
type Enqueuer interface {
	Enqueue(ctx context.Context, appID, trigger, commit string) error
}

// PollAppLister returns the apps that opted into branch polling.
type PollAppLister interface {
	ListPollableApps(ctx context.Context) ([]store.App, error)
}

// PollerDeps are the collaborators of the branch poller.
type PollerDeps struct {
	Apps     PollAppLister
	Prober   Prober
	Enqueue  Enqueuer
	Interval time.Duration
	Logger   *slog.Logger
	// Ticker is injectable for tests. It returns a receive-only tick channel
	// and the function that stops the ticker.
	Ticker func(time.Duration) (<-chan time.Time, func())
}

// Poller records the last commit it has seen for every app and enqueues a
// deployment when the branch moves.
type Poller struct {
	apps     PollAppLister
	prober   Prober
	enqueue  Enqueuer
	interval time.Duration
	logger   *slog.Logger
	ticker   func(time.Duration) (<-chan time.Time, func())
	last     map[string]string
}

// NewPoller returns a poller with the production defaults filled in.
func NewPoller(deps PollerDeps) *Poller {
	interval := deps.Interval
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ticker := deps.Ticker
	if ticker == nil {
		ticker = func(interval time.Duration) (<-chan time.Time, func()) {
			t := time.NewTicker(interval)
			return t.C, t.Stop
		}
	}
	return &Poller{
		apps:     deps.Apps,
		prober:   deps.Prober,
		enqueue:  deps.Enqueue,
		interval: interval,
		logger:   logger,
		ticker:   ticker,
		last:     map[string]string{},
	}
}

// Run polls immediately and then at every interval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	p.pollOnce(ctx)
	ticks, stop := p.ticker(p.interval)
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			p.pollOnce(ctx)
		}
	}
}

// pollOnce walks the apps in order, so a server never has two probes in
// flight. Errors are logged and do not stop the remaining apps.
func (p *Poller) pollOnce(ctx context.Context) {
	if p.apps == nil || p.prober == nil || p.enqueue == nil {
		p.logger.Error("poller is missing a dependency")
		return
	}
	apps, err := p.apps.ListPollableApps(ctx)
	if err != nil {
		if ctx.Err() == nil {
			p.logger.Error("list pollable apps", "error", err)
		}
		return
	}

	for _, app := range apps {
		if ctx.Err() != nil {
			return
		}
		if app.Source != store.AppSourceGit || !app.AutoDeploy || !app.PollEnabled {
			continue
		}
		commit, err := p.prober.Probe(ctx, app)
		if err != nil {
			if ctx.Err() == nil {
				p.logger.Error("probe app branch", "app_id", app.ID, "server_id", app.ServerID, "error", err)
			}
			continue
		}
		previous, seen := p.last[app.ID]
		if !seen {
			p.last[app.ID] = commit
			continue
		}
		if previous == commit {
			continue
		}
		if err := p.enqueue.Enqueue(ctx, app.ID, "poll", commit); err != nil {
			if ctx.Err() == nil {
				p.logger.Error("enqueue polled deployment", "app_id", app.ID, "server_id", app.ServerID, "error", err)
			}
			continue
		}
		p.last[app.ID] = commit
	}
}

// Package metrics buffers telemetry samples and persists them asynchronously.
package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

const MaxSamples = 20000

type Store interface {
	InsertServerSamples(context.Context, []store.MetricServerSample) error
	InsertContainerSamples(context.Context, []store.MetricContainerSample) error
	DownsampleServer(context.Context, store.Resolution, store.Resolution, time.Time) error
	DownsampleContainer(context.Context, store.Resolution, store.Resolution, time.Time) error
	DropOldMetrics(context.Context, string, time.Time) error
}

type Recorder struct {
	store       Store
	logger      *slog.Logger
	mu          sync.Mutex
	servers     []store.MetricServerSample
	containers  []store.MetricContainerSample
	lastWarning time.Time
	clock       clock
}

type ticker interface {
	C() <-chan time.Time
	Stop()
}
type timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(time.Duration) bool
}
type clock interface {
	Now() time.Time
	NewTicker(time.Duration) ticker
	NewTimer(time.Duration) timer
}
type realClock struct{}
type realTicker struct{ ticker *time.Ticker }
type realTimer struct{ timer *time.Timer }

func (realClock) Now() time.Time                   { return time.Now() }
func (realClock) NewTicker(d time.Duration) ticker { return realTicker{time.NewTicker(d)} }
func (realClock) NewTimer(d time.Duration) timer   { return realTimer{time.NewTimer(d)} }
func (t realTicker) C() <-chan time.Time           { return t.ticker.C }
func (t realTicker) Stop()                         { t.ticker.Stop() }
func (t realTimer) C() <-chan time.Time            { return t.timer.C }
func (t realTimer) Stop() bool                     { return t.timer.Stop() }
func (t realTimer) Reset(d time.Duration) bool     { return t.timer.Reset(d) }

func NewRecorder(s Store, logger *slog.Logger) *Recorder {
	if logger == nil {
		logger = slog.Default()
	}
	return &Recorder{store: s, logger: logger, clock: realClock{}}
}

func (r *Recorder) AddServerSample(sample store.MetricServerSample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.servers) == MaxSamples {
		r.servers = r.servers[1:]
	}
	r.servers = append(r.servers, sample)
}

func (r *Recorder) AddContainerSample(sample store.MetricContainerSample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.containers) == MaxSamples {
		r.containers = r.containers[1:]
	}
	r.containers = append(r.containers, sample)
}

func (r *Recorder) Run(ctx context.Context) {
	ticker := r.clock.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			r.flush(ctx)
		}
	}
}

func (r *Recorder) flush(ctx context.Context) {
	r.mu.Lock()
	servers, containers := r.servers, r.containers
	r.servers = nil
	r.containers = nil
	r.mu.Unlock()
	if len(servers) == 0 && len(containers) == 0 {
		return
	}
	var failed bool
	if len(servers) > 0 {
		if err := r.store.InsertServerSamples(ctx, servers); err != nil {
			failed = true
		}
	}
	if len(containers) > 0 {
		if err := r.store.InsertContainerSamples(ctx, containers); err != nil {
			failed = true
		}
	}
	if failed {
		now := r.clock.Now()
		r.mu.Lock()
		warn := r.lastWarning.IsZero() || now.Sub(r.lastWarning) >= time.Minute
		if warn {
			r.lastWarning = now
		}
		r.mu.Unlock()
		if warn {
			r.logger.Warn("metrics batch was dropped after database error")
		}
	}
}

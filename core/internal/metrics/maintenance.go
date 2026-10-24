package metrics

import (
	"context"
	"log/slog"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

type Retention struct {
	ServerRaw, Server5m, Server1h          time.Duration
	ContainerRaw, Container5m, Container1h time.Duration
}

func DefaultRetention() Retention {
	return Retention{48 * time.Hour, 30 * 24 * time.Hour, 400 * 24 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour, 90 * 24 * time.Hour}
}

func (r *Recorder) Maintenance(ctx context.Context, retention Retention) {
	downsamples := r.clock.NewTicker(5 * time.Minute)
	defer downsamples.Stop()
	retentionTimer := r.clock.NewTimer(time.Minute)
	defer retentionTimer.Stop()
	retentionTicker := r.clock.NewTicker(24 * time.Hour)
	defer retentionTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-downsamples.C():
			r.downsample(ctx)
		case <-retentionTimer.C():
			r.applyRetention(ctx, retention)
			retentionTimer.Reset(24 * time.Hour)
		case <-retentionTicker.C():
			r.applyRetention(ctx, retention)
		}
	}
}

func (r *Recorder) downsample(ctx context.Context) {
	until := r.clock.Now()
	for _, step := range []struct{ from, to store.Resolution }{{store.ResolutionRaw, store.Resolution5m}, {store.Resolution5m, store.Resolution1h}} {
		if err := r.store.DownsampleServer(ctx, step.from, step.to, until); err != nil {
			r.logger.Warn("server metrics downsampling failed", slog.Any("error", err))
		}
		if err := r.store.DownsampleContainer(ctx, step.from, step.to, until); err != nil {
			r.logger.Warn("container metrics downsampling failed", slog.Any("error", err))
		}
	}
}

func (r *Recorder) applyRetention(ctx context.Context, retention Retention) {
	now := r.clock.Now()
	items := []struct {
		table string
		age   time.Duration
	}{{"metric_server", retention.ServerRaw}, {"metric_server_5m", retention.Server5m}, {"metric_server_1h", retention.Server1h}, {"metric_container", retention.ContainerRaw}, {"metric_container_5m", retention.Container5m}, {"metric_container_1h", retention.Container1h}}
	for _, item := range items {
		if err := r.store.DropOldMetrics(ctx, item.table, now.Add(-item.age)); err != nil {
			r.logger.Warn("metrics retention failed", slog.String("table", item.table), slog.Any("error", err))
		}
	}
}

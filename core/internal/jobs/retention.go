// Package jobs runs the background maintenance of the job table.
package jobs

import (
	"context"
	"log/slog"
	"time"
)

const (
	// ContainerLogsKind is the kind of the jobs that read container logs.
	ContainerLogsKind = "container_logs"
	// LogRetention is how long a finished log job is kept.
	LogRetention = 15 * time.Minute
	// PurgeInterval is how often the finished log jobs are purged.
	PurgeInterval = 5 * time.Minute
)

// Purger deletes finished jobs; the store implements it.
type Purger interface {
	PurgeFinishedJobs(ctx context.Context, kind string, olderThan time.Duration) (int64, error)
}

// PurgeLoop deletes finished container_logs jobs at start and every interval
// until ctx ends. Live logs create a job every few seconds, so without this the
// table would only grow; unfinished jobs and other kinds are never touched.
func PurgeLoop(ctx context.Context, purger Purger, retention, interval time.Duration) {
	if retention <= 0 {
		retention = LogRetention
	}
	if interval <= 0 {
		interval = PurgeInterval
	}

	purge := func() {
		deleted, err := purger.PurgeFinishedJobs(ctx, ContainerLogsKind, retention)
		if err != nil {
			slog.Error("purge finished log jobs", "error", err)
			return
		}
		if deleted > 0 {
			slog.Info("purged finished log jobs", "count", deleted, "older_than", retention.String())
		}
	}

	purge()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}

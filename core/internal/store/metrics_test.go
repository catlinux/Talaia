package store

import (
	"context"
	"testing"
	"time"
)

func TestMetricsDownsampleAndRetention(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)
	base := time.Now().UTC().Truncate(5 * time.Minute).Add(-30 * 24 * time.Hour)
	closed := base.Add(-5 * time.Minute)
	if err := s.InsertServerSamples(ctx, []MetricServerSample{{Time: closed, ServerID: serverID}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DownsampleServer(ctx, ResolutionRaw, Resolution5m, base); err != nil {
		t.Fatal(err)
	}
	if err := s.DownsampleServer(ctx, ResolutionRaw, Resolution5m, base); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM metric_server_5m WHERE server_id=$1`, serverID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("idempotent downsample rows = %d", count)
	}
	open := base.Add(time.Minute)
	if err := s.InsertServerSamples(ctx, []MetricServerSample{{Time: open, ServerID: serverID}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DownsampleServer(ctx, ResolutionRaw, Resolution5m, base); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM metric_server_5m WHERE server_id=$1`, serverID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("open bucket was downsampled: %d", count)
	}

	old := time.Now().UTC().Add(-40 * 24 * time.Hour)
	newer := time.Now().UTC().Add(-10 * 24 * time.Hour)
	if err := s.InsertServerSamples(ctx, []MetricServerSample{{Time: old, ServerID: serverID}, {Time: newer, ServerID: serverID}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DropOldMetrics(ctx, "metric_server", time.Now().UTC().Add(-20*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM metric_server WHERE server_id=$1 AND time=$2`, serverID, old).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("old chunk row remains: %d", count)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM metric_server WHERE server_id=$1 AND time=$2`, serverID, newer).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("new row missing: %d", count)
	}
	if err := s.DeleteServerMetrics(ctx, serverID); err != nil {
		t.Fatal(err)
	}
}

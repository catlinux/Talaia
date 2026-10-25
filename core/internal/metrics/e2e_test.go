package metrics

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestAgentSamplesBecomeQueryableSeries follows what the hub does with the
// telemetry of an agent against the real database: samples go through the
// feed and the recorder, are stored, queried raw, downsampled to 5 minutes and
// queried again.
func TestAgentSamplesBecomeQueryableSeries(t *testing.T) {
	url := os.Getenv("TALAIA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TALAIA_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	serverID, err := st.CreateServer(ctx, "metrics-e2e-"+time.Now().Format("150405.000000000"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.DeleteServerMetrics(context.Background(), serverID) })

	recorder := NewRecorder(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	feed := NewFeed(recorder)
	// Whole 5-minute buckets in the past, so they are closed.
	base := time.Now().UTC().Truncate(5 * time.Minute).Add(-30 * time.Minute)
	containers := []*agentv1.Container{{Id: "aaaaaaaaaaaa", Name: "shop-web-1", ComposeProject: "shop"}}
	for i := 0; i < 20; i++ {
		at := base.Add(time.Duration(i) * 15 * time.Second)
		feed.RecordServerMetrics(serverID, &agentv1.Metrics{
			SampledAt:      timestamppb.New(at),
			CpuPercent:     40 + float64(i),
			MemUsedBytes:   4 << 30,
			DiskUsedBytes:  50 << 30,
			DiskTotalBytes: 100 << 30,
			NetRxBytes:     uint64(i) * 15 * 1000, // 1000 bytes per second
			NetTxBytes:     uint64(i) * 15 * 500,
		}, 8<<30)
		feed.RecordContainerStats(serverID, &agentv1.ContainerStats{
			SampledAt: timestamppb.New(at),
			Items:     []*agentv1.ContainerStat{{Id: "aaaaaaaaaaaa", CpuPercent: 12.5, MemUsedBytes: 256 << 20, MemLimitBytes: 1 << 30}},
		}, containers)
	}
	recorder.flush(ctx)

	from, to := base.Add(-time.Minute), base.Add(10*time.Minute)
	raw, err := st.QueryServerSeries(ctx, serverID, from, to, store.ResolutionRaw)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 20 {
		t.Fatalf("raw points = %d, want 20", len(raw))
	}
	first, last := raw[0], raw[len(raw)-1]
	if first.CPUAvg == nil || *first.CPUAvg != 40 || last.CPUAvg == nil || *last.CPUAvg != 59 {
		t.Fatalf("cpu series = %v ... %v", first.CPUAvg, last.CPUAvg)
	}
	if last.MemTotal == nil || *last.MemTotal != 8<<30 || last.DiskTotal == nil || *last.DiskTotal != 100<<30 {
		t.Fatalf("totals = %v / %v", last.MemTotal, last.DiskTotal)
	}
	if first.NetRxBPSAvg != nil {
		t.Fatalf("the first sample has no rate, got %v", *first.NetRxBPSAvg)
	}
	if last.NetRxBPSAvg == nil || *last.NetRxBPSAvg != 1000 || last.NetTxBPSAvg == nil || *last.NetTxBPSAvg != 500 {
		t.Fatalf("network rates = %v / %v, want 1000 and 500 bytes per second", last.NetRxBPSAvg, last.NetTxBPSAvg)
	}
	perContainer, err := st.QueryContainerSeries(ctx, serverID, "shop-web-1", from, to, store.ResolutionRaw)
	if err != nil || len(perContainer) != 20 || perContainer[0].CPUAvg == nil || *perContainer[0].CPUAvg != 12.5 {
		t.Fatalf("container series = %d points, err %v", len(perContainer), err)
	}

	recorder.downsample(ctx)
	fiveMinutes, err := st.QueryServerSeries(ctx, serverID, from, to, store.Resolution5m)
	if err != nil {
		t.Fatal(err)
	}
	if len(fiveMinutes) != 1 {
		t.Fatalf("5-minute points = %d, want 1 (20 samples of 15 s fill one 5-minute bucket)", len(fiveMinutes))
	}
	bucket := fiveMinutes[0]
	if bucket.CPUMax == nil || *bucket.CPUMax != 59 || bucket.CPUAvg == nil || *bucket.CPUAvg != 49.5 {
		t.Fatalf("5-minute cpu avg/max = %v / %v, want 49.5 / 59", bucket.CPUAvg, bucket.CPUMax)
	}
}

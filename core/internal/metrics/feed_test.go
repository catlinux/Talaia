package metrics

import (
	"io"
	"log/slog"
	"testing"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestFeedRecordsRatesAndContainerInventory(t *testing.T) {
	r := NewRecorder(&fakeStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f := NewFeed(r)
	start := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return start }
	metric := func(at time.Time, rx, tx uint64) *agentv1.Metrics {
		return &agentv1.Metrics{SampledAt: timestamppb.New(at), CpuPercent: 12, MemUsedBytes: 20, DiskUsedBytes: 30, DiskTotalBytes: 40, NetRxBytes: rx, NetTxBytes: tx}
	}
	f.RecordServerMetrics("server", metric(start, 100, 200), 1000)
	f.RecordServerMetrics("server", metric(start.Add(2*time.Second), 160, 240), 1000)
	if len(r.servers) != 2 || r.servers[0].NetRxBPS != nil || *r.servers[1].NetRxBPS != 30 || *r.servers[1].NetTxBPS != 20 || *r.servers[1].MemTotal != 1000 {
		t.Fatalf("unexpected server samples: %#v", r.servers)
	}
	f.RecordServerMetrics("server", metric(start.Add(4*time.Second), 1, 280), 0)
	if r.servers[2].NetRxBPS != nil || r.servers[2].NetTxBPS == nil || *r.servers[2].NetTxBPS != 20 || r.servers[2].MemTotal != nil {
		t.Fatalf("counter reset handling: %#v", r.servers[2])
	}
	stats := &agentv1.ContainerStats{SampledAt: timestamppb.New(start), Items: []*agentv1.ContainerStat{{Id: "abc", CpuPercent: 4, MemUsedBytes: 5, MemLimitBytes: 6}, {Id: "missing"}}}
	f.RecordContainerStats("server", stats, []*agentv1.Container{{Id: "abc", Name: "api", ComposeProject: "shop"}})
	if len(r.containers) != 1 || r.containers[0].ContainerName != "api" || r.containers[0].Stack != "shop" {
		t.Fatalf("unexpected container samples: %#v", r.containers)
	}
}

func TestFeedUsesCurrentTimeWithoutSampleTimestamp(t *testing.T) {
	r := NewRecorder(&fakeStore{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f := NewFeed(r)
	want := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return want }
	f.RecordServerMetrics("server", &agentv1.Metrics{}, 0)
	if len(r.servers) != 1 || !r.servers[0].Time.Equal(want) {
		t.Fatalf("got sample %#v, want %s", r.servers, want)
	}
}

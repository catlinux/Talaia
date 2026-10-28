package store

import (
	"context"
	"math"
	"testing"
	"time"
)

func f32(v float32) *float32 { return &v }
func i64(v int64) *int64     { return &v }
func f64(v float64) *float64 { return &v }

func near32(p *float32, want float64) bool { return p != nil && math.Abs(float64(*p)-want) < 0.01 }
func near64(p *float64, want float64) bool { return p != nil && math.Abs(*p-want) < 0.01 }
func eq64(p *int64, want int64) bool       { return p != nil && *p == want }

func TestMetricsDownsampleFiveMinutesToHour(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	serverID := newTestServerID(t, s)
	hour := time.Now().UTC().Truncate(time.Hour).Add(-30 * 24 * time.Hour)
	until := hour.Add(2 * time.Hour)
	first, second := hour.Add(time.Minute), hour.Add(6*time.Minute)

	if err := s.InsertServerSamples(ctx, []MetricServerSample{
		{Time: first, ServerID: serverID, CPUPct: f32(10), MemUsed: i64(100), MemTotal: i64(1000), DiskUsed: i64(200), DiskTotal: i64(2000), NetRxBPS: f64(10), NetTxBPS: f64(1)},
		{Time: second, ServerID: serverID, CPUPct: f32(30), MemUsed: i64(300), MemTotal: i64(1000), DiskUsed: i64(400), DiskTotal: i64(2000), NetRxBPS: f64(30), NetTxBPS: f64(3)},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertContainerSamples(ctx, []MetricContainerSample{
		{Time: first, ServerID: serverID, ContainerName: "web", Stack: "app", CPUPct: f32(10), MemUsed: i64(100), MemLimit: i64(500)},
		{Time: second, ServerID: serverID, ContainerName: "web", Stack: "app", CPUPct: f32(50), MemUsed: i64(300), MemLimit: i64(500)},
	}); err != nil {
		t.Fatal(err)
	}

	for _, step := range []struct{ from, to Resolution }{{ResolutionRaw, Resolution5m}, {Resolution5m, Resolution1h}} {
		if err := s.DownsampleServer(ctx, step.from, step.to, until); err != nil {
			t.Fatalf("server %s to %s: %v", step.from, step.to, err)
		}
		if err := s.DownsampleContainer(ctx, step.from, step.to, until); err != nil {
			t.Fatalf("container %s to %s: %v", step.from, step.to, err)
		}
	}
	// Running the step again must not duplicate the hour.
	if err := s.DownsampleServer(ctx, Resolution5m, Resolution1h, until); err != nil {
		t.Fatal(err)
	}

	points, err := s.QueryServerSeries(ctx, serverID, hour, hour.Add(time.Hour), Resolution1h)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 {
		t.Fatalf("server hourly points = %d, want 1", len(points))
	}
	p := points[0]
	if !p.Time.Equal(hour) || !near32(p.CPUAvg, 20) || !near32(p.CPUMax, 30) || !near64(p.MemUsedAvg, 200) || !eq64(p.MemUsedMax, 300) ||
		!eq64(p.MemTotal, 1000) || !near64(p.DiskUsedAvg, 300) || !eq64(p.DiskUsedMax, 400) || !eq64(p.DiskTotal, 2000) ||
		!near64(p.NetRxBPSAvg, 20) || !near64(p.NetTxBPSAvg, 2) {
		t.Fatalf("server hourly point = %+v", p)
	}

	containers, err := s.QueryContainerSeries(ctx, serverID, "web", hour, hour.Add(time.Hour), Resolution1h)
	if err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 {
		t.Fatalf("container hourly points = %d, want 1", len(containers))
	}
	c := containers[0]
	if !near32(c.CPUAvg, 30) || !near32(c.CPUMax, 50) || !near64(c.MemUsedAvg, 200) || !eq64(c.MemUsedMax, 300) || !eq64(c.MemLimit, 500) {
		t.Fatalf("container hourly point = %+v", c)
	}
	var stack string
	if err := s.pool.QueryRow(ctx, `SELECT stack FROM metric_container_1h WHERE server_id=$1 AND container_name='web'`, serverID).Scan(&stack); err != nil || stack != "app" {
		t.Fatalf("hourly stack = %q, %v", stack, err)
	}
	if err := s.DeleteServerMetrics(ctx, serverID); err != nil {
		t.Fatal(err)
	}
}

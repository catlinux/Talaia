package inventory

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCPUPercentFollowsDockerStats(t *testing.T) {
	var s dockerStats
	s.PreCPUStats.CPUUsage.TotalUsage = 1_000
	s.PreCPUStats.SystemCPUUsage = 10_000
	s.CPUStats.CPUUsage.TotalUsage = 1_500
	s.CPUStats.SystemCPUUsage = 12_000
	s.CPUStats.OnlineCPUs = 4

	// (500 / 2000) of the machine, on 4 CPUs: one full core.
	if got := cpuPercent(s); math.Abs(got-100) > 0.001 {
		t.Errorf("cpuPercent = %v, want 100", got)
	}
}

func TestCPUPercentIsZeroWithoutAUsableSample(t *testing.T) {
	var s dockerStats
	s.CPUStats.CPUUsage.TotalUsage = 500
	s.CPUStats.SystemCPUUsage = 2_000

	// The first sample has no previous one.
	if got := cpuPercent(s); got != 500.0/2000.0*100 && got != 0 {
		t.Errorf("cpuPercent = %v", got)
	}

	s.PreCPUStats.CPUUsage.TotalUsage = 900 // counters went backwards
	s.PreCPUStats.SystemCPUUsage = 1_000
	if got := cpuPercent(s); got != 0 {
		t.Errorf("cpuPercent with a decreasing counter = %v, want 0", got)
	}
}

func TestMemoryUsedSubtractsTheCache(t *testing.T) {
	var v2, v1, odd dockerStats
	v2.MemoryStats.Usage = 1000
	v2.MemoryStats.Stats = map[string]uint64{"inactive_file": 300}
	v1.MemoryStats.Usage = 1000
	v1.MemoryStats.Stats = map[string]uint64{"cache": 200}
	odd.MemoryStats.Usage = 100
	odd.MemoryStats.Stats = map[string]uint64{"inactive_file": 500}

	if got := memoryUsed(v2); got != 700 {
		t.Errorf("cgroup v2 = %d, want 700", got)
	}
	if got := memoryUsed(v1); got != 800 {
		t.Errorf("cgroup v1 = %d, want 800", got)
	}
	if got := memoryUsed(odd); got != 0 {
		t.Errorf("cache larger than usage = %d, want 0", got)
	}
}

const statsBody = `{
	"cpu_stats": {"cpu_usage": {"total_usage": 2000}, "system_cpu_usage": 20000, "online_cpus": 2},
	"precpu_stats": {"cpu_usage": {"total_usage": 1000}, "system_cpu_usage": 10000},
	"memory_stats": {"usage": 5000, "limit": 8000, "stats": {"inactive_file": 1000}},
	"networks": {"eth0": {"rx_bytes": 10, "tx_bytes": 20}, "eth1": {"rx_bytes": 1, "tx_bytes": 2}}
}`

func TestStatsSamplesEveryRunningContainer(t *testing.T) {
	var asked []string
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"Id": "bbbbbbbbbbbb0000000000", "Names": ["/db"], "State": "running"},
			{"Id": "aaaaaaaaaaaa0000000000", "Names": ["/web"], "State": "running"}
		]`))
	})
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.RequestURI())
		_, _ = w.Write([]byte(statsBody))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	stats := NewDockerWithClient(server.URL, server.Client()).Stats(context.Background())

	if stats == nil || len(stats.GetItems()) != 2 {
		t.Fatalf("stats = %v", stats)
	}
	first := stats.GetItems()[0]
	if first.GetId() != "aaaaaaaaaaaa" || first.GetMemUsedBytes() != 4000 || first.GetMemLimitBytes() != 8000 ||
		first.GetNetRxBytes() != 11 || first.GetNetTxBytes() != 22 || math.Abs(first.GetCpuPercent()-20) > 0.001 {
		t.Errorf("first = %v", first)
	}
	if stats.GetSampledAt() == nil {
		t.Error("the sample has no time")
	}
	for _, request := range asked {
		if !strings.HasSuffix(request, "/stats?stream=false") {
			t.Errorf("unexpected request %q", request)
		}
	}
}

func TestStatsWithoutRunningContainersIsEmptyNotNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	stats := NewDockerWithClient(server.URL, server.Client()).Stats(context.Background())

	if stats == nil || len(stats.GetItems()) != 0 {
		t.Errorf("stats = %v, want an empty message", stats)
	}
}

func TestStatsIsNilWhenDockerIsNotReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	if stats := NewDockerWithClient(server.URL, server.Client()).Stats(context.Background()); stats != nil {
		t.Errorf("stats = %v, want nil", stats)
	}
}

func TestStatsSkipsAContainerThatCannotBeSampled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"Id": "aaaaaaaaaaaa0000"}, {"Id": "cccccccccccc0000"}]`))
	})
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "cccccccccccc") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(statsBody))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	stats := NewDockerWithClient(server.URL, server.Client()).Stats(context.Background())

	if stats == nil || len(stats.GetItems()) != 1 || stats.GetItems()[0].GetId() != "aaaaaaaaaaaa" {
		t.Errorf("stats = %v", stats)
	}
}

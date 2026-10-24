package metrics

import (
	"sync"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Feed converts agent telemetry into recorder samples.
type Feed struct {
	recorder *Recorder
	now      func() time.Time
	mu       sync.Mutex
	previous map[string]counterSample
}

type counterSample struct {
	time time.Time
	rx   uint64
	tx   uint64
}

func NewFeed(recorder *Recorder) *Feed {
	return &Feed{recorder: recorder, now: time.Now, previous: make(map[string]counterSample)}
}

func (f *Feed) RecordServerMetrics(serverID string, m *agentv1.Metrics, memTotal uint64) {
	if f == nil || f.recorder == nil || m == nil || serverID == "" {
		return
	}
	sampledAt := f.timestamp(m.GetSampledAt())
	f.mu.Lock()
	previous, hasPrevious := f.previous[serverID]
	f.previous[serverID] = counterSample{time: sampledAt, rx: m.GetNetRxBytes(), tx: m.GetNetTxBytes()}
	f.mu.Unlock()
	var rxRate, txRate *float64
	if hasPrevious && sampledAt.After(previous.time) {
		seconds := sampledAt.Sub(previous.time).Seconds()
		if m.GetNetRxBytes() >= previous.rx {
			rx := float64(m.GetNetRxBytes()-previous.rx) / seconds
			rxRate = &rx
		}
		if m.GetNetTxBytes() >= previous.tx {
			tx := float64(m.GetNetTxBytes()-previous.tx) / seconds
			txRate = &tx
		}
	}
	var total *int64
	if memTotal > 0 {
		v := int64(memTotal)
		total = &v
	}
	cpu, used, diskUsed, diskTotal := float32(m.GetCpuPercent()), int64(m.GetMemUsedBytes()), int64(m.GetDiskUsedBytes()), int64(m.GetDiskTotalBytes())
	f.recorder.AddServerSample(store.MetricServerSample{Time: sampledAt, ServerID: serverID, CPUPct: &cpu, MemUsed: &used, MemTotal: total, DiskUsed: &diskUsed, DiskTotal: &diskTotal, NetRxBPS: rxRate, NetTxBPS: txRate})
}

func (f *Feed) RecordContainerStats(serverID string, stats *agentv1.ContainerStats, containers []*agentv1.Container) {
	if f == nil || f.recorder == nil || stats == nil || serverID == "" {
		return
	}
	names := make(map[string]*agentv1.Container, len(containers))
	for _, container := range containers {
		if container != nil {
			names[container.GetId()] = container
		}
	}
	sampledAt := f.timestamp(stats.GetSampledAt())
	for _, stat := range stats.GetItems() {
		container := names[stat.GetId()]
		if container == nil {
			continue
		}
		cpu, used, limit := float32(stat.GetCpuPercent()), int64(stat.GetMemUsedBytes()), int64(stat.GetMemLimitBytes())
		f.recorder.AddContainerSample(store.MetricContainerSample{Time: sampledAt, ServerID: serverID, ContainerName: container.GetName(), Stack: container.GetComposeProject(), CPUPct: &cpu, MemUsed: &used, MemLimit: &limit})
	}
}

func (f *Feed) timestamp(timestamp *timestamppb.Timestamp) time.Time {
	if timestamp != nil {
		return timestamp.AsTime()
	}
	return f.now()
}

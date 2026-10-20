package inventory

import (
	"context"
	"net/url"
	"sort"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	// statsTimeout bounds one round of per-container statistics. Docker takes a
	// second per container to sample its CPU, so the containers are read together.
	statsTimeout = 15 * time.Second
	// statsParallel is how many containers are sampled at the same time.
	statsParallel = 8
)

// dockerStats is the part of the Engine answer of /containers/{id}/stats that the
// agent reads.
type dockerStats struct {
	CPUStats    cpuStats `json:"cpu_stats"`
	PreCPUStats cpuStats `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
}

type cpuStats struct {
	CPUUsage struct {
		TotalUsage uint64 `json:"total_usage"`
	} `json:"cpu_usage"`
	SystemCPUUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs     uint32 `json:"online_cpus"`
}

// stat turns the Engine answer into the message of the agent.
func (s dockerStats) stat(id string) *agentv1.ContainerStat {
	stat := &agentv1.ContainerStat{
		Id:            id,
		MemUsedBytes:  memoryUsed(s),
		MemLimitBytes: s.MemoryStats.Limit,
		CpuPercent:    cpuPercent(s),
	}
	for _, network := range s.Networks {
		stat.NetRxBytes += network.RxBytes
		stat.NetTxBytes += network.TxBytes
	}
	return stat
}

// cpuPercent is the CPU use between the two samples Docker takes, as docker stats
// computes it: 100 means one full core.
func cpuPercent(s dockerStats) float64 {
	if s.CPUStats.CPUUsage.TotalUsage < s.PreCPUStats.CPUUsage.TotalUsage ||
		s.CPUStats.SystemCPUUsage <= s.PreCPUStats.SystemCPUUsage {
		return 0
	}
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage - s.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(s.CPUStats.SystemCPUUsage - s.PreCPUStats.SystemCPUUsage)
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = 1
	}
	return cpuDelta / systemDelta * cpus * 100
}

// memoryUsed is the memory a container holds without the cache the kernel can
// reclaim: cgroup v2 reports it as inactive_file, cgroup v1 as cache.
func memoryUsed(s dockerStats) uint64 {
	cache := s.MemoryStats.Stats["inactive_file"]
	if cache == 0 {
		cache = s.MemoryStats.Stats["cache"]
	}
	if cache > s.MemoryStats.Usage {
		return 0
	}
	return s.MemoryStats.Usage - cache
}

// Stats samples every running container and returns the message to send. It
// returns nil when Docker is not reachable, and an empty message when no
// container runs, so the core learns that nothing does.
func (d *Docker) Stats(ctx context.Context) *agentv1.ContainerStats {
	ctx, cancel := context.WithTimeout(ctx, statsTimeout)
	defer cancel()

	var running []dockerContainer
	if err := d.get(ctx, "/containers/json", &running); err != nil {
		d.logUnavailable(err)
		return nil
	}

	var (
		mu    sync.Mutex
		wg    sync.WaitGroup
		items = make([]*agentv1.ContainerStat, 0, len(running))
		slots = make(chan struct{}, statsParallel)
	)
	for _, container := range running {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()

			var raw dockerStats
			if err := d.get(ctx, "/containers/"+url.PathEscape(container.ID)+"/stats?stream=false", &raw); err != nil {
				return
			}
			stat := raw.stat(shortID(container.ID))
			mu.Lock()
			items = append(items, stat)
			mu.Unlock()
		}()
	}
	wg.Wait()

	sort.Slice(items, func(i, j int) bool { return items[i].GetId() < items[j].GetId() })
	return &agentv1.ContainerStats{SampledAt: timestamppb.Now(), Items: items}
}

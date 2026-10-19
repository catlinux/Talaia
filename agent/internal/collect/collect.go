// Package collect gathers the host facts and metrics talaia-agent reports to
// the core.
package collect

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// version is the agent build version, overridden at build time with
// -ldflags "-X github.com/catlinux/Talaia/agent/internal/collect.version=<v>".
var version = "dev"

// Version returns the agent build version.
func Version() string {
	return version
}

// cpuSampleInterval is the window used to measure whole-machine CPU use.
const cpuSampleInterval = 250 * time.Millisecond

// virtualInterfacePrefixes are the interface name prefixes that are not
// physical: loopback and the usual container, bridge and VM interfaces.
var virtualInterfacePrefixes = []string{"lo", "docker", "veth", "br-", "virbr"}

// Collector gathers the information the agent sends to the core. It is hidden
// behind this small interface so the link tests can use a fake.
type Collector interface {
	Inventory() (*agentv1.Inventory, error)
	Metrics() (*agentv1.Metrics, error)
}

// System is the Collector for the host the agent runs on.
type System struct{}

// New returns the Collector for the local host.
func New() Collector {
	return System{}
}

// Inventory reports the static facts of the host.
func (System) Inventory() (*agentv1.Inventory, error) {
	info, err := host.Info()
	if err != nil {
		return nil, fmt.Errorf("read host info: %w", err)
	}
	memory, err := mem.VirtualMemory()
	if err != nil {
		return nil, fmt.Errorf("read memory info: %w", err)
	}

	hostname := info.Hostname
	if hostname == "" {
		hostname, _ = os.Hostname()
	}

	return &agentv1.Inventory{
		Hostname:      hostname,
		OsName:        info.Platform,
		OsVersion:     info.PlatformVersion,
		Kernel:        info.KernelVersion,
		Arch:          runtime.GOARCH,
		AgentVersion:  version,
		CpuCount:      uint32(runtime.NumCPU()),
		MemTotalBytes: memory.Total,
	}, nil
}

// Metrics reports a live sample of the host. A source that cannot be read is
// logged and leaves its fields at zero; an error is only returned when no
// metric could be read at all.
func (System) Metrics() (*agentv1.Metrics, error) {
	sample := &agentv1.Metrics{SampledAt: timestamppb.Now()}

	if hostname, err := os.Hostname(); err != nil {
		slog.Warn("collect hostname", "error", err)
	} else {
		sample.Hostname = hostname
	}

	read := 0

	if percent, err := cpuPercent(); err != nil {
		slog.Warn("collect cpu", "error", err)
	} else {
		sample.CpuPercent = percent
		read++
	}

	if memory, err := mem.VirtualMemory(); err != nil {
		slog.Warn("collect memory", "error", err)
	} else {
		sample.MemUsedBytes = memory.Used
		read++
	}

	if usage, err := disk.Usage(rootPath()); err != nil {
		slog.Warn("collect disk", "error", err)
	} else {
		sample.DiskUsedBytes = usage.Used
		sample.DiskTotalBytes = usage.Total
		read++
	}

	if rx, tx, err := networkCounters(); err != nil {
		slog.Warn("collect network", "error", err)
	} else {
		sample.NetRxBytes = rx
		sample.NetTxBytes = tx
		read++
	}

	if read == 0 {
		return nil, errors.New("no metric could be read")
	}
	return sample, nil
}

// cpuPercent returns the whole-machine CPU use over a short sample, in the
// 0-100 range.
func cpuPercent() (float64, error) {
	percents, err := cpu.Percent(cpuSampleInterval, false)
	if err != nil {
		return 0, fmt.Errorf("read cpu: %w", err)
	}
	if len(percents) == 0 {
		return 0, nil
	}
	return percents[0], nil
}

// networkCounters sums the received and sent bytes of the physical interfaces.
func networkCounters() (rx, tx uint64, err error) {
	counters, err := net.IOCounters(true)
	if err != nil {
		return 0, 0, fmt.Errorf("read network counters: %w", err)
	}

	for _, counter := range counters {
		if !isPhysicalInterface(counter.Name) {
			continue
		}
		rx += counter.BytesRecv
		tx += counter.BytesSent
	}
	return rx, tx, nil
}

// isPhysicalInterface reports whether an interface name looks like a physical
// interface rather than loopback or a container, bridge or VM interface.
func isPhysicalInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range virtualInterfacePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	return true
}

// rootPath is the filesystem the disk usage refers to: the root on Linux and
// the system drive (e.g. C:\) on Windows.
func rootPath() string {
	if runtime.GOOS == "windows" {
		return os.Getenv("SystemDrive") + `\`
	}
	return "/"
}

// Package collect gathers the host facts and metrics talaia-agent reports to
// the core.
package collect

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"

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

// Collector gathers the information the agent sends to the core. It is hidden
// behind this small interface so the link tests can use a fake.
type Collector interface {
	Inventory() (*agentv1.Inventory, error)
	CPUPercent() (float64, error)
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

// CPUPercent returns the whole-machine CPU use, in the 0-100 range, over a
// short sample.
func (System) CPUPercent() (float64, error) {
	percents, err := cpu.Percent(cpuSampleInterval, false)
	if err != nil {
		return 0, fmt.Errorf("read cpu: %w", err)
	}
	if len(percents) == 0 {
		return 0, nil
	}
	return percents[0], nil
}

package collect

import (
	"context"
	"runtime"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"

	"github.com/catlinux/Talaia/agent/internal/inventory"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeDockerSource struct {
	result inventory.DockerInventory
}

func (f fakeDockerSource) Collect(context.Context) inventory.DockerInventory {
	return f.result
}

type fakeServiceSource struct {
	units []*agentv1.SystemdUnit
}

func (f fakeServiceSource) Units(context.Context) []*agentv1.SystemdUnit {
	return f.units
}

func TestSystemInventory(t *testing.T) {
	inventory, err := System{}.Inventory()
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}

	if inventory.GetHostname() == "" {
		t.Error("hostname is empty")
	}
	if got, want := inventory.GetArch(), runtime.GOARCH; got != want {
		t.Errorf("arch = %q, want %q", got, want)
	}
	if got, want := inventory.GetCpuCount(), uint32(runtime.NumCPU()); got != want {
		t.Errorf("cpu count = %d, want %d", got, want)
	}
	if inventory.GetMemTotalBytes() == 0 {
		t.Error("mem total is zero")
	}
	if got, want := inventory.GetAgentVersion(), version; got != want {
		t.Errorf("agent version = %q, want %q", got, want)
	}
}

func TestSystemMetrics(t *testing.T) {
	sample, err := System{}.Metrics()
	if err != nil {
		t.Skipf("metrics are not available on this host: %v", err)
	}

	if sample.GetSampledAt() == nil {
		t.Error("sampled_at is not set")
	}
	if sample.GetHostname() == "" {
		t.Error("hostname is empty")
	}
	if percent := sample.GetCpuPercent(); percent < 0 || percent > 100 {
		t.Errorf("cpu_percent = %v, want a value in the 0-100 range", percent)
	}

	// Memory and disk are only asserted when the system calls work here.
	if _, err := mem.VirtualMemory(); err != nil {
		t.Skipf("memory info is not available on this host: %v", err)
	}
	if _, err := disk.Usage(rootPath()); err != nil {
		t.Skipf("disk usage is not available on this host: %v", err)
	}
	if sample.GetMemUsedBytes() == 0 {
		t.Error("mem_used_bytes = 0, want > 0")
	}
	if sample.GetDiskTotalBytes() == 0 {
		t.Error("disk_total_bytes = 0, want > 0")
	}
}

func TestIsPhysicalInterface(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"eth0", true},
		{"ens18", true},
		{"enp0s3", true},
		{"wlan0", true},
		{"Ethernet", true},
		{"Wi-Fi", true},
		{"lo", false},
		{"lo0", false},
		{"Loopback Pseudo-Interface 1", false},
		{"docker0", false},
		{"veth1a2b3c", false},
		{"br-1234", false},
		{"virbr0", false},
		{"BR-9", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPhysicalInterface(tt.name); got != tt.want {
				t.Fatalf("isPhysicalInterface(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestSystemInventoryIncludesDockerAndServices(t *testing.T) {
	provider := inventory.Provider{
		Docker: fakeDockerSource{result: inventory.DockerInventory{
			Available:  true,
			Version:    "27.1.1",
			Containers: []*agentv1.Container{{Id: "abcdefghijkl", Name: "web", State: "running"}},
			Projects:   []*agentv1.ComposeProject{{Name: "shop"}},
		}},
		Systemd: fakeServiceSource{units: []*agentv1.SystemdUnit{{Name: "docker.service"}}},
	}

	sample, err := System{Provider: provider}.Inventory()
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}

	if sample.GetHostname() == "" {
		t.Error("hostname is empty")
	}
	if !sample.GetDockerAvailable() || sample.GetDockerVersion() != "27.1.1" {
		t.Errorf("docker = %v %q", sample.GetDockerAvailable(), sample.GetDockerVersion())
	}
	if len(sample.GetContainers()) != 1 || len(sample.GetComposeProjects()) != 1 {
		t.Errorf("containers = %d, projects = %d", len(sample.GetContainers()), len(sample.GetComposeProjects()))
	}
	if len(sample.GetServices()) != 1 {
		t.Errorf("services = %+v", sample.GetServices())
	}
}

// The real provider must work on a host without Docker or systemd: it reports
// nothing instead of failing.
func TestSystemInventoryDegradesGracefully(t *testing.T) {
	sample, err := System{Provider: inventory.New()}.Inventory()
	if err != nil {
		t.Fatalf("inventory: %v", err)
	}

	t.Logf("docker available=%v services=%d", sample.GetDockerAvailable(), len(sample.GetServices()))

	if !sample.GetDockerAvailable() {
		if len(sample.GetContainers()) != 0 || len(sample.GetComposeProjects()) != 0 {
			t.Errorf("docker is unavailable but containers = %d and projects = %d",
				len(sample.GetContainers()), len(sample.GetComposeProjects()))
		}
	}
}

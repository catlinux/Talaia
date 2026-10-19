package collect

import (
	"runtime"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

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

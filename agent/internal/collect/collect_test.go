package collect

import (
	"runtime"
	"testing"
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

func TestSystemCPUPercent(t *testing.T) {
	percent, err := System{}.CPUPercent()
	if err != nil {
		t.Fatalf("cpu percent: %v", err)
	}
	if percent < 0 || percent > 100 {
		t.Errorf("cpu percent = %v, want a value in the 0-100 range", percent)
	}
}

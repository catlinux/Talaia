package link

import (
	"context"
	"testing"

	"github.com/catlinux/Talaia/agent/internal/proxy"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type staticPorts struct {
	free bool
}

func (s staticPorts) Free(uint16) bool { return s.free }

func TestApplyInventorySetsTheProxyStatus(t *testing.T) {
	manager := proxy.New(proxy.Options{Root: t.TempDir(), Ports: staticPorts{free: true}})
	inventory := &agentv1.Inventory{}
	applyInventory(context.Background(), inventory, nil, false, manager)

	status := inventory.GetProxy()
	if status == nil {
		t.Fatal("proxy status is nil")
	}
	if status.GetMode() != agentv1.ProxyMode_PROXY_MODE_EXTERNAL {
		t.Errorf("mode = %v", status.GetMode())
	}
	if !status.GetPort_80Free() || !status.GetPort_443Free() {
		t.Errorf("ports = %v / %v", status.GetPort_80Free(), status.GetPort_443Free())
	}
}

func TestApplyInventoryLeavesTheProxyUnsetWithoutAManager(t *testing.T) {
	inventory := &agentv1.Inventory{}
	applyInventory(context.Background(), inventory, nil, false, nil)
	if inventory.GetProxy() != nil {
		t.Errorf("proxy status = %v, want nil", inventory.GetProxy())
	}
}

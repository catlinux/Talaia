package link

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/catlinux/Talaia/agent/internal/apt"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type aptRunner struct{}

func (aptRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	switch {
	case name == "apt-get" && len(args) > 0 && args[0] == "-s":
		return []byte("Inst libc6 [1] (2 Debian-Security:12/stable-security [amd64])\n" +
			"Inst nginx [1] (2 Debian:12/stable [amd64])\n"), nil
	case name == "apt-mark":
		return nil, nil
	case name == "uname":
		return []byte("6.1.0-29-amd64\n"), nil
	default:
		return nil, errors.New("unexpected command")
	}
}

func TestApplyAptSummaryFillsTheInventory(t *testing.T) {
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	checker := apt.New(apt.Options{
		Runner: aptRunner{},
		Root:   t.TempDir(),
		AptGet: "apt-get",
		Now:    func() time.Time { return now },
	})
	if _, err := checker.StatusJSON(context.Background(), false); err != nil {
		t.Fatalf("StatusJSON: %v", err)
	}

	inventory := &agentv1.Inventory{}
	applyAptSummary(inventory, checker)

	if inventory.GetApt() == nil || inventory.GetApt().GetUpgradable() != 2 ||
		inventory.GetApt().GetSecurity() != 1 || inventory.GetApt().GetRebootRequired() {
		t.Errorf("inventory apt = %+v", inventory.GetApt())
	}
	if inventory.GetApt().GetCheckedAt() == nil || !inventory.GetApt().GetCheckedAt().AsTime().Equal(now) {
		t.Errorf("checked_at = %v, want %v", inventory.GetApt().GetCheckedAt(), now)
	}
}

func TestApplyAptSummaryLeavesTheInventoryUnsetWithoutApt(t *testing.T) {
	checker := apt.New(apt.Options{
		Runner:   aptRunner{},
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
	})
	inventory := &agentv1.Inventory{}
	applyAptSummary(inventory, checker)
	if inventory.GetApt() != nil {
		t.Errorf("inventory apt = %+v, want nil", inventory.GetApt())
	}
}

func TestApplyInventorySetsHostTerminal(t *testing.T) {
	inventory := &agentv1.Inventory{}
	applyInventory(inventory, nil, true)
	if !inventory.GetHostTerminal() {
		t.Error("host_terminal = false, want true")
	}
	applyInventory(inventory, nil, false)
	if inventory.GetHostTerminal() {
		t.Error("host_terminal = true, want false")
	}
}

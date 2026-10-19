package inventory

import (
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// systemdTimeout bounds the systemctl call.
const systemdTimeout = 10 * time.Second

// CommandRunner runs a command and returns its output. The production
// implementation uses os/exec and the tests inject a fake.
type CommandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Systemd reads the systemd service units through systemctl.
type Systemd struct {
	runner CommandRunner

	unavailableLogged sync.Once
}

// NewSystemd returns a Systemd that runs systemctl from PATH.
func NewSystemd() *Systemd {
	return NewSystemdWithRunner(execRunner{})
}

// NewSystemdWithRunner returns a Systemd using the given runner; tests use it.
func NewSystemdWithRunner(runner CommandRunner) *Systemd {
	return &Systemd{runner: runner}
}

// systemdUnit is one element of systemctl list-units --output=json.
type systemdUnit struct {
	Unit        string `json:"unit"`
	Load        string `json:"load"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Description string `json:"description"`
}

// Units returns the service units, sorted by name. It never fails: when
// systemctl is missing or answers with an error, it returns an empty list.
func (s *Systemd) Units(ctx context.Context) []*agentv1.SystemdUnit {
	ctx, cancel := context.WithTimeout(ctx, systemdTimeout)
	defer cancel()

	output, err := s.runner.Output(ctx, "systemctl",
		"list-units", "--type=service", "--all", "--no-pager", "--output=json")
	if err != nil {
		s.unavailableLogged.Do(func() {
			slog.Debug("systemd is not available", "error", err)
		})
		return nil
	}

	var raw []systemdUnit
	if err := json.Unmarshal(output, &raw); err != nil {
		s.unavailableLogged.Do(func() {
			slog.Debug("parse systemctl output", "error", err)
		})
		return nil
	}

	units := make([]*agentv1.SystemdUnit, 0, len(raw))
	for _, unit := range raw {
		if !strings.HasSuffix(unit.Unit, ".service") {
			continue
		}
		units = append(units, &agentv1.SystemdUnit{
			Name:        unit.Unit,
			Description: unit.Description,
			LoadState:   unit.Load,
			ActiveState: unit.Active,
			SubState:    unit.Sub,
		})
	}
	sort.Slice(units, func(i, j int) bool { return units[i].GetName() < units[j].GetName() })
	return units
}

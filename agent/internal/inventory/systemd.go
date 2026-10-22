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

// systemdUnitFile is one element of systemctl list-unit-files --output=json.
type systemdUnitFile struct {
	UnitFile string `json:"unit_file"`
	State    string `json:"state"`
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

	fileStates := s.unitFileStates(ctx)

	units := make([]*agentv1.SystemdUnit, 0, len(raw))
	for _, unit := range raw {
		if !strings.HasSuffix(unit.Unit, ".service") {
			continue
		}
		state := fileStates[unit.Unit]
		if state == "" {
			if template := templateServiceUnit(unit.Unit); template != "" {
				state = fileStates[template]
			}
		}
		units = append(units, &agentv1.SystemdUnit{
			Name:          unit.Unit,
			Description:   unit.Description,
			LoadState:     unit.Load,
			ActiveState:   unit.Active,
			SubState:      unit.Sub,
			UnitFileState: state,
		})
	}
	sort.Slice(units, func(i, j int) bool { return units[i].GetName() < units[j].GetName() })
	return units
}

// unitFileStates reads the enabled/disabled/masked state of the unit files. A
// failure leaves every state empty: the active state of the units is still
// useful on its own.
func (s *Systemd) unitFileStates(ctx context.Context) map[string]string {
	output, err := s.runner.Output(ctx, "systemctl",
		"list-unit-files", "--type=service", "--no-pager", "--output=json")
	if err != nil {
		slog.Debug("list systemd unit files", "error", err)
		return nil
	}
	var raw []systemdUnitFile
	if err := json.Unmarshal(output, &raw); err != nil {
		slog.Debug("parse systemctl unit files", "error", err)
		return nil
	}
	states := make(map[string]string, len(raw))
	for _, unit := range raw {
		if strings.HasSuffix(unit.UnitFile, ".service") && unit.State != "" {
			states[unit.UnitFile] = unit.State
		}
	}
	return states
}

// templateServiceUnit returns the template of an instance such as
// getty@tty1.service, which is getty@.service. A template itself and a unit
// without an instance return an empty string.
func templateServiceUnit(name string) string {
	if !strings.HasSuffix(name, ".service") {
		return ""
	}
	at := strings.IndexByte(name, '@')
	if at <= 0 {
		return ""
	}
	if name[at+1:] == ".service" {
		return ""
	}
	return name[:at+1] + ".service"
}

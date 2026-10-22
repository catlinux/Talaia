package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	serviceActionTimeout = 60 * time.Second
	serviceShowTimeout   = 30 * time.Second
	serviceErrorLimit    = 500
)

var (
	serviceUnitRE = regexp.MustCompile(`^[A-Za-z0-9@._:-]{1,250}\.service$`)
	errNoServices = errors.New("services are not available on this agent")
)

// ServiceManager runs one typed systemd service action.
type ServiceManager interface {
	Do(ctx context.Context, unit string, action agentv1.ServiceActionType) (string, error)
}

// ServiceRunner runs systemctl. The production runner uses os/exec; tests
// inject a fake and inspect the exact arguments.
type ServiceRunner interface {
	CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error)
}

type serviceExecRunner struct{}

func (serviceExecRunner) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// ServiceControl runs service actions and reports the state systemd shows after
// each one.
type ServiceControl struct {
	runner ServiceRunner
}

// NewServiceManager returns a manager that uses the production systemctl
// runner when runner is nil.
func NewServiceManager(runner ServiceRunner) *ServiceControl {
	if runner == nil {
		runner = serviceExecRunner{}
	}
	return &ServiceControl{runner: runner}
}

var serviceActionVerbs = map[agentv1.ServiceActionType]string{
	agentv1.ServiceActionType_SERVICE_ACTION_TYPE_START:   "start",
	agentv1.ServiceActionType_SERVICE_ACTION_TYPE_STOP:    "stop",
	agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RESTART: "restart",
	agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RELOAD:  "reload",
	agentv1.ServiceActionType_SERVICE_ACTION_TYPE_ENABLE:  "enable",
	agentv1.ServiceActionType_SERVICE_ACTION_TYPE_DISABLE: "disable",
}

// Do validates the action, runs it in its own 60-second context and returns the
// state systemctl reports afterwards as JSON.
func (s *ServiceControl) Do(ctx context.Context, unit string, action agentv1.ServiceActionType) (string, error) {
	if !validServiceUnit(unit) {
		return "", fmt.Errorf("invalid service unit %q", unit)
	}
	verb, known := serviceActionVerbs[action]
	if !known {
		return "", fmt.Errorf("invalid service action %s", action)
	}
	if unit == "talaia-agent.service" &&
		(action == agentv1.ServiceActionType_SERVICE_ACTION_TYPE_STOP ||
			action == agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RESTART ||
			action == agentv1.ServiceActionType_SERVICE_ACTION_TYPE_DISABLE) {
		return "", errors.New("talaia-agent cannot manage itself; update it with an agent update")
	}

	actionCtx, cancel := context.WithTimeout(ctx, serviceActionTimeout)
	defer cancel()
	output, err := s.runner.CombinedOutput(actionCtx, "systemctl", "--no-ask-password", verb, "--", unit)
	if err != nil {
		return "", servicectlError(verb, output, err)
	}

	showCtx, cancelShow := context.WithTimeout(ctx, serviceShowTimeout)
	defer cancelShow()
	output, err = s.runner.CombinedOutput(showCtx, "systemctl", "show",
		"-p", "ActiveState,SubState,UnitFileState,LoadState", "--", unit)
	if err != nil {
		return "", servicectlError("show", output, err)
	}

	state := parseServiceState(output, unit)
	data, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode the service state: %w", err)
	}
	return string(data), nil
}

func validServiceUnit(unit string) bool {
	return !strings.HasPrefix(unit, "-") && serviceUnitRE.MatchString(unit)
}

func servicectlError(verb string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if len(detail) > serviceErrorLimit {
		detail = detail[:serviceErrorLimit]
	}
	if detail == "" {
		return fmt.Errorf("systemctl %s: %w", verb, err)
	}
	return fmt.Errorf("systemctl %s: %s: %w", verb, detail, err)
}

type serviceState struct {
	Unit          string `json:"unit"`
	ActiveState   string `json:"active_state"`
	SubState      string `json:"sub_state"`
	UnitFileState string `json:"unit_file_state"`
	LoadState     string `json:"load_state"`
}

// parseServiceState reads the key=value lines of systemctl show.
func parseServiceState(output []byte, unit string) serviceState {
	state := serviceState{Unit: unit}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "ActiveState":
			state.ActiveState = value
		case "SubState":
			state.SubState = value
		case "UnitFileState":
			state.UnitFileState = value
		case "LoadState":
			state.LoadState = value
		}
	}
	return state
}

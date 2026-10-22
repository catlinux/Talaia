package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type serviceCall struct {
	name string
	args []string
}

type serviceRunner struct {
	mu      sync.Mutex
	calls   []serviceCall
	handler func(name string, args []string) ([]byte, error)
}

func (r *serviceRunner) CombinedOutput(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, serviceCall{name: name, args: append([]string(nil), args...)})
	handler := r.handler
	r.mu.Unlock()
	if handler != nil {
		return handler(name, args)
	}
	return nil, errors.New("unexpected systemctl call")
}

func (r *serviceRunner) recorded() []serviceCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]serviceCall(nil), r.calls...)
}

const serviceShowOutput = `ActiveState=active
SubState=running
UnitFileState=enabled
LoadState=loaded
`

func serviceActionCommand(jobID, unit string, action agentv1.ServiceActionType) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd: &agentv1.CoreMsg_ServiceAction{
			ServiceAction: &agentv1.ServiceAction{Unit: unit, Action: action},
		},
	}
}

func TestServiceActionArgv(t *testing.T) {
	tests := []struct {
		action agentv1.ServiceActionType
		verb   string
	}{
		{action: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_START, verb: "start"},
		{action: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_STOP, verb: "stop"},
		{action: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RESTART, verb: "restart"},
		{action: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RELOAD, verb: "reload"},
		{action: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_ENABLE, verb: "enable"},
		{action: agentv1.ServiceActionType_SERVICE_ACTION_TYPE_DISABLE, verb: "disable"},
	}
	for _, tt := range tests {
		t.Run(tt.verb, func(t *testing.T) {
			runner := &serviceRunner{handler: func(_ string, args []string) ([]byte, error) {
				if len(args) > 0 && args[0] == "show" {
					return []byte(serviceShowOutput), nil
				}
				return nil, nil
			}}
			manager := NewServiceManager(runner)

			output, err := manager.Do(context.Background(), "docker.service", tt.action)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			var state serviceState
			if err := json.Unmarshal([]byte(output), &state); err != nil {
				t.Fatalf("decode state: %v", err)
			}
			if state.Unit != "docker.service" || state.ActiveState != "active" ||
				state.SubState != "running" || state.UnitFileState != "enabled" ||
				state.LoadState != "loaded" {
				t.Errorf("state = %+v", state)
			}

			calls := runner.recorded()
			if len(calls) != 2 {
				t.Fatalf("calls = %v, want the action and show", calls)
			}
			wantAction := []string{"--no-ask-password", tt.verb, "--", "docker.service"}
			if calls[0].name != "systemctl" || !sameArgs(calls[0].args, wantAction) {
				t.Errorf("action call = %v %v, want %v", calls[0].name, calls[0].args, wantAction)
			}
			wantShow := []string{"show", "-p", "ActiveState,SubState,UnitFileState,LoadState", "--", "docker.service"}
			if calls[1].name != "systemctl" || !sameArgs(calls[1].args, wantShow) {
				t.Errorf("show call = %v %v, want %v", calls[1].name, calls[1].args, wantShow)
			}
		})
	}
}

func TestServiceActionRejectsInvalidUnits(t *testing.T) {
	invalid := []string{
		"",
		"docker",
		"docker.socket",
		"-docker.service",
		"/etc/systemd/system/docker.service",
		"docker service",
		"docker.service;rm",
		"../docker.service",
		"docker\n.service",
		strings.Repeat("a", 251) + ".service",
	}
	for _, unit := range invalid {
		t.Run(unit, func(t *testing.T) {
			runner := &serviceRunner{}
			if _, err := NewServiceManager(runner).Do(context.Background(), unit, agentv1.ServiceActionType_SERVICE_ACTION_TYPE_START); err == nil {
				t.Errorf("unit %q was accepted", unit)
			}
			if calls := runner.recorded(); len(calls) != 0 {
				t.Errorf("invalid unit reached systemctl: %v", calls)
			}
		})
	}
}

func TestServiceActionRejectsManagingTheAgent(t *testing.T) {
	for _, action := range []agentv1.ServiceActionType{
		agentv1.ServiceActionType_SERVICE_ACTION_TYPE_STOP,
		agentv1.ServiceActionType_SERVICE_ACTION_TYPE_RESTART,
		agentv1.ServiceActionType_SERVICE_ACTION_TYPE_DISABLE,
	} {
		t.Run(action.String(), func(t *testing.T) {
			runner := &serviceRunner{}
			_, err := NewServiceManager(runner).Do(context.Background(), "talaia-agent.service", action)
			if err == nil || err.Error() != "talaia-agent cannot manage itself; update it with an agent update" {
				t.Errorf("error = %v, want the self-management refusal", err)
			}
			if calls := runner.recorded(); len(calls) != 0 {
				t.Errorf("the agent unit reached systemctl: %v", calls)
			}
		})
	}
}

func TestServiceActionReportsSystemctlErrors(t *testing.T) {
	t.Run("action", func(t *testing.T) {
		runner := &serviceRunner{handler: func(_ string, args []string) ([]byte, error) {
			if len(args) > 0 && args[0] == "show" {
				t.Fatal("show ran after a failed action")
			}
			return []byte("Failed to start docker.service: Unit docker.service not found.\n"), errors.New("exit status 5")
		}}
		_, err := NewServiceManager(runner).Do(context.Background(), "docker.service", agentv1.ServiceActionType_SERVICE_ACTION_TYPE_START)
		if err == nil || !strings.Contains(err.Error(), "systemctl start") ||
			!strings.Contains(err.Error(), "Unit docker.service not found") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("show", func(t *testing.T) {
		runner := &serviceRunner{handler: func(_ string, args []string) ([]byte, error) {
			if len(args) > 0 && args[0] == "show" {
				return []byte("Failed to get properties: Unit not found"), errors.New("exit status 4")
			}
			return nil, nil
		}}
		_, err := NewServiceManager(runner).Do(context.Background(), "docker.service", agentv1.ServiceActionType_SERVICE_ACTION_TYPE_START)
		if err == nil || !strings.Contains(err.Error(), "systemctl show") ||
			!strings.Contains(err.Error(), "Failed to get properties") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("trimmed", func(t *testing.T) {
		detail := strings.Repeat("x", serviceErrorLimit+100)
		runner := &serviceRunner{handler: func(string, []string) ([]byte, error) {
			return []byte(detail), errors.New("exit status 1")
		}}
		_, err := NewServiceManager(runner).Do(context.Background(), "docker.service", agentv1.ServiceActionType_SERVICE_ACTION_TYPE_START)
		if err == nil || strings.Contains(err.Error(), detail) || len(err.Error()) > serviceErrorLimit+50 {
			t.Errorf("error was not trimmed: %d bytes", len(err.Error()))
		}
	})
}

func TestParseServiceState(t *testing.T) {
	state := parseServiceState([]byte("LoadState=loaded\nActiveState=inactive\nUnknown=value\nSubState=dead\nUnitFileState=disabled\n"), "nginx.service")
	if state.Unit != "nginx.service" || state.ActiveState != "inactive" ||
		state.SubState != "dead" || state.UnitFileState != "disabled" ||
		state.LoadState != "loaded" {
		t.Errorf("state = %+v", state)
	}
}

func TestHandleRunsServiceActions(t *testing.T) {
	services := &fakeServiceManager{output: `{"unit":"docker.service"}`}
	out := newCollector()
	executor := New(&fakeActions{}).WithServices(services)

	if !executor.Handle(context.Background(), serviceActionCommand("job-svc-1", "docker.service", agentv1.ServiceActionType_SERVICE_ACTION_TYPE_START), out.emit) {
		t.Fatal("the service action command was not handled")
	}
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_RUNNING {
		t.Errorf("first event = %v", got)
	}
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || got.GetOutput() != services.output {
		t.Errorf("final event = %v", got)
	}
	if len(services.calls) != 1 || services.calls[0] != "docker.service:SERVICE_ACTION_TYPE_START" {
		t.Errorf("calls = %v", services.calls)
	}
}

func TestServiceActionFailsWithoutAManager(t *testing.T) {
	out := newCollector()
	New(&fakeActions{}).Handle(context.Background(), serviceActionCommand("job-svc-2", "docker.service", agentv1.ServiceActionType_SERVICE_ACTION_TYPE_STOP), out.emit)
	out.next(t)
	got := out.next(t)
	if got.GetState() != agentv1.JobState_JOB_STATE_FAILED || got.GetMessage() != errNoServices.Error() {
		t.Errorf("final event = %v", got)
	}
}

type fakeServiceManager struct {
	mu     sync.Mutex
	calls  []string
	output string
	err    error
}

func (f *fakeServiceManager) Do(_ context.Context, unit string, action agentv1.ServiceActionType) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, unit+":"+action.String())
	return f.output, f.err
}

func sameArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

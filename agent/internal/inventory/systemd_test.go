package inventory

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeRunner struct {
	output  []byte
	err     error
	handler func(name string, args []string) ([]byte, error)
	calls   [][]string
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	if f.handler != nil {
		return f.handler(name, args)
	}
	return f.output, f.err
}

const systemdJSON = `[
  {"unit":"docker.service","load":"loaded","active":"active","sub":"running","description":"Docker Application Container Engine"},
  {"unit":"nginx.service","load":"loaded","active":"inactive","sub":"dead","description":"A high performance web server and a reverse proxy server"},
  {"unit":"ssh.service","load":"loaded","active":"failed","sub":"failed","description":"OpenBSD Secure Shell server"}
]`

const systemdUnitFilesJSON = `[
  {"unit_file":"docker.service","state":"enabled"},
  {"unit_file":"nginx.service","state":"disabled"},
  {"unit_file":"ssh.service","state":"static"}
]`

const canReloadShow = `Id=docker.service
CanReload=yes

Id=nginx.service
CanReload=no

Id=ssh.service
CanReload=yes
`

func TestSystemdUnits(t *testing.T) {
	runner := &fakeRunner{handler: func(_ string, args []string) ([]byte, error) {
		if len(args) > 0 && args[0] == "list-unit-files" {
			return []byte(systemdUnitFilesJSON), nil
		}
		if len(args) > 0 && args[0] == "show" {
			return []byte(canReloadShow), nil
		}
		return []byte(systemdJSON), nil
	}}
	systemd := NewSystemdWithRunner(runner)

	units := systemd.Units(context.Background())

	if len(units) != 3 {
		t.Fatalf("units = %d, want 3", len(units))
	}

	// Sorted by name: docker, nginx, ssh.
	if units[0].GetName() != "docker.service" {
		t.Errorf("first unit = %q, want docker.service", units[0].GetName())
	}
	if units[0].GetActiveState() != "active" || units[0].GetSubState() != "running" || units[0].GetLoadState() != "loaded" {
		t.Errorf("docker.service states = %+v", units[0])
	}
	if units[0].GetUnitFileState() != "enabled" || units[1].GetUnitFileState() != "disabled" || units[2].GetUnitFileState() != "static" {
		t.Errorf("unit file states = %q, %q, %q", units[0].GetUnitFileState(), units[1].GetUnitFileState(), units[2].GetUnitFileState())
	}
	if units[0].GetDescription() != "Docker Application Container Engine" {
		t.Errorf("description = %q", units[0].GetDescription())
	}
	if units[2].GetName() != "ssh.service" || units[2].GetActiveState() != "failed" {
		t.Errorf("last unit = %+v", units[2])
	}

	if !units[0].GetCanReload() || units[1].GetCanReload() || !units[2].GetCanReload() {
		t.Errorf("can_reload = %v, %v, %v, want true, false, true", units[0].GetCanReload(), units[1].GetCanReload(), units[2].GetCanReload())
	}

	if len(runner.calls) != 3 {
		t.Fatalf("systemctl calls = %d, want 3", len(runner.calls))
	}
	wantShow := []string{"systemctl", "show", "-p", "Id,CanReload", "--", "docker.service", "nginx.service", "ssh.service"}
	if strings.Join(runner.calls[2], " ") != strings.Join(wantShow, " ") {
		t.Fatalf("systemctl show args = %v, want %v", runner.calls[2], wantShow)
	}
	want := []string{"systemctl", "list-units", "--type=service", "--all", "--no-pager", "--output=json"}
	if len(runner.calls[0]) != len(want) {
		t.Fatalf("systemctl args = %v, want %v", runner.calls[0], want)
	}
	for i := range want {
		if runner.calls[0][i] != want[i] {
			t.Fatalf("systemctl args = %v, want %v", runner.calls[0], want)
		}
	}
	wantFiles := []string{"systemctl", "list-unit-files", "--type=service", "--no-pager", "--output=json"}
	if strings.Join(runner.calls[1], " ") != strings.Join(wantFiles, " ") {
		t.Fatalf("systemctl unit-file args = %v, want %v", runner.calls[1], wantFiles)
	}
}

func TestSystemdUsesTheTemplateUnitFileState(t *testing.T) {
	unitsJSON := `[
	  {"unit":"getty@tty1.service","load":"loaded","active":"active","sub":"running","description":"Getty"},
	  {"unit":"custom@one.service","load":"loaded","active":"inactive","sub":"dead","description":"Custom"},
	  {"unit":"plain.service","load":"loaded","active":"inactive","sub":"dead","description":"Plain"}
	]`
	filesJSON := `[
	  {"unit_file":"getty@.service","state":"enabled"},
	  {"unit_file":"custom@.service","state":"disabled"},
	  {"unit_file":"custom@one.service","state":"masked"}
	]`
	runner := &fakeRunner{handler: func(_ string, args []string) ([]byte, error) {
		if len(args) > 0 && args[0] == "list-unit-files" {
			return []byte(filesJSON), nil
		}
		return []byte(unitsJSON), nil
	}}

	units := NewSystemdWithRunner(runner).Units(context.Background())
	states := make(map[string]string, len(units))
	for _, unit := range units {
		states[unit.GetName()] = unit.GetUnitFileState()
	}

	if states["getty@tty1.service"] != "enabled" {
		t.Errorf("getty@tty1 state = %q, want the template state", states["getty@tty1.service"])
	}
	if states["custom@one.service"] != "masked" {
		t.Errorf("custom@one state = %q, want its own state", states["custom@one.service"])
	}
	if states["plain.service"] != "" {
		t.Errorf("plain state = %q, want empty", states["plain.service"])
	}
}

func TestSystemdKeepsUnitsWhenUnitFilesFail(t *testing.T) {
	runner := &fakeRunner{handler: func(_ string, args []string) ([]byte, error) {
		if len(args) > 0 && args[0] == "list-unit-files" {
			return nil, errors.New("list-unit-files failed")
		}
		return []byte(systemdJSON), nil
	}}

	units := NewSystemdWithRunner(runner).Units(context.Background())
	if len(units) != 3 {
		t.Fatalf("units = %d, want 3", len(units))
	}
	for _, unit := range units {
		if unit.GetUnitFileState() != "" {
			t.Errorf("%s state = %q, want empty", unit.GetName(), unit.GetUnitFileState())
		}
	}
}

func TestSystemdSkipsNonServices(t *testing.T) {
	runner := &fakeRunner{output: []byte(`[
	  {"unit":"docker.socket","load":"loaded","active":"active","sub":"running","description":"Docker Socket"},
	  {"unit":"docker.service","load":"loaded","active":"active","sub":"running","description":"Docker"}
	]`)}

	units := NewSystemdWithRunner(runner).Units(context.Background())

	if len(units) != 1 || units[0].GetName() != "docker.service" {
		t.Fatalf("units = %+v, want only docker.service", units)
	}
}

func TestSystemdUnavailable(t *testing.T) {
	tests := []struct {
		name   string
		runner *fakeRunner
	}{
		{name: "command fails", runner: &fakeRunner{err: errors.New("systemctl not found")}},
		{name: "invalid json", runner: &fakeRunner{output: []byte("not json")}},
		{name: "empty output", runner: &fakeRunner{output: nil}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			units := NewSystemdWithRunner(tt.runner).Units(context.Background())
			if len(units) != 0 {
				t.Fatalf("units = %+v, want an empty list", units)
			}
		})
	}
}

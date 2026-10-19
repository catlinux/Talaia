package inventory

import (
	"context"
	"errors"
	"testing"
)

type fakeRunner struct {
	output []byte
	err    error
	calls  [][]string
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.output, f.err
}

const systemdJSON = `[
  {"unit":"docker.service","load":"loaded","active":"active","sub":"running","description":"Docker Application Container Engine"},
  {"unit":"nginx.service","load":"loaded","active":"inactive","sub":"dead","description":"A high performance web server and a reverse proxy server"},
  {"unit":"ssh.service","load":"loaded","active":"failed","sub":"failed","description":"OpenBSD Secure Shell server"}
]`

func TestSystemdUnits(t *testing.T) {
	runner := &fakeRunner{output: []byte(systemdJSON)}
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
	if units[0].GetDescription() != "Docker Application Container Engine" {
		t.Errorf("description = %q", units[0].GetDescription())
	}
	if units[2].GetName() != "ssh.service" || units[2].GetActiveState() != "failed" {
		t.Errorf("last unit = %+v", units[2])
	}

	if len(runner.calls) != 1 {
		t.Fatalf("systemctl calls = %d, want 1", len(runner.calls))
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

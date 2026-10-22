package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/catlinux/Talaia/agent/internal/apt"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	upgradeSimulation = `Inst openssl [3.0.15-1] (3.0.16-1 Debian-Security:13/stable-security [amd64])
Inst curl [8.10.1-1] (8.12.1-1 Debian:13/stable [amd64])
`
	noSecuritySimulation = "Reading package lists...\n0 upgraded, 0 newly installed.\n"

	unitSucceeded = "ActiveState=active\nSubState=exited\nResult=success\nExecMainStatus=0\n"
	unitFailed    = "ActiveState=failed\nSubState=failed\nResult=exit-code\nExecMainStatus=100\n"
)

type aptSimRunner struct {
	output string
}

func (r aptSimRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	if name == "apt-get" && len(args) > 0 && args[0] == "-s" {
		return []byte(r.output), nil
	}
	return nil, errors.New("unexpected apt command")
}

func newUpgradeChecker(t *testing.T, simulation string) *apt.Checker {
	t.Helper()
	return apt.New(apt.Options{
		Runner: aptSimRunner{output: simulation},
		Root:   t.TempDir(),
		AptGet: "apt-get",
		Now:    func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) },
	})
}

type recordedCommand struct {
	name string
	args []string
}

type systemRecorder struct {
	mu       sync.Mutex
	calls    []recordedCommand
	onRun    func(name string, args []string) error
	onOutput func(name string, args []string) ([]byte, error)
}

func (f *systemRecorder) Run(_ context.Context, name string, args ...string) error {
	f.mu.Lock()
	f.calls = append(f.calls, recordedCommand{name: name, args: append([]string(nil), args...)})
	onRun := f.onRun
	f.mu.Unlock()
	if onRun != nil {
		return onRun(name, args)
	}
	return nil
}

func (f *systemRecorder) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, recordedCommand{name: name, args: append([]string(nil), args...)})
	onOutput := f.onOutput
	f.mu.Unlock()
	if onOutput != nil {
		return onOutput(name, args)
	}
	return nil, errors.New("unexpected command")
}

func (f *systemRecorder) recorded() []recordedCommand {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedCommand(nil), f.calls...)
}

func (f *systemRecorder) first(name string) (recordedCommand, bool) {
	for _, call := range f.recorded() {
		if call.name == name {
			return call, true
		}
	}
	return recordedCommand{}, false
}

func newUpgradeManager(t *testing.T, checker *apt.Checker, runner CommandRunner) *AptManager {
	t.Helper()
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	return NewAptManager(AptManagerOptions{
		Checker:       checker,
		Dir:           t.TempDir(),
		Runner:        runner,
		SystemdRun:    "systemd-run",
		Systemctl:     "systemctl",
		AptGet:        "apt-get",
		Now:           func() time.Time { return now },
		Hostname:      func() (string, error) { return "vps-web-01", nil },
		PollInterval:  time.Millisecond,
		EventInterval: time.Millisecond,
	})
}

func TestUpgradeCommandLineForEachMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     agentv1.AptUpgradeMode
		packages []string
		want     []string
	}{
		{
			name: "all",
			mode: agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL,
			want: []string{"--no-remove", "upgrade", "--with-new-pkgs"},
		},
		{
			name:     "security",
			mode:     agentv1.AptUpgradeMode_APT_UPGRADE_MODE_SECURITY,
			packages: []string{"openssl", "libc6:amd64"},
			want:     []string{"--no-remove", "install", "--only-upgrade", "--", "openssl", "libc6:amd64"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &systemRecorder{}
			manager := newUpgradeManager(t, nil, runner)
			logPath := filepath.Join(manager.dir, "job.log")

			if err := manager.startUnit(context.Background(), "talaia-apt-jobtest", logPath, tt.mode, tt.packages); err != nil {
				t.Fatalf("startUnit: %v", err)
			}

			call, ok := runner.first("systemd-run")
			if !ok {
				t.Fatal("systemd-run was not called")
			}
			for _, arg := range tt.want {
				if !slices.Contains(call.args, arg) {
					t.Errorf("args = %v, want %q", call.args, arg)
				}
			}
			for _, forbidden := range []string{"full-upgrade", "dist-upgrade", "autoremove"} {
				if slices.Contains(call.args, forbidden) {
					t.Errorf("args = %v, must not contain %q", call.args, forbidden)
				}
			}
			for _, property := range []string{
				"--collect",
				"--property=Type=exec",
				"--property=RemainAfterExit=yes",
				"--property=StandardOutput=append:" + logPath,
				"--property=StandardError=append:" + logPath,
				"--setenv=DEBIAN_FRONTEND=noninteractive",
				"--setenv=LC_ALL=C",
			} {
				if !slices.Contains(call.args, property) {
					t.Errorf("args = %v, want %q", call.args, property)
				}
			}
			for i, arg := range call.args {
				if arg == "--" {
					if i+3 >= len(call.args) || call.args[i+1] != stdbufPath || call.args[i+2] != "-oL" || call.args[i+3] != "-eL" || call.args[i+4] != "apt-get" {
						t.Errorf("args after -- = %v, want stdbuf -oL -eL apt-get", call.args[i:])
					}
					break
				}
			}
		})
	}
}

func TestAptUnitNameIsShortAndSafe(t *testing.T) {
	tests := map[string]string{
		"1234abcd-rest": "talaia-apt-1234abcd",
		"JOB_ID":        "talaia-apt-jobid",
		"---":           "talaia-apt-job",
		"a":             "talaia-apt-a",
	}
	for jobID, want := range tests {
		if got := aptUnitName(jobID); got != want {
			t.Errorf("aptUnitName(%q) = %q, want %q", jobID, got, want)
		}
	}
}

func TestValidatePackageNames(t *testing.T) {
	tests := []struct {
		name     string
		packages []string
		wantErr  bool
	}{
		{name: "ordinary names", packages: []string{"libc6", "linux-image-6.1.0-30-amd64", "g++", "python3.11"}},
		{name: "multiarch", packages: []string{"libc6:amd64"}},
		{name: "semicolon", packages: []string{"libc6;rm"}, wantErr: true},
		{name: "path", packages: []string{"../etc/passwd"}, wantErr: true},
		{name: "space", packages: []string{"libc 6"}, wantErr: true},
		{name: "upper case", packages: []string{"Libc6"}, wantErr: true},
		{name: "empty architecture", packages: []string{"libc6:"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePackageNames(tt.packages)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePackageNames(%v) error = %v, wantErr %t", tt.packages, err, tt.wantErr)
			}
		})
	}
}

func TestAptUpgradeRejectsUnsafeJobIDs(t *testing.T) {
	runner := &systemRecorder{}
	manager := newUpgradeManager(t, nil, runner)
	ids := []string{"", "job/1", "job 1", "job_1", strings.Repeat("a", 65)}

	for _, jobID := range ids {
		if _, err := manager.Upgrade(context.Background(), jobID, agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL, func(*agentv1.AgentMsg) {}); err == nil || !strings.Contains(err.Error(), "invalid job id") {
			t.Errorf("Upgrade(%q) error = %v, want an invalid job id", jobID, err)
		}
		if err := manager.Reboot(context.Background(), jobID, "vps-web-01"); err == nil || !strings.Contains(err.Error(), "invalid job id") {
			t.Errorf("Reboot(%q) error = %v, want an invalid job id", jobID, err)
		}
	}
	if calls := runner.recorded(); len(calls) != 0 {
		t.Errorf("unsafe job ids reached the command runner: %v", calls)
	}
}

func TestSecurityUpgradeUsesOnlyTheComputedPackages(t *testing.T) {
	checker := newUpgradeChecker(t, upgradeSimulation)
	runner := &systemRecorder{onOutput: func(string, []string) ([]byte, error) {
		return []byte(unitSucceeded), nil
	}}
	manager := newUpgradeManager(t, checker, runner)

	output, err := manager.Upgrade(context.Background(), "job-sec-1", agentv1.AptUpgradeMode_APT_UPGRADE_MODE_SECURITY, func(*agentv1.AgentMsg) {})
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	if output != "" {
		t.Errorf("output = %q, want no log", output)
	}
	call, ok := runner.first("systemd-run")
	if !ok {
		t.Fatal("systemd-run was not called")
	}
	if !slices.Contains(call.args, "openssl") {
		t.Errorf("args = %v, want openssl", call.args)
	}
	if slices.Contains(call.args, "curl") {
		t.Errorf("args = %v, must not include the non-security curl", call.args)
	}
	if _, err := os.Stat(manager.statePath("job-sec-1")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state file still exists: %v", err)
	}
}

func TestSecurityUpgradeWithNothingToUpgrade(t *testing.T) {
	checker := newUpgradeChecker(t, noSecuritySimulation)
	runner := &systemRecorder{}
	manager := newUpgradeManager(t, checker, runner)

	output, err := manager.Upgrade(context.Background(), "job-sec-2", agentv1.AptUpgradeMode_APT_UPGRADE_MODE_SECURITY, func(*agentv1.AgentMsg) {})
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	if output != "nothing to upgrade" {
		t.Errorf("output = %q, want nothing to upgrade", output)
	}
	if calls := runner.recorded(); len(calls) != 0 {
		t.Errorf("system commands were called: %v", calls)
	}
	if _, err := os.Stat(manager.statePath("job-sec-2")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state file exists: %v", err)
	}
}

func TestAptUpgradeOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		err     error
		wantErr string
	}{
		{name: "success", output: unitSucceeded},
		{name: "exit code", output: unitFailed, wantErr: "apt-get exited with status 100"},
		{name: "unit gone", err: errors.New("unit not found"), wantErr: "read the apt unit state"},
		{name: "empty unit state", output: "", wantErr: "apt unit is gone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := newUpgradeChecker(t, noSecuritySimulation)
			runner := &systemRecorder{onOutput: func(string, []string) ([]byte, error) {
				return []byte(tt.output), tt.err
			}}
			manager := newUpgradeManager(t, checker, runner)

			output, err := manager.Upgrade(context.Background(), "job-all-1", agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL, func(*agentv1.AgentMsg) {})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Upgrade: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Upgrade error = %v, want %q", err, tt.wantErr)
			}
			if output != "" {
				t.Errorf("output = %q, want empty", output)
			}
			if _, err := os.Stat(manager.statePath("job-all-1")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("state file still exists: %v", err)
			}
			if _, ok := runner.first("systemd-run"); !ok {
				t.Error("systemd-run was not called")
			}
		})
	}
}

func TestAptLogTailIsBounded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "job.log")
	data := strings.Repeat("a", 70<<10)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewAptManager(AptManagerOptions{
		Dir:          dir,
		Runner:       &systemRecorder{},
		LogTailBytes: 64 << 10,
	})

	tail := manager.logTail(path)
	if len(tail) > 64<<10 {
		t.Errorf("tail is %d bytes, want at most %d", len(tail), 64<<10)
	}
	if tail != data[len(data)-(64<<10):] {
		t.Error("the tail is not the end of the log")
	}
}

func TestAptUpgradeHasOneOperationAtATime(t *testing.T) {
	started := make(chan struct{}, 1)
	runner := &systemRecorder{
		onRun: func(name string, args []string) error {
			if name == "systemd-run" {
				select {
				case started <- struct{}{}:
				default:
				}
			}
			return nil
		},
		onOutput: func(string, []string) ([]byte, error) {
			return []byte("ActiveState=active\nSubState=running\nResult=\nExecMainStatus=0\n"), nil
		},
	}
	checker := newUpgradeChecker(t, noSecuritySimulation)
	manager := newUpgradeManager(t, checker, runner)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := manager.Upgrade(ctx, "job-running", agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL, func(*agentv1.AgentMsg) {})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the first upgrade did not start")
	}

	if _, err := manager.Upgrade(context.Background(), "job-second", agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL, func(*agentv1.AgentMsg) {}); !errors.Is(err, errAptBusy) {
		t.Errorf("second upgrade error = %v, want %v", err, errAptBusy)
	}
	if err := manager.Reboot(context.Background(), "job-reboot", "vps-web-01"); !errors.Is(err, errAptBusy) {
		t.Errorf("reboot error = %v, want %v", err, errAptBusy)
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("first upgrade error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the first upgrade did not stop")
	}
}

func TestHostReboot(t *testing.T) {
	t.Run("schedules the reboot after a matching hostname", func(t *testing.T) {
		runner := &systemRecorder{}
		manager := newUpgradeManager(t, nil, runner)

		if err := manager.Reboot(context.Background(), "job-reboot-1", " VPS-WEB-01 "); err != nil {
			t.Fatalf("Reboot: %v", err)
		}
		call, ok := runner.first("systemd-run")
		if !ok {
			t.Fatal("systemd-run was not called")
		}
		for _, arg := range []string{"--on-active=5", "--unit=talaia-reboot", "systemctl", "reboot"} {
			if !slices.Contains(call.args, arg) {
				t.Errorf("args = %v, want %q", call.args, arg)
			}
		}
	})

	t.Run("refuses another hostname", func(t *testing.T) {
		runner := &systemRecorder{}
		manager := newUpgradeManager(t, nil, runner)

		err := manager.Reboot(context.Background(), "job-reboot-2", "another-server")
		if err == nil || err.Error() != "hostname mismatch" {
			t.Errorf("error = %v, want hostname mismatch", err)
		}
		if calls := runner.recorded(); len(calls) != 0 {
			t.Errorf("wrong hostname scheduled a command: %v", calls)
		}
	})

	t.Run("reports a schedule failure", func(t *testing.T) {
		runner := &systemRecorder{onRun: func(string, []string) error {
			return errors.New("systemd-run failed")
		}}
		manager := newUpgradeManager(t, nil, runner)

		err := manager.Reboot(context.Background(), "job-reboot-3", "vps-web-01")
		if err == nil || !strings.Contains(err.Error(), "schedule the reboot") {
			t.Errorf("error = %v, want a schedule failure", err)
		}
	})
}

const unitRunning = "ActiveState=active\nSubState=running\nResult=\nExecMainStatus=0\n"

// waitGone waits until the state file of jobID is removed, which happens when
// the follower of its unit has reported the result.
func waitGone(t *testing.T, manager *AptManager, jobID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(manager.statePath(jobID)); errors.Is(err, os.ErrNotExist) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the state file of %s was not removed", jobID)
}

// A reboot must never interrupt dpkg: while an upgrade left by an earlier agent
// process may still run, the reboot is refused and that unit is followed.
func TestRebootWaitsForAnUpgradeLeftOnDisk(t *testing.T) {
	var finished atomic.Bool
	runner := &systemRecorder{onOutput: func(string, []string) ([]byte, error) {
		if finished.Load() {
			return []byte(unitSucceeded), nil
		}
		return []byte(unitRunning), nil
	}}
	manager := newUpgradeManager(t, newUpgradeChecker(t, noSecuritySimulation), runner)
	if err := manager.ensureDir(); err != nil {
		t.Fatal(err)
	}
	state := aptUpgradeState{JobID: "job-left-1", Unit: "talaia-apt-jobleft1", Mode: "ALL", StartedAt: time.Now().UTC()}
	if err := manager.writeState(state); err != nil {
		t.Fatal(err)
	}

	if err := manager.Reboot(context.Background(), "job-reboot-left", "vps-web-01"); !errors.Is(err, errAptBusy) {
		t.Fatalf("Reboot during an upgrade = %v, want errAptBusy", err)
	}
	if _, ran := runner.first("systemd-run"); ran {
		t.Fatal("a reboot was scheduled during an upgrade")
	}

	finished.Store(true)
	waitGone(t, manager, state.JobID)
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := manager.Reboot(context.Background(), "job-reboot-after", "vps-web-01")
		if err == nil {
			break
		}
		if !errors.Is(err, errAptBusy) || time.Now().After(deadline) {
			t.Fatalf("Reboot after the upgrade = %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// An upgrade that outlives its job keeps the apt lock until apt-get ends.
func TestUpgradeKeepsTheLockAfterItsTimeLimit(t *testing.T) {
	var finished atomic.Bool
	runner := &systemRecorder{onOutput: func(string, []string) ([]byte, error) {
		if finished.Load() {
			return []byte(unitSucceeded), nil
		}
		return []byte(unitRunning), nil
	}}
	manager := newUpgradeManager(t, newUpgradeChecker(t, noSecuritySimulation), runner)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	// Progress is discarded: nothing reads it in this test.
	discard := func(*agentv1.AgentMsg) {}
	_, err := manager.Upgrade(ctx, "job-slow-1", agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL, discard)
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("Upgrade past its time limit = %v, want a still running error", err)
	}
	if err := manager.Reboot(context.Background(), "job-reboot-slow", "vps-web-01"); !errors.Is(err, errAptBusy) {
		t.Fatalf("Reboot while apt-get runs = %v, want errAptBusy", err)
	}

	finished.Store(true)
	waitGone(t, manager, "job-slow-1")
}

func nextFinalEvent(t *testing.T, out *collector) *agentv1.JobEvent {
	t.Helper()
	for i := 0; i < 40; i++ {
		event := out.next(t)
		if event.GetState() == agentv1.JobState_JOB_STATE_SUCCEEDED || event.GetState() == agentv1.JobState_JOB_STATE_FAILED {
			return event
		}
	}
	t.Fatal("no final event")
	return nil
}

func TestResumeFollowsActiveAndFinishedUnits(t *testing.T) {
	tests := []struct {
		name      string
		show      func(int) ([]byte, error)
		wantState agentv1.JobState
	}{
		{
			name: "active then success",
			show: func(call int) ([]byte, error) {
				if call < 2 {
					return []byte("ActiveState=active\nSubState=running\nResult=\nExecMainStatus=0\n"), nil
				}
				return []byte(unitSucceeded), nil
			},
			wantState: agentv1.JobState_JOB_STATE_SUCCEEDED,
		},
		{
			name: "already finished",
			show: func(int) ([]byte, error) {
				return []byte(unitSucceeded), nil
			},
			wantState: agentv1.JobState_JOB_STATE_SUCCEEDED,
		},
		{
			name: "finished with failure",
			show: func(int) ([]byte, error) {
				return []byte(unitFailed), nil
			},
			wantState: agentv1.JobState_JOB_STATE_FAILED,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			checker := newUpgradeChecker(t, noSecuritySimulation)
			runner := &systemRecorder{onOutput: func(string, []string) ([]byte, error) {
				return tt.show(int(calls.Add(1)))
			}}
			manager := newUpgradeManager(t, checker, runner)
			manager.now = time.Now
			if err := manager.ensureDir(); err != nil {
				t.Fatal(err)
			}
			state := aptUpgradeState{
				JobID:     "job-resume-1",
				Unit:      "talaia-apt-jobresum",
				Mode:      "ALL",
				StartedAt: time.Now().UTC(),
			}
			if err := manager.writeState(state); err != nil {
				t.Fatal(err)
			}

			out := newCollector()
			manager.Resume(context.Background(), out.emit)
			final := nextFinalEvent(t, out)
			if final.GetState() != tt.wantState {
				t.Errorf("final = %v, want %v", final.GetState(), tt.wantState)
			}
			if _, err := os.Stat(manager.statePath(state.JobID)); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("state file still exists: %v", err)
			}
		})
	}
}

func TestResumeEmitsOnTheLatestStream(t *testing.T) {
	var calls atomic.Int32
	checker := newUpgradeChecker(t, noSecuritySimulation)
	runner := &systemRecorder{onOutput: func(string, []string) ([]byte, error) {
		if calls.Add(1) <= 5 {
			return []byte("ActiveState=active\nSubState=running\nResult=\nExecMainStatus=0\n"), nil
		}
		return []byte(unitSucceeded), nil
	}}
	manager := newUpgradeManager(t, checker, runner)
	manager.now = time.Now
	if err := manager.ensureDir(); err != nil {
		t.Fatal(err)
	}
	state := aptUpgradeState{
		JobID:     "job-resume-live",
		Unit:      "talaia-apt-jobresum",
		Mode:      "ALL",
		StartedAt: time.Now().UTC(),
	}
	if err := manager.writeState(state); err != nil {
		t.Fatal(err)
	}

	first := newCollector()
	second := newCollector()
	manager.Resume(context.Background(), first.emit)
	manager.Resume(context.Background(), second.emit)

	final := nextFinalEvent(t, second)
	if final.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED {
		t.Errorf("final = %v, want success", final)
	}
}

func TestResumeCleansStaleFiles(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	manager := NewAptManager(AptManagerOptions{
		Dir:          dir,
		Runner:       &systemRecorder{},
		Now:          func() time.Time { return now },
		PollInterval: time.Millisecond,
	})
	if err := manager.ensureDir(); err != nil {
		t.Fatal(err)
	}
	oldState := filepath.Join(dir, "old.json")
	oldLog := filepath.Join(dir, "old.log")
	if err := os.WriteFile(oldState, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldLog, []byte("old log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldState, now.Add(-25*time.Hour), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldLog, now.Add(-31*24*time.Hour), now.Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}

	manager.Resume(context.Background(), func(*agentv1.AgentMsg) {})

	for _, path := range []string{oldState, oldLog} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("stale file %s still exists: %v", path, err)
		}
	}
}

type fakeAptOperations struct {
	mu       sync.Mutex
	upgrades []string
	reboots  []string
	output   string
	err      error
}

func (f *fakeAptOperations) Upgrade(_ context.Context, jobID string, mode agentv1.AptUpgradeMode, _ func(*agentv1.AgentMsg)) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upgrades = append(f.upgrades, jobID+":"+mode.String())
	return f.output, f.err
}

func (f *fakeAptOperations) Reboot(_ context.Context, jobID, hostname string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reboots = append(f.reboots, jobID+":"+hostname)
	return f.err
}

func (f *fakeAptOperations) Resume(context.Context, func(*agentv1.AgentMsg)) {}

func aptUpgradeCommand(jobID string, mode agentv1.AptUpgradeMode) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd:   &agentv1.CoreMsg_AptUpgrade{AptUpgrade: &agentv1.AptUpgrade{Mode: mode}},
	}
}

func hostRebootCommand(jobID, hostname string) *agentv1.CoreMsg {
	return &agentv1.CoreMsg{
		JobId: jobID,
		Cmd:   &agentv1.CoreMsg_HostReboot{HostReboot: &agentv1.HostReboot{Hostname: hostname}},
	}
}

func TestHandleRunsAptOperations(t *testing.T) {
	t.Run("apt upgrade", func(t *testing.T) {
		ops := &fakeAptOperations{output: "upgraded"}
		out := newCollector()
		executor := New(&fakeActions{}).WithAptOperations(ops)

		if !executor.Handle(context.Background(), aptUpgradeCommand("job-up-1", agentv1.AptUpgradeMode_APT_UPGRADE_MODE_SECURITY), out.emit) {
			t.Fatal("the apt upgrade command was not handled")
		}
		if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_RUNNING {
			t.Errorf("first event = %v", got)
		}
		if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || got.GetOutput() != "upgraded" {
			t.Errorf("final event = %v", got)
		}
		if len(ops.upgrades) != 1 || ops.upgrades[0] != "job-up-1:"+agentv1.AptUpgradeMode_APT_UPGRADE_MODE_SECURITY.String() {
			t.Errorf("upgrades = %v", ops.upgrades)
		}
	})

	t.Run("host reboot", func(t *testing.T) {
		ops := &fakeAptOperations{}
		out := newCollector()
		executor := New(&fakeActions{}).WithAptOperations(ops)

		if !executor.Handle(context.Background(), hostRebootCommand("job-reboot-1", "vps-web-01"), out.emit) {
			t.Fatal("the host reboot command was not handled")
		}
		out.next(t)
		if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED {
			t.Errorf("final event = %v", got)
		}
		if len(ops.reboots) != 1 || ops.reboots[0] != "job-reboot-1:vps-web-01" {
			t.Errorf("reboots = %v", ops.reboots)
		}
	})
}

func TestHandleRejectsUnsafeJobIDsForAptOperations(t *testing.T) {
	tests := []struct {
		name string
		msg  *agentv1.CoreMsg
	}{
		{name: "upgrade", msg: aptUpgradeCommand("bad/id", agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL)},
		{name: "reboot", msg: hostRebootCommand("bad/id", "vps-web-01")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops := &fakeAptOperations{}
			out := newCollector()
			executor := New(&fakeActions{}).WithAptOperations(ops)

			executor.Handle(context.Background(), tt.msg, out.emit)
			out.next(t) // running
			got := out.next(t)
			if got.GetState() != agentv1.JobState_JOB_STATE_FAILED || !strings.Contains(got.GetMessage(), "invalid job id") {
				t.Errorf("final event = %v, want an invalid job id failure", got)
			}
			if len(ops.upgrades) != 0 || len(ops.reboots) != 0 {
				t.Errorf("unsafe job id reached the manager: %v %v", ops.upgrades, ops.reboots)
			}
		})
	}
}

func TestAptOperationsFailWithoutAManager(t *testing.T) {
	tests := []struct {
		name string
		msg  *agentv1.CoreMsg
	}{
		{name: "upgrade", msg: aptUpgradeCommand("job-no-ops-1", agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL)},
		{name: "reboot", msg: hostRebootCommand("job-no-ops-2", "vps-web-01")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := newCollector()
			New(&fakeActions{}).Handle(context.Background(), tt.msg, out.emit)
			out.next(t)
			got := out.next(t)
			if got.GetState() != agentv1.JobState_JOB_STATE_FAILED || got.GetMessage() != errNoAptOperations.Error() {
				t.Errorf("final event = %v", got)
			}
		})
	}
}

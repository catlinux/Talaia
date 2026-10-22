package apt

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const debian12Output = `NOTE: This is only a simulation!
Reading package lists...
Reading state information...
Calculating upgrade...
The following packages will be upgraded:
  libc6 nginx
2 upgraded, 1 newly installed.
Inst libc6 [2.36-9+deb12u4] (2.36-9+deb12u7 Debian-Security:12/stable-security [amd64])
Inst nginx [1.22.1-9] (1.22.1-9+deb12u1 Debian:12.7/stable [amd64])
Inst linux-image-6.1.0-30-amd64 (6.1.0-30-1 Debian:12.7/stable [amd64])
Conf libc6 (2.36-9+deb12u7 Debian-Security:12/stable-security [amd64])
Remv an-old-package [1.0]
Inst malformed line without parentheses
Inst Bad;Name [1] (2 Debian:12/stable [amd64])
`

const debian13Output = `Reading package lists...
Inst libssl3t64 [3.5.1-1] (3.5.1-1+deb13u1 Debian-Security:13/stable-security [amd64])
Inst podman [5.4.1+ds1-1] (5.4.1+ds1-1+deb13u1 Debian:13.1/stable [amd64])
`

type runnerCall struct {
	name string
	args []string
}

type fakeRunner struct {
	mu      sync.Mutex
	calls   []runnerCall
	handler func(name string, args []string) ([]byte, error)
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, runnerCall{name: name, args: append([]string(nil), args...)})
	f.mu.Unlock()
	if f.handler == nil {
		return nil, errors.New("unexpected command")
	}
	return f.handler(name, args)
}

func TestParseInst(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []Package
	}{
		{
			name:   "debian 12 mix",
			output: debian12Output,
			want: []Package{
				{Name: "libc6", Current: "2.36-9+deb12u4", Candidate: "2.36-9+deb12u7", Origin: "Debian-Security:12/stable-security", Security: true},
				{Name: "nginx", Current: "1.22.1-9", Candidate: "1.22.1-9+deb12u1", Origin: "Debian:12.7/stable"},
				{Name: "linux-image-6.1.0-30-amd64", Candidate: "6.1.0-30-1", Origin: "Debian:12.7/stable"},
			},
		},
		{
			name:   "debian 13 security",
			output: debian13Output,
			want: []Package{
				{Name: "libssl3t64", Current: "3.5.1-1", Candidate: "3.5.1-1+deb13u1", Origin: "Debian-Security:13/stable-security", Security: true},
				{Name: "podman", Current: "5.4.1+ds1-1", Candidate: "5.4.1+ds1-1+deb13u1", Origin: "Debian:13.1/stable"},
			},
		},
		{
			name:   "no updates",
			output: "Reading package lists...\n0 upgraded, 0 newly installed.\n",
			want:   []Package{},
		},
		{
			name:   "multiarch package name",
			output: `Inst libc6:amd64 [1] (2 Debian-Security:12/stable-security [amd64])`,
			want: []Package{
				{Name: "libc6:amd64", Current: "1", Candidate: "2", Origin: "Debian-Security:12/stable-security", Security: true},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseInst([]byte(tt.output)); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseInst() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestStatusIncludesPackagesHeldAndSummary(t *testing.T) {
	now := time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	root := t.TempDir()
	lists := filepath.Join(root, "var", "lib", "apt", "lists")
	if err := os.MkdirAll(lists, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTime(t, filepath.Join(lists, "deb.debian.org_debian_dists_stable_InRelease"), now.Add(-time.Hour))
	writeTime(t, filepath.Join(lists, "deb.debian.org_debian_dists_stable_Release"), now.Add(-30*time.Minute))

	runner := &fakeRunner{handler: func(name string, args []string) ([]byte, error) {
		switch {
		case name == "apt-get" && containsArg(args, "-s"):
			return []byte(debian12Output), nil
		case name == "apt-mark":
			return []byte("nginx\nlibc6\nnginx\nbad;name\n"), nil
		case name == "uname":
			return []byte("6.1.0-30-amd64\n"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}}
	checker := New(Options{
		Runner:   runner,
		Root:     root,
		AptGet:   "apt-get",
		Now:      func() time.Time { return now },
		LookPath: func(string) (string, error) { return "", errors.New("not used") },
	})

	output, err := checker.StatusJSON(context.Background(), false)
	if err != nil {
		t.Fatalf("StatusJSON: %v", err)
	}
	var status Status
	if err := json.Unmarshal([]byte(output), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if len(status.Packages) != 3 {
		t.Fatalf("packages = %v, want three", status.Packages)
	}
	if !status.Packages[0].Security || status.Packages[0].Name != "libc6" ||
		status.Packages[1].Name != "linux-image-6.1.0-30-amd64" || status.Packages[2].Name != "nginx" {
		t.Errorf("packages are not security-first by name: %+v", status.Packages)
	}
	if !reflect.DeepEqual(status.Held, []string{"libc6", "nginx"}) {
		t.Errorf("held = %v", status.Held)
	}
	if status.RebootRequired || len(status.RebootReasons) != 0 {
		t.Errorf("reboot = %t %v, want false", status.RebootRequired, status.RebootReasons)
	}
	if status.CheckedAt != now.Format(time.RFC3339) {
		t.Errorf("checked_at = %q", status.CheckedAt)
	}
	if status.ListsUpdatedAt != now.Add(-30*time.Minute).Format(time.RFC3339) {
		t.Errorf("lists_updated_at = %q", status.ListsUpdatedAt)
	}

	summary := checker.Summary()
	if summary == nil || summary.Upgradable != 3 || summary.Security != 1 || summary.RebootRequired {
		t.Errorf("summary = %+v, want 3 upgradable and 1 security", summary)
	}
}

func TestStatusRefreshRunsUpdateFirst(t *testing.T) {
	runner := &fakeRunner{handler: func(name string, args []string) ([]byte, error) {
		switch {
		case name == "apt-get" && len(args) > 0 && args[len(args)-1] == "update":
			return []byte("Hit:1 http://deb.debian.org stable InRelease\n"), nil
		case name == "apt-get" && containsArg(args, "-s"):
			return []byte(debian13Output), nil
		case name == "apt-mark":
			return nil, nil
		case name == "uname":
			return []byte("6.12.0-amd64\n"), nil
		default:
			return nil, errors.New("unexpected command")
		}
	}}
	checker := New(Options{Runner: runner, Root: t.TempDir(), AptGet: "apt-get"})

	if _, err := checker.StatusJSON(context.Background(), true); err != nil {
		t.Fatalf("StatusJSON: %v", err)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.calls) == 0 {
		t.Fatal("no commands ran")
	}
	if runner.calls[0].name != "apt-get" || !containsArg(runner.calls[0].args, "update") ||
		!containsArg(runner.calls[0].args, "DPkg::Lock::Timeout=120") {
		t.Errorf("first command = %+v, want apt-get update with the lock timeout", runner.calls[0])
	}
}

func TestStatusRefreshFailsWithTheUpdateError(t *testing.T) {
	runner := &fakeRunner{handler: func(name string, args []string) ([]byte, error) {
		if name == "apt-get" && containsArg(args, "update") {
			return nil, errors.New("Could not get lock /var/lib/dpkg/lock-frontend")
		}
		return nil, errors.New("the update should stop the job")
	}}
	checker := New(Options{Runner: runner, Root: t.TempDir(), AptGet: "apt-get"})

	_, err := checker.StatusJSON(context.Background(), true)
	if err == nil || !strings.Contains(err.Error(), "apt-get update") ||
		!strings.Contains(err.Error(), "lock") {
		t.Fatalf("error = %v, want a clear apt-get update lock error", err)
	}
}

func TestRebootRequired(t *testing.T) {
	tests := []struct {
		name        string
		flag        bool
		packages    string
		modules     []string
		running     string
		want        bool
		wantReasons []string
	}{
		{
			name:        "flag file",
			flag:        true,
			packages:    "libc6\nlinux-image-6.1.0-30-amd64\n",
			want:        true,
			wantReasons: []string{"reboot-required file: libc6, linux-image-6.1.0-30-amd64"},
		},
		{
			name:        "newer kernel",
			modules:     []string{"6.1.0-29-amd64", "6.1.0-30-amd64"},
			running:     "6.1.0-29-amd64",
			want:        true,
			wantReasons: []string{"kernel 6.1.0-30-amd64 installed, 6.1.0-29-amd64 running"},
		},
		{
			name:        "same kernel",
			modules:     []string{"6.1.0-29-amd64"},
			running:     "6.1.0-29-amd64",
			want:        false,
			wantReasons: []string{},
		},
		{
			name:        "no modules",
			running:     "6.1.0-29-amd64",
			want:        false,
			wantReasons: []string{},
		},
		{
			name:        "flag and kernel",
			flag:        true,
			modules:     []string{"6.1.0-30-amd64"},
			running:     "6.1.0-29-amd64",
			want:        true,
			wantReasons: []string{"reboot-required file", "kernel 6.1.0-30-amd64 installed, 6.1.0-29-amd64 running"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.flag {
				if err := os.MkdirAll(filepath.Join(root, "run"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "run", "reboot-required"), []byte{}, 0o644); err != nil {
					t.Fatal(err)
				}
				if tt.packages != "" {
					if err := os.WriteFile(filepath.Join(root, "run", "reboot-required.pkgs"), []byte(tt.packages), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, kernel := range tt.modules {
				if err := os.MkdirAll(filepath.Join(root, "lib", "modules", kernel), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			runner := &fakeRunner{handler: func(name string, args []string) ([]byte, error) {
				if name == "uname" && reflect.DeepEqual(args, []string{"-r"}) {
					return []byte(tt.running + "\n"), nil
				}
				return nil, errors.New("unexpected command")
			}}
			checker := New(Options{Runner: runner, Root: root, AptGet: "apt-get"})

			required, reasons := checker.rebootRequired(context.Background())
			if required != tt.want || !reflect.DeepEqual(reasons, tt.wantReasons) {
				t.Errorf("rebootRequired() = %t %v, want %t %v", required, reasons, tt.want, tt.wantReasons)
			}
		})
	}
}

func TestListsUpdatedAtUsesTheNewestReleaseFile(t *testing.T) {
	root := t.TempDir()
	lists := filepath.Join(root, "var", "lib", "apt", "lists")
	if err := os.MkdirAll(lists, 0o755); err != nil {
		t.Fatal(err)
	}
	newest := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	writeTime(t, filepath.Join(lists, "old_InRelease"), newest.Add(-2*time.Hour))
	writeTime(t, filepath.Join(lists, "new_Release"), newest)
	writeTime(t, filepath.Join(lists, "ignored.txt"), newest.Add(time.Hour))
	checker := New(Options{Runner: &fakeRunner{}, Root: root, AptGet: "apt-get"})

	if got := checker.listsUpdatedAt(); !got.Equal(newest) {
		t.Errorf("listsUpdatedAt() = %s, want %s", got, newest)
	}
}

func TestMissingAptLeavesTheSummaryUnset(t *testing.T) {
	runner := &fakeRunner{}
	checker := New(Options{
		Runner:   runner,
		Root:     t.TempDir(),
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
	})
	if checker.Available() || checker.Summary() != nil {
		t.Errorf("checker with no apt = available %t, summary %+v", checker.Available(), checker.Summary())
	}
	checker.Run(context.Background())
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.calls) != 0 {
		t.Errorf("commands ran without apt: %v", runner.calls)
	}
	if _, err := checker.StatusJSON(context.Background(), false); !errors.Is(err, ErrUnavailable) {
		t.Errorf("StatusJSON error = %v, want ErrUnavailable", err)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "equal", a: "6.1.0-29-amd64", b: "6.1.0-29-amd64", want: 0},
		{name: "newer revision", a: "6.1.0-30-amd64", b: "6.1.0-29-amd64", want: 1},
		{name: "older revision", a: "6.1.0-9-amd64", b: "6.1.0-30-amd64", want: -1},
		{name: "numeric not lexical", a: "6.1.0-100", b: "6.1.0-99", want: 1},
		{name: "tilde before release", a: "1.0~rc1", b: "1.0", want: -1},
		{name: "epoch wins", a: "1:1.0", b: "2.0", want: 1},
		{name: "revision wins", a: "1.0-2", b: "1.0-1", want: 1},
		{name: "letters before punctuation", a: "1.0a", b: "1.0+", want: -1},
		{name: "zero padding", a: "1.01", b: "1.1", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := compareVersions(tt.a, tt.b); got != tt.want {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			if got := compareVersions(tt.b, tt.a); got != -tt.want {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.b, tt.a, got, -tt.want)
			}
		})
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func writeTime(t *testing.T, path string, at time.Time) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// Package apt reads the pending Debian package updates of the host without
// changing it. Every apt command is a simulation or a read; the agent never
// builds a shell line.
package apt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultRefreshInterval is how often the inventory summary is recomputed.
	DefaultRefreshInterval = 6 * time.Hour
	// summaryTimeout bounds one periodic simulation.
	summaryTimeout = 2 * time.Minute
	// maxStatusBytes bounds the JSON the job sends to the core.
	maxStatusBytes = 256 << 10
)

const packageNamePattern = `[a-z0-9][a-z0-9+.-]*(?::[a-z0-9]+)?`

var (
	packageNameRE = regexp.MustCompile(`^` + packageNamePattern + `$`)
	instLineRE    = regexp.MustCompile(`^Inst (` + packageNamePattern + `)(?: \[([^]]+)\])? \(([^ ]+) (.+) \[[a-z0-9]+\]\)$`)
)

// ErrUnavailable means the host has no apt-get.
var ErrUnavailable = errors.New("apt-get is not available on this agent")

// CommandRunner runs a command and returns its output. The production
// implementation uses os/exec and the tests inject a fake.
type CommandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs a command with LC_ALL=C, so apt always emits its stable,
// parseable messages.
type ExecRunner struct{}

// Output executes name with the given arguments, never through a shell.
func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	environ := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "LC_ALL=") {
			continue
		}
		environ = append(environ, value)
	}
	command.Env = append(environ, "LC_ALL=C")

	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return output, fmt.Errorf("%s: %w", strings.TrimSpace(string(exit.Stderr)), err)
		}
		return output, err
	}
	return output, nil
}

// Package is one pending upgrade.
type Package struct {
	Name      string `json:"name"`
	Current   string `json:"current"`
	Candidate string `json:"candidate"`
	Origin    string `json:"origin"`
	Security  bool   `json:"security"`
}

// Status is the complete answer of the AptStatus job.
type Status struct {
	Packages       []Package `json:"packages"`
	Held           []string  `json:"held"`
	RebootRequired bool      `json:"reboot_required"`
	RebootReasons  []string  `json:"reboot_reasons"`
	ListsUpdatedAt string    `json:"lists_updated_at"`
	CheckedAt      string    `json:"checked_at"`
}

// Summary is the small snapshot of the inventory. It has no package list.
type Summary struct {
	Upgradable     int
	Security       int
	RebootRequired bool
	CheckedAt      time.Time
	ListsUpdatedAt time.Time
}

// Options configure a Checker.
type Options struct {
	// Runner runs apt-get, apt-mark and uname. Empty uses ExecRunner.
	Runner CommandRunner
	// Root is the filesystem root used for /run and /lib/modules.
	Root string
	// AptGet is the apt-get path. Empty looks it up once; when it is missing
	// the checker stays unavailable and Summary returns nil.
	AptGet string
	// LookPath overrides exec.LookPath in tests.
	LookPath func(string) (string, error)
	// Now overrides time.Now in tests.
	Now func() time.Time
	// RefreshInterval is how often Run recomputes the summary.
	RefreshInterval time.Duration
}

// Checker keeps the last apt summary and runs the AptStatus simulation.
type Checker struct {
	runner   CommandRunner
	root     string
	aptGet   string
	now      func() time.Time
	interval time.Duration

	mu      sync.RWMutex
	summary *Summary
}

// New returns a Checker. When apt-get is not installed, it logs once at debug
// level and leaves the summary unset.
func New(options Options) *Checker {
	runner := options.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	root := options.Root
	if root == "" {
		root = string(filepath.Separator)
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	interval := options.RefreshInterval
	if interval <= 0 {
		interval = DefaultRefreshInterval
	}
	lookPath := options.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	aptGet := options.AptGet
	if aptGet == "" {
		path, err := lookPath("apt-get")
		if err != nil {
			slog.Debug("apt-get is not available", "error", err)
			aptGet = ""
		} else {
			aptGet = path
		}
	}
	return &Checker{
		runner:   runner,
		root:     root,
		aptGet:   aptGet,
		now:      now,
		interval: interval,
	}
}

// Available reports whether the host has apt-get.
func (c *Checker) Available() bool {
	return c.aptGet != ""
}

// AptGetPath returns the apt-get binary this checker uses, or "" when the host
// has none.
func (c *Checker) AptGetPath() string {
	return c.aptGet
}

// ValidPackageName reports whether name is a Debian package name the agent is
// willing to pass to apt-get.
func ValidPackageName(name string) bool {
	return packageNameRE.MatchString(name)
}

// Summary returns a copy of the last summary, or nil until one is computed.
func (c *Checker) Summary() *Summary {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.summary == nil {
		return nil
	}
	copy := *c.summary
	return &copy
}

// Run computes the summary at start and every RefreshInterval until ctx ends.
func (c *Checker) Run(ctx context.Context) {
	if !c.Available() {
		return
	}
	c.refreshWithLog(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshWithLog(ctx)
		}
	}
}

func (c *Checker) refreshWithLog(ctx context.Context) {
	refreshCtx, cancel := context.WithTimeout(ctx, summaryTimeout)
	defer cancel()
	if err := c.Refresh(refreshCtx); err != nil {
		slog.Warn("refresh apt summary", "error", err)
	}
}

// Refresh recomputes the summary without asking apt-get to update its lists.
func (c *Checker) Refresh(ctx context.Context) error {
	if !c.Available() {
		return ErrUnavailable
	}
	_, err := c.collect(ctx, false)
	return err
}

// SecurityPackages simulates an upgrade and returns the packages whose
// candidate comes from a security archive. It is the list the SECURITY upgrade
// installs: the core never sends package names. The simulation takes no lock.
func (c *Checker) SecurityPackages(ctx context.Context) ([]string, error) {
	if !c.Available() {
		return nil, ErrUnavailable
	}
	output, err := c.run(ctx, c.aptGet, "-s", "-o", "Debug::NoLocking=1", "upgrade", "--with-new-pkgs")
	if err != nil {
		return nil, fmt.Errorf("simulate the apt upgrade: %w", err)
	}

	seen := make(map[string]bool)
	names := []string{}
	for _, pkg := range parseInst(output) {
		if !pkg.Security || seen[pkg.Name] || !ValidPackageName(pkg.Name) {
			continue
		}
		seen[pkg.Name] = true
		names = append(names, pkg.Name)
	}
	sort.Strings(names)
	return names, nil
}

// StatusJSON runs the full AptStatus query and returns its bounded JSON. With
// refresh it first runs apt-get update, which needs the lock.
func (c *Checker) StatusJSON(ctx context.Context, refresh bool) (string, error) {
	if !c.Available() {
		return "", ErrUnavailable
	}
	if refresh {
		if _, err := c.run(ctx, c.aptGet, "-o", "DPkg::Lock::Timeout=120", "update"); err != nil {
			return "", fmt.Errorf("apt-get update: %w", err)
		}
	}

	status, err := c.collect(ctx, true)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(status)
	if err != nil {
		return "", fmt.Errorf("encode the apt status: %w", err)
	}
	if len(data) > maxStatusBytes {
		return "", fmt.Errorf("the apt status is larger than %d bytes", maxStatusBytes)
	}
	return string(data), nil
}

func (c *Checker) collect(ctx context.Context, withHeld bool) (Status, error) {
	output, err := c.run(ctx, c.aptGet, "-s", "-o", "Debug::NoLocking=1", "upgrade", "--with-new-pkgs")
	if err != nil {
		return Status{}, fmt.Errorf("simulate the apt upgrade: %w", err)
	}
	packages := parseInst(output)
	sort.SliceStable(packages, func(i, j int) bool {
		if packages[i].Security != packages[j].Security {
			return packages[i].Security
		}
		return packages[i].Name < packages[j].Name
	})

	held := []string{}
	if withHeld {
		output, err := c.run(ctx, "apt-mark", "showhold")
		if err != nil {
			return Status{}, fmt.Errorf("list the held packages: %w", err)
		}
		held = parseHeld(output)
	}

	rebootRequired, reasons := c.rebootRequired(ctx)
	listsUpdatedAt := c.listsUpdatedAt()
	checkedAt := c.now().UTC()
	security := 0
	for _, pkg := range packages {
		if pkg.Security {
			security++
		}
	}

	status := Status{
		Packages:       packages,
		Held:           held,
		RebootRequired: rebootRequired,
		RebootReasons:  reasons,
		ListsUpdatedAt: formatTime(listsUpdatedAt),
		CheckedAt:      checkedAt.Format(time.RFC3339),
	}
	c.setSummary(&Summary{
		Upgradable:     len(packages),
		Security:       security,
		RebootRequired: rebootRequired,
		CheckedAt:      checkedAt,
		ListsUpdatedAt: listsUpdatedAt,
	})
	return status, nil
}

func (c *Checker) setSummary(summary *Summary) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.summary = summary
}

func (c *Checker) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return c.runner.Output(ctx, name, args...)
}

// parseInst keeps only well-formed Inst lines. Conf, Remv and anything else are
// ignored, and malformed Inst lines are skipped.
func parseInst(output []byte) []Package {
	packages := []Package{}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}
		match := instLineRE.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		packages = append(packages, Package{
			Name:      match[1],
			Current:   match[2],
			Candidate: match[3],
			Origin:    match[4],
			Security:  strings.Contains(match[4], "-security"),
		})
	}
	return packages
}

func parseHeld(output []byte) []string {
	seen := make(map[string]bool)
	held := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || !packageNameRE.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		held = append(held, name)
	}
	sort.Strings(held)
	return held
}

func (c *Checker) listsUpdatedAt() time.Time {
	entries, err := os.ReadDir(filepath.Join(c.root, "var", "lib", "apt", "lists"))
	if err != nil {
		return time.Time{}
	}
	var newest time.Time
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, "_InRelease") && !strings.HasSuffix(name, "_Release") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.IsDir() {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest.UTC()
}

func (c *Checker) rebootRequired(ctx context.Context) (bool, []string) {
	reasons := []string{}
	flag := filepath.Join(c.root, "run", "reboot-required")
	if _, err := os.Stat(flag); err == nil {
		reason := "reboot-required file"
		if data, err := os.ReadFile(filepath.Join(c.root, "run", "reboot-required.pkgs")); err == nil {
			var packages []string
			for _, line := range strings.Split(string(data), "\n") {
				if name := strings.TrimSpace(line); name != "" {
					packages = append(packages, name)
				}
			}
			if len(packages) > 0 {
				reason += ": " + strings.Join(packages, ", ")
			}
		}
		reasons = append(reasons, reason)
	}

	newestKernel := c.newestKernel()
	if newestKernel != "" {
		runningOutput, err := c.run(ctx, "uname", "-r")
		if err == nil {
			running := strings.TrimSpace(string(runningOutput))
			if running != "" && newestKernel != running {
				reasons = append(reasons, fmt.Sprintf("kernel %s installed, %s running", newestKernel, running))
			}
		}
	}
	return len(reasons) > 0, reasons
}

func (c *Checker) newestKernel() string {
	entries, err := os.ReadDir(filepath.Join(c.root, "lib", "modules"))
	if err != nil {
		return ""
	}
	newest := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue
		}
		if newest == "" || compareVersions(name, newest) > 0 {
			newest = name
		}
	}
	return newest
}

// compareVersions implements the Debian version ordering used by dpkg. It
// returns -1, 0 or 1.
func compareVersions(a, b string) int {
	ae, av := splitEpoch(a)
	be, bv := splitEpoch(b)
	switch {
	case ae < be:
		return -1
	case ae > be:
		return 1
	}
	return compareDebianVersion(av, bv)
}

func splitEpoch(version string) (int64, string) {
	epoch, rest, found := strings.Cut(version, ":")
	if !found {
		return 0, version
	}
	value, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		return 0, version
	}
	return value, rest
}

func compareDebianVersion(a, b string) int {
	aUpstream, aRevision := splitRevision(a)
	bUpstream, bRevision := splitRevision(b)
	if result := compareVersionPart(aUpstream, bUpstream); result != 0 {
		return result
	}
	return compareVersionPart(aRevision, bRevision)
}

func splitRevision(version string) (string, string) {
	index := strings.LastIndexByte(version, '-')
	if index < 0 {
		return version, ""
	}
	return version[:index], version[index+1:]
}

func compareVersionPart(a, b string) int {
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		for (i < len(a) && !isDigit(a[i])) || (j < len(b) && !isDigit(b[j])) {
			var left, right byte
			if i < len(a) {
				left = a[i]
			}
			if j < len(b) {
				right = b[j]
			}
			switch {
			case versionCharOrder(left) < versionCharOrder(right):
				return -1
			case versionCharOrder(left) > versionCharOrder(right):
				return 1
			}
			if i < len(a) {
				i++
			}
			if j < len(b) {
				j++
			}
		}
		for i < len(a) && a[i] == '0' {
			i++
		}
		for j < len(b) && b[j] == '0' {
			j++
		}
		startA, startB := i, j
		for i < len(a) && isDigit(a[i]) {
			i++
		}
		for j < len(b) && isDigit(b[j]) {
			j++
		}
		digitsA := a[startA:i]
		digitsB := b[startB:j]
		if len(digitsA) != len(digitsB) {
			if len(digitsA) < len(digitsB) {
				return -1
			}
			return 1
		}
		if digitsA != digitsB {
			if digitsA < digitsB {
				return -1
			}
			return 1
		}
	}
	return 0
}

func isDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

// versionCharOrder follows dpkg: '~' sorts before the end of the string,
// letters sort before non-letters, and the end sorts before letters.
func versionCharOrder(value byte) int {
	switch {
	case value == '~':
		return -1
	case value == 0:
		return 0
	case (value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z'):
		return int(value)
	default:
		return int(value) + 256
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

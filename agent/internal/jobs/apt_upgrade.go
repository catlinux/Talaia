package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"

	"github.com/catlinux/Talaia/agent/internal/apt"
)

const (
	// aptUpgradeTimeout bounds an apt upgrade, including a slow download.
	aptUpgradeTimeout = 60 * time.Minute
	// hostRebootTimeout bounds the scheduling of a reboot.
	hostRebootTimeout = time.Minute

	defaultAptDir        = "/var/lib/talaia/apt"
	defaultPollInterval  = 2 * time.Second
	defaultEventInterval = 5 * time.Second
	defaultLogTailBytes  = 64 << 10
	maxAptStateAge       = 24 * time.Hour
	maxAptLogAge         = 30 * 24 * time.Hour
	aptUnitPrefix        = "talaia-apt-"
	aptRebootUnit        = "talaia-reboot"
	aptJobIDPattern      = `^[A-Za-z0-9-]{1,64}$`
)

var (
	// errNoAptOperations is the failure of an apt job on an executor without
	// an apt manager.
	errNoAptOperations = errors.New("apt operations are not available on this agent")
	// errAptBusy is returned while another apt upgrade is in progress.
	errAptBusy = errors.New("another apt operation is running")

	aptJobIDRE = regexp.MustCompile(aptJobIDPattern)
)

// CommandRunner runs a command for the apt manager. Tests inject a fake; the
// production runner uses os/exec and never a shell.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with the minimal environment apt needs.
type ExecRunner struct{}

// Run executes name with args and returns its error.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = aptEnvironment()
	if err := command.Run(); err != nil {
		return fmt.Errorf("run %s: %w", name, err)
	}
	return nil
}

// Output executes name with args and returns its standard output.
func (ExecRunner) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = aptEnvironment()
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

func aptEnvironment() []string {
	return []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"DEBIAN_FRONTEND=noninteractive",
		"LC_ALL=C",
	}
}

// AptOperations are the apt write commands the executor can run.
type AptOperations interface {
	// Upgrade starts a detached upgrade and follows it until it finishes.
	Upgrade(ctx context.Context, jobID string, mode agentv1.AptUpgradeMode, emit func(*agentv1.AgentMsg)) (string, error)
	// Reboot schedules a reboot after checking the hostname and the apt lock.
	Reboot(ctx context.Context, jobID, hostname string) error
	// Resume follows the units left by an earlier agent process.
	Resume(ctx context.Context, emit func(*agentv1.AgentMsg))
}

// AptManagerOptions configure an AptManager. Tests override the paths and the
// command runner; production uses the defaults.
type AptManagerOptions struct {
	Checker *apt.Checker
	// Dir is where the state and log files live. Empty uses /var/lib/talaia/apt.
	Dir string
	// Runner runs systemd-run and systemctl. Empty uses ExecRunner.
	Runner CommandRunner
	// SystemdRun and Systemctl are the binary paths. Empty uses their PATH names.
	SystemdRun string
	Systemctl  string
	// AptGet is the apt-get binary. Empty uses the checker's path or apt-get.
	AptGet string
	// Now and Hostname override the clock and the machine name in tests.
	Now      func() time.Time
	Hostname func() (string, error)
	// PollInterval is how often systemctl show is asked. Empty uses 2 seconds.
	PollInterval time.Duration
	// EventInterval is the minimum time between RUNNING events with log tails.
	EventInterval time.Duration
	// LogTailBytes bounds the output of a progress or final event.
	LogTailBytes int64
}

// AptManager runs a single apt operation at a time and follows its detached
// systemd unit until it finishes.
type AptManager struct {
	checker       *apt.Checker
	dir           string
	runner        CommandRunner
	systemdRun    string
	systemctl     string
	aptGet        string
	now           func() time.Time
	hostname      func() (string, error)
	pollInterval  time.Duration
	eventInterval time.Duration
	logTailBytes  int64

	// aptMu is the process-wide apt lock; TryLock is what makes a second job
	// fail at once instead of waiting.
	aptMu sync.Mutex

	emitMu   sync.Mutex
	emit     func(*agentv1.AgentMsg)
	resumeMu sync.Mutex
	resumed  bool
}

// NewAptManager returns an apt manager with the production defaults filled in.
func NewAptManager(options AptManagerOptions) *AptManager {
	dir := options.Dir
	if dir == "" {
		dir = defaultAptDir
	}
	runner := options.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	systemdRun := options.SystemdRun
	if systemdRun == "" {
		systemdRun = "systemd-run"
	}
	systemctl := options.Systemctl
	if systemctl == "" {
		systemctl = "systemctl"
	}
	aptGet := options.AptGet
	if aptGet == "" && options.Checker != nil {
		aptGet = options.Checker.AptGetPath()
	}
	if aptGet == "" {
		aptGet = "apt-get"
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	hostname := options.Hostname
	if hostname == nil {
		hostname = os.Hostname
	}
	pollInterval := options.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultPollInterval
	}
	eventInterval := options.EventInterval
	if eventInterval <= 0 {
		eventInterval = defaultEventInterval
	}
	logTailBytes := options.LogTailBytes
	if logTailBytes <= 0 {
		logTailBytes = defaultLogTailBytes
	}
	return &AptManager{
		checker:       options.Checker,
		dir:           dir,
		runner:        runner,
		systemdRun:    systemdRun,
		systemctl:     systemctl,
		aptGet:        aptGet,
		now:           now,
		hostname:      hostname,
		pollInterval:  pollInterval,
		eventInterval: eventInterval,
		logTailBytes:  logTailBytes,
	}
}

// Upgrade starts an apt upgrade in a transient unit and follows it. The whole
// operation holds the apt lock, so a second apt or reboot job fails at once.
func (m *AptManager) Upgrade(ctx context.Context, jobID string, mode agentv1.AptUpgradeMode, emit func(*agentv1.AgentMsg)) (string, error) {
	if !ValidJobID(jobID) {
		return "", fmt.Errorf("invalid job id %q", jobID)
	}
	modeName, ok := aptUpgradeModeName(mode)
	if !ok {
		return "", fmt.Errorf("invalid apt upgrade mode %s", mode)
	}
	if m.checker == nil || !m.checker.Available() {
		return "", apt.ErrUnavailable
	}
	if !m.aptMu.TryLock() {
		return "", errAptBusy
	}
	defer m.aptMu.Unlock()
	m.setEmit(emit)

	var packages []string
	if mode == agentv1.AptUpgradeMode_APT_UPGRADE_MODE_SECURITY {
		var err error
		packages, err = m.checker.SecurityPackages(ctx)
		if err != nil {
			return "", err
		}
		if err := validatePackageNames(packages); err != nil {
			return "", err
		}
		if len(packages) == 0 {
			m.refresh()
			return "nothing to upgrade", nil
		}
	}

	unit := aptUnitName(jobID)
	logPath := m.logPath(jobID)
	state := aptUpgradeState{
		JobID:     jobID,
		Unit:      unit,
		Mode:      modeName,
		StartedAt: m.now().UTC(),
	}
	if err := m.ensureDir(); err != nil {
		return "", err
	}
	if err := m.writeState(state); err != nil {
		return "", err
	}
	if err := m.startUnit(ctx, unit, logPath, mode, packages); err != nil {
		_ = os.Remove(m.statePath(jobID))
		return "", err
	}

	output, err := m.poll(ctx, jobID, unit, logPath)
	if err != nil && ctx.Err() != nil {
		// The unit outlives this context. Keep the state file so a later
		// process can report its result.
		return output, err
	}
	_ = os.Remove(m.statePath(jobID))
	m.refresh()
	return output, err
}

// Reboot schedules a reboot five seconds from now. It refuses a wrong hostname
// and refuses while an apt upgrade holds the lock.
func (m *AptManager) Reboot(ctx context.Context, jobID, hostname string) error {
	if !ValidJobID(jobID) {
		return fmt.Errorf("invalid job id %q", jobID)
	}
	if !m.aptMu.TryLock() {
		return errAptBusy
	}
	defer m.aptMu.Unlock()

	current, err := m.hostname()
	if err != nil {
		return fmt.Errorf("read the hostname: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(hostname), strings.TrimSpace(current)) {
		return errors.New("hostname mismatch")
	}
	if err := m.runner.Run(ctx, m.systemdRun, "--on-active=5", "--unit="+aptRebootUnit, m.systemctl, "reboot"); err != nil {
		return fmt.Errorf("schedule the reboot: %w", err)
	}
	return nil
}

// Resume follows the units an earlier agent process left behind. It scans the
// state directory once per process; later stream sessions only update where
// progress is emitted.
func (m *AptManager) Resume(ctx context.Context, emit func(*agentv1.AgentMsg)) {
	m.setEmit(emit)
	m.resumeMu.Lock()
	if m.resumed {
		m.resumeMu.Unlock()
		return
	}
	m.resumed = true
	m.resumeMu.Unlock()

	m.cleanupStale()
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("read apt state directory", "error", err)
		}
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		jobID := strings.TrimSuffix(entry.Name(), ".json")
		if !ValidJobID(jobID) {
			slog.Warn("ignoring an apt state file with an invalid job id", "file", entry.Name())
			continue
		}
		state, err := m.readState(jobID)
		if err != nil {
			slog.Warn("read apt state", "job_id", jobID, "error", err)
			continue
		}
		if !ValidJobID(state.JobID) || state.JobID != jobID || !strings.HasPrefix(state.Unit, aptUnitPrefix) {
			slog.Warn("ignoring a malformed apt state file", "job_id", jobID)
			continue
		}
		go m.resumeOne(ctx, state)
	}
}

// resumeOne polls a unit left by an earlier process and reports its final
// event. A cancelled context leaves the state file for the next process.
func (m *AptManager) resumeOne(ctx context.Context, state aptUpgradeState) {
	if !m.aptMu.TryLock() {
		slog.Warn("another apt operation is running; leaving the apt job for later", "job_id", state.JobID)
		return
	}
	defer m.aptMu.Unlock()

	output, err := m.poll(ctx, state.JobID, state.Unit, m.logPath(state.JobID))
	if ctx.Err() != nil {
		return
	}
	_ = os.Remove(m.statePath(state.JobID))
	m.refresh()

	final := event(state.JobID, agentv1.JobState_JOB_STATE_SUCCEEDED, "")
	final.Output = strings.ToValidUTF8(output, "�")
	if err != nil {
		final = event(state.JobID, agentv1.JobState_JOB_STATE_FAILED, strings.ToValidUTF8(err.Error(), "�"))
		final.Output = strings.ToValidUTF8(output, "�")
	}
	m.emitMessage(jobMsg(final))
}

// ValidJobID reports whether a core job id is safe to use in file and unit
// names.
func ValidJobID(jobID string) bool {
	return aptJobIDRE.MatchString(jobID)
}

func aptUpgradeModeName(mode agentv1.AptUpgradeMode) (string, bool) {
	switch mode {
	case agentv1.AptUpgradeMode_APT_UPGRADE_MODE_SECURITY:
		return "SECURITY", true
	case agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL:
		return "ALL", true
	default:
		return "", false
	}
}

func validatePackageNames(packages []string) error {
	for _, name := range packages {
		if !apt.ValidPackageName(name) {
			return fmt.Errorf("invalid package name %q", name)
		}
	}
	return nil
}

// aptUnitName keeps the unit name short and inside the safe systemd alphabet.
func aptUnitName(jobID string) string {
	var suffix strings.Builder
	for _, char := range strings.ToLower(jobID) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			suffix.WriteRune(char)
		}
		if suffix.Len() == 8 {
			break
		}
	}
	if suffix.Len() == 0 {
		suffix.WriteString("job")
	}
	return aptUnitPrefix + suffix.String()
}

// startUnit asks systemd to run apt-get in its own transient unit. RemainAfterExit
// keeps the finished unit around until we have read Result and ExecMainStatus.
func (m *AptManager) startUnit(ctx context.Context, unit, logPath string, mode agentv1.AptUpgradeMode, packages []string) error {
	args := []string{
		"--unit=" + unit,
		"--collect",
		"--property=Type=exec",
		"--property=RemainAfterExit=yes",
		"--property=StandardOutput=append:" + logPath,
		"--property=StandardError=append:" + logPath,
		"--setenv=DEBIAN_FRONTEND=noninteractive",
		"--setenv=LC_ALL=C",
		"--",
		m.aptGet,
		"-y",
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
		"-o", "DPkg::Lock::Timeout=120",
		"--no-remove",
	}
	switch mode {
	case agentv1.AptUpgradeMode_APT_UPGRADE_MODE_ALL:
		args = append(args, "upgrade", "--with-new-pkgs")
	case agentv1.AptUpgradeMode_APT_UPGRADE_MODE_SECURITY:
		args = append(args, "install", "--only-upgrade", "--")
		args = append(args, packages...)
	default:
		return fmt.Errorf("invalid apt upgrade mode %s", mode)
	}
	if err := m.runner.Run(ctx, m.systemdRun, args...); err != nil {
		return fmt.Errorf("start the apt unit: %w", err)
	}
	return nil
}

type aptUnitState struct {
	active string
	sub    string
	result string
	exit   int
}

func (s aptUnitState) finished() bool {
	if s.active == "inactive" || s.active == "failed" {
		return true
	}
	// With RemainAfterExit=yes a successful Type=exec unit stays active while
	// its SubState is exited, which is how it reports that apt-get finished.
	return s.sub == "exited" || s.sub == "dead"
}

func (s aptUnitState) succeeded() bool {
	return s.result == "success" && s.exit == 0
}

// poll waits for the unit to finish, emitting a bounded log tail every
// eventInterval.
func (m *AptManager) poll(ctx context.Context, jobID, unit, logPath string) (string, error) {
	lastOutput := time.Time{}
	for {
		state, err := m.unitState(ctx, unit)
		if err != nil {
			if ctx.Err() != nil {
				// The agent is stopping: leave the unit and its state alone.
				return "", ctx.Err()
			}
			output := m.logTail(logPath)
			m.stopUnit(unit)
			return output, fmt.Errorf("read the apt unit state: %w", err)
		}
		if state.finished() {
			output := m.logTail(logPath)
			m.stopUnit(unit)
			if state.succeeded() {
				return output, nil
			}
			return output, fmt.Errorf("apt-get exited with status %d", state.exit)
		}
		if time.Since(lastOutput) >= m.eventInterval {
			m.emitRunning(jobID, m.logTail(logPath))
			lastOutput = time.Now()
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(m.pollInterval):
		}
	}
}

func (m *AptManager) unitState(ctx context.Context, unit string) (aptUnitState, error) {
	output, err := m.runner.Output(ctx, m.systemctl, "show", "-p", "ActiveState,SubState,Result,ExecMainStatus", unit)
	if err != nil {
		return aptUnitState{}, err
	}
	fields := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			fields[key] = value
		}
	}
	if len(fields) == 0 {
		return aptUnitState{}, errors.New("the apt unit is gone")
	}
	exit := 1
	if value, err := strconv.Atoi(fields["ExecMainStatus"]); err == nil {
		exit = value
	}
	return aptUnitState{
		active: fields["ActiveState"],
		sub:    fields["SubState"],
		result: fields["Result"],
		exit:   exit,
	}, nil
}

// stopUnit removes a finished transient unit after its result has been read.
// The cleanup context is not cancelled with the job: the unit must not stay
// behind when the job times out.
func (m *AptManager) stopUnit(unit string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(context.Background()), 10*time.Second)
	defer cancel()
	if err := m.runner.Run(ctx, m.systemctl, "stop", unit); err != nil {
		slog.Debug("stop apt unit", "unit", unit, "error", err)
	}
	if err := m.runner.Run(ctx, m.systemctl, "reset-failed", unit); err != nil {
		slog.Debug("reset failed apt unit", "unit", unit, "error", err)
	}
}

func (m *AptManager) logTail(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return ""
	}
	offset := info.Size() - m.logTailBytes
	if offset < 0 {
		offset = 0
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(file, m.logTailBytes))
	if err != nil {
		return ""
	}
	return strings.ToValidUTF8(string(data), "�")
}

func (m *AptManager) ensureDir() error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return fmt.Errorf("create the apt state directory: %w", err)
	}
	if err := os.Chmod(m.dir, 0o700); err != nil {
		return fmt.Errorf("set mode of the apt state directory: %w", err)
	}
	return nil
}

type aptUpgradeState struct {
	JobID     string    `json:"job_id"`
	Unit      string    `json:"unit"`
	Mode      string    `json:"mode"`
	StartedAt time.Time `json:"started_at"`
}

func (m *AptManager) statePath(jobID string) string {
	return filepath.Join(m.dir, jobID+".json")
}

func (m *AptManager) logPath(jobID string) string {
	return filepath.Join(m.dir, jobID+".log")
}

func (m *AptManager) writeState(state aptUpgradeState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode the apt state: %w", err)
	}
	path := m.statePath(state.JobID)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write the apt state: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set mode of the apt state: %w", err)
	}
	return nil
}

func (m *AptManager) readState(jobID string) (aptUpgradeState, error) {
	data, err := os.ReadFile(m.statePath(jobID))
	if err != nil {
		return aptUpgradeState{}, fmt.Errorf("read the apt state: %w", err)
	}
	var state aptUpgradeState
	if err := json.Unmarshal(data, &state); err != nil {
		return aptUpgradeState{}, fmt.Errorf("decode the apt state: %w", err)
	}
	return state, nil
}

// cleanupStale removes old state files and old logs. A state file older than a
// day can no longer belong to a meaningful job; an apt log older than 30 days
// is no longer useful.
func (m *AptManager) cleanupStale() {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return
	}
	now := m.now()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(m.dir, entry.Name())
		switch {
		case strings.HasSuffix(entry.Name(), ".json") && now.Sub(info.ModTime()) > maxAptStateAge:
			_ = os.Remove(path)
		case strings.HasSuffix(entry.Name(), ".log") && now.Sub(info.ModTime()) > maxAptLogAge:
			_ = os.Remove(path)
		}
	}
}

// refresh recomputes the inventory summary after a job. The job result does not
// depend on it: a failed refresh is logged and the next periodic run retries.
func (m *AptManager) refresh() {
	if m.checker == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := m.checker.Refresh(ctx); err != nil {
		slog.Warn("refresh apt summary", "error", err)
	}
}

func (m *AptManager) setEmit(emit func(*agentv1.AgentMsg)) {
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	m.emit = emit
}

func (m *AptManager) emitRunning(jobID, output string) {
	m.emitMu.Lock()
	emit := m.emit
	m.emitMu.Unlock()
	if emit == nil {
		return
	}
	progress := event(jobID, agentv1.JobState_JOB_STATE_RUNNING, "")
	progress.Output = strings.ToValidUTF8(output, "�")
	emit(jobMsg(progress))
}

func (m *AptManager) emitMessage(msg *agentv1.AgentMsg) {
	m.emitMu.Lock()
	emit := m.emit
	m.emitMu.Unlock()
	if emit != nil {
		emit(msg)
	}
}

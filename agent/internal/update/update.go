// Package update downloads and starts the installation of a newer agent
// package. The core sends the version and the SHA-256 of the package; the agent
// verifies both before handing the package to apt.
package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/catlinux/Talaia/agent/internal/collect"
	"github.com/catlinux/Talaia/agent/internal/enroll"
)

const (
	// DefaultDir is where the downloaded packages and the rollback copy live.
	DefaultDir = "/var/lib/talaia/update"

	// maxPackageSize bounds the .deb the agent is willing to write.
	maxPackageSize int64 = 64 << 20
	// downloadTimeout bounds the complete HTTP download.
	downloadTimeout = 5 * time.Minute

	dirMode     os.FileMode = 0o700
	packageMode os.FileMode = 0o600
)

var (
	versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	sha256Pattern  = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// Runner starts a command. The production runner uses os/exec; the tests use a
// fake and inspect the exact arguments.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// ExecRunner runs a command with a fixed, minimal environment.
type ExecRunner struct{}

// Run executes name with args. The environment is reduced to what apt needs, so
// nothing from the agent environment leaks into the installer.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin",
		"DEBIAN_FRONTEND=noninteractive",
	}
	if err := command.Run(); err != nil {
		return fmt.Errorf("run %s: %w", name, err)
	}
	return nil
}

// Options configures an Updater. Tests set Dir, Runner and CurrentVersion.
type Options struct {
	// CoreURL is the base URL of the core, for example https://panel.example.com:8444.
	CoreURL string
	// Identity is the enrolled identity; it provides the CA and the mTLS client
	// certificate for the download.
	Identity *enroll.Identity
	// Dir is where the packages are stored. Empty uses DefaultDir.
	Dir string
	// Runner starts the detached installation. Empty uses ExecRunner.
	Runner Runner
	// CurrentVersion is the running agent version. Empty uses collect.Version().
	CurrentVersion string
}

// Updater downloads an agent package and starts its installation.
type Updater struct {
	coreURL        string
	identity       *enroll.Identity
	dir            string
	runner         Runner
	currentVersion string

	// The fields below are changed by the tests only.
	client *http.Client
	arch   string
	limit  int64
}

// New returns an Updater with the production defaults filled in.
func New(options Options) *Updater {
	dir := options.Dir
	if dir == "" {
		dir = DefaultDir
	}
	runner := options.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	currentVersion := options.CurrentVersion
	if currentVersion == "" {
		currentVersion = collect.Version()
	}
	return &Updater{
		coreURL:        options.CoreURL,
		identity:       options.Identity,
		dir:            dir,
		runner:         runner,
		currentVersion: currentVersion,
		arch:           runtime.GOARCH,
		limit:          maxPackageSize,
	}
}

// Update validates the order, downloads the package, verifies its SHA-256 and
// starts apt-get through systemd-run. The detached unit keeps running if the
// agent restarts during the installation.
func (u *Updater) Update(ctx context.Context, version, sha256Hex string) (message string, err error) {
	defer func() {
		if err != nil {
			slog.Warn("agent update failed", "version", version, "error", err)
			return
		}
		slog.Info("agent update started", "version", version)
	}()

	if err := validateVersion(version); err != nil {
		return "", err
	}
	if !sha256Pattern.MatchString(sha256Hex) {
		return "", errors.New("the order does not carry a SHA-256 of 64 hexadecimal characters")
	}
	if !strictlyNewer(version, u.currentVersion) {
		return "", fmt.Errorf("agent version %s is not newer than %s", version, u.currentVersion)
	}

	endpoint, serverName, err := u.endpoint()
	if err != nil {
		return "", err
	}

	client := u.client
	if client == nil {
		if u.identity == nil {
			return "", errors.New("an agent update needs the enrolled agent identity")
		}
		client = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: u.identity.TLSConfig(serverName),
			},
			Timeout: downloadTimeout,
		}
	}

	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", fmt.Errorf("build the agent download request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download the agent package: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download the agent package: unexpected HTTP status %s", response.Status)
	}

	if err := os.MkdirAll(u.dir, dirMode); err != nil {
		return "", fmt.Errorf("create the update directory %s: %w", u.dir, err)
	}
	if err := os.Chmod(u.dir, dirMode); err != nil {
		return "", fmt.Errorf("set mode of the update directory %s: %w", u.dir, err)
	}

	name := localPackageName(version)
	target := filepath.Join(u.dir, name)
	temp, err := os.CreateTemp(u.dir, "."+name+".*")
	if err != nil {
		return "", fmt.Errorf("create the agent package file: %w", err)
	}
	tempName := temp.Name()
	renamed := false
	defer func() {
		_ = temp.Close()
		if !renamed {
			_ = os.Remove(tempName)
		}
	}()

	if err := temp.Chmod(packageMode); err != nil {
		return "", fmt.Errorf("set mode of the agent package: %w", err)
	}
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hasher), io.LimitReader(response.Body, u.limit+1))
	if err != nil {
		return "", fmt.Errorf("write the agent package: %w", err)
	}
	if written > u.limit {
		return "", fmt.Errorf("the agent package is larger than the %d-byte limit", u.limit)
	}
	if err := temp.Sync(); err != nil {
		return "", fmt.Errorf("sync the agent package: %w", err)
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close the agent package: %w", err)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); !strings.EqualFold(got, sha256Hex) {
		return "", errors.New("the agent package does not match the SHA-256 of the order")
	}

	if err := u.keepPrevious(); err != nil {
		return "", err
	}
	if err := os.Rename(tempName, target); err != nil {
		return "", fmt.Errorf("save the agent package: %w", err)
	}
	renamed = true

	if err := u.runner.Run(ctx, "systemd-run",
		"--unit=talaia-agent-upgrade", "--collect", "--no-block",
		"apt-get", "install", "-y", "--reinstall", target); err != nil {
		return "", fmt.Errorf("start the agent installation: %w", err)
	}
	return fmt.Sprintf("update to %s started; the agent will restart", version), nil
}

// validateVersion accepts only the x.y.z form used by the agent releases.
func validateVersion(version string) error {
	if _, ok := parseVersion(version); !ok {
		return fmt.Errorf("invalid agent version %q: expected x.y.z", version)
	}
	return nil
}

// parseVersion parses x.y.z with no leading zeroes.
func parseVersion(version string) ([3]uint64, bool) {
	var parts [3]uint64
	if !versionPattern.MatchString(version) {
		return parts, false
	}
	for index, value := range strings.Split(version, ".") {
		number, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return [3]uint64{}, false
		}
		parts[index] = number
	}
	return parts, true
}

// strictlyNewer reports whether offered is newer than running. A running
// version that is not x.y.z, such as "dev", is not comparable and is allowed.
func strictlyNewer(offered, running string) bool {
	next, ok := parseVersion(offered)
	if !ok {
		return false
	}
	current, ok := parseVersion(running)
	if !ok {
		return true
	}
	for index := range next {
		if next[index] != current[index] {
			return next[index] > current[index]
		}
	}
	return false
}

// remotePackageName returns the name the core serves for the architecture of
// the agent.
func remotePackageName(arch string) (string, error) {
	switch arch {
	case "amd64", "arm64":
		return "talaia-agent_" + arch + ".deb", nil
	default:
		return "", fmt.Errorf("unsupported agent architecture %q", arch)
	}
}

// localPackageName returns the package name kept in the update directory.
func localPackageName(version string) string {
	return "talaia-agent_" + version + ".deb"
}

// endpoint builds the download URL and the TLS server name.
func (u *Updater) endpoint() (*url.URL, string, error) {
	parsed, err := url.Parse(u.coreURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, "", fmt.Errorf("agent updates need the HTTPS core URL, got %q", u.coreURL)
	}
	name, err := remotePackageName(u.arch)
	if err != nil {
		return nil, "", err
	}
	return &url.URL{Scheme: "https", Host: parsed.Host, Path: "/agent/" + name}, parsed.Hostname(), nil
}

// keepPrevious copies the package of the running version to previous.deb when
// that file is still in the update directory.
func (u *Updater) keepPrevious() error {
	if _, ok := parseVersion(u.currentVersion); !ok {
		return nil
	}
	source := filepath.Join(u.dir, localPackageName(u.currentVersion))
	info, err := os.Stat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat the previous agent package: %w", err)
	}
	if info.IsDir() {
		return nil
	}
	if err := copyFile(source, filepath.Join(u.dir, "previous.deb")); err != nil {
		return fmt.Errorf("keep the previous agent package: %w", err)
	}
	return nil
}

// copyFile writes source to destination through a temporary file and a rename.
func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer func() { _ = input.Close() }()

	temp, err := os.CreateTemp(filepath.Dir(destination), "."+filepath.Base(destination)+".*")
	if err != nil {
		return fmt.Errorf("create the previous package file: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}()

	if err := temp.Chmod(packageMode); err != nil {
		return fmt.Errorf("set mode of the previous package: %w", err)
	}
	if _, err := io.Copy(temp, input); err != nil {
		return fmt.Errorf("copy the previous agent package: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync the previous agent package: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close the previous agent package: %w", err)
	}
	if err := os.Rename(tempName, destination); err != nil {
		return fmt.Errorf("save the previous agent package: %w", err)
	}
	return nil
}

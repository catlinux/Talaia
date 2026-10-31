// Package release deploys an app image into its Compose stack, optionally
// without downtime, and keeps only the images Talaia still needs.
package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/catlinux/Talaia/agent/internal/stacks"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	// DefaultHealthTimeout is how long a new container has to become healthy.
	DefaultHealthTimeout = 120 * time.Second
	// DefaultPollInterval is how often the health of a new container is read.
	DefaultPollInterval = 2 * time.Second
	maxReleaseOutput    = 256 << 10
)

// StackFiles reads and writes the files of a stack.
type StackFiles interface {
	Read(ctx context.Context, name string) (string, error)
	Write(ctx context.Context, name, compose, env string) (string, error)
	ComposeCommand(name string) (string, []string, error)
}

// Runner runs docker with fixed arguments.
type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (string, error)
}

// Options are the collaborators of a Manager.
type Options struct {
	Stacks        StackFiles
	Runner        Runner
	Dialer        Dialer
	HealthTimeout time.Duration
	PollInterval  time.Duration
}

// Manager releases app images on one server.
type Manager struct {
	stacks        StackFiles
	runner        Runner
	dialer        Dialer
	healthTimeout time.Duration
	pollInterval  time.Duration
}

// New returns a Manager with the defaults filled in.
func New(options Options) *Manager {
	runner := options.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	dialer := options.Dialer
	if dialer == nil {
		dialer = &netDialer{timeout: 2 * time.Second}
	}
	healthTimeout := options.HealthTimeout
	if healthTimeout <= 0 {
		healthTimeout = DefaultHealthTimeout
	}
	pollInterval := options.PollInterval
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	return &Manager{
		stacks:        options.Stacks,
		runner:        runner,
		dialer:        dialer,
		healthTimeout: healthTimeout,
		pollInterval:  pollInterval,
	}
}

// NewDefault returns the Manager of a real server.
func NewDefault() *Manager {
	return New(Options{
		Stacks: stacks.NewDefault(),
		Runner: ExecRunner{},
	})
}

type stackContents struct {
	Compose string `json:"compose"`
	Env     string `json:"env"`
}

// Release updates the image of the app service and starts it. A zero-downtime
// release is attempted only when asked and the stack is a single service
// without volumes; every other case uses a plain compose up.
func (m *Manager) Release(ctx context.Context, app *agentv1.AppRelease) (string, error) {
	if app == nil {
		return "", errors.New("app release is required")
	}
	name := app.GetName()
	if !stacks.ValidName(name) {
		return "", errors.New("invalid stack name")
	}
	tag := app.GetTag()
	if !tagRe.MatchString(tag) {
		return "", errors.New("invalid image tag")
	}
	service := app.GetService()
	if !serviceRe.MatchString(service) {
		return "", errors.New("invalid service name")
	}
	port := app.GetPort()
	if port > 65535 {
		return "", errors.New("invalid app port")
	}
	if m.stacks == nil {
		return "", errors.New("the stack files are not available on this agent")
	}

	old, err := m.readStack(ctx, name)
	if err != nil {
		return "", err
	}
	image := "talaia/" + name + ":" + tag
	compose, err := setServiceImage(old.Compose, service, image)
	if err != nil {
		return "", err
	}

	zeroDowntime := app.GetZeroDowntime() && m.canZeroDowntime(ctx, name, service)
	if _, err := m.stacks.Write(ctx, name, compose, old.Env); err != nil {
		return "", fmt.Errorf("write the app stack: %w", err)
	}

	if !zeroDowntime {
		output, err := m.compose(ctx, name, "up", "-d")
		if err != nil {
			restoreErr := m.restore(ctx, name, old)
			return output, errors.Join(fmt.Errorf("start the app: %w", err), restoreErr)
		}
		cleanup := m.cleanupImages(ctx, name, tag, app.GetKeepTags())
		return joinOutput(output, cleanup), nil
	}

	return m.releaseWithoutDowntime(ctx, name, service, tag, uint16(port), app.GetKeepTags(), old)
}

func (m *Manager) releaseWithoutDowntime(ctx context.Context, name, service, tag string, port uint16, keepTags []string, old stackContents) (string, error) {
	oldIDs, err := m.serviceIDs(ctx, name, service)
	if err != nil {
		restoreErr := m.restore(ctx, name, old)
		return "", errors.Join(fmt.Errorf("read the running containers: %w", err), restoreErr)
	}

	output, err := m.compose(ctx, name, "up", "-d", "--no-recreate", "--scale", service+"=2")
	if err != nil {
		restoreErr := m.restore(ctx, name, old)
		return output, errors.Join(fmt.Errorf("start the new container: %w", err), restoreErr)
	}

	allIDs, err := m.serviceIDs(ctx, name, service)
	if err != nil {
		return m.undoZeroDowntime(ctx, name, service, nil, old, output, fmt.Errorf("read the new container: %w", err))
	}
	newIDs := missing(allIDs, oldIDs)
	if len(newIDs) == 0 {
		return m.undoZeroDowntime(ctx, name, service, nil, old, output, errors.New("the new container did not start"))
	}

	if err := m.waitHealthy(ctx, name, newIDs, port); err != nil {
		return m.undoZeroDowntime(ctx, name, service, newIDs, old, output, err)
	}

	removed, err := m.remove(ctx, name, oldIDs)
	output = joinOutput(output, removed)
	if err != nil {
		return output, fmt.Errorf("remove the old container: %w", err)
	}
	scaled, err := m.compose(ctx, name, "up", "-d", "--no-recreate", "--scale", service+"=1")
	output = joinOutput(output, scaled)
	if err != nil {
		return output, fmt.Errorf("return the app to one container: %w", err)
	}

	cleanup := m.cleanupImages(ctx, name, tag, keepTags)
	return joinOutput(output, cleanup), nil
}

func (m *Manager) undoZeroDowntime(ctx context.Context, name, service string, newIDs []string, old stackContents, output string, cause error) (string, error) {
	removed, removeErr := m.remove(ctx, name, newIDs)
	var scaled string
	var scaleErr error
	if removeErr == nil {
		scaled, scaleErr = m.compose(ctx, name, "up", "-d", "--no-recreate", "--scale", service+"=1")
	}
	restoreErr := m.restore(ctx, name, old)
	return joinOutput(output, removed, scaled), errors.Join(cause, removeErr, scaleErr, restoreErr)
}

func (m *Manager) readStack(ctx context.Context, name string) (stackContents, error) {
	data, err := m.stacks.Read(ctx, name)
	if err != nil {
		return stackContents{}, fmt.Errorf("read the app stack: %w", err)
	}
	var contents stackContents
	if err := json.Unmarshal([]byte(data), &contents); err != nil {
		return stackContents{}, fmt.Errorf("decode the app stack: %w", err)
	}
	if strings.TrimSpace(contents.Compose) == "" {
		return stackContents{}, errors.New("compose.yaml is empty")
	}
	return contents, nil
}

func (m *Manager) restore(ctx context.Context, name string, old stackContents) error {
	if _, err := m.stacks.Write(ctx, name, old.Compose, old.Env); err != nil {
		return fmt.Errorf("restore the previous app stack: %w", err)
	}
	return nil
}

func (m *Manager) compose(ctx context.Context, name string, args ...string) (string, error) {
	dir, prefix, err := m.stacks.ComposeCommand(name)
	if err != nil {
		return "", err
	}
	full := append(append([]string(nil), prefix...), args...)
	return m.runner.Run(ctx, dir, full...)
}

func (m *Manager) serviceIDs(ctx context.Context, name, service string) ([]string, error) {
	output, err := m.compose(ctx, name, "ps", "-q", service)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		id := strings.TrimSpace(line)
		if id == "" {
			continue
		}
		if !containerRef.MatchString(id) {
			return nil, fmt.Errorf("docker returned an invalid container id %q", id)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *Manager) remove(ctx context.Context, name string, ids []string) (string, error) {
	if len(ids) == 0 {
		return "", nil
	}
	dir, _, err := m.stacks.ComposeCommand(name)
	if err != nil {
		return "", err
	}
	args := append([]string{"rm", "-f"}, ids...)
	return m.runner.Run(ctx, dir, args...)
}

func (m *Manager) cleanupImages(ctx context.Context, name, tag string, keepTags []string) string {
	dir, _, err := m.stacks.ComposeCommand(name)
	if err != nil {
		return "image cleanup skipped: " + err.Error()
	}
	output, err := m.runner.Run(ctx, dir, "image", "ls", "--format", "{{.Repository}}:{{.Tag}}", "talaia/"+name)
	if err != nil {
		return "image cleanup skipped: " + err.Error()
	}

	keep := map[string]bool{"talaia/" + name + ":" + tag: true}
	for _, item := range keepTags {
		if validImageTag(item) {
			keep["talaia/"+name+":"+item] = true
		}
	}

	refs := make([]string, 0)
	seen := map[string]bool{}
	prefix := "talaia/" + name + ":"
	for _, line := range strings.Split(output, "\n") {
		ref := strings.TrimSpace(line)
		if !strings.HasPrefix(ref, prefix) || seen[ref] {
			continue
		}
		seen[ref] = true
		if !validImageTag(strings.TrimPrefix(ref, prefix)) {
			continue
		}
		if !keep[ref] {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)

	notes := make([]string, 0)
	for _, ref := range refs {
		if _, err := m.runner.Run(ctx, dir, "image", "rm", ref); err != nil {
			notes = append(notes, "could not remove "+ref+": "+err.Error())
		}
	}
	return strings.Join(notes, "\n")
}

func missing(all, old []string) []string {
	seen := make(map[string]bool, len(old))
	for _, id := range old {
		seen[id] = true
	}
	result := make([]string, 0, len(all))
	for _, id := range all {
		if !seen[id] {
			result = append(result, id)
		}
	}
	return result
}

func joinOutput(parts ...string) string {
	text := ""
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if text != "" {
			text += "\n"
		}
		text += part
	}
	if len(text) > maxReleaseOutput {
		text = text[len(text)-maxReleaseOutput:]
	}
	return text
}

func validImageTag(tag string) bool {
	return imageTagRe.MatchString(tag)
}

func (m *Manager) runDocker(ctx context.Context, dir string, args ...string) (string, error) {
	return m.runner.Run(ctx, dir, args...)
}

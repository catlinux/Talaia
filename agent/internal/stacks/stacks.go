// Package stacks manages the Docker Compose projects Talaia keeps on a server:
// one directory per stack under /opt/talaia/stacks, holding compose.yaml and an
// optional .env. It validates what it is given, writes files atomically and runs
// docker compose with fixed arguments. It never builds a shell command.
package stacks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// DefaultRoot is where the managed stacks live on a server.
const DefaultRoot = "/opt/talaia/stacks"

const (
	composeFile = "compose.yaml"
	envFile     = ".env"

	// maxComposeBytes and maxEnvBytes bound what a stack may hold.
	maxComposeBytes = 256 << 10
	maxEnvBytes     = 64 << 10
)

// nameRe is a Compose project name: lower case letters, digits, dash and
// underscore, starting with a letter or a digit.
var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// ValidName reports whether name can be the name of a managed stack.
func ValidName(name string) bool {
	return nameRe.MatchString(name)
}

// Manager reads, writes and runs the managed stacks of one server.
type Manager struct {
	root   string
	runner Runner
}

// New returns a Manager that keeps its stacks under root and runs docker with
// runner.
func New(root string, runner Runner) *Manager {
	return &Manager{root: root, runner: runner}
}

// NewDefault returns the Manager of a real server.
func NewDefault() *Manager {
	return New(DefaultRoot, ExecRunner{})
}

// Write validates a Compose project and stores it as the stack name. Nothing is
// installed unless docker compose accepts it, so a broken edit never replaces a
// working stack.
func (m *Manager) Write(ctx context.Context, name, compose, env string) (string, error) {
	if !ValidName(name) {
		return "", errors.New("invalid stack name")
	}
	if strings.TrimSpace(compose) == "" {
		return "", errors.New("compose.yaml is empty")
	}
	if len(compose) > maxComposeBytes || len(env) > maxEnvBytes {
		return "", errors.New("the stack files are too large")
	}

	if err := os.MkdirAll(m.root, 0o755); err != nil {
		return "", fmt.Errorf("create the stacks directory: %w", err)
	}
	staging, err := os.MkdirTemp(m.root, ".staging-")
	if err != nil {
		return "", fmt.Errorf("create a staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	if err := os.WriteFile(filepath.Join(staging, composeFile), []byte(compose), 0o644); err != nil {
		return "", err
	}
	if env != "" {
		if err := os.WriteFile(filepath.Join(staging, envFile), []byte(env), 0o600); err != nil {
			return "", err
		}
	}

	if output, err := m.runner.Run(ctx, staging, "compose", "--project-name", name, "config", "--quiet"); err != nil {
		return "", fmt.Errorf("compose validation failed: %s", firstLines(output, err))
	}

	final := m.dir(name)
	if err := os.MkdirAll(final, 0o755); err != nil {
		return "", fmt.Errorf("create the stack directory: %w", err)
	}
	if err := replaceFile(filepath.Join(final, composeFile), []byte(compose), 0o644); err != nil {
		return "", err
	}
	if env != "" {
		if err := replaceFile(filepath.Join(final, envFile), []byte(env), 0o600); err != nil {
			return "", err
		}
	} else if err := os.Remove(filepath.Join(final, envFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return "stack saved", nil
}

// Do runs a docker compose operation on the stack name and returns what it printed.
func (m *Manager) Do(ctx context.Context, name string, action agentv1.StackActionType) (string, error) {
	dir, err := m.existing(name)
	if err != nil {
		return "", err
	}

	project := []string{"compose", "--project-name", name}
	switch action {
	case agentv1.StackActionType_STACK_ACTION_TYPE_UP:
		return m.runner.Run(ctx, dir, append(project, "up", "-d", "--remove-orphans")...)
	case agentv1.StackActionType_STACK_ACTION_TYPE_DOWN:
		return m.runner.Run(ctx, dir, append(project, "down")...)
	case agentv1.StackActionType_STACK_ACTION_TYPE_PULL:
		return m.runner.Run(ctx, dir, append(project, "pull")...)
	case agentv1.StackActionType_STACK_ACTION_TYPE_RESTART:
		return m.runner.Run(ctx, dir, append(project, "restart")...)
	case agentv1.StackActionType_STACK_ACTION_TYPE_REMOVE:
		output, err := m.runner.Run(ctx, dir, append(project, "down")...)
		if err != nil {
			return output, err
		}
		if err := os.RemoveAll(dir); err != nil {
			return output, fmt.Errorf("delete the stack files: %w", err)
		}
		return output, nil
	case agentv1.StackActionType_STACK_ACTION_TYPE_UNSPECIFIED:
		return "", errors.New("the stack action is not set")
	default:
		return "", fmt.Errorf("unsupported stack action %d", action)
	}
}

// Read returns the files of the stack as JSON: {"compose": "...", "env": "..."}.
func (m *Manager) Read(_ context.Context, name string) (string, error) {
	dir, err := m.existing(name)
	if err != nil {
		return "", err
	}

	compose, err := os.ReadFile(filepath.Join(dir, composeFile))
	if err != nil {
		return "", err
	}
	env, err := os.ReadFile(filepath.Join(dir, envFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	out, err := json.Marshal(struct {
		Compose string `json:"compose"`
		Env     string `json:"env"`
	}{string(compose), string(env)})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// List returns the names of the managed stacks, sorted.
func (m *Manager) List() []string {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return nil
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || !ValidName(entry.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(m.root, entry.Name(), composeFile)); err == nil {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func (m *Manager) dir(name string) string {
	return filepath.Join(m.root, name)
}

// existing returns the directory of a stack that exists. A symbolic link in its
// place is refused, so a stack can never point outside the stacks directory.
func (m *Manager) existing(name string) (string, error) {
	if !ValidName(name) {
		return "", errors.New("invalid stack name")
	}
	dir := m.dir(name)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return "", errors.New("the stack does not exist")
	}
	if _, err := os.Stat(filepath.Join(dir, composeFile)); err != nil {
		return "", errors.New("the stack does not exist")
	}
	return dir, nil
}

// replaceFile writes data next to path and renames it over it, so a reader never
// sees a half-written file.
func replaceFile(path string, data []byte, mode os.FileMode) error {
	temporary := path + ".new"
	if err := os.WriteFile(temporary, data, mode); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

// firstLines trims the output of a failed command to something a person can
// read in a message.
func firstLines(output string, err error) string {
	text := strings.TrimSpace(output)
	if text == "" {
		return err.Error()
	}
	const maxLines, maxChars = 8, 1200
	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	text = strings.Join(lines, "\n")
	if len(text) > maxChars {
		text = text[:maxChars]
	}
	return text
}

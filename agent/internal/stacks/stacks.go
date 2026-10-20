// Package stacks manages the Docker Compose projects Talaia keeps on a server.
// A managed stack is a directory under /opt/talaia/stacks holding compose.yaml
// and an optional .env. An adopted stack is a project that was already running:
// its files stay where they are and the directory of Talaia only holds a record
// of where. The package validates what it is given, writes files atomically and
// runs docker compose with fixed arguments. It never builds a shell command.
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
	// adoptionFile is the record an adopted stack keeps instead of its files.
	adoptionFile = "adopted.json"

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

// Project is a Compose project that is running on the host, as its containers
// describe it.
type Project struct {
	WorkingDir  string
	ConfigFiles []string
}

// ProjectSource finds a running Compose project by its name.
type ProjectSource interface {
	Project(ctx context.Context, name string) (Project, bool)
}

// adoption is the record of an adopted stack.
type adoption struct {
	WorkingDir string `json:"working_dir"`
	ConfigFile string `json:"config_file"`
}

// Manager reads, writes and runs the managed stacks of one server.
type Manager struct {
	root     string
	runner   Runner
	projects ProjectSource
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

// WithProjects lets the manager adopt the projects projects reports.
func (m *Manager) WithProjects(projects ProjectSource) *Manager {
	m.projects = projects
	return m
}

// Write validates a Compose project and stores it as the stack name. Nothing is
// installed unless docker compose accepts it, so a broken edit never replaces a
// working stack. For an adopted stack the files of the original directory are
// the ones replaced.
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

	// An adopted stack keeps living in its own directory, so the relative paths
	// of the file resolve there while it is validated.
	record, adopted, err := m.adoption(name)
	if err != nil {
		return "", err
	}
	validate := []string{"compose", "--project-name", name}
	validateDir := staging
	if adopted {
		validate = append(validate, "--project-directory", record.WorkingDir, "-f", filepath.Join(staging, composeFile))
		if env != "" {
			validate = append(validate, "--env-file", filepath.Join(staging, envFile))
		}
		validateDir = record.WorkingDir
	}
	validate = append(validate, "config", "--quiet")
	if output, err := m.runner.Run(ctx, validateDir, validate...); err != nil {
		return "", fmt.Errorf("compose validation failed: %s", firstLines(output, err))
	}

	target := m.dir(name)
	if adopted {
		target = record.WorkingDir
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("create the stack directory: %w", err)
	}
	composePath := filepath.Join(target, composeFile)
	if adopted {
		composePath = record.ConfigFile
	}
	if err := replaceFile(composePath, []byte(compose), fileMode(composePath, 0o644)); err != nil {
		return "", err
	}
	envPath := filepath.Join(target, envFile)
	if env != "" {
		if err := replaceFile(envPath, []byte(env), fileMode(envPath, 0o600)); err != nil {
			return "", err
		}
	} else if err := os.Remove(envPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return "stack saved", nil
}

// Adopt makes Talaia manage the running Compose project name. It finds the
// project among the containers of the host, checks that its single compose file
// exists and records where it is. Nothing is copied, moved or restarted.
func (m *Manager) Adopt(ctx context.Context, name string) (string, error) {
	if !ValidName(name) {
		return "", errors.New("invalid stack name")
	}
	if m.projects == nil {
		return "", errors.New("adopting projects is not available on this agent")
	}
	if _, err := os.Lstat(m.dir(name)); err == nil {
		return "", errors.New("a stack with this name is already managed")
	}

	project, found := m.projects.Project(ctx, name)
	if !found {
		return "", errors.New("no running Compose project has this name")
	}
	if len(project.ConfigFiles) != 1 {
		return "", errors.New("only projects with a single compose file can be adopted")
	}
	configFile := filepath.Clean(project.ConfigFiles[0])
	workingDir := filepath.Clean(project.WorkingDir)
	if !filepath.IsAbs(configFile) || !filepath.IsAbs(workingDir) {
		return "", errors.New("the project does not report absolute paths")
	}
	if info, err := os.Stat(configFile); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("the compose file of the project is not on this host")
	}
	if info, err := os.Stat(workingDir); err != nil || !info.IsDir() {
		return "", errors.New("the directory of the project is not on this host")
	}

	record, err := json.Marshal(adoption{WorkingDir: workingDir, ConfigFile: configFile})
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(m.dir(name), 0o755); err != nil {
		return "", fmt.Errorf("create the stack directory: %w", err)
	}
	if err := replaceFile(filepath.Join(m.dir(name), adoptionFile), record, 0o644); err != nil {
		return "", err
	}
	return "stack adopted", nil
}

// Do runs a docker compose operation on the stack name and returns what it printed.
func (m *Manager) Do(ctx context.Context, name string, action agentv1.StackActionType) (string, error) {
	dir, err := m.existing(name)
	if err != nil {
		return "", err
	}
	record, adopted, err := m.adoption(name)
	if err != nil {
		return "", err
	}

	// The directory docker compose runs in, and the arguments that say which
	// project it is. An adopted project is addressed by its own files.
	runDir := dir
	project := []string{"compose", "--project-name", name}
	if adopted {
		runDir = record.WorkingDir
		project = append(project, "--project-directory", record.WorkingDir, "-f", record.ConfigFile)
	}

	switch action {
	case agentv1.StackActionType_STACK_ACTION_TYPE_UP:
		return m.runner.Run(ctx, runDir, append(project, "up", "-d", "--remove-orphans")...)
	case agentv1.StackActionType_STACK_ACTION_TYPE_DOWN:
		return m.runner.Run(ctx, runDir, append(project, "down")...)
	case agentv1.StackActionType_STACK_ACTION_TYPE_PULL:
		return m.runner.Run(ctx, runDir, append(project, "pull")...)
	case agentv1.StackActionType_STACK_ACTION_TYPE_RESTART:
		return m.runner.Run(ctx, runDir, append(project, "restart")...)
	case agentv1.StackActionType_STACK_ACTION_TYPE_REMOVE:
		if adopted {
			return "", errors.New("an adopted stack is not removed: stop managing it instead")
		}
		output, err := m.runner.Run(ctx, runDir, append(project, "down")...)
		if err != nil {
			return output, err
		}
		if err := os.RemoveAll(dir); err != nil {
			return output, fmt.Errorf("delete the stack files: %w", err)
		}
		return output, nil
	case agentv1.StackActionType_STACK_ACTION_TYPE_FORGET:
		if !adopted {
			return "", errors.New("only an adopted stack can be forgotten")
		}
		// Only the record goes: the containers and the files stay as they are.
		if err := os.RemoveAll(dir); err != nil {
			return "", fmt.Errorf("delete the record of the stack: %w", err)
		}
		return "stack no longer managed", nil
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
	record, adopted, err := m.adoption(name)
	if err != nil {
		return "", err
	}

	composePath, envPath := filepath.Join(dir, composeFile), filepath.Join(dir, envFile)
	if adopted {
		composePath, envPath = record.ConfigFile, filepath.Join(record.WorkingDir, envFile)
	}
	compose, err := os.ReadFile(composePath)
	if err != nil {
		return "", err
	}
	env, err := os.ReadFile(envPath)
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
	return m.names(func(string) bool { return true })
}

// Adopted returns the names of the managed stacks that were adopted, sorted.
func (m *Manager) Adopted() []string {
	return m.names(func(name string) bool {
		_, adopted, err := m.adoption(name)
		return err == nil && adopted
	})
}

func (m *Manager) names(keep func(name string) bool) []string {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return nil
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() || !ValidName(entry.Name()) {
			continue
		}
		if _, err := m.existing(entry.Name()); err == nil && keep(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func (m *Manager) dir(name string) string {
	return filepath.Join(m.root, name)
}

// existing returns the directory of a stack that exists: one with a compose.yaml
// or with the record of an adoption. A symbolic link in its place is refused, so
// a stack can never point outside the stacks directory.
func (m *Manager) existing(name string) (string, error) {
	if !ValidName(name) {
		return "", errors.New("invalid stack name")
	}
	dir := m.dir(name)
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return "", errors.New("the stack does not exist")
	}
	for _, file := range []string{composeFile, adoptionFile} {
		if stat, err := os.Lstat(filepath.Join(dir, file)); err == nil && stat.Mode().IsRegular() {
			return dir, nil
		}
	}
	return "", errors.New("the stack does not exist")
}

// adoption reads the record of an adopted stack. It reports false for a stack
// that is not adopted, and an error for a record that cannot be trusted.
func (m *Manager) adoption(name string) (adoption, bool, error) {
	data, err := os.ReadFile(filepath.Join(m.dir(name), adoptionFile))
	if errors.Is(err, os.ErrNotExist) {
		return adoption{}, false, nil
	}
	if err != nil {
		return adoption{}, false, err
	}

	var record adoption
	if err := json.Unmarshal(data, &record); err != nil {
		return adoption{}, false, fmt.Errorf("the record of the stack is damaged: %w", err)
	}
	if !filepath.IsAbs(record.WorkingDir) || !filepath.IsAbs(record.ConfigFile) {
		return adoption{}, false, errors.New("the record of the stack is damaged")
	}
	return record, true, nil
}

// fileMode keeps the mode of an existing file and falls back to def.
func fileMode(path string, def os.FileMode) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return def
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

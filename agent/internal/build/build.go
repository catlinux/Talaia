// Package build builds the image of an app from the Dockerfile of the source
// workspace with docker build and fixed arguments. It never uses a shell and
// runs one build at a time on the server.
package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/catlinux/Talaia/agent/internal/stacks"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// DefaultRoot is where AppSource leaves the workspace of each stack.
const DefaultRoot = "/var/lib/talaia/builds"

const (
	// Timeout bounds one docker build.
	Timeout = 30 * time.Minute
	// MinFreeBytes is the free space a build needs before it starts.
	MinFreeBytes uint64 = 2 << 30
)

const maxBuildArgs = 128

var (
	// ErrBuildRunning is returned when another build of this server is running.
	ErrBuildRunning = errors.New("another build is already running on this server")
	// ErrNotEnoughSpace is returned when the workspace filesystem has less than
	// MinFreeBytes free.
	ErrNotEnoughSpace = errors.New("less than 2 GiB free for the build")
)

var (
	tagRe      = regexp.MustCompile(`^[a-f0-9]{12}$`)
	buildArgRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Builder builds app images on one server.
type Builder struct {
	root      string
	runner    Runner
	freeBytes func(string) (uint64, error)
	building  sync.Mutex
}

// New returns a Builder that finds the workspaces under root and runs docker
// with runner.
func New(root string, runner Runner) *Builder {
	if root == "" {
		root = DefaultRoot
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Builder{root: root, runner: runner, freeBytes: freeBytes}
}

// NewDefault returns the Builder of a real server.
func NewDefault() *Builder {
	return New(DefaultRoot, ExecRunner{})
}

// WithFreeBytes replaces how free space is measured. It is meant for tests.
func (b *Builder) WithFreeBytes(fn func(string) (uint64, error)) *Builder {
	if fn != nil {
		b.freeBytes = fn
	}
	return b
}

// Build builds the image talaia/<name>:<tag> from the Dockerfile of the
// workspace that AppSource filled. The workspace is removed whatever happens,
// and a second build on the same server is refused while one is running.
func (b *Builder) Build(ctx context.Context, app *agentv1.AppBuild) (string, error) {
	if app == nil {
		return "", errors.New("app build is required")
	}
	name := app.GetName()
	if !stacks.ValidName(name) {
		return "", errors.New("invalid stack name")
	}
	tag := app.GetTag()
	if !tagRe.MatchString(tag) {
		return "", errors.New("invalid image tag")
	}

	if !b.building.TryLock() {
		return "", ErrBuildRunning
	}
	defer b.building.Unlock()

	workspace, err := b.workspace(name)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(workspace) }()

	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return "", fmt.Errorf("create the build workspace: %w", err)
	}
	if err := os.Chmod(workspace, 0o700); err != nil {
		return "", fmt.Errorf("set the build workspace permissions: %w", err)
	}

	free, err := b.freeBytes(workspace)
	if err != nil {
		return "", fmt.Errorf("measure the free space of the build workspace: %w", err)
	}
	if free < MinFreeBytes {
		return "", fmt.Errorf("%w: %d bytes available", ErrNotEnoughSpace, free)
	}

	dockerfile, err := inside(workspace, app.GetDockerfilePath(), "Dockerfile")
	if err != nil {
		return "", fmt.Errorf("invalid Dockerfile path: %w", err)
	}

	contextDir, err := inside(workspace, app.GetBuildContext(), ".")
	if err != nil {
		return "", fmt.Errorf("invalid build context: %w", err)
	}

	args, err := buildArguments(app.GetBuildArgs())
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	command := []string{"build", "--pull", "--tag", "talaia/" + name + ":" + tag, "--file", dockerfile}
	for _, arg := range args {
		command = append(command, "--build-arg", arg.key+"="+arg.value)
	}
	command = append(command, contextDir)

	output, err := b.runner.Run(ctx, workspace, command...)
	if err != nil {
		return output, fmt.Errorf("build image %s: %w", name, err)
	}
	return output, nil
}

// workspace returns the absolute directory of one stack and makes sure it is
// below the root of this builder.
func (b *Builder) workspace(name string) (string, error) {
	root, err := filepath.Abs(b.root)
	if err != nil {
		return "", fmt.Errorf("resolve the build root: %w", err)
	}
	workspace, err := filepath.Abs(filepath.Join(root, name))
	if err != nil {
		return "", fmt.Errorf("resolve the build workspace: %w", err)
	}
	relative, err := filepath.Rel(root, workspace)
	if err != nil {
		return "", fmt.Errorf("resolve the build workspace: %w", err)
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("the build workspace is outside the build root")
	}
	return workspace, nil
}

// inside resolves value below workspace. Empty values use fallback; "." means
// the workspace itself only for the build context. Absolute paths and paths
// that climb out of the workspace are refused.
func inside(workspace, value, fallback string) (string, error) {
	if strings.IndexByte(value, 0) >= 0 {
		return "", errors.New("path contains a NUL byte")
	}
	if value == "" {
		if fallback == "." {
			return workspace, nil
		}
		value = fallback
	}

	normalized := strings.ReplaceAll(value, `\`, "/")
	if strings.HasPrefix(normalized, "/") || isWindowsAbsolute(normalized) {
		return "", errors.New("path is absolute")
	}
	cleaned := path.Clean(normalized)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("path leaves the workspace")
	}

	full := filepath.Join(workspace, filepath.FromSlash(cleaned))
	relative, err := filepath.Rel(workspace, full)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("path leaves the workspace")
	}
	return full, nil
}

func isWindowsAbsolute(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	letter := value[0]
	return letter >= 'a' && letter <= 'z' || letter >= 'A' && letter <= 'Z'
}

type buildArgument struct {
	key   string
	value string
}

func buildArguments(items []*agentv1.BuildArg) ([]buildArgument, error) {
	if len(items) > maxBuildArgs {
		return nil, errors.New("too many build arguments")
	}
	args := make([]buildArgument, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if item == nil {
			return nil, errors.New("invalid build argument")
		}
		key := item.GetKey()
		if !buildArgRe.MatchString(key) {
			return nil, fmt.Errorf("invalid build argument %q", key)
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate build argument %q", key)
		}
		seen[key] = true
		value := item.GetValue()
		if strings.IndexByte(value, 0) >= 0 {
			return nil, fmt.Errorf("invalid build argument %q", key)
		}
		args = append(args, buildArgument{key: key, value: value})
	}
	return args, nil
}

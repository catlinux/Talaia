package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	appbuild "github.com/catlinux/Talaia/agent/internal/build"
	"github.com/catlinux/Talaia/agent/internal/stacks"
)

// cleanupTagRe accepts the tags Docker accepts for an app image. It leaves out
// dangling images, whose tag is <none>.
var cleanupTagRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

// CleanupRunner runs docker with fixed arguments.
type CleanupRunner interface {
	Run(ctx context.Context, args ...string) (string, error)
}

type cleanupExecRunner struct{}

func (cleanupExecRunner) Run(ctx context.Context, args ...string) (string, error) {
	var stderr bytes.Buffer
	command := exec.CommandContext(ctx, "docker", args...)
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return "", fmt.Errorf("%s: %w", detail, err)
		}
		return "", fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return string(output), nil
}

// AppCleanupManager removes the images and build workspace of one app.
type AppCleanupManager struct {
	root   string
	runner CleanupRunner
}

// NewAppCleanup returns the cleaner of a real agent.
func NewAppCleanup() *AppCleanupManager {
	return newAppCleanup(appbuild.DefaultRoot, cleanupExecRunner{})
}

func newAppCleanup(root string, runner CleanupRunner) *AppCleanupManager {
	return &AppCleanupManager{root: root, runner: runner}
}

// Cleanup removes every image talaia/<name>:* and the build workspace of the
// stack. It never removes volumes, other repositories or other stacks.
func (m *AppCleanupManager) Cleanup(ctx context.Context, name string) (string, error) {
	if m == nil || m.runner == nil {
		return "", errors.New("the app cleanup is not available")
	}
	if !stacks.ValidName(name) {
		return "", errors.New("invalid stack name")
	}

	listing, err := m.runner.Run(ctx, "image", "ls", "--format", "{{.Repository}}:{{.Tag}}", "talaia/"+name)
	if err != nil {
		return "", fmt.Errorf("list the app images: %w", err)
	}

	prefix := "talaia/" + name + ":"
	seen := make(map[string]bool)
	refs := make([]string, 0)
	for _, line := range strings.Split(listing, "\n") {
		ref := strings.TrimSpace(line)
		if !strings.HasPrefix(ref, prefix) || seen[ref] {
			continue
		}
		if !cleanupTagRe.MatchString(strings.TrimPrefix(ref, prefix)) {
			continue
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	sort.Strings(refs)

	output := make([]string, 0, len(refs)+1)
	for _, ref := range refs {
		if _, err := m.runner.Run(ctx, "image", "rm", ref); err != nil {
			return "", fmt.Errorf("remove the image %s: %w", ref, err)
		}
		output = append(output, "removed image "+ref)
	}

	workspace := filepath.Join(m.root, name)
	if err := os.RemoveAll(workspace); err != nil {
		return "", fmt.Errorf("remove the build workspace %s: %w", workspace, err)
	}
	output = append(output, "removed build workspace "+workspace)
	return strings.Join(output, "\n"), nil
}

// Package source fetches Git repositories for the build of an app.
package source

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/catlinux/Talaia/agent/internal/stacks"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// DefaultRoot is where the agent keeps the build workspace of each stack.
const DefaultRoot = "/var/lib/talaia/builds"

const (
	maxDeployKeyBytes    = 16 << 10
	defaultWorkspaceSize = int64(1 << 30)
	deployKeyFile        = "deploy_key"
	knownHostsFile       = "known_hosts"
	probeTimeout         = time.Minute
	fetchTimeout         = 5 * time.Minute
)

var gitHardening = []string{
	"-c", "protocol.allow=never",
	"-c", "protocol.https.allow=always",
	"-c", "protocol.ssh.allow=always",
	"-c", "core.hooksPath=/dev/null",
	"-c", "submodule.recurse=false",
	"-c", "core.symlinks=false",
	"-c", "advice.detachedHead=false",
}

var errWorkspaceTooLarge = errors.New("the fetched workspace is larger than 1 GiB")

// Result is what Fetch reports about the commit it checked out.
type Result struct {
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Note    string `json:"note,omitempty"`
}

// Source fetches repositories for the apps of one agent.
type Source struct {
	root              string
	runner            Runner
	maxWorkspaceBytes int64
}

// New returns a Source that keeps workspaces under root and runs git with
// runner.
func New(root string, runner Runner) *Source {
	if root == "" {
		root = DefaultRoot
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Source{root: root, runner: runner, maxWorkspaceBytes: defaultWorkspaceSize}
}

// NewDefault returns the Source of a real server.
func NewDefault() *Source {
	return New(DefaultRoot, ExecRunner{})
}

// Probe returns the commit the branch points at.
func (s *Source) Probe(ctx context.Context, probe *agentv1.GitProbe) (string, error) {
	if probe == nil {
		return "", errors.New("git probe is required")
	}
	if _, err := validateRepoURL(probe.RepoUrl); err != nil {
		return "", err
	}
	if !validBranch(probe.Branch) {
		return "", errors.New("invalid branch name")
	}

	auth, err := s.auth(probe.RepoUrl, probe.DeployKey)
	if err != nil {
		return "", err
	}
	defer auth.cleanup()

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	ref := "refs/heads/" + probe.Branch
	output, err := s.run(ctx, auth.dir, auth.env, "ls-remote", "--heads", "--", probe.RepoUrl, ref)
	if err != nil {
		return "", fmt.Errorf("probe repository: %w", err)
	}
	commit, err := parseRemoteCommit(output, ref)
	if err != nil {
		return "", err
	}
	return commit, nil
}

// Fetch checks out one commit of the branch at the tip of the workspace. A
// wanted commit that the branch has already left behind is reported in Note.
func (s *Source) Fetch(ctx context.Context, app *agentv1.AppSource) (Result, error) {
	if app == nil {
		return Result{}, errors.New("app source is required")
	}
	if !stacks.ValidName(app.Name) {
		return Result{}, errors.New("invalid stack name")
	}
	if _, err := validateRepoURL(app.RepoUrl); err != nil {
		return Result{}, err
	}
	if !validBranch(app.Branch) {
		return Result{}, errors.New("invalid branch name")
	}
	if !validCommit(app.Commit) {
		return Result{}, errors.New("invalid commit")
	}

	auth, err := s.auth(app.RepoUrl, app.DeployKey)
	if err != nil {
		return Result{}, err
	}
	defer auth.cleanup()

	workspace, err := s.recreateWorkspace(app.Name)
	if err != nil {
		return Result{}, err
	}

	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	if _, err := s.run(ctx, workspace, auth.env, "init", "--quiet"); err != nil {
		return Result{}, fmt.Errorf("init workspace: %w", err)
	}
	if _, err := s.run(ctx, workspace, auth.env, "fetch", "--quiet", "--depth", "1", "--",
		app.RepoUrl, "refs/heads/"+app.Branch); err != nil {
		return Result{}, fmt.Errorf("fetch repository: %w", err)
	}
	if _, err := s.run(ctx, workspace, auth.env, "checkout", "--quiet", "--detach", "FETCH_HEAD"); err != nil {
		return Result{}, fmt.Errorf("checkout commit: %w", err)
	}
	commitOutput, err := s.run(ctx, workspace, auth.env, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return Result{}, fmt.Errorf("read fetched commit: %w", err)
	}
	commit := strings.TrimSpace(commitOutput)
	if commit == "" || !validCommit(commit) {
		return Result{}, errors.New("git returned an invalid commit")
	}
	subjectOutput, err := s.run(ctx, workspace, auth.env, "log", "-1", "--format=%s", "HEAD")
	if err != nil {
		return Result{}, fmt.Errorf("read commit subject: %w", err)
	}

	tooLarge, err := s.workspaceTooLarge(workspace)
	if err != nil {
		return Result{}, err
	}
	if tooLarge {
		_ = os.RemoveAll(workspace)
		return Result{}, errWorkspaceTooLarge
	}

	result := Result{Commit: commit, Subject: strings.TrimSpace(subjectOutput)}
	if app.Commit != "" && app.Commit != commit {
		result.Note = "the branch has moved; using its current tip"
	}
	return result, nil
}

func (s *Source) recreateWorkspace(name string) (string, error) {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return "", fmt.Errorf("create the build root: %w", err)
	}
	workspace := filepath.Join(s.root, name)
	if err := os.RemoveAll(workspace); err != nil {
		return "", fmt.Errorf("clear the workspace: %w", err)
	}
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return "", fmt.Errorf("create the workspace: %w", err)
	}
	if err := os.Chmod(workspace, 0o700); err != nil {
		return "", fmt.Errorf("set workspace permissions: %w", err)
	}
	return workspace, nil
}

func (s *Source) workspaceTooLarge(root string) (bool, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		total += info.Size()
		if total > s.maxWorkspaceBytes {
			return errWorkspaceTooLarge
		}
		return nil
	})
	if errors.Is(err, errWorkspaceTooLarge) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("measure the workspace: %w", err)
	}
	return false, nil
}

func (s *Source) run(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	full := make([]string, 0, len(gitHardening)+len(args))
	full = append(full, gitHardening...)
	full = append(full, args...)
	return s.runner.Run(ctx, dir, env, full...)
}

func parseRemoteCommit(output, ref string) (string, error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != ref {
			continue
		}
		if !validCommit(fields[0]) {
			return "", errors.New("git returned an invalid commit")
		}
		return fields[0], nil
	}
	return "", errors.New("the branch was not found in the repository")
}

type commandAuth struct {
	dir     string
	env     []string
	cleanup func()
}

// auth creates the temporary HOME, the fixed known_hosts file and, for SSH,
// the deploy key. The caller cleans the whole directory up when the command
// ends. The key itself never goes into the environment or the arguments.
func (s *Source) auth(repoURL, deployKey string) (commandAuth, error) {
	kind, err := validateRepoURL(repoURL)
	if err != nil {
		return commandAuth{}, err
	}

	var keyText, keyPath, sshCommand string
	if kind == repoSSH && deployKey != "" {
		keyText, err = normalizeDeployKey(deployKey)
		if err != nil {
			return commandAuth{}, err
		}
	}

	dir, err := os.MkdirTemp("", "talaia-source-")
	if err != nil {
		return commandAuth{}, fmt.Errorf("create the temporary git directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if strings.ContainsAny(dir, " \t\r\n") {
		cleanup()
		return commandAuth{}, errors.New("the temporary git directory path contains spaces")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		cleanup()
		return commandAuth{}, fmt.Errorf("set temporary git permissions: %w", err)
	}

	knownHostsPath := filepath.Join(dir, knownHostsFile)
	if err := os.WriteFile(knownHostsPath, []byte(knownHosts), 0o600); err != nil {
		cleanup()
		return commandAuth{}, fmt.Errorf("write the known hosts file: %w", err)
	}

	if kind == repoSSH {
		if keyText != "" {
			path := filepath.Join(dir, deployKeyFile)
			if err := os.WriteFile(path, []byte(keyText), 0o600); err != nil {
				cleanup()
				return commandAuth{}, fmt.Errorf("write the deploy key: %w", err)
			}
			keyPath = path
		}
		sshCommand = buildSSHCommand(keyPath, knownHostsPath)
	}

	return commandAuth{dir: dir, env: commandEnv(dir, sshCommand), cleanup: cleanup}, nil
}

func normalizeDeployKey(key string) (string, error) {
	if len(key) > maxDeployKeyBytes || strings.IndexByte(key, 0) >= 0 {
		return "", errors.New("invalid deploy key")
	}
	trimmed := strings.TrimSpace(key)
	if len(trimmed) == 0 || len(trimmed) > maxDeployKeyBytes ||
		!strings.HasPrefix(trimmed, "-----BEGIN ") || !strings.HasSuffix(trimmed, "-----") {
		return "", errors.New("invalid deploy key")
	}
	return trimmed, nil
}

func commandEnv(home, sshCommand string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ALLOW_PROTOCOL=https:ssh",
	}
	if sshCommand != "" {
		env = append(env, "GIT_SSH_COMMAND="+sshCommand)
	}
	return env
}

func buildSSHCommand(keyPath, knownHostsPath string) string {
	parts := []string{"ssh"}
	if keyPath != "" {
		parts = append(parts, "-i", keyPath)
	}
	parts = append(parts,
		"-o", "IdentitiesOnly=yes",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile="+knownHostsPath,
		"-o", "GlobalKnownHostsFile=/dev/null",
	)
	return strings.Join(parts, " ")
}

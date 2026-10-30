package source

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	fakeRepoURL = "https://github.com/example/app.git"
	mainRef     = "refs/heads/main"
	testKey     = "-----BEGIN OPENSSH PRIVATE KEY-----\nAAAA\n-----END OPENSSH PRIVATE KEY-----"
)

type testCall struct {
	dir  string
	env  []string
	args []string
}

type fakeRunner struct {
	calls []testCall
	run   func(dir string, env []string, args []string) (string, error)
}

func (f *fakeRunner) Run(_ context.Context, dir string, env []string, args ...string) (string, error) {
	f.calls = append(f.calls, testCall{
		dir:  dir,
		env:  append([]string(nil), env...),
		args: append([]string(nil), args...),
	})
	if f.run != nil {
		return f.run(dir, env, args)
	}
	return "", nil
}

func successfulRunner(commit, subject, ref string) *fakeRunner {
	return &fakeRunner{run: func(_ string, _ []string, args []string) (string, error) {
		switch {
		case hasArg(args, "ls-remote"):
			return commit + "\t" + ref + "\n", nil
		case hasArg(args, "rev-parse"):
			return commit + "\n", nil
		case hasArg(args, "log"):
			return subject + "\n", nil
		default:
			return "", nil
		}
	}}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func argIndex(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
	}
	return -1
}

func envValue(env []string, name string) string {
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok && key == name {
			return value
		}
	}
	return ""
}

func assertHardening(t *testing.T, args []string) {
	t.Helper()
	want := []string{
		"-c", "protocol.allow=never",
		"-c", "protocol.https.allow=always",
		"-c", "protocol.ssh.allow=always",
		"-c", "core.hooksPath=/dev/null",
		"-c", "submodule.recurse=false",
		"-c", "core.symlinks=false",
		"-c", "advice.detachedHead=false",
	}
	if len(args) < len(want) || !reflect.DeepEqual(args[:len(want)], want) {
		t.Fatalf("args do not start with the hardening flags: %v", args)
	}
	if hasArg(args, "--recurse-submodules") {
		t.Fatalf("submodule checkout flag found: %v", args)
	}
}

func assertBaseEnv(t *testing.T, env []string, ssh bool) (string, string) {
	t.Helper()
	values := make(map[string]string, len(env))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			t.Fatalf("environment item has no equals sign: %q", item)
		}
		if _, exists := values[key]; exists {
			t.Fatalf("environment key %q appears twice", key)
		}
		values[key] = value
	}
	if got := values["PATH"]; got != os.Getenv("PATH") {
		t.Errorf("PATH = %q, want the host PATH", got)
	}
	for key, want := range map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL":   "/dev/null",
		"GIT_ALLOW_PROTOCOL":  "https:ssh",
	} {
		if got := values[key]; got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	home := values["HOME"]
	if home == "" {
		t.Fatal("HOME is not set")
	}
	if strings.ContainsAny(home, " \t\r\n") {
		t.Errorf("HOME has whitespace: %q", home)
	}
	allowed := 6
	if ssh {
		allowed++
	}
	if len(values) != allowed {
		t.Errorf("environment has %d keys, want %d: %v", len(values), allowed, values)
	}
	return home, values["GIT_SSH_COMMAND"]
}

func TestProbeUsesHardeningAndCleansTheTemporaryHome(t *testing.T) {
	commit := strings.Repeat("a", 40)
	runner := successfulRunner(commit, "subject", mainRef)
	s := New(t.TempDir(), runner)

	got, err := s.Probe(context.Background(), &agentv1.GitProbe{RepoUrl: fakeRepoURL, Branch: "main"})
	if err != nil || got != commit {
		t.Fatalf("Probe = %q, %v", got, err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("git ran %d times, want 1", len(runner.calls))
	}
	call := runner.calls[0]
	assertHardening(t, call.args)
	dash := argIndex(call.args, "--")
	if dash < 0 || dash > argIndex(call.args, fakeRepoURL) || dash > argIndex(call.args, mainRef) {
		t.Fatalf("ls-remote positional arguments are not after --: %v", call.args)
	}
	home, sshCommand := assertBaseEnv(t, call.env, false)
	if sshCommand != "" {
		t.Errorf("HTTPS set GIT_SSH_COMMAND = %q", sshCommand)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temporary HOME was not removed: %v", err)
	}
}

func TestSSHUsesTheKeyAndFixedKnownHosts(t *testing.T) {
	commit := strings.Repeat("b", 40)
	var (
		keyMode    os.FileMode
		keyContent string
		hosts      string
	)
	runner := &fakeRunner{run: func(_ string, env []string, _ []string) (string, error) {
		home := envValue(env, "HOME")
		info, err := os.Stat(filepath.Join(home, deployKeyFile))
		if err != nil {
			t.Fatalf("stat deploy key: %v", err)
		}
		keyMode = info.Mode().Perm()
		data, err := os.ReadFile(filepath.Join(home, deployKeyFile))
		if err != nil {
			t.Fatalf("read deploy key: %v", err)
		}
		keyContent = string(data)
		data, err = os.ReadFile(filepath.Join(home, knownHostsFile))
		if err != nil {
			t.Fatalf("read known hosts: %v", err)
		}
		hosts = string(data)
		return commit + "\t" + mainRef + "\n", nil
	}}
	s := New(t.TempDir(), runner)

	if _, err := s.Probe(context.Background(), &agentv1.GitProbe{
		RepoUrl: "ssh://git@github.com/owner/repo", Branch: "main", DeployKey: testKey,
	}); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	home, sshCommand := assertBaseEnv(t, runner.calls[0].env, true)
	if keyContent != testKey {
		t.Errorf("stored key = %q", keyContent)
	}
	if hosts != knownHosts {
		t.Error("the known hosts file is not the fixed GitHub set")
	}
	if runtime.GOOS != "windows" && keyMode != 0o600 {
		t.Errorf("key mode = %o, want 600", keyMode)
	}
	keyPath := filepath.Join(home, deployKeyFile)
	wantSSH := "ssh -i " + keyPath +
		" -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=yes" +
		" -o UserKnownHostsFile=" + filepath.Join(home, knownHostsFile) +
		" -o GlobalKnownHostsFile=/dev/null"
	if sshCommand != wantSSH {
		t.Errorf("GIT_SSH_COMMAND = %q, want %q", sshCommand, wantSSH)
	}
	joinedArgs := strings.Join(runner.calls[0].args, " ")
	joinedEnv := strings.Join(runner.calls[0].env, " ")
	if strings.Contains(joinedArgs, testKey) || strings.Contains(joinedEnv, testKey) {
		t.Fatal("the deploy key appears in the arguments or the environment")
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temporary HOME with the key was not removed: %v", err)
	}
}

func TestSSHWithoutAKeyIsAllowed(t *testing.T) {
	commit := strings.Repeat("c", 40)
	runner := successfulRunner(commit, "subject", mainRef)
	s := New(t.TempDir(), runner)
	if _, err := s.Probe(context.Background(), &agentv1.GitProbe{
		RepoUrl: "git@github.com:owner/repo", Branch: "main",
	}); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	_, sshCommand := assertBaseEnv(t, runner.calls[0].env, true)
	if !strings.HasPrefix(sshCommand, "ssh -o IdentitiesOnly=yes") || strings.Contains(sshCommand, " -i ") {
		t.Errorf("GIT_SSH_COMMAND without a key = %q", sshCommand)
	}
}

func TestHTTPSIgnoresTheDeployKey(t *testing.T) {
	commit := strings.Repeat("d", 40)
	runner := successfulRunner(commit, "subject", mainRef)
	s := New(t.TempDir(), runner)
	if _, err := s.Probe(context.Background(), &agentv1.GitProbe{
		RepoUrl: fakeRepoURL, Branch: "main", DeployKey: "not a key",
	}); err != nil {
		t.Fatalf("Probe with a key on HTTPS: %v", err)
	}
	home, sshCommand := assertBaseEnv(t, runner.calls[0].env, false)
	if sshCommand != "" {
		t.Error("HTTPS set GIT_SSH_COMMAND")
	}
	if _, err := os.Stat(filepath.Join(home, deployKeyFile)); !errors.Is(err, os.ErrNotExist) {
		t.Error("HTTPS wrote the ignored deploy key")
	}
}

func TestInvalidDeployKeyIsRejectedWithoutLeakingIt(t *testing.T) {
	for _, key := range []string{"not a key", "-----BEGIN KEY\nema\000il", "-----BEGIN KEY\n" + strings.Repeat("a", maxDeployKeyBytes)} {
		runner := &fakeRunner{}
		s := New(t.TempDir(), runner)
		_, err := s.Probe(context.Background(), &agentv1.GitProbe{
			RepoUrl: "ssh://git@github.com/owner/repo", Branch: "main", DeployKey: key,
		})
		if err == nil {
			t.Fatalf("key %.20q was accepted", key)
		}
		if strings.Contains(err.Error(), key) || len(runner.calls) != 0 {
			t.Fatalf("invalid key ran git or leaked: %v, calls=%d", err, len(runner.calls))
		}
	}
}

func TestTemporaryDirectoryIsRemovedAfterFailure(t *testing.T) {
	runner := &fakeRunner{run: func(_ string, _ []string, _ []string) (string, error) {
		return "", errors.New("fetch failed")
	}}
	s := New(t.TempDir(), runner)
	_, err := s.Probe(context.Background(), &agentv1.GitProbe{
		RepoUrl: "ssh://git@github.com/owner/repo", Branch: "main", DeployKey: testKey,
	})
	if err == nil {
		t.Fatal("Probe accepted a failing git command")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatal("the error leaked the deploy key")
	}
	home := envValue(runner.calls[0].env, "HOME")
	if _, statErr := os.Stat(home); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("temporary HOME was not removed: %v", statErr)
	}
}

func TestFetchRecreatesTheWorkspaceAndReportsTheCommit(t *testing.T) {
	commit := strings.Repeat("e", 40)
	runner := successfulRunner(commit, "first commit", mainRef)
	root := t.TempDir()
	s := New(root, runner)
	workspace := filepath.Join(root, "shop")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "old"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := s.Fetch(context.Background(), &agentv1.AppSource{
		Name: "shop", RepoUrl: fakeRepoURL, Branch: "main",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.Commit != commit || result.Subject != "first commit" || result.Note != "" {
		t.Fatalf("Fetch result = %+v", result)
	}
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		t.Fatalf("workspace: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("workspace mode = %o, want 700", info.Mode().Perm())
	}
	entries, err := os.ReadDir(workspace)
	if err != nil || len(entries) != 0 {
		t.Errorf("workspace is not empty: %v, %v", entries, err)
	}
	if len(runner.calls) != 5 {
		t.Fatalf("git ran %d times, want 5", len(runner.calls))
	}
	for _, call := range runner.calls {
		assertHardening(t, call.args)
	}
	fetchArgs := runner.calls[1].args
	dash := argIndex(fetchArgs, "--")
	if dash < 0 || dash > argIndex(fetchArgs, fakeRepoURL) || dash > argIndex(fetchArgs, mainRef) {
		t.Fatalf("fetch positional arguments are not after --: %v", fetchArgs)
	}
	if hasArg(fetchArgs, "--recurse-submodules") {
		t.Fatal("fetch asked for submodules")
	}
	home := envValue(runner.calls[0].env, "HOME")
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temporary HOME was not removed: %v", err)
	}
}

func TestFetchNotesWhenTheBranchMoved(t *testing.T) {
	commit := strings.Repeat("f", 40)
	s := New(t.TempDir(), successfulRunner(commit, "subject", mainRef))
	result, err := s.Fetch(context.Background(), &agentv1.AppSource{
		Name: "shop", RepoUrl: fakeRepoURL, Branch: "main", Commit: strings.Repeat("1", 40),
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if result.Commit != commit || result.Note == "" {
		t.Fatalf("Fetch result = %+v", result)
	}
}

func TestFetchRemovesAnOversizedWorkspace(t *testing.T) {
	commit := strings.Repeat("a", 40)
	runner := successfulRunner(commit, "subject", mainRef)
	base := runner.run
	runner.run = func(dir string, env []string, args []string) (string, error) {
		if hasArg(args, "fetch") {
			if err := os.WriteFile(filepath.Join(dir, "large"), make([]byte, 64), 0o600); err != nil {
				return "", err
			}
		}
		return base(dir, env, args)
	}
	root := t.TempDir()
	s := New(root, runner)
	s.maxWorkspaceBytes = 32

	_, err := s.Fetch(context.Background(), &agentv1.AppSource{
		Name: "shop", RepoUrl: fakeRepoURL, Branch: "main",
	})
	if !errors.Is(err, errWorkspaceTooLarge) {
		t.Fatalf("Fetch error = %v, want errWorkspaceTooLarge", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "shop")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("oversized workspace was not removed: %v", statErr)
	}
}

func TestFetchValidationHappensBeforeAnyCommand(t *testing.T) {
	cases := []*agentv1.AppSource{
		{Name: "../shop", RepoUrl: fakeRepoURL, Branch: "main"},
		{Name: "shop", RepoUrl: "file:///tmp/repo", Branch: "main"},
		{Name: "shop", RepoUrl: fakeRepoURL, Branch: "-main"},
		{Name: "shop", RepoUrl: fakeRepoURL, Branch: "main", Commit: strings.Repeat("A", 40)},
	}
	for _, app := range cases {
		runner := &fakeRunner{}
		s := New(t.TempDir(), runner)
		if _, err := s.Fetch(context.Background(), app); err == nil {
			t.Errorf("invalid input was accepted: %+v", app)
		}
		if len(runner.calls) != 0 {
			t.Errorf("invalid input ran git: %+v", app)
		}
	}
}

func TestGitHubEd25519Fingerprint(t *testing.T) {
	line := strings.SplitN(knownHosts, "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != "github.com" || fields[1] != "ssh-ed25519" {
		t.Fatalf("first known-host line = %q", line)
	}
	blob, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil {
		t.Fatalf("decode ed25519 key: %v", err)
	}
	sum := sha256.Sum256(blob)
	got := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	const want = "SHA256:+DiY3wvvV6TuJJhbpZisF/zLDA0zPMSvHdkr4UvCOqU"
	if got != want {
		t.Fatalf("ed25519 fingerprint = %q, want %q", got, want)
	}
	if githubEd25519Fingerprint != want {
		t.Fatalf("fingerprint constant = %q, want %q", githubEd25519Fingerprint, want)
	}
}

func TestLimitedOutputKeepsTheEnd(t *testing.T) {
	var buffer limitedBuffer
	text := strings.Repeat("a", maxGitOutput+100) + "END"
	if _, err := buffer.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	got := buffer.String()
	if len(got) != maxGitOutput || !strings.HasSuffix(got, "END") {
		t.Fatalf("buffered %d bytes with suffix %q", len(got), got[len(got)-8:])
	}
}

func TestFetchAgainstLocalRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	bare := filepath.Join(t.TempDir(), "remote.git")
	work := filepath.Join(t.TempDir(), "work")
	runTestGit(t, "", "init", "--bare", "--quiet", bare)
	runTestGit(t, "", "init", "--quiet", work)
	if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, work, "add", "hello.txt")
	runTestGit(t, work, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--quiet", "-m", "first commit")
	runTestGit(t, work, "branch", "-M", "main")
	runTestGit(t, work, "push", "--quiet", bare, "main")

	root := t.TempDir()
	s := New(root, localRunner{fakeURL: fakeRepoURL, bare: bare})
	result, err := s.Fetch(context.Background(), &agentv1.AppSource{
		Name: "app", RepoUrl: fakeRepoURL, Branch: "main",
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(result.Commit) != 40 || result.Subject != "first commit" {
		t.Fatalf("result = %+v", result)
	}
	data, err := os.ReadFile(filepath.Join(root, "app", "hello.txt"))
	if err != nil || string(data) != "hello\n" {
		t.Fatalf("checked-out file = %q, %v", data, err)
	}
}

// localRunner is test-only: it maps the validated GitHub URL to a local bare
// repository and permits the file protocol, which production never does.
type localRunner struct {
	fakeURL string
	bare    string
}

func (r localRunner) Run(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	rewritten := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" && i+1 < len(args) && strings.HasPrefix(args[i+1], "protocol.") {
			i++
			continue
		}
		arg := args[i]
		if arg == r.fakeURL {
			arg = filepath.ToSlash(r.bare)
		}
		rewritten = append(rewritten, arg)
	}
	filteredEnv := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, "GIT_ALLOW_PROTOCOL=") {
			continue
		}
		filteredEnv = append(filteredEnv, item)
	}
	filteredEnv = append(filteredEnv, "GIT_ALLOW_PROTOCOL=file")
	return (ExecRunner{}).Run(ctx, dir, filteredEnv, rewritten...)
}

func runTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	if dir != "" {
		command.Dir = dir
	}
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

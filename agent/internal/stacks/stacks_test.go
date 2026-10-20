package stacks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// call is one docker invocation the fake runner saw.
type call struct {
	dir  string
	args []string
}

// fakeRunner records the calls and answers with output and err.
type fakeRunner struct {
	mu     sync.Mutex
	calls  []call
	output string
	err    error
}

func (f *fakeRunner) Run(_ context.Context, dir string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{dir: dir, args: args})
	return f.output, f.err
}

func newManager(t *testing.T) (*Manager, *fakeRunner, string) {
	t.Helper()
	root := t.TempDir()
	runner := &fakeRunner{}
	return New(root, runner), runner, root
}

const sampleCompose = "services:\n  web:\n    image: nginx:1.27\n"

func TestValidName(t *testing.T) {
	for _, name := range []string{"shop", "a", "my-app_2", "0day"} {
		if !ValidName(name) {
			t.Errorf("%q should be valid", name)
		}
	}
	for _, name := range []string{"", "Shop", "-x", "_x", "a b", "a/b", "..", "a.b", "x;rm", strings.Repeat("a", 64)} {
		if ValidName(name) {
			t.Errorf("%q should be invalid", name)
		}
	}
}

func TestWriteValidatesThenInstallsTheFiles(t *testing.T) {
	m, runner, root := newManager(t)

	message, err := m.Write(context.Background(), "shop", sampleCompose, "TOKEN=abc\n")
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if message != "stack saved" {
		t.Errorf("message = %q", message)
	}

	// The validation ran in a staging directory, with fixed arguments.
	if len(runner.calls) != 1 {
		t.Fatalf("docker ran %d times, want 1", len(runner.calls))
	}
	got := runner.calls[0]
	if !reflect.DeepEqual(got.args, []string{"compose", "--project-name", "shop", "config", "--quiet"}) {
		t.Errorf("args = %v", got.args)
	}
	if !strings.Contains(got.dir, ".staging-") {
		t.Errorf("validated in %q, want a staging directory", got.dir)
	}

	// The files are in place and the staging directory is gone.
	compose, err := os.ReadFile(filepath.Join(root, "shop", "compose.yaml"))
	if err != nil || string(compose) != sampleCompose {
		t.Errorf("compose.yaml = %q, %v", compose, err)
	}
	env, err := os.ReadFile(filepath.Join(root, "shop", ".env"))
	if err != nil || string(env) != "TOKEN=abc\n" {
		t.Errorf(".env = %q, %v", env, err)
	}
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".staging-") {
			t.Errorf("staging directory %q was left behind", entry.Name())
		}
	}
}

func TestWriteKeepsTheWorkingStackWhenTheEditIsInvalid(t *testing.T) {
	m, runner, root := newManager(t)
	if _, err := m.Write(context.Background(), "shop", sampleCompose, ""); err != nil {
		t.Fatalf("first Write: %v", err)
	}

	runner.output = "services.web.image must be a string\nmore detail"
	runner.err = errors.New("exit status 1")
	_, err := m.Write(context.Background(), "shop", "services: [broken", "")
	if err == nil || !strings.Contains(err.Error(), "compose validation failed") || !strings.Contains(err.Error(), "must be a string") {
		t.Fatalf("error = %v", err)
	}

	compose, _ := os.ReadFile(filepath.Join(root, "shop", "compose.yaml"))
	if string(compose) != sampleCompose {
		t.Errorf("the working stack was replaced by %q", compose)
	}
}

func TestWriteRemovesTheEnvFileWhenItIsEmptied(t *testing.T) {
	m, _, root := newManager(t)
	if _, err := m.Write(context.Background(), "shop", sampleCompose, "A=1\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := m.Write(context.Background(), "shop", sampleCompose, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "shop", ".env")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".env still exists: %v", err)
	}
}

func TestWriteRefusesBadInput(t *testing.T) {
	m, runner, _ := newManager(t)
	tests := []struct {
		name, stack, compose, env string
	}{
		{"bad name", "../etc", sampleCompose, ""},
		{"upper case name", "Shop", sampleCompose, ""},
		{"empty compose", "shop", "  \n", ""},
		{"huge compose", "shop", strings.Repeat("a", maxComposeBytes+1), ""},
		{"huge env", "shop", sampleCompose, strings.Repeat("A", maxEnvBytes+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := m.Write(context.Background(), tt.stack, tt.compose, tt.env); err == nil {
				t.Error("expected an error")
			}
		})
	}
	if len(runner.calls) != 0 {
		t.Errorf("docker ran %d times for invalid input", len(runner.calls))
	}
}

func TestDoRunsComposeWithFixedArguments(t *testing.T) {
	m, runner, root := newManager(t)
	if _, err := m.Write(context.Background(), "shop", sampleCompose, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}
	runner.calls = nil

	tests := []struct {
		action agentv1.StackActionType
		want   []string
	}{
		{agentv1.StackActionType_STACK_ACTION_TYPE_UP, []string{"compose", "--project-name", "shop", "up", "-d", "--remove-orphans"}},
		{agentv1.StackActionType_STACK_ACTION_TYPE_DOWN, []string{"compose", "--project-name", "shop", "down"}},
		{agentv1.StackActionType_STACK_ACTION_TYPE_PULL, []string{"compose", "--project-name", "shop", "pull"}},
		{agentv1.StackActionType_STACK_ACTION_TYPE_RESTART, []string{"compose", "--project-name", "shop", "restart"}},
	}
	for _, tt := range tests {
		runner.calls = nil
		if _, err := m.Do(context.Background(), "shop", tt.action); err != nil {
			t.Fatalf("%s: %v", tt.action, err)
		}
		if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0].args, tt.want) {
			t.Errorf("%s ran %v, want %v", tt.action, runner.calls, tt.want)
		}
		if runner.calls[0].dir != filepath.Join(root, "shop") {
			t.Errorf("%s ran in %q", tt.action, runner.calls[0].dir)
		}
	}
}

func TestDoRemoveBringsTheStackDownAndDeletesItsFiles(t *testing.T) {
	m, runner, root := newManager(t)
	if _, err := m.Write(context.Background(), "shop", sampleCompose, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}
	runner.calls = nil

	if _, err := m.Do(context.Background(), "shop", agentv1.StackActionType_STACK_ACTION_TYPE_REMOVE); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if len(runner.calls) != 1 || runner.calls[0].args[len(runner.calls[0].args)-1] != "down" {
		t.Errorf("calls = %v, want a single down", runner.calls)
	}
	if _, err := os.Stat(filepath.Join(root, "shop")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the stack directory still exists: %v", err)
	}
}

func TestDoRemoveKeepsTheFilesWhenDownFails(t *testing.T) {
	m, runner, root := newManager(t)
	if _, err := m.Write(context.Background(), "shop", sampleCompose, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}
	runner.err = errors.New("exit status 1")

	if _, err := m.Do(context.Background(), "shop", agentv1.StackActionType_STACK_ACTION_TYPE_REMOVE); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(filepath.Join(root, "shop", "compose.yaml")); err != nil {
		t.Errorf("the files were deleted although down failed: %v", err)
	}
}

func TestDoRefusesUnknownStacksAndUnsetActions(t *testing.T) {
	m, runner, _ := newManager(t)

	if _, err := m.Do(context.Background(), "ghost", agentv1.StackActionType_STACK_ACTION_TYPE_UP); err == nil {
		t.Error("expected an error for a stack that does not exist")
	}
	if _, err := m.Do(context.Background(), "../x", agentv1.StackActionType_STACK_ACTION_TYPE_UP); err == nil {
		t.Error("expected an error for an invalid name")
	}
	if _, err := m.Write(context.Background(), "shop", sampleCompose, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}
	runner.calls = nil
	if _, err := m.Do(context.Background(), "shop", agentv1.StackActionType_STACK_ACTION_TYPE_UNSPECIFIED); err == nil {
		t.Error("expected an error for an unset action")
	}
	if len(runner.calls) != 0 {
		t.Error("docker ran for an invalid request")
	}
}

func TestDoRefusesASymbolicLinkInsteadOfAStack(t *testing.T) {
	m, runner, root := newManager(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "compose.yaml"), []byte(sampleCompose), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "evil")); err != nil {
		t.Skipf("symbolic links are not available: %v", err)
	}

	if _, err := m.Do(context.Background(), "evil", agentv1.StackActionType_STACK_ACTION_TYPE_UP); err == nil {
		t.Error("a symbolic link was accepted as a stack")
	}
	if len(runner.calls) != 0 {
		t.Error("docker ran inside a linked directory")
	}
}

func TestReadReturnsTheFilesAsJSON(t *testing.T) {
	m, _, _ := newManager(t)
	if _, err := m.Write(context.Background(), "shop", sampleCompose, "A=1\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	text, err := m.Read(context.Background(), "shop")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var files struct{ Compose, Env string }
	if err := json.Unmarshal([]byte(strings.ReplaceAll(strings.ReplaceAll(text, `"compose"`, `"Compose"`), `"env"`, `"Env"`)), &files); err != nil {
		t.Fatalf("the output is not JSON: %v", err)
	}
	if files.Compose != sampleCompose || files.Env != "A=1\n" {
		t.Errorf("files = %+v", files)
	}

	if _, err := m.Read(context.Background(), "ghost"); err == nil {
		t.Error("expected an error for a stack that does not exist")
	}
}

func TestListReturnsTheManagedStacksSorted(t *testing.T) {
	m, _, root := newManager(t)
	for _, name := range []string{"zeta", "alpha"} {
		if _, err := m.Write(context.Background(), name, sampleCompose, ""); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	// Not stacks: a directory without compose.yaml, a bad name and a file.
	_ = os.MkdirAll(filepath.Join(root, "empty"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "Bad Name"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "file"), nil, 0o644)

	if got := m.List(); !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Errorf("List() = %v", got)
	}
	if got := New(filepath.Join(root, "missing"), &fakeRunner{}).List(); len(got) != 0 {
		t.Errorf("List() of a missing directory = %v", got)
	}
}

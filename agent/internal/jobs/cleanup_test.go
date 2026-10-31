package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type cleanupFakeRunner struct {
	calls     [][]string
	list      string
	listErr   error
	removeErr map[string]error
}

func (f *cleanupFakeRunner) Run(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	switch {
	case len(args) >= 2 && args[0] == "image" && args[1] == "ls":
		if f.listErr != nil {
			return "", f.listErr
		}
		return f.list, nil
	case len(args) >= 3 && args[0] == "image" && args[1] == "rm":
		if err := f.removeErr[args[2]]; err != nil {
			return "", err
		}
		return "", nil
	default:
		return "", errors.New("unexpected command")
	}
}

func (f *cleanupFakeRunner) has(args ...string) bool {
	for _, call := range f.calls {
		if len(call) != len(args) {
			continue
		}
		matches := true
		for index := range args {
			if call[index] != args[index] {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func TestAppCleanupRemovesOnlyItsImagesAndWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "shop", "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "other"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &cleanupFakeRunner{list: strings.Join([]string{
		"talaia/shop:aaaaaaaaaaaa",
		"talaia/shop:bbbbbbbbbbbb",
		"talaia/other:cccccccccccc",
		"talaia/shop:<none>",
		"nginx:latest",
	}, "\n")}

	output, err := newAppCleanup(root, runner).Cleanup(context.Background(), "shop")
	if err != nil {
		t.Fatal(err)
	}
	if !runner.has("image", "ls", "--format", "{{.Repository}}:{{.Tag}}", "talaia/shop") {
		t.Errorf("image list call = %v", runner.calls)
	}
	for _, ref := range []string{"talaia/shop:aaaaaaaaaaaa", "talaia/shop:bbbbbbbbbbbb"} {
		if !runner.has("image", "rm", ref) {
			t.Errorf("image %s was not removed: %v", ref, runner.calls)
		}
		if !strings.Contains(output, "removed image "+ref) {
			t.Errorf("output does not list %s: %q", ref, output)
		}
	}
	for _, ref := range []string{"talaia/other:cccccccccccc", "nginx:latest", "talaia/shop:<none>"} {
		if runner.has("image", "rm", ref) {
			t.Errorf("image %s was removed", ref)
		}
	}
	if runner.has("prune") || runner.has("image", "prune") {
		t.Errorf("the cleaner ran a prune: %v", runner.calls)
	}
	if _, err := os.Stat(filepath.Join(root, "shop")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the app workspace still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "other")); err != nil {
		t.Errorf("another workspace was touched: %v", err)
	}
	if !strings.Contains(output, "removed build workspace "+filepath.Join(root, "shop")) {
		t.Errorf("output does not mention the workspace: %q", output)
	}
}

func TestAppCleanupKeepsTheWorkspaceWhenAnImageFails(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "shop"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &cleanupFakeRunner{
		list:      "talaia/shop:aaaaaaaaaaaa\ntalaia/shop:bbbbbbbbbbbb\n",
		removeErr: map[string]error{"talaia/shop:bbbbbbbbbbbb": errors.New("image is in use")},
	}
	_, err := newAppCleanup(root, runner).Cleanup(context.Background(), "shop")
	if err == nil || !strings.Contains(err.Error(), "talaia/shop:bbbbbbbbbbbb") {
		t.Fatalf("Cleanup error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "shop")); statErr != nil {
		t.Errorf("the workspace was removed after a failed image: %v", statErr)
	}
}

func TestAppCleanupWithNoImagesStillRemovesTheWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "shop"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &cleanupFakeRunner{}
	output, err := newAppCleanup(root, runner).Cleanup(context.Background(), "shop")
	if err != nil {
		t.Fatal(err)
	}
	if runner.has("image", "rm") {
		t.Errorf("an image was removed: %v", runner.calls)
	}
	if !strings.Contains(output, "removed build workspace") {
		t.Errorf("output = %q", output)
	}
	if _, err := os.Stat(filepath.Join(root, "shop")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the workspace still exists: %v", err)
	}
}

func TestAppCleanupRejectsAnInvalidName(t *testing.T) {
	runner := &cleanupFakeRunner{}
	if _, err := newAppCleanup(t.TempDir(), runner).Cleanup(context.Background(), "../shop"); err == nil {
		t.Fatal("an invalid name was accepted")
	}
	if len(runner.calls) != 0 {
		t.Errorf("docker ran for an invalid name: %v", runner.calls)
	}
}

func TestAppCleanupReportsTheImageListFailure(t *testing.T) {
	runner := &cleanupFakeRunner{listErr: errors.New("docker is not running")}
	if _, err := newAppCleanup(t.TempDir(), runner).Cleanup(context.Background(), "shop"); err == nil ||
		!strings.Contains(err.Error(), "list the app images") {
		t.Fatalf("Cleanup error = %v", err)
	}
}

type fakeAppCleaner struct {
	name   string
	output string
	err    error
	calls  int
}

func (f *fakeAppCleaner) Cleanup(_ context.Context, name string) (string, error) {
	f.calls++
	f.name = name
	return f.output, f.err
}

func TestHandleRunsAppCleanup(t *testing.T) {
	cleaner := &fakeAppCleaner{output: "removed image talaia/shop:abcdef123456"}
	out := newCollector()
	executor := New(&fakeActions{}).WithCleanup(cleaner)
	msg := &agentv1.CoreMsg{
		JobId: "job-cleanup",
		Cmd:   &agentv1.CoreMsg_AppCleanup{AppCleanup: &agentv1.AppCleanup{Name: "shop"}},
	}

	if !executor.Handle(context.Background(), msg, out.emit) {
		t.Fatal("the app cleanup command was not handled")
	}
	if got := out.next(t); got.GetState() != agentv1.JobState_JOB_STATE_RUNNING {
		t.Errorf("first event = %v", got)
	}
	final := out.next(t)
	if final.GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED || final.GetOutput() != cleaner.output {
		t.Errorf("final event = %v", final)
	}
	if cleaner.calls != 1 || cleaner.name != "shop" {
		t.Errorf("cleaner calls = %d, name = %q", cleaner.calls, cleaner.name)
	}
}

func TestHandleRejectsAnInvalidCleanupNameAndMissingCleaner(t *testing.T) {
	for _, test := range []struct {
		name     string
		executor *Executor
		jobID    string
		command  string
	}{
		{
			name:     "invalid name",
			executor: New(&fakeActions{}).WithCleanup(&fakeAppCleaner{}),
			jobID:    "job-cleanup-name",
			command:  "../shop",
		},
		{
			name:     "no cleaner",
			executor: New(&fakeActions{}),
			jobID:    "job-cleanup-none",
			command:  "shop",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := newCollector()
			test.executor.Handle(context.Background(), &agentv1.CoreMsg{
				JobId: test.jobID,
				Cmd:   &agentv1.CoreMsg_AppCleanup{AppCleanup: &agentv1.AppCleanup{Name: test.command}},
			}, out.emit)
			out.next(t) // running
			if final := out.next(t); final.GetState() != agentv1.JobState_JOB_STATE_FAILED {
				t.Errorf("final event = %v", final)
			}
		})
	}
}

package stacks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// fakeProjects knows the running projects by name.
type fakeProjects map[string]Project

func (f fakeProjects) Project(_ context.Context, name string) (Project, bool) {
	project, ok := f[name]
	return project, ok
}

// existingProject lays out a project the way an administrator had it: a directory
// with a compose file, an env file and some data next to them.
func existingProject(t *testing.T) (workingDir, configFile string) {
	t.Helper()

	workingDir = filepath.Join(t.TempDir(), "apps", "wiki")
	if err := os.MkdirAll(filepath.Join(workingDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	configFile = filepath.Join(workingDir, "docker-compose.yml")
	if err := os.WriteFile(configFile, []byte(sampleCompose), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workingDir, ".env"), []byte("DB=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return workingDir, configFile
}

func adoptingManager(t *testing.T, projects fakeProjects) (*Manager, *fakeRunner, string) {
	t.Helper()
	root := t.TempDir()
	runner := &fakeRunner{}
	return New(root, runner).WithProjects(projects), runner, root
}

func TestAdoptRecordsWhereTheProjectIsWithoutCopyingAnything(t *testing.T) {
	workingDir, configFile := existingProject(t)
	m, runner, root := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{configFile}}})

	message, err := m.Adopt(context.Background(), "wiki")
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if message != "stack adopted" {
		t.Errorf("message = %q", message)
	}

	// Only a record exists in the directory of Talaia: no compose file was copied.
	entries, _ := os.ReadDir(filepath.Join(root, "wiki"))
	if len(entries) != 1 || entries[0].Name() != "adopted.json" {
		t.Errorf("the stack directory holds %v, want only adopted.json", entries)
	}
	if len(runner.calls) != 0 {
		t.Error("adopting must not run docker")
	}
	if got := m.List(); !reflect.DeepEqual(got, []string{"wiki"}) {
		t.Errorf("List() = %v", got)
	}
	if got := m.Adopted(); !reflect.DeepEqual(got, []string{"wiki"}) {
		t.Errorf("Adopted() = %v", got)
	}
}

func TestAdoptRefusals(t *testing.T) {
	workingDir, configFile := existingProject(t)
	projects := fakeProjects{
		"wiki":     {WorkingDir: workingDir, ConfigFiles: []string{configFile}},
		"multi":    {WorkingDir: workingDir, ConfigFiles: []string{configFile, configFile}},
		"relative": {WorkingDir: "apps/wiki", ConfigFiles: []string{"docker-compose.yml"}},
		"gone":     {WorkingDir: workingDir, ConfigFiles: []string{filepath.Join(workingDir, "missing.yml")}},
		"nofiles":  {WorkingDir: workingDir},
	}
	m, _, _ := adoptingManager(t, projects)

	for _, name := range []string{"ghost", "multi", "relative", "gone", "nofiles", "../x", "Wiki"} {
		if _, err := m.Adopt(context.Background(), name); err == nil {
			t.Errorf("Adopt(%q) succeeded", name)
		}
	}

	// A name that is already managed cannot be adopted over.
	if _, err := m.Adopt(context.Background(), "wiki"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if _, err := m.Adopt(context.Background(), "wiki"); err == nil {
		t.Error("adopting twice succeeded")
	}

	// Without a project source nothing can be adopted.
	if _, err := New(t.TempDir(), &fakeRunner{}).Adopt(context.Background(), "wiki"); err == nil {
		t.Error("Adopt without a project source succeeded")
	}
}

func TestAdoptedStackIsRunInItsOwnDirectoryWithItsOwnFile(t *testing.T) {
	workingDir, configFile := existingProject(t)
	m, runner, _ := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{configFile}}})
	if _, err := m.Adopt(context.Background(), "wiki"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	if _, err := m.Do(context.Background(), "wiki", agentv1.StackActionType_STACK_ACTION_TYPE_UP); err != nil {
		t.Fatalf("Do: %v", err)
	}

	want := []string{"compose", "--project-name", "wiki", "--project-directory", workingDir, "-f", configFile, "up", "-d", "--remove-orphans"}
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0].args, want) || runner.calls[0].dir != workingDir {
		t.Errorf("ran %v, want %v in %s", runner.calls, want, workingDir)
	}
}

func TestReadAndWriteOfAnAdoptedStackUseTheOriginalFiles(t *testing.T) {
	workingDir, configFile := existingProject(t)
	m, runner, root := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{configFile}}})
	if _, err := m.Adopt(context.Background(), "wiki"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	text, err := m.Read(context.Background(), "wiki")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var files struct{ Compose, Env string }
	if err := json.Unmarshal([]byte(strings.ReplaceAll(strings.ReplaceAll(text, `"compose"`, `"Compose"`), `"env"`, `"Env"`)), &files); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if files.Compose != sampleCompose || files.Env != "DB=1\n" {
		t.Errorf("files = %+v", files)
	}

	updated := "services:\n  web:\n    image: nginx:1.28\n"
	runner.calls = nil
	if _, err := m.Write(context.Background(), "wiki", updated, "DB=2\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// The file at its original place changed and kept its mode; nothing was
	// written under the directory of Talaia except the record.
	got, _ := os.ReadFile(configFile)
	if string(got) != updated {
		t.Errorf("the original compose file holds %q", got)
	}
	// Windows has no Unix modes, so this only holds on a real server.
	if info, _ := os.Stat(configFile); runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Errorf("the compose file mode is %v, want 0640", info.Mode().Perm())
	}
	if env, _ := os.ReadFile(filepath.Join(workingDir, ".env")); string(env) != "DB=2\n" {
		t.Errorf(".env holds %q", env)
	}
	if _, err := os.Stat(filepath.Join(root, "wiki", "compose.yaml")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a compose file was written under the directory of Talaia")
	}

	// It was validated against the project directory, so relative paths resolve.
	if len(runner.calls) != 1 || runner.calls[0].dir != workingDir {
		t.Fatalf("calls = %v", runner.calls)
	}
	args := strings.Join(runner.calls[0].args, " ")
	if !strings.Contains(args, "--project-directory "+workingDir) || !strings.HasSuffix(args, "config --quiet") {
		t.Errorf("validation args = %q", args)
	}
}

func TestInvalidEditOfAnAdoptedStackLeavesTheOriginalAlone(t *testing.T) {
	workingDir, configFile := existingProject(t)
	m, runner, _ := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{configFile}}})
	if _, err := m.Adopt(context.Background(), "wiki"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	runner.err = errors.New("exit status 1")
	runner.output = "bad yaml"

	if _, err := m.Write(context.Background(), "wiki", "broken: [", ""); err == nil {
		t.Fatal("expected an error")
	}
	if got, _ := os.ReadFile(configFile); string(got) != sampleCompose {
		t.Errorf("the original file was changed to %q", got)
	}
}

func TestForgetDeletesOnlyTheRecord(t *testing.T) {
	workingDir, configFile := existingProject(t)
	m, runner, root := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{configFile}}})
	if _, err := m.Adopt(context.Background(), "wiki"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	if _, err := m.Do(context.Background(), "wiki", agentv1.StackActionType_STACK_ACTION_TYPE_FORGET); err != nil {
		t.Fatalf("Do: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "wiki")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the record still exists: %v", err)
	}
	for _, kept := range []string{configFile, filepath.Join(workingDir, ".env"), filepath.Join(workingDir, "data")} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s was deleted: %v", kept, err)
		}
	}
	if len(runner.calls) != 0 {
		t.Error("forgetting must not run docker: the containers keep running")
	}
}

func TestBrokenAdoptedStackIsReportedAndCanOnlyBeForgotten(t *testing.T) {
	tests := []struct {
		name   string
		remove func(t *testing.T, workingDir, configFile string)
	}{
		{
			name: "compose file gone",
			remove: func(t *testing.T, _, configFile string) {
				t.Helper()
				if err := os.Remove(configFile); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "directory gone",
			remove: func(t *testing.T, workingDir, _ string) {
				t.Helper()
				if err := os.RemoveAll(workingDir); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workingDir, configFile := existingProject(t)
			m, _, root := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{configFile}}})
			if _, err := m.Adopt(context.Background(), "wiki"); err != nil {
				t.Fatalf("Adopt: %v", err)
			}
			tt.remove(t, workingDir, configFile)

			if got := m.Broken(); !reflect.DeepEqual(got, []string{"wiki"}) {
				t.Errorf("Broken() = %v, want [wiki]", got)
			}
			if got := m.Adopted(); !reflect.DeepEqual(got, []string{"wiki"}) {
				t.Errorf("Adopted() = %v, want [wiki]", got)
			}

			for name, run := range map[string]func() error{
				"read":  func() error { _, err := m.Read(context.Background(), "wiki"); return err },
				"write": func() error { _, err := m.Write(context.Background(), "wiki", sampleCompose, ""); return err },
				"up": func() error {
					_, err := m.Do(context.Background(), "wiki", agentv1.StackActionType_STACK_ACTION_TYPE_UP)
					return err
				},
			} {
				err := run()
				if err == nil || !strings.Contains(err.Error(), "gone") || !strings.Contains(err.Error(), "forget") {
					t.Errorf("%s error = %v, want a message about the missing files and forgetting", name, err)
				}
			}

			if _, err := m.Do(context.Background(), "wiki", agentv1.StackActionType_STACK_ACTION_TYPE_FORGET); err != nil {
				t.Fatalf("forget a broken stack: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "wiki")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the record still exists: %v", err)
			}
			if got := m.List(); len(got) != 0 {
				t.Errorf("List() = %v, want empty", got)
			}
		})
	}
}

func TestBrokenRefusesASymbolicLinkInPlaceOfTheFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symbolic links needs privileges on Windows")
	}
	workingDir, configFile := existingProject(t)
	other := filepath.Join(t.TempDir(), "other.yml")
	if err := os.WriteFile(other, []byte(sampleCompose), 0o640); err != nil {
		t.Fatal(err)
	}
	m, _, _ := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{configFile}}})
	if _, err := m.Adopt(context.Background(), "wiki"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if err := os.Remove(configFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, configFile); err != nil {
		t.Fatal(err)
	}

	if got := m.Broken(); !reflect.DeepEqual(got, []string{"wiki"}) {
		t.Errorf("Broken() = %v, want [wiki]", got)
	}
}

func TestRemoveIsRefusedForAnAdoptedStackAndForgetForAManagedOne(t *testing.T) {
	workingDir, configFile := existingProject(t)
	m, runner, root := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{configFile}}})
	if _, err := m.Adopt(context.Background(), "wiki"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if _, err := m.Write(context.Background(), "shop", sampleCompose, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}
	runner.calls = nil

	if _, err := m.Do(context.Background(), "wiki", agentv1.StackActionType_STACK_ACTION_TYPE_REMOVE); err == nil {
		t.Error("an adopted stack was removed")
	}
	if _, err := os.Stat(configFile); err != nil {
		t.Errorf("the original compose file was deleted: %v", err)
	}
	if _, err := m.Do(context.Background(), "shop", agentv1.StackActionType_STACK_ACTION_TYPE_FORGET); err == nil {
		t.Error("a managed stack was forgotten")
	}
	if _, err := os.Stat(filepath.Join(root, "shop", "compose.yaml")); err != nil {
		t.Errorf("the managed stack was deleted: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Errorf("docker ran: %v", runner.calls)
	}
}

func TestADamagedRecordIsNotTrusted(t *testing.T) {
	m, _, root := adoptingManager(t, nil)
	if err := os.MkdirAll(filepath.Join(root, "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "wiki", "adopted.json"), []byte(`{"working_dir":"relative","config_file":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Do(context.Background(), "wiki", agentv1.StackActionType_STACK_ACTION_TYPE_UP); err == nil {
		t.Error("a record with relative paths was accepted")
	}
}

func TestAdoptRefusesAWritableProjectAndSaysHowToFixIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not meaningful on Windows")
	}
	workingDir, configFile := existingProject(t)
	if err := os.Chmod(workingDir, 0o775); err != nil {
		t.Fatal(err)
	}
	m, _, _ := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{configFile}}})

	_, err := m.Adopt(context.Background(), "wiki")
	if err == nil {
		t.Fatal("Adopt accepted a directory writable by its group")
	}
	want := "chmod go-w '" + workingDir + "' '" + configFile + "'"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to suggest %q", err, want)
	}
}

func TestShellQuote(t *testing.T) {
	if got, want := shellQuote("/srv/it's here"), `'/srv/it'''s here'`; got != want {
		t.Errorf("shellQuote = %s, want %s", got, want)
	}
}

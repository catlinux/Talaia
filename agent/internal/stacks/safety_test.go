package stacks

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// skipWithoutUnix skips the tests of symbolic links and mode bits, which Windows
// does not have in the same way. The agent only runs on Linux, where the CI runs
// them.
func skipWithoutUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs Unix symbolic links and modes")
	}
}

func TestReplaceFileDoesNotFollowAPlantedLink(t *testing.T) {
	skipWithoutUnix(t)
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "compose.yaml")
	// The names an older version wrote to, and a link in place of the file.
	for _, link := range []string{target + ".new", target} {
		if err := os.Symlink(victim, link); err != nil {
			t.Fatal(err)
		}
	}

	if err := replaceFile(target, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatalf("replaceFile: %v", err)
	}
	if data, _ := os.ReadFile(victim); string(data) != "keep" {
		t.Errorf("the file behind the link was changed to %q", data)
	}
	info, err := os.Lstat(target)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("the target is not a regular file: %v, %v", info, err)
	}
	if data, _ := os.ReadFile(target); string(data) != "services: {}\n" {
		t.Errorf("target = %q", data)
	}
}

func TestReadOfAnAdoptedStackRefusesLinks(t *testing.T) {
	skipWithoutUnix(t)
	workingDir, configFile := existingProject(t)
	m, _, _ := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{configFile}}})
	if _, err := m.Adopt(context.Background(), "wiki"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	secret := filepath.Join(t.TempDir(), "shadow")
	if err := os.WriteFile(secret, []byte("root:hash"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(configFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, configFile); err != nil {
		t.Fatal(err)
	}

	out, err := m.Read(context.Background(), "wiki")
	if err == nil || strings.Contains(out, "root:hash") {
		t.Errorf("Read = %q, %v; want a refusal", out, err)
	}
}

func TestAdoptRefusesFilesOthersCanChange(t *testing.T) {
	skipWithoutUnix(t)
	workingDir, configFile := existingProject(t)
	m, _, _ := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{configFile}}})

	if err := os.Chmod(configFile, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Adopt(context.Background(), "wiki"); err == nil {
		t.Error("a compose file anyone can write was adopted")
	}

	if err := os.Chmod(configFile, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(workingDir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Adopt(context.Background(), "wiki"); err == nil {
		t.Error("a directory anyone can write was adopted")
	}
}

func TestAdoptRefusesALinkAsComposeFile(t *testing.T) {
	skipWithoutUnix(t)
	workingDir, configFile := existingProject(t)
	link := filepath.Join(workingDir, "linked.yml")
	if err := os.Symlink(configFile, link); err != nil {
		t.Fatal(err)
	}
	m, _, _ := adoptingManager(t, fakeProjects{"wiki": {WorkingDir: workingDir, ConfigFiles: []string{link}}})
	if _, err := m.Adopt(context.Background(), "wiki"); err == nil {
		t.Error("a compose file that is a link was adopted")
	}
}

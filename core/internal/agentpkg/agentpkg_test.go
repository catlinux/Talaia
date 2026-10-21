package agentpkg

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInfoHashesBothArchitectures(t *testing.T) {
	dir := t.TempDir()
	writePackage(t, dir, "amd64", "amd64 package")
	writePackage(t, dir, "arm64", "arm64 package")

	tests := []struct {
		arch    string
		content string
	}{
		{arch: "amd64", content: "amd64 package"},
		{arch: "arm64", content: "arm64 package"},
	}
	for _, tt := range tests {
		got, err := Info(dir, tt.arch)
		if err != nil {
			t.Fatalf("Info(%q): %v", tt.arch, err)
		}
		if want := hashOf(tt.content); got != want {
			t.Errorf("Info(%q) = %q, want %q", tt.arch, got, want)
		}
	}
}

func TestInfoRejectsMissingPackagesAndUnknownArchitectures(t *testing.T) {
	dir := t.TempDir()
	writePackage(t, dir, "amd64", "only amd64")
	if _, err := Info(dir, "arm64"); err == nil {
		t.Error("a missing package was accepted")
	}
	if _, err := Info(dir, "386"); err == nil {
		t.Error("an unsupported architecture was accepted")
	}
	if _, err := Info("", "amd64"); err == nil {
		t.Error("an empty package directory was accepted")
	}
}

func TestInfoCachesBySizeAndModificationTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "talaia-agent_amd64.deb")
	originalTime := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	if err := os.WriteFile(path, []byte("first v1"), 0o600); err != nil {
		t.Fatalf("write package: %v", err)
	}
	if err := os.Chtimes(path, originalTime, originalTime); err != nil {
		t.Fatalf("set package time: %v", err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat package: %v", err)
	}
	if !stat.ModTime().Equal(originalTime) {
		t.Skip("this filesystem does not keep the modification time")
	}

	first, err := Info(dir, "amd64")
	if err != nil {
		t.Fatalf("first Info: %v", err)
	}
	if want := hashOf("first v1"); first != want {
		t.Fatalf("first Info = %q, want %q", first, want)
	}

	// Same size and modification time: the cached hash is still valid.
	if err := os.WriteFile(path, []byte("other v2"), 0o600); err != nil {
		t.Fatalf("rewrite package: %v", err)
	}
	if err := os.Chtimes(path, originalTime, originalTime); err != nil {
		t.Fatalf("restore package time: %v", err)
	}
	second, err := Info(dir, "amd64")
	if err != nil {
		t.Fatalf("second Info: %v", err)
	}
	if second != first {
		t.Errorf("the cache was not used: first %q, second %q", first, second)
	}

	// A changed modification time invalidates the cache.
	later := originalTime.Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatalf("change package time: %v", err)
	}
	third, err := Info(dir, "amd64")
	if err != nil {
		t.Fatalf("third Info: %v", err)
	}
	if want := hashOf("other v2"); third != want {
		t.Errorf("third Info = %q, want %q", third, want)
	}
}

func TestNewer(t *testing.T) {
	tests := []struct {
		name    string
		offered string
		running string
		want    bool
	}{
		{name: "patch", offered: "1.2.4", running: "1.2.3", want: true},
		{name: "minor", offered: "1.3.0", running: "1.2.3", want: true},
		{name: "major", offered: "2.0.0", running: "1.2.3", want: true},
		{name: "numeric not lexical", offered: "1.10.0", running: "1.9.9", want: true},
		{name: "equal", offered: "1.2.3", running: "1.2.3", want: false},
		{name: "older", offered: "1.2.2", running: "1.2.3", want: false},
		{name: "older minor", offered: "1.9.9", running: "1.10.0", want: false},
		{name: "dev agent", offered: "1.0.0", running: "dev", want: true},
		{name: "empty agent", offered: "1.0.0", running: "", want: true},
		{name: "non-semver agent", offered: "1.0.0", running: "v1.0.0", want: true},
		{name: "invalid offered", offered: "1.0", running: "dev", want: false},
		{name: "offered with leading zero", offered: "01.0.0", running: "dev", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Newer(tt.offered, tt.running); got != tt.want {
				t.Errorf("Newer(%q, %q) = %t, want %t", tt.offered, tt.running, got, tt.want)
			}
		})
	}
}

func TestNormalizeArch(t *testing.T) {
	tests := []struct {
		value string
		want  string
		ok    bool
	}{
		{value: "amd64", want: "amd64", ok: true},
		{value: "x86_64", want: "amd64", ok: true},
		{value: "x86-64", want: "amd64", ok: true},
		{value: "arm64", want: "arm64", ok: true},
		{value: "aarch64", want: "arm64", ok: true},
		{value: " AMD64 ", want: "amd64", ok: true},
		{value: "386", ok: false},
		{value: "", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, ok := NormalizeArch(tt.value)
			if got != tt.want || ok != tt.ok {
				t.Errorf("NormalizeArch(%q) = %q, %t, want %q, %t", tt.value, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestPackageName(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		want := "talaia-agent_" + arch + ".deb"
		if got, ok := PackageName(arch); !ok || got != want {
			t.Errorf("PackageName(%q) = %q, %t, want %q", arch, got, ok, want)
		}
	}
	if _, ok := PackageName("386"); ok {
		t.Error("386 was accepted")
	}
}

func TestValidVersion(t *testing.T) {
	for _, version := range []string{"0.0.0", "1.2.3", "1.10.20"} {
		if !ValidVersion(version) {
			t.Errorf("ValidVersion(%q) = false", version)
		}
	}
	for _, version := range []string{"", "dev", "1.2", "1.2.3.4", "01.2.3", "v1.2.3", "1.2.3-rc1"} {
		if ValidVersion(version) {
			t.Errorf("ValidVersion(%q) = true", version)
		}
	}
}

func writePackage(t *testing.T, dir, arch, content string) {
	t.Helper()
	name, ok := PackageName(arch)
	if !ok {
		t.Fatalf("package name for %q", arch)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write package: %v", err)
	}
}

func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommand(t *testing.T) {
	hash := strings.Repeat("cd", 32)
	got := Command("https://192.0.2.10:8443", "tl_enr_abc", "ff00", hash)
	want := `f=$(mktemp) && curl -fsSk https://192.0.2.10:8443/install.sh -o "$f" && echo "` + hash +
		`  $f" | sha256sum -c - && sudo bash "$f" --url https://192.0.2.10:8443 --token tl_enr_abc --ca-fingerprint ff00; rm -f "$f"`
	if got != want {
		t.Errorf("Command() = %q, want %q", got, want)
	}

	if got := Command("", "tl_enr_abc", "ff00", hash); got != "" {
		t.Errorf("Command() without an address = %q, want empty", got)
	}
	if got := Command("https://192.0.2.10:8443", "tl_enr_abc", "ff00", ""); got != "" {
		t.Errorf("Command() without a script hash = %q, want empty", got)
	}
}

func TestScriptSHA256(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), []byte("abc"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// SHA-256 of "abc".
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := ScriptSHA256(dir); got != want {
		t.Errorf("ScriptSHA256() = %q, want %q", got, want)
	}
	if got := ScriptSHA256(t.TempDir()); got != "" {
		t.Errorf("ScriptSHA256() of an empty directory = %q, want empty", got)
	}
	if got := ScriptSHA256(""); got != "" {
		t.Errorf("ScriptSHA256() without a directory = %q, want empty", got)
	}
}

func TestUpgradeCommand(t *testing.T) {
	hash := strings.Repeat("cd", 32)
	got := UpgradeCommand("https://192.0.2.10:8443", "ff00", hash)
	want := `f=$(mktemp) && curl -fsSk https://192.0.2.10:8443/install.sh -o "$f" && echo "` + hash +
		`  $f" | sha256sum -c - && sudo bash "$f" --upgrade --url https://192.0.2.10:8443 --ca-fingerprint ff00; rm -f "$f"`
	if got != want {
		t.Errorf("UpgradeCommand() = %q, want %q", got, want)
	}
	if strings.Contains(got, "--token") {
		t.Error("the upgrade command must not carry a token")
	}
	if got := UpgradeCommand("", "ff00", hash); got != "" {
		t.Errorf("UpgradeCommand() without an address = %q, want empty", got)
	}
}

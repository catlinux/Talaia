package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/store"
)

type plainOpener struct{}

func (plainOpener) Open(sealed []byte) ([]byte, error) {
	return sealed, nil
}

type failingOpener struct {
	err error
}

func (o failingOpener) Open([]byte) ([]byte, error) {
	return nil, o.err
}

func TestEscapeEnvValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain", value: "hello", want: "hello"},
		{name: "quote", value: `a"b`, want: `a\"b`},
		{name: "backslash", value: `a\b`, want: `a\\b`},
		{name: "newline", value: "a\nb", want: `a\nb`},
		{name: "carriage return", value: "a\rb", want: "ab"},
		{name: "dollar", value: "$HOME", want: "$$HOME"},
		{name: "unicode", value: "café", want: "café"},
		{name: "empty", value: "", want: ""},
		{name: "combination", value: "a\\\r\n\"$b", want: `a\\\n\"$$b`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := escapeEnvValue(test.value); got != test.want {
				t.Errorf("escapeEnvValue(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestRenderEnvFileSortsAndExcludesTheTemplatePassword(t *testing.T) {
	envs := []store.AppEnv{
		{Key: "Z_LAST", ValueEnc: []byte("last")},
		{Key: "A_FIRST", ValueEnc: []byte("first")},
		{Key: "BACKSLASH", ValueEnc: []byte(`a\b`)},
		{Key: "CR", ValueEnc: []byte("a\rb")},
		{Key: "DOLLAR", ValueEnc: []byte("$HOME")},
		{Key: "EMPTY", ValueEnc: []byte("")},
		{Key: "NEWLINE", ValueEnc: []byte("a\nb")},
		{Key: "QUOTE", ValueEnc: []byte(`a"b`)},
		{Key: "TEMPLATE_PASSWORD", ValueEnc: []byte("do-not-export")},
		{Key: "UNICODE", ValueEnc: []byte("café")},
	}
	got, count, err := renderEnvFile(envs, plainOpener{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 9 {
		t.Errorf("count = %d, want 9", count)
	}
	want := `A_FIRST="first"
BACKSLASH="a\\b"
CR="ab"
DOLLAR="$$HOME"
EMPTY=""
NEWLINE="a\nb"
QUOTE="a\"b"
UNICODE="café"
Z_LAST="last"
`
	if got != want {
		t.Errorf("renderEnvFile() =\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "TEMPLATE_PASSWORD") || strings.Contains(got, "do-not-export") {
		t.Errorf("the template password was exported:\n%s", got)
	}
}

func TestRenderEnvFileWithoutVariables(t *testing.T) {
	envs := []store.AppEnv{{Key: "TEMPLATE_PASSWORD", ValueEnc: []byte("secret")}}
	got, count, err := renderEnvFile(envs, plainOpener{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" || count != 0 {
		t.Errorf("renderEnvFile() = %q, %d; want empty", got, count)
	}
}

func TestRenderEnvFileDoesNotLeakValuesInErrors(t *testing.T) {
	secret := "very-secret-value"
	_, _, err := renderEnvFile([]store.AppEnv{{
		Key: "DATABASE_URL", ValueEnc: []byte(secret),
	}}, failingOpener{err: errors.New("cannot open")})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error leaked the secret: %v", err)
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("the error does not name the variable: %v", err)
	}

	_, _, err = renderEnvFile([]store.AppEnv{{
		Key: "DATABASE_URL", ValueEnc: []byte(secret + "\x00"),
	}}, plainOpener{})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("the NUL error leaked the secret or was missing: %v", err)
	}
}

func TestAddServiceEnvFileOnlyChangesTheSelectedService(t *testing.T) {
	compose := "services:\n  app:\n    image: nginx:1.27\n    environment:\n      KEEP: yes\n  db:\n    image: postgres:17\n"
	got, ok := addServiceEnvFile(compose, "app")
	if !ok {
		t.Fatal("the service was not found")
	}
	if strings.Count(got, "env_file: [.env]") != 1 {
		t.Fatalf("env_file was not added exactly once:\n%s", got)
	}
	if !strings.Contains(got, "  app:\n    env_file: [.env]\n    image: nginx:1.27") {
		t.Errorf("env_file was added to the wrong place:\n%s", got)
	}
	if !strings.Contains(got, "  db:\n    image: postgres:17") {
		t.Errorf("the other service changed:\n%s", got)
	}
	again, ok := addServiceEnvFile(got, "app")
	if !ok || again != got {
		t.Errorf("addServiceEnvFile is not idempotent:\n%s", again)
	}
	if !serviceHasEnvFile(got, "app") || serviceHasEnvFile(got, "db") {
		t.Error("serviceHasEnvFile reported the wrong service")
	}
}

func TestAddServiceEnvFileReportsAMissingService(t *testing.T) {
	if _, ok := addServiceEnvFile("services:\n  db:\n    image: postgres:17\n", "app"); ok {
		t.Fatal("a missing service was reported as found")
	}
}

func TestComposeConfigResolvesGeneratedEnvFile(t *testing.T) {
	dockerComposeOrSkip(t)

	app := testApp()
	compose, err := generateCompose(app, "app", "talaia/shop:abcdef123456", false, 23456)
	if err != nil {
		t.Fatal(err)
	}
	envs := []store.AppEnv{
		{Key: "QUOTE", ValueEnc: []byte(`a"b`)},
		{Key: "BACKSLASH", ValueEnc: []byte(`a\b`)},
		{Key: "NEWLINE", ValueEnc: []byte("line1\nline2")},
		{Key: "DOLLAR", ValueEnc: []byte("$HOME")},
		{Key: "UNICODE", ValueEnc: []byte("café")},
		{Key: "CR", ValueEnc: []byte("a\rb")},
	}
	env, count, err := renderEnvFile(envs, plainOpener{})
	if err != nil || count != len(envs) {
		t.Fatalf("renderEnvFile() = %d, %v", count, err)
	}

	dir := t.TempDir()
	composePath := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composePath, []byte(compose), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", "compose", "-p", "talaia-env-test", "-f", composePath, "config", "--format", "json")
	command.Dir = dir
	output, err := command.Output()
	if err != nil {
		detail := ""
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			detail = string(exit.Stderr)
		}
		t.Fatalf("docker compose config: %v\n%s", err, detail)
	}

	var config struct {
		Services map[string]struct {
			Environment map[string]string `json:"environment"`
		} `json:"services"`
	}
	if err := json.Unmarshal(output, &config); err != nil {
		t.Fatalf("decode docker compose config: %v\n%s", err, output)
	}
	service, ok := config.Services["app"]
	if !ok {
		t.Fatalf("the app service is missing:\n%s", output)
	}
	for key, want := range map[string]string{
		"QUOTE":     `a"b`,
		"BACKSLASH": `a\b`,
		"NEWLINE":   "line1\nline2",
		"DOLLAR":    "$HOME",
		"UNICODE":   "café",
		"CR":        "ab",
	} {
		if got, exists := service.Environment[key]; !exists || got != want {
			t.Errorf("environment[%s] = %q, present = %v; want %q", key, got, exists, want)
		}
	}
}

func dockerComposeOrSkip(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "compose", "version").Run(); err != nil {
		t.Skipf("docker compose is not available: %v", err)
	}
}

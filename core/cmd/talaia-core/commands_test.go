package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/catlinux/Talaia/core/internal/store"
)

type fakeTokenCreator struct {
	token string
	err   error
	name  string
	ttl   time.Duration
}

func (f *fakeTokenCreator) CreateEnrollmentToken(_ context.Context, serverName string, ttl time.Duration) (string, error) {
	f.name = serverName
	f.ttl = ttl
	return f.token, f.err
}

type fakeRevoker struct {
	err  error
	last string
}

func (f *fakeRevoker) RevokeServer(_ context.Context, id string) error {
	f.last = id
	return f.err
}

func TestSplitCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantCommand string
		wantRest    []string
	}{
		{name: "no arguments", args: nil, wantCommand: "", wantRest: nil},
		{name: "serve", args: []string{"serve"}, wantCommand: "serve", wantRest: nil},
		{
			name:        "token create",
			args:        []string{"token", "create", "--name", "vps-1"},
			wantCommand: "token",
			wantRest:    []string{"create", "--name", "vps-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, rest := splitCommand(tt.args)
			if command != tt.wantCommand {
				t.Fatalf("command = %q, want %q", command, tt.wantCommand)
			}
			if len(rest) != len(tt.wantRest) {
				t.Fatalf("rest = %v, want %v", rest, tt.wantRest)
			}
			for i := range rest {
				if rest[i] != tt.wantRest[i] {
					t.Fatalf("rest = %v, want %v", rest, tt.wantRest)
				}
			}
		})
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"nope"}, &stdout, &stderr)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unknown command "nope"`) {
		t.Errorf("stderr = %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Usage: talaia-core") {
		t.Errorf("stderr = %q, want the usage", stderr.String())
	}
}

func TestRunMissingSubcommands(t *testing.T) {
	for _, command := range []string{"token", "server", "user", "session"} {
		t.Run(command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			if code := run([]string{command}, &stdout, &stderr); code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), "missing subcommand") {
				t.Errorf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestRunUnknownSubcommands(t *testing.T) {
	for _, args := range [][]string{{"token", "nope"}, {"server", "nope"}, {"user", "nope"}, {"session", "nope"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			if code := run(args, &stdout, &stderr); code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
			if !strings.Contains(stderr.String(), "unknown subcommand") {
				t.Errorf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestTokenCreatePrintsTheToken(t *testing.T) {
	creator := &fakeTokenCreator{token: "tl_enr_dGVzdA"}
	fingerprint := strings.Repeat("ab", 32)
	scriptHash := strings.Repeat("cd", 32)
	var stdout bytes.Buffer

	if err := tokenCreate(context.Background(), &stdout, creator, "vps-web-01", 15*time.Minute, fingerprint, "https://192.0.2.10:8443", scriptHash); err != nil {
		t.Fatalf("tokenCreate: %v", err)
	}

	printed := stdout.String()
	for _, want := range []string{
		"token: tl_enr_dGVzdA",
		"ca-fingerprint: " + fingerprint,
		"shown only once",
		"curl -fsSk https://192.0.2.10:8443/install.sh -o",
		scriptHash + "  $f\" | sha256sum -c -",
		"--token tl_enr_dGVzdA --ca-fingerprint " + fingerprint,
	} {
		if !strings.Contains(printed, want) {
			t.Errorf("output %q does not contain %q", printed, want)
		}
	}
	if creator.name != "vps-web-01" || creator.ttl != 15*time.Minute {
		t.Errorf("creator called with %q and %s", creator.name, creator.ttl)
	}
}

func TestTokenCreateReportsErrors(t *testing.T) {
	creator := &fakeTokenCreator{err: errors.New("server is already enrolled")}
	var stdout bytes.Buffer

	err := tokenCreate(context.Background(), &stdout, creator, "vps-web-01", time.Minute, "abcd", "", "")

	if err == nil {
		t.Fatal("expected an error")
	}
	if stdout.Len() != 0 {
		t.Errorf("nothing must be printed on error, got %q", stdout.String())
	}
}

func TestServerRevokePrintsTheId(t *testing.T) {
	revoker := &fakeRevoker{}
	var stdout bytes.Buffer

	if err := serverRevoke(context.Background(), &stdout, revoker, "11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatalf("serverRevoke: %v", err)
	}

	if stdout.String() != "revoked 11111111-1111-1111-1111-111111111111\n" {
		t.Errorf("output = %q", stdout.String())
	}
	if revoker.last != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("revoker called with %q", revoker.last)
	}
}

func TestServerRevokeUnknownServer(t *testing.T) {
	revoker := &fakeRevoker{err: store.ErrNotFound}
	var stdout bytes.Buffer

	err := serverRevoke(context.Background(), &stdout, revoker, "abc")

	if err == nil || !strings.Contains(err.Error(), "no server with id abc") {
		t.Fatalf("error = %v, want a clear not found message", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("nothing must be printed on error, got %q", stdout.String())
	}
}

// TestCommandsAgainstDatabase runs the real commands against the local
// PostgreSQL. It is skipped when TALAIA_TEST_DATABASE_URL is not set.
func TestCommandsAgainstDatabase(t *testing.T) {
	url := os.Getenv("TALAIA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TALAIA_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	dataDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "core.toml")
	if err := os.WriteFile(configPath, []byte{}, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("TALAIA_CONFIG", configPath)
	t.Setenv("TALAIA_DATABASE_URL", url)
	t.Setenv("TALAIA_DATA_DIR", dataDir)

	name := fmt.Sprintf("test-%d", time.Now().UnixNano())
	revokeName := name + "-revoke"

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM enrollment_token WHERE server_name = $1`, name)
		_, _ = pool.Exec(ctx, `DELETE FROM server WHERE name = $1 OR name = $2`, name, revokeName)
		pool.Close()
	})

	var stdout, stderr bytes.Buffer
	if code := run([]string{"token", "create", "--name", name}, &stdout, &stderr); code != 0 {
		t.Fatalf("token create exit code = %d, stderr = %s", code, stderr.String())
	}

	printed := stdout.String()
	if !strings.Contains(printed, "token: tl_enr_") {
		t.Errorf("output = %q, want an enrollment token", printed)
	}
	fingerprintFromOutput(t, printed)

	// The CA is created inside data_dir.
	if _, err := os.Stat(filepath.Join(dataDir, "ca", "ca.crt")); err != nil {
		t.Errorf("ca.crt: %v", err)
	}

	// The same name cannot be enrolled twice.
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"token", "create", "--name", name}, &stdout, &stderr); code != 1 {
		t.Fatalf("second token create exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "already") {
		t.Errorf("stderr = %q, want a clear message", stderr.String())
	}

	// server revoke.
	var serverID string
	if err := pool.QueryRow(ctx, `INSERT INTO server (name) VALUES ($1) RETURNING id`, revokeName).Scan(&serverID); err != nil {
		t.Fatalf("insert server: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"server", "revoke", serverID}, &stdout, &stderr); code != 0 {
		t.Fatalf("server revoke exit code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.String() != "revoked "+serverID+"\n" {
		t.Errorf("output = %q", stdout.String())
	}

	var revoked *time.Time
	if err := pool.QueryRow(ctx, `SELECT revoked_at FROM server WHERE id = $1`, serverID).Scan(&revoked); err != nil {
		t.Fatalf("read revoked_at: %v", err)
	}
	if revoked == nil {
		t.Error("revoked_at is not set")
	}
}

func fingerprintFromOutput(t *testing.T, output string) string {
	t.Helper()

	for _, line := range strings.Split(output, "\n") {
		value, ok := strings.CutPrefix(line, "ca-fingerprint: ")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) != 64 {
			t.Fatalf("fingerprint = %q, want 64 hex characters", value)
		}
		if _, err := hex.DecodeString(value); err != nil {
			t.Fatalf("fingerprint %q is not hexadecimal: %v", value, err)
		}
		return value
	}

	t.Fatalf("no ca-fingerprint line in %q", output)
	return ""
}

type fakeLister struct {
	servers []store.Server
	err     error
}

func (f *fakeLister) ListServers(context.Context) ([]store.Server, error) {
	return f.servers, f.err
}

func TestServerListPrintsIdsNamesAndState(t *testing.T) {
	lister := &fakeLister{servers: []store.Server{
		{ID: "c7a3a3b1", Name: "vps-web-01"},
		{ID: "9f2e0d44", Name: "vps-old", RevokedAt: time.Now()},
	}}
	var stdout bytes.Buffer

	if err := serverList(context.Background(), &stdout, lister); err != nil {
		t.Fatalf("serverList: %v", err)
	}

	for _, want := range []string{"c7a3a3b1  vps-web-01  active", "9f2e0d44  vps-old  revoked"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output %q does not contain %q", stdout.String(), want)
		}
	}
}

func TestServerListWithoutServers(t *testing.T) {
	var stdout bytes.Buffer

	if err := serverList(context.Background(), &stdout, &fakeLister{}); err != nil {
		t.Fatalf("serverList: %v", err)
	}
	if !strings.Contains(stdout.String(), "no servers enrolled") {
		t.Errorf("output = %q", stdout.String())
	}
}

func TestServerListReportsErrors(t *testing.T) {
	var stdout bytes.Buffer

	if err := serverList(context.Background(), &stdout, &fakeLister{err: errors.New("db down")}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestUpgradeCommandPrintsTheCommand(t *testing.T) {
	var stdout bytes.Buffer

	if err := upgradeCommand(&stdout, "https://192.0.2.10:8443", strings.Repeat("ab", 32), strings.Repeat("cd", 32)); err != nil {
		t.Fatalf("upgradeCommand: %v", err)
	}
	for _, want := range []string{"--upgrade --url https://192.0.2.10:8443", "--ca-fingerprint " + strings.Repeat("ab", 32), "sha256sum -c -"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("output %q does not contain %q", stdout.String(), want)
		}
	}
}

func TestUpgradeCommandNeedsAnAddressAndTheInstaller(t *testing.T) {
	var stdout bytes.Buffer

	if err := upgradeCommand(&stdout, "", "ab", "cd"); err == nil {
		t.Error("expected an error without a public address")
	}
	if err := upgradeCommand(&stdout, "https://192.0.2.10:8443", "ab", ""); err == nil {
		t.Error("expected an error without the installer")
	}
	if stdout.Len() != 0 {
		t.Errorf("nothing must be printed on error, got %q", stdout.String())
	}
}

type fakeSessions []store.TerminalSession

func (f fakeSessions) ListTerminalSessions(context.Context, int) ([]store.TerminalSession, error) {
	return f, nil
}

func TestTerminalList(t *testing.T) {
	var out bytes.Buffer
	if err := terminalList(context.Background(), &out, fakeSessions{}); err != nil || strings.TrimSpace(out.String()) != "no terminal sessions" {
		t.Errorf("empty list = %q, %v", out.String(), err)
	}

	opened := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	sessions := fakeSessions{
		{ID: "id-open", Container: "web", CreatedBy: "admin", OpenedAt: opened},
		{ID: "id-done", Container: "db", CreatedBy: "admin", OpenedAt: opened, ClosedAt: opened.Add(90 * time.Second),
			CloseReason: "exited", Privileged: true, RecordedBytes: 1234},
	}
	out.Reset()
	if err := terminalList(context.Background(), &out, sessions); err != nil {
		t.Fatalf("terminalList: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "id-open  2026-10-20T10:00:00Z  open  admin  web") ||
		!strings.Contains(lines[1], "exited  admin  db  1m30s  1234 bytes  root-equivalent") {
		t.Errorf("list =\n%s", out.String())
	}
}

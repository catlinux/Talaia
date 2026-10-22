//go:build linux

package terminal

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestHostBackendStartsAWorkingShellAndReapsIt(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("/bin/sh is not available")
	}
	process, err := NewHostBackend().Start(context.Background(), []string{"/bin/sh", "-l"}, 80, 24)
	if err != nil {
		t.Fatalf("start the host shell: %v", err)
	}
	host, ok := process.(*hostProcess)
	if !ok {
		t.Fatalf("process = %T, want *hostProcess", process)
	}
	pid := host.command.Process.Pid
	wantDir := "/"
	if os.Geteuid() == 0 {
		if info, err := os.Stat("/root"); err == nil && info.IsDir() {
			wantDir = "/root"
		}
	}
	if host.command.Dir != wantDir {
		t.Errorf("working directory = %q, want %q", host.command.Dir, wantDir)
	}
	lang := strings.TrimSpace(os.Getenv("LANG"))
	if lang == "" {
		lang = "C.UTF-8"
	}
	wantEnv := []string{
		"TERM=xterm-256color",
		"HOME=/root",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG=" + lang,
		"USER=root",
		"SHELL=/bin/sh",
	}
	if !slices.Equal(host.command.Env, wantEnv) {
		t.Errorf("environment = %v, want %v", host.command.Env, wantEnv)
	}
	defer func() {
		_ = process.Close()
		select {
		case <-host.done:
		case <-time.After(5 * time.Second):
		}
	}()

	if _, err := process.Write([]byte("printf 'HOSTPTY-%s\\n' ok\n")); err != nil {
		t.Fatalf("write to the host shell: %v", err)
	}
	var output []byte
	buffer := make([]byte, 512)
	deadline := time.Now().Add(5 * time.Second)
	timeout := time.AfterFunc(5*time.Second, func() { _ = process.Close() })
	defer timeout.Stop()
	for !strings.Contains(string(output), "HOSTPTY-ok") && time.Now().Before(deadline) {
		n, err := process.Read(buffer)
		if n > 0 {
			output = append(output, buffer[:n]...)
		}
		if err != nil {
			break
		}
	}
	timeout.Stop()
	if !strings.Contains(string(output), "HOSTPTY-ok") {
		t.Fatalf("the host shell did not answer: %q", output)
	}

	if err := process.Resize(context.Background(), 120, 40); err != nil {
		t.Fatalf("resize the host terminal: %v", err)
	}
	rows, cols, err := pty.Getsize(host.master)
	if err != nil {
		t.Fatalf("read the host terminal size: %v", err)
	}
	if rows != 40 || cols != 120 {
		t.Errorf("terminal size = %dx%d, want 40x120", rows, cols)
	}

	if err := process.Close(); err != nil {
		t.Fatalf("close the host terminal: %v", err)
	}
	select {
	case <-host.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the host shell was not reaped")
	}
	if err := syscall.Kill(pid, 0); err == nil {
		t.Errorf("process %d still exists after close", pid)
	} else if !errors.Is(err, syscall.ESRCH) {
		t.Errorf("check process %d: %v", pid, err)
	}
}

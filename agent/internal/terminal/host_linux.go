//go:build linux

package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// hostBackend starts local login shells on a pseudo-terminal.
type hostBackend struct{}

// NewHostBackend returns the backend of host terminal sessions.
func NewHostBackend() HostBackend { return hostBackend{} }

// Start implements HostBackend.
func (hostBackend) Start(ctx context.Context, shell []string, cols, rows uint32) (Process, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if !validHostShell(shell) {
		return nil, errors.New("invalid host shell")
	}
	command := exec.Command(shell[0], shell[1:]...)
	command.Env = hostEnvironment(shell[0])
	command.Dir = hostWorkDir()
	process, err := pty.StartWithAttrs(command, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}, &syscall.SysProcAttr{Setsid: true, Setctty: true})
	if err != nil {
		return nil, fmt.Errorf("start the host shell: %w", err)
	}
	return newHostProcess(command, process), nil
}

func validHostShell(shell []string) bool {
	if len(shell) != 2 || shell[1] != "-l" {
		return false
	}
	return shell[0] == "/bin/sh" || shell[0] == "/bin/bash"
}

func hostEnvironment(shell string) []string {
	lang := strings.TrimSpace(os.Getenv("LANG"))
	if lang == "" {
		lang = "C.UTF-8"
	}
	return []string{
		"TERM=xterm-256color",
		"HOME=/root",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG=" + lang,
		"USER=root",
		"SHELL=" + shell,
	}
}

func hostWorkDir() string {
	if os.Geteuid() == 0 {
		if info, err := os.Stat("/root"); err == nil && info.IsDir() {
			return "/root"
		}
	}
	return "/"
}

type hostProcess struct {
	command *exec.Cmd
	master  *os.File
	done    chan struct{}

	closeOnce sync.Once
	closeErr  error
	waitErr   error
}

func newHostProcess(command *exec.Cmd, master *os.File) *hostProcess {
	process := &hostProcess{command: command, master: master, done: make(chan struct{})}
	go func() {
		process.waitErr = command.Wait()
		close(process.done)
	}()
	return process
}

func (p *hostProcess) Read(b []byte) (int, error)  { return p.master.Read(b) }
func (p *hostProcess) Write(b []byte) (int, error) { return p.master.Write(b) }

func (p *hostProcess) Resize(ctx context.Context, cols, rows uint32) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := pty.Setsize(p.master, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}); err != nil {
		return fmt.Errorf("resize the host terminal: %w", err)
	}
	return nil
}

func (p *hostProcess) ExitCode(ctx context.Context) (int, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-p.done:
	}
	if p.command.ProcessState == nil {
		if p.waitErr != nil {
			return 0, fmt.Errorf("wait for the host shell: %w", p.waitErr)
		}
		return 0, errors.New("the host shell has no process state")
	}
	return p.command.ProcessState.ExitCode(), nil
}

// Close closes the terminal, sends SIGHUP to the process group and SIGKILL
// after two seconds. Wait runs in a goroutine from the start, so the shell is
// reaped even when it ignores the hangup.
func (p *hostProcess) Close() error {
	p.closeOnce.Do(func() {
		p.closeErr = p.master.Close()
		process := p.command.Process
		if process == nil {
			return
		}
		_ = syscall.Kill(-process.Pid, syscall.SIGHUP)
		go func(pid int) {
			// Once the shell is reaped its pid may belong to someone else.
			select {
			case <-p.done:
			case <-time.After(2 * time.Second):
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}(process.Pid)
	})
	return p.closeErr
}

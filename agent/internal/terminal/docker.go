package terminal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	// defaultDockerSocket is the Docker Engine API socket.
	defaultDockerSocket = "/var/run/docker.sock"
	// maxErrorBody bounds how much of an error answer is read.
	maxErrorBody = 4 << 10
)

// shells maps the shells of the contract to the command that starts them. The
// list is closed: there is no way to pass any other command.
var shells = map[agentv1.TerminalShell][]string{
	agentv1.TerminalShell_TERMINAL_SHELL_SH:   {"/bin/sh"},
	agentv1.TerminalShell_TERMINAL_SHELL_BASH: {"/bin/bash"},
}

// Process is a running shell with a terminal.
type Process interface {
	io.ReadWriteCloser
	// Resize changes the size of the terminal.
	Resize(ctx context.Context, cols, rows uint32) error
	// ExitCode returns the exit code of the shell once it has ended.
	ExitCode(ctx context.Context) (int, error)
}

// Backend starts shells in containers.
type Backend interface {
	// Start runs a shell in the container. privileged reports that the container
	// is as good as root on the host.
	Start(ctx context.Context, container string, shell []string, cols, rows uint32) (process Process, privileged bool, err error)
}

// DockerBackend starts shells through the Docker Engine API (exec with a TTY).
// It never builds a shell command line.
type DockerBackend struct {
	baseURL string
	dial    func(ctx context.Context) (net.Conn, error)
	client  *http.Client
}

// NewDockerBackend returns a backend that talks to the local Docker socket.
func NewDockerBackend() *DockerBackend {
	return NewDockerBackendWithDialer(func(ctx context.Context) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", defaultDockerSocket)
	})
}

// NewDockerBackendWithDialer connects with dial. Tests use it with a TCP server,
// because Windows has no unix sockets.
func NewDockerBackendWithDialer(dial func(ctx context.Context) (net.Conn, error)) *DockerBackend {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
	}
	return &DockerBackend{baseURL: "http://docker", dial: dial, client: &http.Client{Transport: transport}}
}

// containerInfo is the part of "docker inspect" the backend needs.
type containerInfo struct {
	State struct {
		Running bool `json:"Running"`
	} `json:"State"`
	HostConfig struct {
		Privileged bool   `json:"Privileged"`
		PidMode    string `json:"PidMode"`
	} `json:"HostConfig"`
	Mounts []struct {
		Source string `json:"Source"`
	} `json:"Mounts"`
}

// asRoot reports whether a shell in the container is as good as root on the host:
// it is privileged, shares the PID namespace of the host or has the Docker socket.
func (c containerInfo) asRoot() bool {
	if c.HostConfig.Privileged || c.HostConfig.PidMode == "host" {
		return true
	}
	for _, mount := range c.Mounts {
		if mount.Source == "/var/run/docker.sock" || mount.Source == "/run/docker.sock" {
			return true
		}
	}
	return false
}

// Start implements Backend.
func (b *DockerBackend) Start(ctx context.Context, container string, shell []string, cols, rows uint32) (Process, bool, error) {
	id := url.PathEscape(container)

	var info containerInfo
	if err := b.json(ctx, http.MethodGet, "/containers/"+id+"/json", nil, http.StatusOK, &info); err != nil {
		return nil, false, err
	}
	if !info.State.Running {
		return nil, false, errors.New("the container is not running")
	}

	// No User and not Privileged: the shell runs as the container says. Nothing of
	// the environment of the agent is passed on.
	var created struct {
		ID string `json:"Id"`
	}
	create := map[string]any{
		"AttachStdin": true, "AttachStdout": true, "AttachStderr": true, "Tty": true,
		"Cmd": shell, "Env": []string{"TERM=xterm-256color"}, "Privileged": false,
	}
	if err := b.json(ctx, http.MethodPost, "/containers/"+id+"/exec", create, http.StatusCreated, &created); err != nil {
		return nil, false, err
	}
	execID := url.PathEscape(created.ID)

	conn, err := b.hijack(ctx, "/exec/"+execID+"/start", map[string]any{"Detach": false, "Tty": true})
	if err != nil {
		return nil, false, err
	}
	process := &dockerProcess{backend: b, execID: execID, conn: conn}
	// The size can only be set once the exec runs.
	if err := process.Resize(ctx, cols, rows); err != nil {
		_ = conn.Close()
		return nil, false, err
	}
	return process, info.asRoot(), nil
}

// json sends a request and decodes the answer into out.
func (b *DockerBackend) json(ctx context.Context, method, path string, body any, want int, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, b.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := b.client.Do(request)
	if err != nil {
		return fmt.Errorf("docker is not reachable: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	// Docker answers 204 where 200 is expected for actions without a body.
	noContent := want == http.StatusOK && response.StatusCode == http.StatusNoContent
	if response.StatusCode != want && !noContent {
		return dockerError(response)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("read the answer of docker: %w", err)
	}
	return nil
}

// hijack starts an exec and takes over the connection: after the answer, the
// bytes of the connection are the terminal.
func (b *DockerBackend) hijack(ctx context.Context, path string, body any) (net.Conn, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "tcp")

	conn, err := b.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("docker is not reachable: %w", err)
	}
	// The context only bounds the handshake: the session ends by closing the
	// connection.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if err := request.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("start the shell: %w", err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("start the shell: %w", err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols && response.StatusCode != http.StatusOK {
		defer func() { _ = conn.Close() }()
		return nil, dockerError(response)
	}
	if !stop() {
		_ = conn.Close()
		return nil, context.Cause(ctx)
	}
	return &bufferedConn{Conn: conn, reader: reader}, nil
}

// bufferedConn reads through the buffer that may already hold terminal bytes.
type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

// dockerProcess is the shell of one exec.
type dockerProcess struct {
	backend *DockerBackend
	execID  string
	conn    net.Conn
	once    sync.Once
}

func (p *dockerProcess) Read(b []byte) (int, error)  { return p.conn.Read(b) }
func (p *dockerProcess) Write(b []byte) (int, error) { return p.conn.Write(b) }

func (p *dockerProcess) Close() error {
	var err error
	p.once.Do(func() { err = p.conn.Close() })
	return err
}

func (p *dockerProcess) Resize(ctx context.Context, cols, rows uint32) error {
	path := fmt.Sprintf("/exec/%s/resize?h=%d&w=%d", p.execID, rows, cols)
	return p.backend.json(ctx, http.MethodPost, path, nil, http.StatusOK, nil)
}

func (p *dockerProcess) ExitCode(ctx context.Context) (int, error) {
	var state struct {
		Running  bool `json:"Running"`
		ExitCode int  `json:"ExitCode"`
	}
	if err := p.backend.json(ctx, http.MethodGet, "/exec/"+p.execID+"/json", nil, http.StatusOK, &state); err != nil {
		return 0, err
	}
	if state.Running {
		return 0, errors.New("the shell is still running")
	}
	return state.ExitCode, nil
}

// dockerError turns a failed Engine answer into an error with Docker's message.
func dockerError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	var detail struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &detail) == nil && detail.Message != "" {
		return fmt.Errorf("docker: %s", strings.TrimSpace(detail.Message))
	}
	return fmt.Errorf("docker answered %s", response.Status)
}

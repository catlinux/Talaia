package terminal

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// fakeDocker is a Docker Engine that supports exec with a hijacked connection.
type fakeDocker struct {
	t         *testing.T
	inspect   string
	mu        sync.Mutex
	createdAs map[string]any
	resized   []string
	typed     chan string
}

func (d *fakeDocker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/containers/web/json":
		_, _ = io.WriteString(w, d.inspect)
	case r.Method == http.MethodPost && r.URL.Path == "/containers/web/exec":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			d.t.Errorf("exec body: %v", err)
		}
		d.mu.Lock()
		d.createdAs = body
		d.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"Id":"exec1"}`)
	case r.Method == http.MethodPost && r.URL.Path == "/exec/exec1/start":
		d.hijack(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/exec/exec1/resize":
		d.mu.Lock()
		d.resized = append(d.resized, r.URL.RawQuery)
		d.mu.Unlock()
	case r.Method == http.MethodGet && r.URL.Path == "/exec/exec1/json":
		_, _ = io.WriteString(w, `{"Running":false,"ExitCode":3}`)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"message":"No such container: nope"}`)
	}
}

func (d *fakeDocker) hijack(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Upgrade") != "tcp" {
		d.t.Error("the start request does not ask for an upgrade")
	}
	_, _ = io.Copy(io.Discard, r.Body)
	conn, buffered, err := w.(http.Hijacker).Hijack() //nolint:forcetypeassert // httptest supports it
	if err != nil {
		d.t.Errorf("hijack: %v", err)
		return
	}
	defer func() { _ = conn.Close() }()
	_, _ = io.WriteString(buffered, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\nprompt$ ")
	_ = buffered.Flush()
	line, _ := bufio.NewReader(buffered).ReadString('\n')
	d.typed <- line
	_, _ = io.WriteString(conn, "echo\n")
}

func newBackend(t *testing.T, docker *fakeDocker) *DockerBackend {
	t.Helper()
	server := httptest.NewServer(docker)
	t.Cleanup(server.Close)
	return NewDockerBackendWithDialer(func(ctx context.Context) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "tcp", server.Listener.Addr().String())
	})
}

const runningContainer = `{"State":{"Running":true},"HostConfig":{"Privileged":false,"PidMode":""},"Mounts":[]}`

func TestDockerBackendRunsAShellWithATerminal(t *testing.T) {
	docker := &fakeDocker{t: t, inspect: runningContainer, typed: make(chan string, 1)}
	backend := newBackend(t, docker)

	process, privileged, err := backend.Start(context.Background(), "web", shells[agentv1.TerminalShell_TERMINAL_SHELL_BASH], 100, 30)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = process.Close() }()
	if privileged {
		t.Error("a plain container was reported as privileged")
	}

	// What the exec was created with: the closed command, the container's user
	// and no privileges.
	docker.mu.Lock()
	created := docker.createdAs
	resized := docker.resized
	docker.mu.Unlock()
	if cmd, _ := json.Marshal(created["Cmd"]); string(cmd) != `["/bin/bash"]` {
		t.Errorf("Cmd = %s", cmd)
	}
	if _, set := created["User"]; set || created["Privileged"] != false || created["Tty"] != true {
		t.Errorf("exec options = %v", created)
	}
	if env, _ := json.Marshal(created["Env"]); string(env) != `["TERM=xterm-256color"]` {
		t.Errorf("Env = %s", env)
	}
	if len(resized) != 1 || resized[0] != "h=30&w=100" {
		t.Errorf("resize = %v", resized)
	}

	// The bytes that follow the answer belong to the terminal, also those that
	// arrived together with the headers.
	prompt := make([]byte, len("prompt$ "))
	if _, err := io.ReadFull(process, prompt); err != nil || string(prompt) != "prompt$ " {
		t.Fatalf("read %q, %v", prompt, err)
	}
	if _, err := process.Write([]byte("ls\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := <-docker.typed; got != "ls\n" {
		t.Errorf("the shell received %q", got)
	}
	reply := make([]byte, 5)
	if _, err := io.ReadFull(process, reply); err != nil || string(reply) != "echo\n" {
		t.Fatalf("read %q, %v", reply, err)
	}

	if code, err := process.ExitCode(context.Background()); err != nil || code != 3 {
		t.Errorf("ExitCode = %d, %v", code, err)
	}
}

func TestDockerBackendFlagsRootEquivalentContainers(t *testing.T) {
	for name, inspect := range map[string]string{
		"privileged":    `{"State":{"Running":true},"HostConfig":{"Privileged":true},"Mounts":[]}`,
		"host pid":      `{"State":{"Running":true},"HostConfig":{"PidMode":"host"},"Mounts":[]}`,
		"docker socket": `{"State":{"Running":true},"HostConfig":{},"Mounts":[{"Source":"/var/run/docker.sock"}]}`,
		"run socket":    `{"State":{"Running":true},"HostConfig":{},"Mounts":[{"Source":"/run/docker.sock"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			docker := &fakeDocker{t: t, inspect: inspect, typed: make(chan string, 1)}
			process, privileged, err := newBackend(t, docker).Start(context.Background(), "web", []string{"/bin/sh"}, 80, 24)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			_ = process.Close()
			if !privileged {
				t.Error("the container was not flagged")
			}
		})
	}
}

func TestDockerBackendRefusals(t *testing.T) {
	docker := &fakeDocker{t: t, inspect: `{"State":{"Running":false},"HostConfig":{}}`, typed: make(chan string, 1)}
	backend := newBackend(t, docker)

	if _, _, err := backend.Start(context.Background(), "web", []string{"/bin/sh"}, 80, 24); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("stopped container: %v", err)
	}
	if _, _, err := backend.Start(context.Background(), "nope", []string{"/bin/sh"}, 80, 24); err == nil || !strings.Contains(err.Error(), "No such container") {
		t.Errorf("unknown container: %v", err)
	}
}

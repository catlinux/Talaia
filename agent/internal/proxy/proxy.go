package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/catlinux/Talaia/agent/internal/stacks"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

const (
	// DefaultRoot is where the agent keeps the Caddy bootstrap and admin socket.
	DefaultRoot = "/var/lib/talaia/proxy"
	// StackName is the internal Compose project of the managed proxy.
	StackName = "talaia-proxy"

	stateFile     = "state.json"
	bootstrapJSON = `{"admin":{"listen":"unix//run/caddy/admin.sock"}}` + "\n"
)

// StackManager writes and runs the internal Compose stack of the proxy.
type StackManager interface {
	Write(ctx context.Context, name, compose, env string) (string, error)
	Do(ctx context.Context, name string, action agentv1.StackActionType) (string, error)
}

// Options are the collaborators of a Manager. Root, Ports and Admin have real
// defaults; Stacks is required to install the managed proxy.
type Options struct {
	Root   string
	Stacks StackManager
	Ports  PortChecker
	Admin  Admin
}

// Manager runs the proxy mode of one server.
type Manager struct {
	root   string
	stacks StackManager
	ports  PortChecker
	admin  Admin

	mu        sync.Mutex
	mode      agentv1.ProxyMode
	hostnames []string
}

type proxyState struct {
	Mode      string   `json:"mode"`
	Hostnames []string `json:"hostnames,omitempty"`
}

// New returns a Manager. It reads the stored mode but trusts the core to send
// ProxyInstall again after an agent restart.
func New(options Options) *Manager {
	root := options.Root
	if root == "" {
		root = DefaultRoot
	}
	ports := options.Ports
	if ports == nil {
		ports = SystemPorts{}
	}
	admin := options.Admin
	if admin == nil {
		admin = NewUnixAdmin(filepath.Join(root, adminSocketFile))
	}

	manager := &Manager{
		root:   root,
		stacks: options.Stacks,
		ports:  ports,
		admin:  admin,
		mode:   agentv1.ProxyMode_PROXY_MODE_EXTERNAL,
	}
	manager.loadState()
	return manager
}

// NewDefault returns the Manager of a real server: the internal Compose stack
// under its own directory, the admin socket under /var/lib/talaia/proxy and
// Caddy pinned by digest. Keeping the stack out of /opt/talaia/stacks stops a
// user stack from taking the name of the proxy.
func NewDefault() *Manager {
	return New(Options{
		Root:   DefaultRoot,
		Stacks: stacks.New(filepath.Join(DefaultRoot, "stacks"), stacks.ExecRunner{}),
		Ports:  SystemPorts{},
		Admin:  NewUnixAdmin(filepath.Join(DefaultRoot, adminSocketFile)),
	})
}

// Install enables or removes the managed proxy. Installing is refused when 80
// or 443 is already in use.
func (m *Manager) Install(ctx context.Context, action agentv1.ProxyInstallAction) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch action {
	case agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_INSTALL:
		if m.mode == agentv1.ProxyMode_PROXY_MODE_MANAGED {
			return "the managed proxy is already installed", nil
		}
		if !m.ports.Free(80) {
			return "", errors.New("port 80 is already in use; the managed proxy is not available")
		}
		if !m.ports.Free(443) {
			return "", errors.New("port 443 is already in use; the managed proxy is not available")
		}
		if m.stacks == nil {
			return "", errors.New("the Compose stacks are not available on this agent")
		}
		if err := m.prepareRoot(); err != nil {
			return "", err
		}
		if _, err := m.stacks.Write(ctx, StackName, m.compose(), ""); err != nil {
			return "", fmt.Errorf("write the proxy stack: %w", err)
		}
		output, err := m.stacks.Do(ctx, StackName, agentv1.StackActionType_STACK_ACTION_TYPE_UP)
		if err != nil {
			return output, fmt.Errorf("start the managed proxy: %w", err)
		}
		m.mode = agentv1.ProxyMode_PROXY_MODE_MANAGED
		m.hostnames = nil
		if err := m.saveState(); err != nil {
			return output, err
		}
		return output, nil

	case agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_REMOVE:
		if m.mode != agentv1.ProxyMode_PROXY_MODE_MANAGED {
			return "the managed proxy is not installed", nil
		}
		if m.stacks == nil {
			return "", errors.New("the Compose stacks are not available on this agent")
		}
		output, err := m.stacks.Do(ctx, StackName, agentv1.StackActionType_STACK_ACTION_TYPE_REMOVE)
		if err != nil {
			return output, fmt.Errorf("remove the managed proxy: %w", err)
		}
		m.mode = agentv1.ProxyMode_PROXY_MODE_EXTERNAL
		m.hostnames = nil
		_ = os.Remove(filepath.Join(m.root, bootstrapFile))
		_ = os.Remove(filepath.Join(m.root, adminSocketFile))
		if err := m.saveState(); err != nil {
			return output, err
		}
		return output, nil

	case agentv1.ProxyInstallAction_PROXY_INSTALL_ACTION_UNSPECIFIED:
		return "", errors.New("the proxy install action is not set")
	default:
		return "", fmt.Errorf("unsupported proxy install action %d", action)
	}
}

// Apply builds the Caddy configuration from routes, never from JSON sent by the
// core, and loads it through the admin API.
func (m *Manager) Apply(ctx context.Context, routes []*agentv1.ProxyRoute) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.mode != agentv1.ProxyMode_PROXY_MODE_MANAGED {
		return "", errors.New("the managed proxy is not installed")
	}
	config, err := buildCaddyConfig(routes)
	if err != nil {
		return "", err
	}
	if err := m.admin.Load(ctx, config); err != nil {
		return "", err
	}
	if err := writeAtomic(filepath.Join(m.root, bootstrapFile), config, 0o600); err != nil {
		return "", fmt.Errorf("save the Caddy configuration: %w", err)
	}
	m.hostnames = routeHostnames(routes)
	if err := m.saveState(); err != nil {
		return "", fmt.Errorf("save the proxy state: %w", err)
	}
	return "proxy configuration applied", nil
}

// Status is what the inventory reports: the mode, whether 80 and 443 are free,
// the Caddy version and the state of each configured certificate in managed
// mode.
func (m *Manager) Status(context.Context) *agentv1.ProxyStatus {
	m.mu.Lock()
	mode := m.mode
	hostnames := append([]string(nil), m.hostnames...)
	m.mu.Unlock()

	status := &agentv1.ProxyStatus{
		Mode:         mode,
		Port_80Free:  m.ports.Free(80),
		Port_443Free: m.ports.Free(443),
	}
	if mode == agentv1.ProxyMode_PROXY_MODE_MANAGED {
		status.CaddyVersion = CaddyVersion
		status.Certificates = m.certificates(hostnames)
	}
	return status
}

func (m *Manager) prepareRoot() error {
	if err := os.MkdirAll(m.root, 0o700); err != nil {
		return fmt.Errorf("create the proxy directory: %w", err)
	}
	if err := os.Chmod(m.root, 0o700); err != nil {
		return fmt.Errorf("set the proxy directory permissions: %w", err)
	}
	for _, name := range []string{"data", "config"} {
		directory := filepath.Join(m.root, name)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create the Caddy %s directory: %w", name, err)
		}
	}
	path := filepath.Join(m.root, bootstrapFile)
	if err := writeAtomic(path, []byte(bootstrapJSON), 0o600); err != nil {
		return fmt.Errorf("write the Caddy bootstrap configuration: %w", err)
	}
	return nil
}

func (m *Manager) compose() string {
	root := strconv.Quote(m.root)
	data := strconv.Quote(filepath.Join(m.root, "data"))
	config := strconv.Quote(filepath.Join(m.root, "config"))
	return fmt.Sprintf(`services:
  caddy:
    image: %s
    command: ["run", "--config", "/run/caddy/%s", "--adapter", "json"]
    user: "0:0"
    restart: unless-stopped
    environment:
      XDG_DATA_HOME: /data
      XDG_CONFIG_HOME: /config
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - %s:/run/caddy
      - %s:/data
      - %s:/config
    networks:
      - %s
networks:
  %s:
    name: %s
`, CaddyImage, bootstrapFile, root, data, config, StackName, StackName, StackName)
}

func (m *Manager) saveState() error {
	if err := os.MkdirAll(m.root, 0o700); err != nil {
		return fmt.Errorf("create the proxy directory: %w", err)
	}
	state := proxyState{Mode: "external", Hostnames: append([]string(nil), m.hostnames...)}
	if m.mode == agentv1.ProxyMode_PROXY_MODE_MANAGED {
		state.Mode = "managed"
	} else {
		state.Hostnames = nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode the proxy state: %w", err)
	}
	path := filepath.Join(m.root, stateFile)
	if err := writeAtomic(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write the proxy state: %w", err)
	}
	return nil
}

func (m *Manager) loadState() {
	data, err := os.ReadFile(filepath.Join(m.root, stateFile))
	if err != nil {
		return
	}
	var state proxyState
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	if state.Mode == "managed" {
		m.mode = agentv1.ProxyMode_PROXY_MODE_MANAGED
		m.hostnames = append([]string(nil), state.Hostnames...)
	}
}

func routeHostnames(routes []*agentv1.ProxyRoute) []string {
	hostnames := make([]string, 0, len(routes))
	for _, item := range routes {
		if item == nil {
			continue
		}
		hostnames = append(hostnames, strings.ToLower(strings.TrimSpace(item.GetHostname())))
	}
	sort.Strings(hostnames)
	return hostnames
}

// writeAtomic writes data next to path and renames it over path, so a restart
// never reads a half-written configuration.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".talaia-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	_, err = file.Write(data)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(temporary, mode)
	}
	if err == nil {
		err = os.Rename(temporary, path)
	}
	if err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

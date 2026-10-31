package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Dialer opens the TCP connection of the health check.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

type netDialer struct {
	timeout time.Duration
}

func (d *netDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := net.Dialer{Timeout: d.timeout}
	return dialer.DialContext(ctx, network, address)
}

type containerState struct {
	Status  string `json:"Status"`
	Running bool   `json:"Running"`
	Health  *struct {
		Status string `json:"Status"`
	} `json:"Health"`
}

type composeConfig struct {
	Services map[string]composeService `json:"services"`
}

type composeService struct {
	Volumes       []json.RawMessage `json:"volumes"`
	Ports         []json.RawMessage `json:"ports"`
	ContainerName string            `json:"container_name"`
	Deploy        *struct {
		Replicas *int `json:"replicas"`
	} `json:"deploy"`
}

// canZeroDowntime reports whether the stack is a single service without
// volumes, published host ports or a fixed container name, so scaling it to two
// is safe. In external proxy mode the app publishes 127.0.0.1:<port>, which a
// second container cannot bind, so those apps use a plain up.
func (m *Manager) canZeroDowntime(ctx context.Context, name, service string) bool {
	output, err := m.compose(ctx, name, "config", "--format", "json")
	if err != nil {
		return false
	}
	var config composeConfig
	if err := json.Unmarshal([]byte(output), &config); err != nil {
		return false
	}
	if len(config.Services) != 1 {
		return false
	}
	item, ok := config.Services[service]
	if !ok {
		return false
	}
	if len(item.Volumes) != 0 || len(item.Ports) != 0 || item.ContainerName != "" {
		return false
	}
	if item.Deploy != nil && item.Deploy.Replicas != nil && *item.Deploy.Replicas > 1 {
		return false
	}
	return true
}

func (m *Manager) waitHealthy(ctx context.Context, name string, ids []string, port uint16) error {
	dir, _, err := m.stacks.ComposeCommand(name)
	if err != nil {
		return err
	}

	deadline := time.Now().Add(m.healthTimeout)
	var last error
	for {
		ready, failed, err := m.checkHealth(ctx, dir, ids, port)
		if ready {
			return nil
		}
		if failed {
			return err
		}
		if err != nil {
			last = err
		}
		if !time.Now().Before(deadline) {
			if last != nil {
				return fmt.Errorf("the new container did not become healthy in %s: %w", m.healthTimeout, last)
			}
			return fmt.Errorf("the new container did not become healthy in %s", m.healthTimeout)
		}

		timer := time.NewTimer(m.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (m *Manager) checkHealth(ctx context.Context, dir string, ids []string, port uint16) (bool, bool, error) {
	for _, id := range ids {
		state, err := m.containerState(ctx, dir, id)
		if err != nil {
			return false, false, err
		}
		status := strings.ToLower(strings.TrimSpace(state.Status))
		if status == "exited" || status == "dead" {
			return false, true, fmt.Errorf("the new container %s is %s", id, status)
		}
		if state.Health != nil {
			switch strings.ToLower(strings.TrimSpace(state.Health.Status)) {
			case "healthy":
				continue
			case "unhealthy":
				return false, true, fmt.Errorf("the new container %s is unhealthy", id)
			default:
				return false, false, nil
			}
		}
		if !state.Running {
			return false, false, nil
		}
		if port == 0 {
			return false, true, errors.New("the app port is not set for the health check")
		}
		address, err := m.containerAddress(ctx, dir, id, port)
		if err != nil {
			return false, false, err
		}
		dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		connection, err := m.dialer.DialContext(dialCtx, "tcp", address)
		cancel()
		if err != nil {
			return false, false, err
		}
		_ = connection.Close()
	}
	return true, false, nil
}

func (m *Manager) containerState(ctx context.Context, dir, id string) (containerState, error) {
	output, err := m.runDocker(ctx, dir, "inspect", "--format", "{{json .State}}", id)
	if err != nil {
		return containerState{}, err
	}
	var state containerState
	if err := json.Unmarshal([]byte(output), &state); err != nil {
		return containerState{}, fmt.Errorf("decode the state of container %s: %w", id, err)
	}
	return state, nil
}

func (m *Manager) containerAddress(ctx context.Context, dir, id string, port uint16) (string, error) {
	output, err := m.runDocker(ctx, dir, "inspect", "--format", "{{json .NetworkSettings.Networks}}", id)
	if err != nil {
		return "", err
	}
	var networks map[string]struct {
		IPAddress string `json:"IPAddress"`
	}
	if err := json.Unmarshal([]byte(output), &networks); err != nil {
		return "", fmt.Errorf("decode the networks of container %s: %w", id, err)
	}
	names := make([]string, 0, len(networks))
	for name := range networks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if networks[name].IPAddress != "" {
			return net.JoinHostPort(networks[name].IPAddress, strconv.Itoa(int(port))), nil
		}
	}
	return "", fmt.Errorf("container %s has no address for the health check", id)
}

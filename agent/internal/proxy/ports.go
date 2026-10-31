// Package proxy publishes the apps of a server. In external mode it only picks
// a free port on 127.0.0.1; in managed mode it runs a pinned Caddy on 80 and
// 443 and loads a configuration built from the routes the core sends. It never
// touches another web server.
package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

const (
	// ExternalHost is where apps listen in external mode.
	ExternalHost = "127.0.0.1"
	// PortRangeStart and PortRangeEnd bound the external app ports.
	PortRangeStart = 20000
	PortRangeEnd   = 29999
)

const dockerSocket = "/var/run/docker.sock"

const dockerTimeout = 5 * time.Second

// Port is a free TCP port of the host for an app in external mode.
type Port struct {
	Host   string
	Number uint16
}

// Address is what an app binds, always on the external host.
func (p Port) Address() string {
	return net.JoinHostPort(p.Host, strconv.Itoa(int(p.Number)))
}

// PublishedPorts reports the host ports Docker publishes. Tests replace it;
// the default reads the Docker Engine API.
type PublishedPorts interface {
	Published(ctx context.Context) (map[uint16]struct{}, error)
}

// Picker chooses a free port for an app in external mode.
type Picker struct {
	published PublishedPorts
	listen    func(network, address string) (net.Listener, error)
}

// NewPicker returns a Picker that avoids the ports published contains and
// checks each candidate by binding it on 127.0.0.1.
func NewPicker(published PublishedPorts) *Picker {
	return &Picker{published: published, listen: net.Listen}
}

// NewDefaultPicker returns a Picker that asks the local Docker Engine.
func NewDefaultPicker() *Picker {
	return NewPicker(NewDockerPorts())
}

// Pick returns the first free port of the range. It never returns a port that
// Docker publishes or that is already bound, and the host is always
// 127.0.0.1.
func (p *Picker) Pick(ctx context.Context) (Port, error) {
	published := map[uint16]struct{}{}
	if p.published != nil {
		used, err := p.published.Published(ctx)
		if err != nil {
			return Port{}, fmt.Errorf("read the ports Docker publishes: %w", err)
		}
		published = used
	}
	listen := p.listen
	if listen == nil {
		listen = net.Listen
	}

	for number := PortRangeStart; number <= PortRangeEnd; number++ {
		if err := ctx.Err(); err != nil {
			return Port{}, err
		}
		if _, used := published[uint16(number)]; used {
			continue
		}
		address := net.JoinHostPort(ExternalHost, strconv.Itoa(number))
		listener, err := listen("tcp4", address)
		if err != nil {
			continue
		}
		if err := listener.Close(); err != nil {
			return Port{}, fmt.Errorf("close the probe listener: %w", err)
		}
		return Port{Host: ExternalHost, Number: uint16(number)}, nil
	}
	return Port{}, errors.New("no free port in the app range")
}

// PortChecker reports whether the host can listen on a port.
type PortChecker interface {
	Free(port uint16) bool
}

// SystemPorts checks with a real listener.
type SystemPorts struct{}

// Free reports whether nothing listens on port.
func (SystemPorts) Free(port uint16) bool {
	if port == 0 {
		return false
	}
	number := strconv.Itoa(int(port))
	for _, address := range []string{"127.0.0.1:" + number, "0.0.0.0:" + number} {
		listener, err := net.Listen("tcp4", address)
		if err != nil {
			return false
		}
		if err := listener.Close(); err != nil {
			return false
		}
	}

	// IPv6 may be turned off, in which case it adds no restriction.
	listener, err := net.Listen("tcp6", "[::]:"+number)
	if err == nil {
		return listener.Close() == nil
	}
	return errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EADDRNOTAVAIL)
}

// DockerPorts reads the ports Docker publishes from the Engine API.
type DockerPorts struct {
	baseURL string
	client  *http.Client
	timeout time.Duration
}

// NewDockerPorts returns a DockerPorts that talks to the local socket.
func NewDockerPorts() *DockerPorts {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", dockerSocket)
		},
	}
	return NewDockerPortsWithClient("http://docker", &http.Client{Transport: transport})
}

// NewDockerPortsWithClient returns a DockerPorts that talks to baseURL with
// httpClient. Tests use it with an httptest server.
func NewDockerPortsWithClient(baseURL string, client *http.Client) *DockerPorts {
	return &DockerPorts{baseURL: baseURL, client: client, timeout: dockerTimeout}
}

type dockerContainer struct {
	Ports []dockerPort `json:"Ports"`
}

type dockerPort struct {
	PublicPort uint16 `json:"PublicPort"`
	Type       string `json:"Type"`
}

// Published reads GET /containers/json?all=1 and collects the public TCP
// ports.
func (d *DockerPorts) Published(ctx context.Context) (map[uint16]struct{}, error) {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+"/containers/json?all=1", nil)
	if err != nil {
		return nil, err
	}
	response, err := d.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker answered %s for the published ports", response.Status)
	}

	var containers []dockerContainer
	if err := json.NewDecoder(response.Body).Decode(&containers); err != nil {
		return nil, fmt.Errorf("decode the published ports: %w", err)
	}
	ports := map[uint16]struct{}{}
	for _, container := range containers {
		for _, port := range container.Ports {
			if port.PublicPort == 0 || port.Type != "" && port.Type != "tcp" {
				continue
			}
			ports[port.PublicPort] = struct{}{}
		}
	}
	return ports, nil
}

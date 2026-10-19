// Package inventory reads what runs on the host: Docker containers and
// Compose projects, and the systemd service units.
package inventory

import (
	"context"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// ContainerSource reads Docker. Docker implements it against the Engine API.
type ContainerSource interface {
	Collect(ctx context.Context) DockerInventory
}

// ServiceSource reads systemd. Systemd implements it against systemctl.
type ServiceSource interface {
	Units(ctx context.Context) []*agentv1.SystemdUnit
}

// Provider combines the two sources. The zero value reads nothing, which lets
// the tests inject their own sources and makes the collector degrade to empty
// values on hosts without Docker or systemd.
type Provider struct {
	Docker  ContainerSource
	Systemd ServiceSource
}

// New returns a Provider that reads the real host.
func New() Provider {
	return Provider{
		Docker:  NewDocker(),
		Systemd: NewSystemd(),
	}
}

// Result is what the provider read from the host.
type Result struct {
	DockerAvailable bool
	DockerVersion   string
	Containers      []*agentv1.Container
	Projects        []*agentv1.ComposeProject
	Services        []*agentv1.SystemdUnit
}

// Collect reads both sources. It never fails: a source that is not available
// leaves its fields empty so the agent keeps working.
func (p Provider) Collect(ctx context.Context) Result {
	var result Result

	if p.Docker != nil {
		docker := p.Docker.Collect(ctx)
		result.DockerAvailable = docker.Available
		result.DockerVersion = docker.Version
		result.Containers = docker.Containers
		result.Projects = docker.Projects
	}
	if p.Systemd != nil {
		result.Services = p.Systemd.Units(ctx)
	}

	return result
}

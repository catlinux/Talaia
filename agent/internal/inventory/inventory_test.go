package inventory

import (
	"context"
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeContainerSource struct {
	result DockerInventory
}

func (f fakeContainerSource) Collect(context.Context) DockerInventory {
	return f.result
}

type fakeServiceSource struct {
	units []*agentv1.SystemdUnit
}

func (f fakeServiceSource) Units(context.Context) []*agentv1.SystemdUnit {
	return f.units
}

func TestProviderCollectCombinesSources(t *testing.T) {
	provider := Provider{
		Docker: fakeContainerSource{result: DockerInventory{
			Available:  true,
			Version:    "27.1.1",
			Containers: []*agentv1.Container{{Id: "abcdefghijkl", Name: "web", State: "running"}},
			Projects:   []*agentv1.ComposeProject{{Name: "shop", ContainerCount: 1, RunningCount: 1}},
		}},
		Systemd: fakeServiceSource{units: []*agentv1.SystemdUnit{{Name: "docker.service"}}},
	}

	result := provider.Collect(context.Background())

	if !result.DockerAvailable || result.DockerVersion != "27.1.1" {
		t.Errorf("docker = %v %q", result.DockerAvailable, result.DockerVersion)
	}
	if len(result.Containers) != 1 || len(result.Projects) != 1 {
		t.Errorf("containers = %d, projects = %d", len(result.Containers), len(result.Projects))
	}
	if len(result.Services) != 1 || result.Services[0].GetName() != "docker.service" {
		t.Errorf("services = %+v", result.Services)
	}
}

func TestProviderEmptyDegradesGracefully(t *testing.T) {
	result := Provider{}.Collect(context.Background())

	if result.DockerAvailable || result.DockerVersion != "" {
		t.Errorf("docker = %v %q, want unavailable", result.DockerAvailable, result.DockerVersion)
	}
	if len(result.Containers) != 0 || len(result.Projects) != 0 || len(result.Services) != 0 {
		t.Errorf("result = %+v, want empty lists", result)
	}
}

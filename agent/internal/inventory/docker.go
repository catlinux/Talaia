package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/catlinux/Talaia/agent/internal/stacks"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// defaultDockerSocket is the Docker Engine API socket.
const defaultDockerSocket = "/var/run/docker.sock"

// dockerTimeout bounds a full Docker collection.
const dockerTimeout = 5 * time.Second

// The labels Docker Compose writes on every container it manages.
const (
	composeProjectLabel     = "com.docker.compose.project"
	composeServiceLabel     = "com.docker.compose.service"
	composeWorkingDirLabel  = "com.docker.compose.project.working_dir"
	composeConfigFilesLabel = "com.docker.compose.project.config_files"
)

// DockerInventory is what the Docker Engine API reports. Available is false
// when the socket is missing or unreachable, and the lists are then empty.
type DockerInventory struct {
	Available  bool
	Version    string
	Containers []*agentv1.Container
	Projects   []*agentv1.ComposeProject
}

// Docker reads the Docker Engine API with the standard library only.
type Docker struct {
	baseURL    string
	httpClient *http.Client

	// unavailableLogged keeps the "not available" message to one per agent.
	unavailableLogged sync.Once
	eventBackoffBase  time.Duration
	eventBackoffMax   time.Duration
}

// NewDocker returns a Docker client that talks to the local socket.
func NewDocker() *Docker {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", defaultDockerSocket)
		},
	}
	return NewDockerWithClient("http://docker", &http.Client{Transport: transport})
}

// NewDockerWithClient returns a Docker client that talks to baseURL with
// httpClient. Tests use it with an httptest server, because Windows has no unix
// sockets.
func NewDockerWithClient(baseURL string, httpClient *http.Client) *Docker {
	return &Docker{baseURL: baseURL, httpClient: httpClient, eventBackoffBase: time.Second, eventBackoffMax: 30 * time.Second}
}

type dockerVersion struct {
	Version string `json:"Version"`
}

// dockerContainer is one element of GET /containers/json.
type dockerContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

// Collect reads the version and the containers. It never fails: a missing
// socket or any connection or HTTP error means Docker is not available.
func (d *Docker) Collect(ctx context.Context) DockerInventory {
	ctx, cancel := context.WithTimeout(ctx, dockerTimeout)
	defer cancel()

	var version dockerVersion
	if err := d.get(ctx, "/version", &version); err != nil {
		d.logUnavailable(err)
		return DockerInventory{}
	}

	var raw []dockerContainer
	if err := d.get(ctx, "/containers/json?all=1", &raw); err != nil {
		d.logUnavailable(err)
		return DockerInventory{}
	}

	containers := make([]*agentv1.Container, 0, len(raw))
	for _, item := range raw {
		containers = append(containers, item.container())
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].GetName() < containers[j].GetName() })

	return DockerInventory{
		Available:  true,
		Version:    version.Version,
		Containers: containers,
		Projects:   composeProjects(raw),
	}
}

func (d *Docker) logUnavailable(err error) {
	d.unavailableLogged.Do(func() {
		slog.Debug("docker is not available", "error", err)
	})
}

func (d *Docker) get(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+path, nil)
	if err != nil {
		return err
	}

	response, err := d.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("docker answered %s for %s", response.Status, path)
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func (c dockerContainer) container() *agentv1.Container {
	return &agentv1.Container{
		Id:             shortID(c.ID),
		Name:           containerName(c.Names),
		Image:          c.Image,
		State:          c.State,
		Status:         c.Status,
		Health:         containerHealth(c.Status),
		ComposeProject: c.Labels[composeProjectLabel],
		ComposeService: c.Labels[composeServiceLabel],
	}
}

func containerHealth(status string) string {
	switch {
	case strings.Contains(status, "(healthy)"):
		return "healthy"
	case strings.Contains(status, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(status, "(health: starting)"):
		return "starting"
	default:
		return ""
	}
}

// shortID keeps the first 12 characters of a container id.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// containerName returns the first container name without the leading slash.
func containerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

// composeProjects groups the containers by Compose project, sorted by name.
func composeProjects(raw []dockerContainer) []*agentv1.ComposeProject {
	projects := map[string]*agentv1.ComposeProject{}

	for _, item := range raw {
		name := item.Labels[composeProjectLabel]
		if name == "" {
			continue
		}

		project, ok := projects[name]
		if !ok {
			project = &agentv1.ComposeProject{
				Name:        name,
				WorkingDir:  item.Labels[composeWorkingDirLabel],
				ConfigFiles: splitConfigFiles(item.Labels[composeConfigFilesLabel]),
			}
			projects[name] = project
		}

		project.ContainerCount++
		if item.State == "running" {
			project.RunningCount++
		}
	}

	result := make([]*agentv1.ComposeProject, 0, len(projects))
	for _, project := range projects {
		result = append(result, project)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].GetName() < result[j].GetName() })
	return result
}

// splitConfigFiles splits the comma separated config_files label.
func splitConfigFiles(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	files := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			files = append(files, trimmed)
		}
	}
	return files
}

// Project finds a running Compose project by its name, from the labels of its
// containers. It lets the stack manager adopt a project without being told where
// its files are.
func (d *Docker) Project(ctx context.Context, name string) (stacks.Project, bool) {
	for _, project := range d.Collect(ctx).Projects {
		if project.GetName() == name {
			return stacks.Project{WorkingDir: project.GetWorkingDir(), ConfigFiles: project.GetConfigFiles()}, true
		}
	}
	return stacks.Project{}, false
}

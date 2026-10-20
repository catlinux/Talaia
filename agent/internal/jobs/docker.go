package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// defaultDockerSocket is the Docker Engine API socket.
const defaultDockerSocket = "/var/run/docker.sock"

// stopGraceSeconds is how long Docker waits for a container to stop before it
// kills it.
const stopGraceSeconds = 10

// maxErrorBody bounds how much of an error answer is read.
const maxErrorBody = 4 << 10

// containerRef matches a container id or a Docker container name. Anything else
// is refused before it reaches the Engine API.
var containerRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

// ValidContainerRef reports whether ref is an acceptable container id or name.
func ValidContainerRef(ref string) bool {
	return containerRef.MatchString(ref)
}

// DockerActions runs container actions through the Docker Engine API. It never
// builds a shell command.
type DockerActions struct {
	baseURL    string
	httpClient *http.Client
}

// NewDockerActions returns a client that talks to the local Docker socket.
func NewDockerActions() *DockerActions {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", defaultDockerSocket)
		},
	}
	return NewDockerActionsWithClient("http://docker", &http.Client{Transport: transport})
}

// NewDockerActionsWithClient talks to baseURL with httpClient. Tests use it with
// an httptest server, because Windows has no unix sockets.
func NewDockerActionsWithClient(baseURL string, httpClient *http.Client) *DockerActions {
	return &DockerActions{baseURL: baseURL, httpClient: httpClient}
}

// Do runs the action on the container. Starting a started container and
// stopping a stopped one succeed, so repeating a command is harmless.
func (d *DockerActions) Do(ctx context.Context, container string, action agentv1.ContainerActionType) error {
	if !ValidContainerRef(container) {
		return errors.New("invalid container reference")
	}
	id := url.PathEscape(container)

	switch action {
	case agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_START:
		return d.call(ctx, http.MethodPost, "/containers/"+id+"/start")
	case agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_STOP:
		return d.call(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/stop?t=%d", id, stopGraceSeconds))
	case agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_RESTART:
		return d.call(ctx, http.MethodPost, fmt.Sprintf("/containers/%s/restart?t=%d", id, stopGraceSeconds))
	case agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_REMOVE:
		// Neither force nor volumes: a running container must be stopped first
		// and its data is kept.
		return d.call(ctx, http.MethodDelete, "/containers/"+id+"?v=0&force=0")
	case agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_UNSPECIFIED:
		return errors.New("the container action is not set")
	default:
		return fmt.Errorf("unsupported container action %d", action)
	}
}

func (d *DockerActions) call(ctx context.Context, method, path string) error {
	request, err := http.NewRequestWithContext(ctx, method, d.baseURL+path, nil)
	if err != nil {
		return err
	}

	response, err := d.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("docker is not reachable: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	switch response.StatusCode {
	case http.StatusNoContent, http.StatusOK, http.StatusNotModified:
		return nil
	}

	body, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBody))
	var detail struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &detail) == nil && detail.Message != "" {
		return fmt.Errorf("docker: %s", strings.TrimSpace(detail.Message))
	}
	return fmt.Errorf("docker answered %s", response.Status)
}

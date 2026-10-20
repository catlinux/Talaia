package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"time"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// maxRemovals bounds how many resources one command may remove.
const maxRemovals = 200

// maxListBytes bounds how much of a Docker listing is read.
const maxListBytes = 16 << 20

var (
	// imageRef matches an image id as Docker reports it, with or without the
	// sha256: prefix.
	imageRef = regexp.MustCompile(`^(sha256:)?[a-f0-9]{12,64}$`)
	// networkRef matches a network id.
	networkRef = regexp.MustCompile(`^[a-f0-9]{12,64}$`)
	// volumeRef matches a volume name.
	volumeRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)
)

// ValidResourceRef reports whether id is acceptable for a resource of the kind.
func ValidResourceRef(kind agentv1.ResourceKind, id string) bool {
	switch kind {
	case agentv1.ResourceKind_RESOURCE_KIND_IMAGE:
		return imageRef.MatchString(id)
	case agentv1.ResourceKind_RESOURCE_KIND_VOLUME:
		return volumeRef.MatchString(id)
	case agentv1.ResourceKind_RESOURCE_KIND_NETWORK:
		return networkRef.MatchString(id)
	case agentv1.ResourceKind_RESOURCE_KIND_UNSPECIFIED:
		return false
	default:
		return false
	}
}

// resourceItem is one resource as the panel sees it.
type resourceItem struct {
	ID      string   `json:"id"`
	Name    string   `json:"name,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Driver  string   `json:"driver,omitempty"`
	Size    int64    `json:"size,omitempty"`
	Created string   `json:"created,omitempty"`
	InUse   bool     `json:"in_use"`
	// System marks the networks Docker needs and never removes.
	System bool `json:"system,omitempty"`
}

// usage is what the containers of the host use.
type usage struct {
	images   map[string]bool
	networks map[string]bool
	volumes  map[string]bool
}

type dockerContainerUse struct {
	ImageID         string `json:"ImageID"`
	NetworkSettings struct {
		Networks map[string]struct {
			NetworkID string `json:"NetworkID"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
	Mounts []struct {
		Type string `json:"Type"`
		Name string `json:"Name"`
	} `json:"Mounts"`
}

// ListResources lists the resources of one kind as JSON:
// {"kind": "...", "items": [...]}. Unused items come first.
func (d *DockerActions) ListResources(ctx context.Context, kind agentv1.ResourceKind) (string, error) {
	used, err := d.usage(ctx)
	if err != nil {
		return "", err
	}

	var (
		items []resourceItem
		name  string
	)
	switch kind {
	case agentv1.ResourceKind_RESOURCE_KIND_IMAGE:
		name = "image"
		items, err = d.listImages(ctx, used)
	case agentv1.ResourceKind_RESOURCE_KIND_VOLUME:
		name = "volume"
		items, err = d.listVolumes(ctx, used)
	case agentv1.ResourceKind_RESOURCE_KIND_NETWORK:
		name = "network"
		items, err = d.listNetworks(ctx, used)
	case agentv1.ResourceKind_RESOURCE_KIND_UNSPECIFIED:
		return "", errors.New("the kind of resource is not set")
	default:
		return "", fmt.Errorf("unsupported kind of resource %d", kind)
	}
	if err != nil {
		return "", err
	}

	// What can be removed first, then by name.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].InUse != items[j].InUse {
			return !items[i].InUse
		}
		return items[i].label() < items[j].label()
	})
	out, err := json.Marshal(struct {
		Kind  string         `json:"kind"`
		Items []resourceItem `json:"items"`
	}{name, nonNil(items)})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (r resourceItem) label() string {
	if r.Name != "" {
		return r.Name
	}
	if len(r.Tags) > 0 {
		return r.Tags[0]
	}
	return r.ID
}

func nonNil(items []resourceItem) []resourceItem {
	if items == nil {
		return []resourceItem{}
	}
	return items
}

func (d *DockerActions) getJSON(ctx context.Context, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+path, nil)
	if err != nil {
		return err
	}
	response, err := d.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("docker is not reachable: %w", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return dockerError(response)
	}
	return json.NewDecoder(io.LimitReader(response.Body, maxListBytes)).Decode(target)
}

// usage reads what every container, running or not, holds on to.
func (d *DockerActions) usage(ctx context.Context) (usage, error) {
	var containers []dockerContainerUse
	if err := d.getJSON(ctx, "/containers/json?all=1", &containers); err != nil {
		return usage{}, err
	}

	used := usage{images: map[string]bool{}, networks: map[string]bool{}, volumes: map[string]bool{}}
	for _, container := range containers {
		used.images[container.ImageID] = true
		for _, network := range container.NetworkSettings.Networks {
			used.networks[network.NetworkID] = true
		}
		for _, mount := range container.Mounts {
			if mount.Type == "volume" && mount.Name != "" {
				used.volumes[mount.Name] = true
			}
		}
	}
	return used, nil
}

func (d *DockerActions) listImages(ctx context.Context, used usage) ([]resourceItem, error) {
	var images []struct {
		ID       string   `json:"Id"`
		RepoTags []string `json:"RepoTags"`
		Created  int64    `json:"Created"`
		Size     int64    `json:"Size"`
	}
	if err := d.getJSON(ctx, "/images/json", &images); err != nil {
		return nil, err
	}

	items := make([]resourceItem, 0, len(images))
	for _, image := range images {
		items = append(items, resourceItem{
			ID:      image.ID,
			Tags:    image.RepoTags,
			Size:    image.Size,
			Created: unixTime(image.Created),
			InUse:   used.images[image.ID],
		})
	}
	return items, nil
}

func (d *DockerActions) listVolumes(ctx context.Context, used usage) ([]resourceItem, error) {
	var body struct {
		Volumes []struct {
			Name      string `json:"Name"`
			Driver    string `json:"Driver"`
			CreatedAt string `json:"CreatedAt"`
		} `json:"Volumes"`
	}
	if err := d.getJSON(ctx, "/volumes", &body); err != nil {
		return nil, err
	}

	items := make([]resourceItem, 0, len(body.Volumes))
	for _, volume := range body.Volumes {
		items = append(items, resourceItem{
			ID:      volume.Name,
			Name:    volume.Name,
			Driver:  volume.Driver,
			Created: volume.CreatedAt,
			InUse:   used.volumes[volume.Name],
		})
	}
	return items, nil
}

func (d *DockerActions) listNetworks(ctx context.Context, used usage) ([]resourceItem, error) {
	var networks []struct {
		ID      string `json:"Id"`
		Name    string `json:"Name"`
		Driver  string `json:"Driver"`
		Created string `json:"Created"`
	}
	if err := d.getJSON(ctx, "/networks", &networks); err != nil {
		return nil, err
	}

	items := make([]resourceItem, 0, len(networks))
	for _, network := range networks {
		items = append(items, resourceItem{
			ID:      network.ID,
			Name:    network.Name,
			Driver:  network.Driver,
			Created: network.Created,
			InUse:   used.networks[network.ID],
			System:  network.Name == "bridge" || network.Name == "host" || network.Name == "none",
		})
	}
	return items, nil
}

// removalResult is the outcome for one resource.
type removalResult struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// RemoveResources removes the resources one by one and reports each result as
// JSON. It never forces: Docker refuses what is in use, and that refusal is the
// message of the item. The command fails as a whole only when it is malformed.
func (d *DockerActions) RemoveResources(ctx context.Context, items []*agentv1.ResourceRef) (string, error) {
	if len(items) == 0 {
		return "", errors.New("there is nothing to remove")
	}
	if len(items) > maxRemovals {
		return "", fmt.Errorf("at most %d resources can be removed at once", maxRemovals)
	}
	for _, item := range items {
		if !ValidResourceRef(item.GetKind(), item.GetId()) {
			return "", errors.New("invalid resource reference")
		}
	}

	results := make([]removalResult, 0, len(items))
	for _, item := range items {
		result := removalResult{Kind: kindName(item.GetKind()), ID: item.GetId(), OK: true}
		if err := d.removeOne(ctx, item); err != nil {
			result.OK, result.Message = false, err.Error()
		}
		results = append(results, result)
	}

	out, err := json.Marshal(struct {
		Results []removalResult `json:"results"`
	}{results})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (d *DockerActions) removeOne(ctx context.Context, item *agentv1.ResourceRef) error {
	id := url.PathEscape(item.GetId())
	switch item.GetKind() {
	case agentv1.ResourceKind_RESOURCE_KIND_IMAGE:
		return d.call(ctx, http.MethodDelete, "/images/"+id+"?force=0&noprune=0")
	case agentv1.ResourceKind_RESOURCE_KIND_VOLUME:
		return d.call(ctx, http.MethodDelete, "/volumes/"+id+"?force=0")
	case agentv1.ResourceKind_RESOURCE_KIND_NETWORK:
		return d.call(ctx, http.MethodDelete, "/networks/"+id)
	case agentv1.ResourceKind_RESOURCE_KIND_UNSPECIFIED:
		return errors.New("the kind of resource is not set")
	default:
		return fmt.Errorf("unsupported kind of resource %d", item.GetKind())
	}
}

func kindName(kind agentv1.ResourceKind) string {
	switch kind {
	case agentv1.ResourceKind_RESOURCE_KIND_IMAGE:
		return "image"
	case agentv1.ResourceKind_RESOURCE_KIND_VOLUME:
		return "volume"
	case agentv1.ResourceKind_RESOURCE_KIND_NETWORK:
		return "network"
	case agentv1.ResourceKind_RESOURCE_KIND_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

// unixTime formats a Unix time as RFC 3339, or "" for zero.
func unixTime(seconds int64) string {
	if seconds == 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

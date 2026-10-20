package api

import (
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

// Job kinds of the resource commands.
const (
	resourcesListKind   = "resources_list"
	resourcesRemoveKind = "resources_remove"
)

// maxRemovedResources is the most resources one request may remove.
const maxRemovedResources = 200

var (
	imageID    = regexp.MustCompile(`^(sha256:)?[a-f0-9]{12,64}$`)
	networkID  = regexp.MustCompile(`^[a-f0-9]{12,64}$`)
	volumeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,254}$`)
)

// resourceKinds maps the kind names of the API to the typed kinds.
var resourceKinds = map[string]agentv1.ResourceKind{
	"image":   agentv1.ResourceKind_RESOURCE_KIND_IMAGE,
	"volume":  agentv1.ResourceKind_RESOURCE_KIND_VOLUME,
	"network": agentv1.ResourceKind_RESOURCE_KIND_NETWORK,
}

// validResourceID reports whether id fits the kind. The agent checks it again.
func validResourceID(kind, id string) bool {
	switch kind {
	case "image":
		return imageID.MatchString(id)
	case "volume":
		return volumeName.MatchString(id)
	case "network":
		return networkID.MatchString(id)
	default:
		return false
	}
}

type resourcesListRequest struct {
	Kind string `json:"kind"`
}

// NewResourcesListHandler returns POST /api/v1/servers/{serverID}/resources/list.
// The listing comes back as the JSON output of the job.
func NewResourcesListHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")

		var request resourcesListRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		kind, known := resourceKinds[request.Kind]
		if !known {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if _, connected := connectedAgent(deps.Agents, serverID); !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}

		submitJob(w, r, deps, serverID, resourcesListKind, "resources_list",
			map[string]any{"kind": request.Kind},
			map[string]any{"kind": request.Kind},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd:   &agentv1.CoreMsg_ListResources{ListResources: &agentv1.ListResources{Kind: kind}},
				}
			})
	})
}

type resourceRefRequest struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type resourcesRemoveRequest struct {
	Items []resourceRefRequest `json:"items"`
	// Confirm must be true: removing images, volumes and networks cannot be undone.
	Confirm bool `json:"confirm"`
}

// NewResourcesRemoveHandler returns POST
// /api/v1/servers/{serverID}/resources/remove. It never forces a removal: Docker
// refuses the resources that are in use, and the job output says which.
func NewResourcesRemoveHandler(deps ContainerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverID := chi.URLParam(r, "serverID")

		var request resourcesRemoveRequest
		if err := decodeJSON(w, r, &request); err != nil ||
			len(request.Items) == 0 || len(request.Items) > maxRemovedResources {
			writeError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		if !request.Confirm {
			writeError(w, http.StatusBadRequest, "confirmation_required")
			return
		}

		refs := make([]*agentv1.ResourceRef, 0, len(request.Items))
		described := make([]string, 0, len(request.Items))
		for _, item := range request.Items {
			kind, known := resourceKinds[item.Kind]
			if !known || !validResourceID(item.Kind, item.ID) {
				writeError(w, http.StatusBadRequest, "invalid_request")
				return
			}
			refs = append(refs, &agentv1.ResourceRef{Kind: kind, Id: item.ID})
			described = append(described, item.Kind+":"+item.ID)
		}
		if _, connected := connectedAgent(deps.Agents, serverID); !connected {
			writeError(w, http.StatusConflict, "agent_offline")
			return
		}

		submitJob(w, r, deps, serverID, resourcesRemoveKind, "resources_remove",
			map[string]any{"count": len(refs)},
			map[string]any{"count": len(refs), "items": described},
			func(jobID string) *agentv1.CoreMsg {
				return &agentv1.CoreMsg{
					JobId: jobID,
					Cmd:   &agentv1.CoreMsg_RemoveResources{RemoveResources: &agentv1.RemoveResources{Items: refs}},
				}
			})
	})
}

package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
)

// ServerLister lists the enrolled servers.
type ServerLister interface {
	ListServers(ctx context.Context) ([]store.Server, error)
}

// LiveAgents reports the agents connected right now.
type LiveAgents interface {
	Connected() []hub.AgentInfo
}

// ServerDeps are the collaborators of the servers endpoint.
type ServerDeps struct {
	Servers ServerLister
	Agents  LiveAgents
}

// devServerPrefix marks the entries that only exist in insecure dev mode.
const devServerPrefix = "dev:"

// serversResponse is the body of GET /api/v1/servers.
type serversResponse struct {
	Servers []serverEntry `json:"servers"`
}

// serverEntry is one server: the enrolled row and, when its agent is connected,
// what the agent reports. Certificate serials are never exposed.
type serverEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// CreatedAt is null for the development entries, which exist only in memory.
	CreatedAt    *time.Time `json:"created_at"`
	LastSeen     *time.Time `json:"last_seen"`
	Revoked      bool       `json:"revoked"`
	CertNotAfter *time.Time `json:"cert_not_after"`
	Connected    bool       `json:"connected"`
	Live         *liveAgent `json:"live,omitempty"`
}

// liveAgent is the state a connected agent reports.
type liveAgent struct {
	Hostname          string    `json:"hostname"`
	AgentVersion      string    `json:"agent_version"`
	OSName            string    `json:"os_name"`
	OSVersion         string    `json:"os_version"`
	Kernel            string    `json:"kernel"`
	Arch              string    `json:"arch"`
	CPUCount          uint32    `json:"cpu_count"`
	CPUPercent        float64   `json:"cpu_percent"`
	MemUsedBytes      uint64    `json:"mem_used_bytes"`
	MemTotalBytes     uint64    `json:"mem_total_bytes"`
	DiskUsedBytes     uint64    `json:"disk_used_bytes"`
	DiskTotalBytes    uint64    `json:"disk_total_bytes"`
	NetRxBytes        uint64    `json:"net_rx_bytes"`
	NetTxBytes        uint64    `json:"net_tx_bytes"`
	DockerAvailable   bool      `json:"docker_available"`
	Containers        int       `json:"containers"`
	RunningContainers int       `json:"running_containers"`
	ComposeProjects   int       `json:"compose_projects"`
	Services          int       `json:"services"`
	SeenAt            time.Time `json:"seen_at"`
}

// NewServersHandler returns GET /api/v1/servers. It is mounted behind the
// session middleware and RequireFull, so only a full session reaches it.
func NewServersHandler(deps ServerDeps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		servers, err := deps.Servers.ListServers(r.Context())
		if err != nil {
			slog.Error("list servers", "error", err)
			writeError(w, http.StatusInternalServerError, "internal")
			return
		}

		agents := deps.Agents.Connected()
		byServerID := make(map[string]hub.AgentInfo, len(agents))
		for _, agent := range agents {
			if agent.ServerID != "" {
				byServerID[agent.ServerID] = agent
			}
		}

		entries := make([]serverEntry, 0, len(servers)+len(agents))
		for _, server := range servers {
			entry := serverEntry{
				ID:      server.ID,
				Name:    server.Name,
				Revoked: !server.RevokedAt.IsZero(),
			}
			if !server.CreatedAt.IsZero() {
				created := server.CreatedAt
				entry.CreatedAt = &created
			}
			if !server.LastSeen.IsZero() {
				seen := server.LastSeen
				entry.LastSeen = &seen
			}
			if !server.CertNotAfter.IsZero() {
				expires := server.CertNotAfter
				entry.CertNotAfter = &expires
			}
			if agent, ok := byServerID[server.ID]; ok {
				entry.Connected = true
				entry.Live = liveOf(agent)
			}
			entries = append(entries, entry)
		}

		// Without certificates there are no enrolled ids: the agents connected in
		// development mode appear under a synthetic id so `make dev` shows them.
		for _, agent := range agents {
			if agent.ServerID != "" {
				continue
			}
			entries = append(entries, serverEntry{
				ID:        devServerPrefix + agent.Hostname,
				Name:      agent.Hostname,
				Connected: true,
				Live:      liveOf(agent),
			})
		}

		writeJSON(w, http.StatusOK, serversResponse{Servers: entries})
	})
}

// liveOf turns the snapshot of a connected agent into the shape of the API.
func liveOf(agent hub.AgentInfo) *liveAgent {
	return &liveAgent{
		Hostname:          agent.Hostname,
		AgentVersion:      agent.AgentVersion,
		OSName:            agent.OSName,
		OSVersion:         agent.OSVersion,
		Kernel:            agent.Kernel,
		Arch:              agent.Arch,
		CPUCount:          agent.CPUCount,
		CPUPercent:        agent.LastCPUPercent,
		MemUsedBytes:      agent.MemUsedBytes,
		MemTotalBytes:     agent.MemTotalBytes,
		DiskUsedBytes:     agent.DiskUsedBytes,
		DiskTotalBytes:    agent.DiskTotalBytes,
		NetRxBytes:        agent.NetRxBytes,
		NetTxBytes:        agent.NetTxBytes,
		DockerAvailable:   agent.DockerAvailable,
		Containers:        agent.Containers,
		RunningContainers: agent.RunningContainers,
		ComposeProjects:   agent.ComposeProjects,
		Services:          agent.Services,
		SeenAt:            agent.LastSeen,
	}
}

package alerts

import (
	"context"
	"fmt"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
)

type connectedAgents interface{ Connected() []hub.AgentInfo }
type serverLister interface {
	ListServers(context.Context) ([]store.Server, error)
}

// HubSource combines enrolled server records with the latest live hub snapshot.
type HubSource struct {
	hub     connectedAgents
	servers serverLister
}

func NewHubSource(h connectedAgents, servers serverLister) *HubSource {
	return &HubSource{hub: h, servers: servers}
}

func (s *HubSource) Snapshot(ctx context.Context) ([]ServerState, error) {
	servers, err := s.servers.ListServers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enrolled servers: %w", err)
	}
	live := make(map[string]hub.AgentInfo)
	for _, agent := range s.hub.Connected() {
		if agent.ServerID != "" {
			live[agent.ServerID] = agent
		}
	}
	states := make([]ServerState, 0, len(servers))
	for _, server := range servers {
		if !server.RemovedAt.IsZero() {
			continue
		}
		state := ServerState{ID: server.ID, GroupID: server.GroupID, LastSeen: server.LastSeen, Revoked: !server.RevokedAt.IsZero()}
		if agent, ok := live[server.ID]; ok {
			state.Connected = true
			if agent.LastSeen.After(state.LastSeen) {
				state.LastSeen = agent.LastSeen
			}
			state.CPUPercent = agent.LastCPUPercent
			state.MemUsed, state.MemTotal = agent.MemUsedBytes, agent.MemTotalBytes
			state.DiskUsed, state.DiskTotal = agent.DiskUsedBytes, agent.DiskTotalBytes
			state.HasMetrics = !agent.MetricsAt.IsZero()
			state.Containers = make([]ContainerState, 0, len(agent.ContainerList))
			for _, container := range agent.ContainerList {
				if container == nil {
					continue
				}
				state.Containers = append(state.Containers, ContainerState{Name: container.GetName(), State: container.GetState(), Health: container.GetHealth(), Stack: container.GetComposeProject()})
			}
		}
		states = append(states, state)
	}
	return states, nil
}

// Package hub implements the talaia-core side of the agent link: it accepts the
// AgentHub.Connect stream and keeps an in-memory registry of the connected
// agents.
package hub

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
	"github.com/catlinux/Talaia/proto/gen/go/agentv1connect"
)

// AgentInfo is a snapshot of a connected agent.
type AgentInfo struct {
	Hostname       string
	AgentVersion   string
	LastSeen       time.Time
	LastCPUPercent float64
	MemUsedBytes   uint64
	DiskUsedBytes  uint64
	DiskTotalBytes uint64
	NetRxBytes     uint64
	NetTxBytes     uint64
}

// agentStream is the part of the server stream the hub uses. Keeping it small
// lets the tests drive the hub without a network.
type agentStream interface {
	Receive() (*agentv1.AgentMsg, error)
	Send(*agentv1.CoreMsg) error
}

// Hub accepts agent streams and keeps the connected agents in memory.
type Hub struct {
	server *connect.Server

	mu     sync.RWMutex
	agents map[string]AgentInfo

	now func() time.Time
}

// New returns a Hub ready to be served by Handler.
func New() *Hub {
	h := &Hub{
		server: connect.NewServer(),
		agents: make(map[string]AgentInfo),
		now:    time.Now,
	}
	agentv1connect.RegisterAgentHubHandler(h.server, h)
	return h
}

// Handler returns the path of the AgentHub.Connect procedure and the HTTP
// handler that serves it, ready to be mounted on the main router.
func (h *Hub) Handler() (path string, handler http.Handler) {
	mux := http.NewServeMux()
	connecthttp.Mount(mux, h.server)
	return agentv1connect.AgentHubConnectProcedure, mux
}

// Connected returns a copy of the connected agents, ordered by hostname.
func (h *Hub) Connected() []AgentInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	agents := make([]AgentInfo, 0, len(h.agents))
	for _, info := range h.agents {
		agents = append(agents, info)
	}
	slices.SortFunc(agents, func(a, b AgentInfo) int { return cmp.Compare(a.Hostname, b.Hostname) })
	return agents
}

// Connect implements agentv1connect.AgentHubHandler.
func (h *Hub) Connect(ctx context.Context, stream agentv1connect.AgentHubConnectServerStream) error {
	return h.serve(ctx, stream)
}

func (h *Hub) serve(_ context.Context, stream agentStream) error {
	first, err := stream.Receive()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	inventory := first.GetInventory()
	if inventory == nil {
		return connect.NewError(connect.CodeInvalidArgument, "first message must be an inventory")
	}
	hostname := inventory.GetHostname()
	if hostname == "" {
		return connect.NewError(connect.CodeInvalidArgument, "inventory hostname is empty")
	}

	info := AgentInfo{
		Hostname:     hostname,
		AgentVersion: inventory.GetAgentVersion(),
		LastSeen:     h.now(),
	}
	h.store(info)
	slog.Info("agent connected", "hostname", hostname, "agent_version", info.AgentVersion)
	defer func() {
		h.remove(hostname)
		slog.Info("agent disconnected", "hostname", hostname)
	}()

	for {
		msg, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		switch {
		case msg.GetHeartbeat() != nil:
			heartbeat := msg.GetHeartbeat()
			info.LastSeen = h.now()
			h.store(info)
			slog.Debug("heartbeat received", "hostname", hostname, "seq", heartbeat.GetSeq())

			ack := &agentv1.CoreMsg{
				Cmd: &agentv1.CoreMsg_HeartbeatAck{
					HeartbeatAck: &agentv1.HeartbeatAck{Seq: heartbeat.GetSeq()},
				},
			}
			if err := stream.Send(ack); err != nil {
				return err
			}

		case msg.GetMetrics() != nil:
			metrics := msg.GetMetrics()
			info.LastSeen = h.now()
			info.LastCPUPercent = metrics.GetCpuPercent()
			info.MemUsedBytes = metrics.GetMemUsedBytes()
			info.DiskUsedBytes = metrics.GetDiskUsedBytes()
			info.DiskTotalBytes = metrics.GetDiskTotalBytes()
			info.NetRxBytes = metrics.GetNetRxBytes()
			info.NetTxBytes = metrics.GetNetTxBytes()
			h.store(info)
			slog.Debug("metrics received", "hostname", hostname, "cpu_percent", metrics.GetCpuPercent())

		case msg.GetInventory() != nil:
			// The inventory is also sent on change.
			info.AgentVersion = msg.GetInventory().GetAgentVersion()
			info.LastSeen = h.now()
			h.store(info)
			slog.Debug("inventory updated", "hostname", hostname)

		default:
			slog.Debug("unhandled message", "hostname", hostname)
		}
	}
}

func (h *Hub) store(info AgentInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.agents[info.Hostname] = info
}

func (h *Hub) remove(hostname string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.agents, hostname)
}

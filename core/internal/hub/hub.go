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

	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
	"github.com/catlinux/Talaia/proto/gen/go/agentv1connect"
)

// defaultCheckInterval is how often at most the hub re-validates a connection
// and writes last_seen.
const defaultCheckInterval = 30 * time.Second

// AgentInfo is a snapshot of a connected agent.
type AgentInfo struct {
	// ServerID is the id of the enrolled server; empty in insecure dev mode.
	ServerID          string
	Hostname          string
	OSName            string
	OSVersion         string
	Kernel            string
	Arch              string
	AgentVersion      string
	CPUCount          uint32
	MemTotalBytes     uint64
	LastSeen          time.Time
	LastCPUPercent    float64
	MemUsedBytes      uint64
	DiskUsedBytes     uint64
	DiskTotalBytes    uint64
	NetRxBytes        uint64
	NetTxBytes        uint64
	DockerAvailable   bool
	Containers        int
	RunningContainers int
	ComposeProjects   int
	Services          int

	// key is the registry key: the server id with certificates, the hostname
	// in insecure dev mode.
	key string
	// token identifies the connection that wrote this entry, so a stale
	// connection does not delete the entry of a newer one.
	token uint64
}

// applyInventory copies the facts of an inventory message, including the
// machine facts and the docker and systemd counts, into the snapshot.
func (info *AgentInfo) applyInventory(inventory *agentv1.Inventory) {
	info.OSName = inventory.GetOsName()
	info.OSVersion = inventory.GetOsVersion()
	info.Kernel = inventory.GetKernel()
	info.Arch = inventory.GetArch()
	info.AgentVersion = inventory.GetAgentVersion()
	info.CPUCount = inventory.GetCpuCount()
	info.MemTotalBytes = inventory.GetMemTotalBytes()
	info.DockerAvailable = inventory.GetDockerAvailable()

	running := 0
	for _, container := range inventory.GetContainers() {
		if container.GetState() == "running" {
			running++
		}
	}

	info.Containers = len(inventory.GetContainers())
	info.RunningContainers = running
	info.ComposeProjects = len(inventory.GetComposeProjects())
	info.Services = len(inventory.GetServices())
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

	// servers is nil in insecure dev mode: no certificate is checked then.
	servers ServerStore

	// CheckInterval is how often a live connection is re-validated against the
	// store. Tests lower it.
	CheckInterval time.Duration

	mu     sync.RWMutex
	agents map[string]AgentInfo
	tokens uint64

	now func() time.Time
}

// New returns a Hub that authenticates the agents with their client certificate
// and keys the registry by server id.
func New(servers ServerStore) *Hub {
	return newHub(servers)
}

// NewInsecure returns a Hub that requires no certificate and keys the registry
// by hostname. It is only for insecure development mode.
func NewInsecure() *Hub {
	return newHub(nil)
}

func newHub(servers ServerStore) *Hub {
	h := &Hub{
		server:        connect.NewServer(),
		servers:       servers,
		CheckInterval: defaultCheckInterval,
		agents:        make(map[string]AgentInfo),
		now:           time.Now,
	}
	agentv1connect.RegisterAgentHubHandler(h.server, h)
	return h
}

// Handler returns the path of the AgentHub.Connect procedure and the HTTP
// handler that serves it. With certificates the handler requires a verified
// client certificate of an enrolled, not revoked server.
func (h *Hub) Handler() (path string, handler http.Handler) {
	mux := http.NewServeMux()
	connecthttp.Mount(mux, h.server)

	if h.servers == nil {
		return agentv1connect.AgentHubConnectProcedure, mux
	}
	return agentv1connect.AgentHubConnectProcedure, authenticate(h.servers, mux)
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

func (h *Hub) serve(ctx context.Context, stream agentStream) error {
	// The identity comes from the client certificate; in insecure dev mode there
	// is none, so the registry is keyed by hostname instead.
	conn, _ := connectionFromContext(ctx)
	serverID := conn.serverID

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

	key := hostname
	if serverID != "" {
		key = serverID
	}

	info := AgentInfo{
		ServerID: serverID,
		Hostname: hostname,
		LastSeen: h.now(),
		key:      key,
		token:    h.newConnectionToken(),
	}
	info.applyInventory(inventory)
	h.store(info)
	h.touchServer(ctx, serverID)
	slog.Info("agent connected", "server_id", serverID, "hostname", hostname, "agent_version", info.AgentVersion)

	defer func() {
		h.remove(info)
		slog.Info("agent disconnected", "server_id", serverID, "hostname", hostname)
	}()

	lastCheck := h.now()

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
			// The stream is re-validated periodically, not only when it opens:
			// a server revoked meanwhile does not stay connected.
			if info.LastSeen.Sub(lastCheck) >= h.CheckInterval {
				lastCheck = info.LastSeen
				if err := h.checkConnection(ctx, serverID, conn.serial); err != nil {
					slog.Info("closing agent stream", "server_id", serverID, "hostname", hostname, "reason", err)
					return connect.NewError(connect.CodePermissionDenied, err.Error())
				}
			}
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
			info.applyInventory(msg.GetInventory())
			info.LastSeen = h.now()
			h.store(info)
			slog.Debug("inventory updated", "hostname", hostname)

		default:
			slog.Debug("unhandled message", "hostname", hostname)
		}
	}
}

// touchServer records that the server has been seen, when the hub has a server
// store.
func (h *Hub) touchServer(ctx context.Context, serverID string) {
	if h.servers == nil || serverID == "" {
		return
	}
	if err := h.servers.TouchServer(ctx, serverID); err != nil {
		slog.Warn("touch server", "server_id", serverID, "error", err)
	}
}

// checkConnection re-reads the server of a live stream and returns an error when
// it may no longer stay connected: it disappeared, it was revoked or its
// certificate serial is no longer accepted. When the server is fine it also
// records last_seen.
func (h *Hub) checkConnection(ctx context.Context, serverID, serial string) error {
	if h.servers == nil || serverID == "" {
		return nil
	}

	server, err := h.servers.GetServer(ctx, serverID)
	if errors.Is(err, store.ErrNotFound) {
		return errors.New("the server is no longer enrolled")
	}
	if err != nil {
		// A store failure is not a reason to drop a healthy stream.
		slog.Warn("check server", "server_id", serverID, "error", err)
		return nil
	}
	if !server.RevokedAt.IsZero() {
		return errors.New("the server is revoked")
	}
	if !server.AcceptsSerial(serial) {
		return errors.New("the certificate serial is no longer accepted")
	}

	if err := h.servers.TouchServer(ctx, serverID); err != nil {
		slog.Warn("touch server", "server_id", serverID, "error", err)
	}
	return nil
}

func (h *Hub) store(info AgentInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.agents[info.key] = info
}

// remove drops the entry only when it still belongs to this connection, so the
// previous connection of a reconnecting agent does not remove the new one.
func (h *Hub) remove(info AgentInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()

	current, ok := h.agents[info.key]
	if ok && current.token == info.token {
		delete(h.agents, info.key)
	}
}

// newConnectionToken identifies a new connection.
func (h *Hub) newConnectionToken() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tokens++
	return h.tokens
}

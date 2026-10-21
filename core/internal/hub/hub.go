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

// outboxSize is how many core messages may wait to be written to one agent.
const outboxSize = 32

// ErrNotConnected is returned when the agent of a server has no open stream.
var ErrNotConnected = errors.New("the agent is not connected")

// ErrAgentBusy is returned when too many messages wait for the agent.
var ErrAgentBusy = errors.New("the agent has too many pending commands")

// JobEvents receives the progress of the commands the agents run.
type JobEvents interface {
	RecordJobEvent(ctx context.Context, serverID string, event *agentv1.JobEvent)
}

// TerminalEvents receives what the agents report about their terminal sessions.
// The hub calls it from the loop that reads the stream of the agent, so it must
// not block. serverID is the server the stream belongs to: a session id from any
// other server is not theirs.
type TerminalEvents interface {
	TerminalOpened(serverID string, msg *agentv1.TerminalOpened)
	TerminalOutput(serverID string, msg *agentv1.TerminalOutput)
	TerminalClosed(serverID string, msg *agentv1.TerminalClosed)
	// AgentGone says the stream of the server ended: its shells are gone too.
	AgentGone(serverID string)
}

// agentConn is the live stream of one agent. Every message to the agent goes
// through outbox, so a single goroutine writes to the stream.
type agentConn struct {
	token  uint64
	outbox chan *agentv1.CoreMsg
	// done is closed when the connection ends.
	done chan struct{}
	// stopOnce makes closing the connection idempotent: the API may remove a
	// server while its stream is ending on its own.
	stopOnce sync.Once
	cancel   context.CancelFunc
	// closeStream unblocks a stream that waits for another message. The real
	// connect stream has no Close, so it stays nil there.
	closeStream func() error
}

// stop ends the connection: it releases waiters, cancels the stream context and,
// when possible, closes the stream so its read loop returns at once.
func (c *agentConn) stop() {
	c.stopOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		close(c.done)
		if c.closeStream != nil {
			_ = c.closeStream()
		}
	})
}

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
	// ContainerList is the last list of containers the agent reported.
	ContainerList []*agentv1.Container
	// ManagedStacks are the stacks Talaia manages on the server, sorted.
	ManagedStacks []string
	// AdoptedStacks are the managed stacks that were adopted, a subset of ManagedStacks.
	AdoptedStacks []string
	// ContainerStats is the last sample of the running containers, by short id;
	// StatsAt says when it was taken.
	ContainerStats map[string]*agentv1.ContainerStat
	StatsAt        time.Time

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
	info.ContainerList = inventory.GetContainers()
	info.ManagedStacks = inventory.GetManagedStacks()
	info.AdoptedStacks = inventory.GetAdoptedStacks()
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
	conns  map[string]*agentConn
	tokens uint64

	// jobEvents is told about the events of the commands; nil ignores them.
	jobEvents JobEvents
	// terminals is told about the terminal sessions; nil ignores them.
	terminals TerminalEvents

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
		conns:         make(map[string]*agentConn),
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

// SetJobEvents sets who is told about the events of the commands.
func (h *Hub) SetJobEvents(events JobEvents) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.jobEvents = events
}

// SetTerminals sets who is told about the terminal sessions of the agents.
func (h *Hub) SetTerminals(events TerminalEvents) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.terminals = events
}

// DispatchWait queues a message for the agent of a server and, when the queue
// is full, waits for room until ctx ends. Terminal input uses it: waiting pushes
// the pressure back to the browser instead of losing keystrokes.
func (h *Hub) DispatchWait(ctx context.Context, serverID string, msg *agentv1.CoreMsg) error {
	h.mu.RLock()
	link, ok := h.conns[serverID]
	h.mu.RUnlock()
	if !ok {
		return ErrNotConnected
	}

	select {
	case link.outbox <- msg:
		return nil
	case <-link.done:
		return ErrNotConnected
	case <-ctx.Done():
		return ErrAgentBusy
	}
}

// Dispatch queues a command for the agent of a server. It never blocks: when the
// agent is not connected or has too many pending messages it returns an error.
func (h *Hub) Dispatch(serverID string, msg *agentv1.CoreMsg) error {
	h.mu.RLock()
	link, ok := h.conns[serverID]
	h.mu.RUnlock()
	if !ok {
		return ErrNotConnected
	}

	select {
	case link.outbox <- msg:
		return nil
	default:
		return ErrAgentBusy
	}
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

// Disconnect closes the live stream of a server at once, drops it from the
// registry and tells the terminal manager that its shells are gone. It is
// idempotent and does nothing when the server is not connected.
func (h *Hub) Disconnect(serverID string) {
	if serverID == "" {
		return
	}

	h.mu.Lock()
	link, ok := h.conns[serverID]
	delete(h.conns, serverID)
	delete(h.agents, serverID)
	h.mu.Unlock()
	if !ok {
		return
	}

	link.stop()
	h.terminalsGone(serverID)
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

	// One goroutine writes to the stream: the acknowledgements and the commands
	// from the rest of the core queue here.
	ctx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()
	link := &agentConn{
		token:  info.token,
		outbox: make(chan *agentv1.CoreMsg, outboxSize),
		done:   make(chan struct{}),
		cancel: cancelStream,
	}
	if closer, ok := stream.(interface{ Close() error }); ok {
		link.closeStream = closer.Close
	}
	h.register(key, link)
	go writeLoop(ctx, stream, link.outbox, cancelStream)
	h.touchServer(ctx, serverID)
	slog.Info("agent connected", "server_id", serverID, "hostname", hostname, "agent_version", info.AgentVersion)

	defer func() {
		link.stop()
		if h.unregister(key, link) {
			h.terminalsGone(serverID)
		}
		h.remove(info)
		slog.Info("agent disconnected", "server_id", serverID, "hostname", hostname)
	}()

	lastCheck := h.now()
	messages := make(chan receivedMessage)
	go receiveMessages(ctx, stream, messages)

	for {
		var msg *agentv1.AgentMsg
		select {
		case <-ctx.Done():
			return nil
		case received := <-messages:
			if received.err != nil {
				if errors.Is(received.err, io.EOF) {
					return nil
				}
				return received.err
			}
			msg = received.msg
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
			select {
			case link.outbox <- ack:
			case <-ctx.Done():
				return ctx.Err()
			}

		case msg.GetJobEvent() != nil:
			h.recordJobEvent(ctx, serverID, msg.GetJobEvent())

		case msg.GetTerminalOpened() != nil || msg.GetTerminalOutput() != nil || msg.GetTerminalClosed() != nil:
			h.terminalEvent(serverID, msg)

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

		case msg.GetContainerStats() != nil:
			stats := make(map[string]*agentv1.ContainerStat, len(msg.GetContainerStats().GetItems()))
			for _, item := range msg.GetContainerStats().GetItems() {
				stats[item.GetId()] = item
			}
			info.ContainerStats = stats
			info.StatsAt = h.now()
			info.LastSeen = info.StatsAt
			h.store(info)

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

// receivedMessage is one message read from the agent stream, or the error that
// ended it.
type receivedMessage struct {
	msg *agentv1.AgentMsg
	err error
}

// receiveMessages reads the stream in its own goroutine so the serve loop can
// also react to the connection being canceled by Disconnect.
func receiveMessages(ctx context.Context, stream agentStream, out chan<- receivedMessage) {
	for {
		msg, err := stream.Receive()
		select {
		case out <- receivedMessage{msg: msg, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
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

// writeLoop writes the queued messages to the stream until ctx ends or a write
// fails, in which case it ends the connection.
func writeLoop(ctx context.Context, stream agentStream, outbox <-chan *agentv1.CoreMsg, cancel context.CancelFunc) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-outbox:
			if err := stream.Send(msg); err != nil {
				slog.Debug("write to agent failed", "error", err)
				cancel()
				return
			}
		}
	}
}

func (h *Hub) register(key string, link *agentConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[key] = link
}

// unregister drops the connection only when it is still the current one, and
// reports whether it was.
func (h *Hub) unregister(key string, link *agentConn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[key] == link {
		delete(h.conns, key)
		return true
	}
	return false
}

func (h *Hub) terminalSink() TerminalEvents {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.terminals
}

// terminalEvent passes a terminal message of the agent of serverID on. The
// server comes from the connection, never from the message.
func (h *Hub) terminalEvent(serverID string, msg *agentv1.AgentMsg) {
	sink := h.terminalSink()
	if sink == nil || serverID == "" {
		return
	}
	switch {
	case msg.GetTerminalOpened() != nil:
		sink.TerminalOpened(serverID, msg.GetTerminalOpened())
	case msg.GetTerminalOutput() != nil:
		sink.TerminalOutput(serverID, msg.GetTerminalOutput())
	case msg.GetTerminalClosed() != nil:
		sink.TerminalClosed(serverID, msg.GetTerminalClosed())
	}
}

func (h *Hub) terminalsGone(serverID string) {
	if sink := h.terminalSink(); sink != nil && serverID != "" {
		sink.AgentGone(serverID)
	}
}

func (h *Hub) recordJobEvent(ctx context.Context, serverID string, event *agentv1.JobEvent) {
	h.mu.RLock()
	sink := h.jobEvents
	h.mu.RUnlock()
	if sink != nil {
		sink.RecordJobEvent(ctx, serverID, event)
	}
}

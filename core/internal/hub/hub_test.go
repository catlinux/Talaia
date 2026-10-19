package hub

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeStream struct {
	in  chan *agentv1.AgentMsg
	out chan *agentv1.CoreMsg
}

func newFakeStream() *fakeStream {
	return &fakeStream{
		in:  make(chan *agentv1.AgentMsg, 8),
		out: make(chan *agentv1.CoreMsg, 8),
	}
}

func (f *fakeStream) Receive() (*agentv1.AgentMsg, error) {
	msg, ok := <-f.in
	if !ok {
		return nil, io.EOF
	}
	return msg, nil
}

func (f *fakeStream) Send(msg *agentv1.CoreMsg) error {
	f.out <- msg
	return nil
}

func (f *fakeStream) close() {
	close(f.in)
}

func inventoryMsg(hostname string) *agentv1.AgentMsg {
	return &agentv1.AgentMsg{
		Msg: &agentv1.AgentMsg_Inventory{
			Inventory: &agentv1.Inventory{
				Hostname:        hostname,
				AgentVersion:    "1.2.3",
				DockerAvailable: true,
				DockerVersion:   "27.1.1",
				Containers: []*agentv1.Container{
					{Id: "cccccccccccc", Name: "job", State: "exited"},
					{Id: "aaaaaaaaaaaa", Name: "shop-db-1", State: "running"},
					{Id: "bbbbbbbbbbbb", Name: "shop-web-1", State: "running"},
				},
				ComposeProjects: []*agentv1.ComposeProject{{Name: "shop", ContainerCount: 2, RunningCount: 2}},
				Services:        []*agentv1.SystemdUnit{{Name: "docker.service"}, {Name: "ssh.service"}},
			},
		},
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within the timeout")
}

// fakeServerStore is a ServerStore for the tests.
type fakeServerStore struct {
	mu      sync.Mutex
	servers map[string]store.Server
	touched []string
}

func newFakeServerStore(servers ...store.Server) *fakeServerStore {
	f := &fakeServerStore{servers: make(map[string]store.Server)}
	for _, server := range servers {
		f.servers[server.ID] = server
	}
	return f
}

// enrolledServer returns a not revoked server with a certificate serial.
func enrolledServer(id, serial string) store.Server {
	return store.Server{ID: id, Name: id, CertSerial: serial}
}

func (f *fakeServerStore) setServer(server store.Server) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.servers[server.ID] = server
}

func (f *fakeServerStore) removeServer(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.servers, id)
}

func (f *fakeServerStore) GetServer(_ context.Context, id string) (store.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	server, ok := f.servers[id]
	if !ok {
		return store.Server{}, store.ErrNotFound
	}
	return server, nil
}

func (f *fakeServerStore) TouchServer(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touched = append(f.touched, id)
	return nil
}

func (f *fakeServerStore) touchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.touched)
}

// storedToken returns the connection token of a registry entry.
func storedToken(h *Hub, key string) uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.agents[key].token
}

func TestServeKeysTheRegistryByServerID(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))
	stream := newFakeStream()
	done := make(chan error, 1)
	go func() {
		done <- h.serve(withConnection(context.Background(), "server-1", "aa"), stream)
	}()

	stream.in <- inventoryMsg("vps-web-01")
	waitFor(t, func() bool { return len(h.Connected()) == 1 })

	info := h.Connected()[0]
	if info.ServerID != "server-1" {
		t.Errorf("server id = %q, want server-1", info.ServerID)
	}
	if info.Hostname != "vps-web-01" {
		t.Errorf("hostname = %q, want the one from the inventory", info.Hostname)
	}
	if _, ok := h.agents["server-1"]; !ok {
		t.Error("the registry is not keyed by server id")
	}

	stream.close()
	waitServe(t, done)
}

func TestServeKeepsTheNewConnectionWhenTheOldOneEnds(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))

	first := newFakeStream()
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- h.serve(withConnection(context.Background(), "server-1", "aa"), first)
	}()
	first.in <- inventoryMsg("vps-web-01")
	waitFor(t, func() bool { return len(h.Connected()) == 1 })
	firstToken := storedToken(h, "server-1")

	// The agent reconnects before the old connection has ended.
	second := newFakeStream()
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- h.serve(withConnection(context.Background(), "server-1", "aa"), second)
	}()
	second.in <- inventoryMsg("vps-web-01")
	waitFor(t, func() bool { return storedToken(h, "server-1") != firstToken })

	// The old connection ending must not remove the new entry.
	first.close()
	waitServe(t, firstDone)
	if len(h.Connected()) != 1 {
		t.Fatal("the old connection removed the entry of the new one")
	}
	if storedToken(h, "server-1") == firstToken {
		t.Fatal("the registry kept the old connection")
	}

	second.close()
	waitServe(t, secondDone)
	if len(h.Connected()) != 0 {
		t.Fatal("the agent was not removed when the stream ended")
	}
}

func TestServeTouchesTheServerAtMostOncePerInterval(t *testing.T) {
	servers := newFakeServerStore(enrolledServer("server-1", "aa"))
	h := New(servers)
	stream := newFakeStream()
	done := make(chan error, 1)
	go func() {
		done <- h.serve(withConnection(context.Background(), "server-1", "aa"), stream)
	}()

	stream.in <- inventoryMsg("vps-web-01")
	waitFor(t, func() bool { return len(h.Connected()) == 1 })

	for seq := uint64(1); seq <= 3; seq++ {
		stream.in <- &agentv1.AgentMsg{
			Msg: &agentv1.AgentMsg_Heartbeat{Heartbeat: &agentv1.Heartbeat{Seq: seq}},
		}
		select {
		case <-stream.out:
		case <-time.After(time.Second):
			t.Fatal("no heartbeat acknowledgement")
		}
	}

	if got := servers.touchCount(); got != 1 {
		t.Errorf("touch calls = %d, want only the one made on connect", got)
	}

	stream.close()
	waitServe(t, done)
}

// heartbeatMsg builds a heartbeat message.
func heartbeatMsg(seq uint64) *agentv1.AgentMsg {
	return &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_Heartbeat{Heartbeat: &agentv1.Heartbeat{Seq: seq}}}
}

// startStreamedAgent connects a stream with a certificate serial and waits until
// the hub has registered the agent.
func startStreamedAgent(t *testing.T, h *Hub, serverID, serial string) (*fakeStream, chan error) {
	t.Helper()

	stream := newFakeStream()
	done := make(chan error, 1)
	go func() {
		done <- h.serve(withConnection(context.Background(), serverID, serial), stream)
	}()

	stream.in <- inventoryMsg("vps-web-01")
	waitFor(t, func() bool { return len(h.Connected()) == 1 })
	return stream, done
}

// expectStreamClosed waits for a serve call to fail with PermissionDenied.
func expectStreamClosed(t *testing.T, done chan error) {
	t.Helper()

	select {
	case err := <-done:
		var connectErr *connect.Error
		if !errors.As(err, &connectErr) || connectErr.Code() != connect.CodePermissionDenied {
			t.Fatalf("serve = %v, want a PermissionDenied connect error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the stream was not closed")
	}
}

func TestServeClosesARevokedStream(t *testing.T) {
	servers := newFakeServerStore(enrolledServer("server-1", "aa"))
	h := New(servers)
	h.CheckInterval = time.Millisecond

	stream, done := startStreamedAgent(t, h, "server-1", "aa")

	// The server is revoked while its stream is open.
	servers.setServer(store.Server{ID: "server-1", Name: "server-1", CertSerial: "aa", RevokedAt: time.Now()})
	time.Sleep(5 * time.Millisecond)
	stream.in <- heartbeatMsg(1)

	expectStreamClosed(t, done)
	if connected := h.Connected(); len(connected) != 0 {
		t.Fatalf("the revoked agent is still registered: %+v", connected)
	}
}

func TestServeClosesAStreamWhoseSerialIsNotAccepted(t *testing.T) {
	servers := newFakeServerStore(enrolledServer("server-1", "aa"))
	h := New(servers)
	h.CheckInterval = time.Millisecond

	stream, done := startStreamedAgent(t, h, "server-1", "aa")

	// A certificate the agent never got: the serial it holds is neither the
	// current one nor the previous one any more.
	servers.setServer(store.Server{ID: "server-1", Name: "server-1", CertSerial: "bb", PrevCertSerial: "cc"})
	time.Sleep(5 * time.Millisecond)
	stream.in <- heartbeatMsg(1)

	expectStreamClosed(t, done)
}

func TestServeClosesAStreamWhenTheServerDisappears(t *testing.T) {
	servers := newFakeServerStore(enrolledServer("server-1", "aa"))
	h := New(servers)
	h.CheckInterval = time.Millisecond

	stream, done := startStreamedAgent(t, h, "server-1", "aa")

	servers.removeServer("server-1")
	time.Sleep(5 * time.Millisecond)
	stream.in <- heartbeatMsg(1)

	expectStreamClosed(t, done)
}

func TestServeKeepsAStreamThatPresentsThePreviousSerial(t *testing.T) {
	servers := newFakeServerStore(enrolledServer("server-1", "aa"))
	h := New(servers)
	h.CheckInterval = time.Millisecond

	stream, done := startStreamedAgent(t, h, "server-1", "aa")

	// The agent renewed but has not stored the new certificate yet: the serial it
	// presents is now the previous one and must still be accepted.
	servers.setServer(store.Server{ID: "server-1", Name: "server-1", CertSerial: "bb", PrevCertSerial: "aa"})
	time.Sleep(5 * time.Millisecond)
	stream.in <- heartbeatMsg(1)

	select {
	case ack := <-stream.out:
		if ack.GetHeartbeatAck() == nil {
			t.Fatalf("ack = %v", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("no heartbeat acknowledgement")
	}
	if connected := h.Connected(); len(connected) != 1 {
		t.Fatalf("the agent was dropped: %+v", connected)
	}

	stream.close()
	waitServe(t, done)
}

func TestServeRejectsFirstMessageThatIsNotInventory(t *testing.T) {
	h := NewInsecure()
	stream := newFakeStream()
	stream.in <- &agentv1.AgentMsg{
		Msg: &agentv1.AgentMsg_Heartbeat{Heartbeat: &agentv1.Heartbeat{Seq: 1}},
	}

	err := h.serve(context.Background(), stream)

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("error = %v, want a connect error", err)
	}
	if connectErr.Code() != connect.CodeInvalidArgument {
		t.Fatalf("code = %v, want %v", connectErr.Code(), connect.CodeInvalidArgument)
	}
	if connected := h.Connected(); len(connected) != 0 {
		t.Fatalf("agent was registered: %+v", connected)
	}
}

func TestServeRegistersAgentAndTracksState(t *testing.T) {
	h := NewInsecure()
	stream := newFakeStream()
	done := make(chan error, 1)
	go func() {
		done <- h.serve(context.Background(), stream)
	}()

	stream.in <- inventoryMsg("vps-test")
	waitFor(t, func() bool { return len(h.Connected()) == 1 })

	info := h.Connected()[0]
	if info.Hostname != "vps-test" {
		t.Fatalf("hostname = %q, want vps-test", info.Hostname)
	}
	if info.AgentVersion != "1.2.3" {
		t.Fatalf("agent version = %q, want 1.2.3", info.AgentVersion)
	}
	if info.LastSeen.IsZero() {
		t.Fatal("last seen is zero")
	}
	if !info.DockerAvailable || info.Containers != 3 || info.RunningContainers != 2 ||
		info.ComposeProjects != 1 || info.Services != 2 {
		t.Fatalf("inventory counts = %+v, want 3 containers (2 running), 1 project and 2 services", info)
	}

	// A later inventory updates the counts too.
	stream.in <- &agentv1.AgentMsg{
		Msg: &agentv1.AgentMsg_Inventory{
			Inventory: &agentv1.Inventory{
				Hostname:        "vps-test",
				AgentVersion:    "1.2.4",
				DockerAvailable: false,
				Containers:      []*agentv1.Container{{Id: "aaaaaaaaaaaa", Name: "web", State: "running"}},
			},
		},
	}
	waitFor(t, func() bool {
		connected := h.Connected()
		return len(connected) == 1 && connected[0].AgentVersion == "1.2.4"
	})

	info = h.Connected()[0]
	if info.DockerAvailable || info.Containers != 1 || info.RunningContainers != 1 ||
		info.ComposeProjects != 0 || info.Services != 0 {
		t.Fatalf("updated inventory counts = %+v, want 1 container (1 running) and nothing else", info)
	}

	stream.in <- &agentv1.AgentMsg{
		Msg: &agentv1.AgentMsg_Heartbeat{Heartbeat: &agentv1.Heartbeat{Seq: 7}},
	}
	select {
	case ack := <-stream.out:
		if ack.GetHeartbeatAck() == nil || ack.GetHeartbeatAck().GetSeq() != 7 {
			t.Fatalf("ack = %v, want seq 7", ack)
		}
	case <-time.After(time.Second):
		t.Fatal("no heartbeat acknowledgement received")
	}

	stream.in <- &agentv1.AgentMsg{
		Msg: &agentv1.AgentMsg_Metrics{Metrics: &agentv1.Metrics{
			Hostname:       "vps-test",
			CpuPercent:     12.5,
			MemUsedBytes:   1024,
			DiskUsedBytes:  2048,
			DiskTotalBytes: 4096,
			NetRxBytes:     11,
			NetTxBytes:     22,
		}},
	}
	waitFor(t, func() bool {
		connected := h.Connected()
		return len(connected) == 1 && connected[0].LastCPUPercent == 12.5
	})

	info = h.Connected()[0]
	if info.MemUsedBytes != 1024 {
		t.Errorf("mem used = %d, want 1024", info.MemUsedBytes)
	}
	if info.DiskUsedBytes != 2048 {
		t.Errorf("disk used = %d, want 2048", info.DiskUsedBytes)
	}
	if info.DiskTotalBytes != 4096 {
		t.Errorf("disk total = %d, want 4096", info.DiskTotalBytes)
	}
	if info.NetRxBytes != 11 {
		t.Errorf("net rx = %d, want 11", info.NetRxBytes)
	}
	if info.NetTxBytes != 22 {
		t.Errorf("net tx = %d, want 22", info.NetTxBytes)
	}

	stream.close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not return after the stream ended")
	}
	if connected := h.Connected(); len(connected) != 0 {
		t.Fatalf("agent was not unregistered: %+v", connected)
	}
}

// waitServe waits for a serve call to return and checks that it ended cleanly.
func waitServe(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve returned an error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return")
	}
}

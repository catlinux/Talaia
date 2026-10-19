package link

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	"github.com/catlinux/Talaia/agent/internal/config"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
	"github.com/catlinux/Talaia/proto/gen/go/agentv1connect"
)

type fakeCollector struct {
	hostname string
	metrics  *agentv1.Metrics
}

func (f fakeCollector) Inventory() (*agentv1.Inventory, error) {
	return &agentv1.Inventory{
		Hostname:      f.hostname,
		OsName:        "test",
		Arch:          "amd64",
		AgentVersion:  "test",
		CpuCount:      2,
		MemTotalBytes: 1024,
	}, nil
}

func (f fakeCollector) Metrics() (*agentv1.Metrics, error) {
	return f.metrics, nil
}

// testHub is a real agent stream served over h2c. It ends the first connection
// after acknowledging one heartbeat, so the link has to reconnect.
type testHub struct {
	mu          sync.Mutex
	connects    int
	inventories []*agentv1.Inventory
	acks        []uint64
	metrics     []*agentv1.Metrics
}

func (h *testHub) Connect(_ context.Context, stream agentv1connect.AgentHubConnectServerStream) error {
	h.mu.Lock()
	h.connects++
	number := h.connects
	h.mu.Unlock()

	for {
		msg, err := stream.Receive()
		if err != nil {
			return nil
		}

		switch {
		case msg.GetInventory() != nil:
			h.mu.Lock()
			h.inventories = append(h.inventories, msg.GetInventory())
			h.mu.Unlock()

		case msg.GetHeartbeat() != nil:
			seq := msg.GetHeartbeat().GetSeq()
			ack := &agentv1.CoreMsg{
				Cmd: &agentv1.CoreMsg_HeartbeatAck{HeartbeatAck: &agentv1.HeartbeatAck{Seq: seq}},
			}
			if err := stream.Send(ack); err != nil {
				return err
			}
			h.mu.Lock()
			h.acks = append(h.acks, seq)
			h.mu.Unlock()
			if number == 1 {
				// Close the first stream so the link has to reconnect.
				return nil
			}

		case msg.GetMetrics() != nil:
			h.mu.Lock()
			h.metrics = append(h.metrics, msg.GetMetrics())
			h.mu.Unlock()
		}
	}
}

func (h *testHub) counts() (connects, inventories, acks, metrics int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connects, len(h.inventories), len(h.acks), len(h.metrics)
}

func firstInventory(h *testHub) *agentv1.Inventory {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.inventories) == 0 {
		return nil
	}
	return h.inventories[0]
}

func firstMetrics(h *testHub) *agentv1.Metrics {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.metrics) == 0 {
		return nil
	}
	return h.metrics[0]
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within the timeout")
}

func TestLinkReportsAndReconnects(t *testing.T) {
	hub := &testHub{}
	server := connect.NewServer()
	agentv1connect.RegisterAgentHubHandler(server, hub)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)
	httpServer := httptest.NewUnstartedServer(mux)
	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	httpServer.Config.Protocols = &protocols
	httpServer.Start()
	defer httpServer.Close()

	cfg := config.Config{CoreURL: httpServer.URL, LogLevel: "info"}
	sample := &agentv1.Metrics{
		Hostname:       "vps-test",
		CpuPercent:     42,
		MemUsedBytes:   111,
		DiskUsedBytes:  222,
		DiskTotalBytes: 333,
		NetRxBytes:     444,
		NetTxBytes:     555,
	}
	agentLink := New(cfg, fakeCollector{hostname: "vps-test", metrics: sample})
	agentLink.HeartbeatInterval = 20 * time.Millisecond
	agentLink.MetricsInterval = 30 * time.Millisecond
	agentLink.BackoffBase = 10 * time.Millisecond
	agentLink.BackoffMax = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- agentLink.Run(ctx)
	}()

	// Two connections prove the link reconnected after the server closed the
	// first stream; the rest prove inventory, heartbeat acks and metrics flow.
	waitFor(t, func() bool {
		connects, inventories, acks, metrics := hub.counts()
		return connects >= 2 && inventories >= 2 && acks >= 1 && metrics >= 1
	})

	if inventory := firstInventory(hub); inventory.GetHostname() != "vps-test" {
		t.Fatalf("inventory hostname = %q, want vps-test", inventory.GetHostname())
	}
	if metrics := firstMetrics(hub); metrics.GetCpuPercent() != 42 ||
		metrics.GetMemUsedBytes() != 111 ||
		metrics.GetDiskTotalBytes() != 333 ||
		metrics.GetNetRxBytes() != 444 {
		t.Fatalf("metrics = %v, want the collected sample", metrics)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

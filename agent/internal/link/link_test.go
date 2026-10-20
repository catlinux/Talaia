package link

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"

	"github.com/catlinux/Talaia/agent/internal/config"
	"github.com/catlinux/Talaia/agent/internal/enroll"
	"github.com/catlinux/Talaia/agent/internal/jobs"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
	"github.com/catlinux/Talaia/proto/gen/go/agentv1connect"
)

// fakeCollector serves a mutable inventory and metrics.
type fakeCollector struct {
	mu        sync.Mutex
	inventory *agentv1.Inventory
	metrics   *agentv1.Metrics
}

func (f *fakeCollector) Inventory() (*agentv1.Inventory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inventory, nil
}

func (f *fakeCollector) Metrics() (*agentv1.Metrics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.metrics, nil
}

func (f *fakeCollector) setInventory(inventory *agentv1.Inventory) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inventory = inventory
}

func inventoryWith(hostname string) *agentv1.Inventory {
	return &agentv1.Inventory{
		Hostname:      hostname,
		OsName:        "test",
		Arch:          "amd64",
		AgentVersion:  "test",
		CpuCount:      2,
		MemTotalBytes: 1024,
	}
}

// testHub is a real agent stream served over h2c. When closeFirst is set it
// ends the first connection after acknowledging one heartbeat, so the link has
// to reconnect.
type testHub struct {
	closeFirst bool
	// command is sent to the agent once, after its first inventory.
	command *agentv1.CoreMsg

	mu          sync.Mutex
	connects    int
	inventories []*agentv1.Inventory
	acks        []uint64
	metrics     []*agentv1.Metrics
	jobEvents   []*agentv1.JobEvent
	sentCommand bool
	stats       []*agentv1.ContainerStats
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
			send := h.command != nil && !h.sentCommand
			h.sentCommand = h.sentCommand || send
			h.mu.Unlock()
			if send {
				if err := stream.Send(h.command); err != nil {
					return err
				}
			}

		case msg.GetContainerStats() != nil:
			h.mu.Lock()
			h.stats = append(h.stats, msg.GetContainerStats())
			h.mu.Unlock()

		case msg.GetJobEvent() != nil:
			h.mu.Lock()
			h.jobEvents = append(h.jobEvents, msg.GetJobEvent())
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
			closeFirst := h.closeFirst
			h.mu.Unlock()
			if number == 1 && closeFirst {
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

func (h *testHub) inventoryCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.inventories)
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

// newTestServer serves the hub over h2c, like the insecure dev core does.
func newTestServer(t *testing.T, hub *testHub) string {
	t.Helper()

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
	t.Cleanup(httpServer.Close)

	return httpServer.URL
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
	hub := &testHub{closeFirst: true}
	url := newTestServer(t, hub)

	collector := &fakeCollector{
		inventory: inventoryWith("vps-test"),
		metrics: &agentv1.Metrics{
			Hostname:       "vps-test",
			CpuPercent:     42,
			MemUsedBytes:   111,
			DiskUsedBytes:  222,
			DiskTotalBytes: 333,
			NetRxBytes:     444,
			NetTxBytes:     555,
		},
	}

	agentLink := NewInsecure(config.Config{CoreURL: url, LogLevel: "info"}, collector)
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

func TestLinkResendsInventoryOnlyWhenItChanges(t *testing.T) {
	hub := &testHub{}
	url := newTestServer(t, hub)

	collector := &fakeCollector{
		inventory: inventoryWith("vps-test"),
		metrics:   &agentv1.Metrics{Hostname: "vps-test"},
	}

	agentLink := NewInsecure(config.Config{CoreURL: url, LogLevel: "info"}, collector)
	agentLink.HeartbeatInterval = time.Hour
	agentLink.MetricsInterval = time.Hour
	agentLink.InventoryInterval = 20 * time.Millisecond
	agentLink.BackoffBase = 10 * time.Millisecond
	agentLink.BackoffMax = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- agentLink.Run(ctx)
	}()

	// The first inventory is sent on connect.
	waitFor(t, func() bool { return hub.inventoryCount() == 1 })

	// An inventory that does not change is not sent again.
	time.Sleep(100 * time.Millisecond)
	if got := hub.inventoryCount(); got != 1 {
		t.Fatalf("inventory sent %d times, want 1 while it does not change", got)
	}

	// A changed inventory is sent again.
	changed := inventoryWith("vps-test")
	changed.DockerAvailable = true
	changed.DockerVersion = "27.1.1"
	changed.Containers = []*agentv1.Container{{Id: "abcdefghijkl", Name: "web", State: "running"}}
	collector.setInventory(changed)

	waitFor(t, func() bool { return hub.inventoryCount() == 2 })

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

func TestInventoryUnchangedIgnoresContainerStatus(t *testing.T) {
	base := func(status string, state string) *agentv1.Inventory {
		return &agentv1.Inventory{
			Hostname:   "vps-test",
			Containers: []*agentv1.Container{{Id: "abc", Name: "web", State: state, Status: status}},
		}
	}

	if !inventoryUnchanged(base("Up 5 minutes", "running"), base("Up 6 minutes", "running")) {
		t.Error("a changed status text must not count as an inventory change")
	}
	if inventoryUnchanged(base("Up 5 minutes", "running"), base("Exited (0) 1 second ago", "exited")) {
		t.Error("a changed container state must count as an inventory change")
	}
}

// testCA is a throwaway CA for the TLS tests.
type testCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
	serial  atomic.Int64
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test ca"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create ca certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse ca certificate: %v", err)
	}

	return &testCA{
		cert:    cert,
		key:     key,
		certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

func (c *testCA) issue(t *testing.T, commonName string, publicKey any, usage x509.ExtKeyUsage, ips ...net.IP) []byte {
	t.Helper()

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(c.serial.Add(1)),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, c.cert, publicKey, c.key)
	if err != nil {
		t.Fatalf("issue certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func (c *testCA) pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(c.cert)
	return pool
}

// issueServer issues the certificate of the test core.
func (c *testCA) issueServer(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	certPEM := c.issue(t, "test core", &key.PublicKey, x509.ExtKeyUsageServerAuth, net.ParseIP("127.0.0.1"))

	block, _ := pem.Decode(certPEM)
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse server certificate: %v", err)
	}

	return tls.Certificate{Certificate: [][]byte{leaf.Raw}, PrivateKey: key, Leaf: leaf}
}

// issueClient issues the client certificate and the key of an agent.
func (c *testCA) issueClient(t *testing.T, serverID string) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	certPEM = c.issue(t, serverID, &key.PublicKey, x509.ExtKeyUsageClientAuth)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal client key: %v", err)
	}

	return certPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

// newTestIdentity writes the identity files and loads them.
func newTestIdentity(t *testing.T, ca *testCA, serverID string) *enroll.Identity {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "certs")
	certPEM, keyPEM := ca.issueClient(t, serverID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("create cert dir: %v", err)
	}

	files := map[string]struct {
		data []byte
		mode os.FileMode
	}{
		"agent.key": {keyPEM, 0o600},
		"agent.crt": {certPEM, 0o644},
		"ca.crt":    {ca.certPEM, 0o644},
	}
	for name, file := range files {
		if err := os.WriteFile(filepath.Join(dir, name), file.data, file.mode); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	identity, err := enroll.LoadIdentity(dir)
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	return identity
}

// newTLSTestServer serves the hub over TLS and requires a client certificate.
func newTLSTestServer(t *testing.T, ca *testCA, hub *testHub) *httptest.Server {
	t.Helper()

	server := connect.NewServer()
	agentv1connect.RegisterAgentHubHandler(server, hub)
	mux := http.NewServeMux()
	connecthttp.Mount(mux, server)

	httpServer := httptest.NewUnstartedServer(mux)
	httpServer.TLS = &tls.Config{
		MinVersion:   tls.VersionTLS13,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    ca.pool(),
		Certificates: []tls.Certificate{ca.issueServer(t)},
		NextProtos:   []string{"h2", "http/1.1"},
	}
	httpServer.StartTLS()
	t.Cleanup(httpServer.Close)

	return httpServer
}

func TestLinkConnectsWithMTLS(t *testing.T) {
	ca := newTestCA(t)
	hub := &testHub{}
	server := newTLSTestServer(t, ca, hub)
	identity := newTestIdentity(t, ca, "server-1")

	collector := &fakeCollector{
		inventory: inventoryWith("vps-web-01"),
		metrics:   &agentv1.Metrics{Hostname: "vps-web-01"},
	}

	agentLink := New(config.Config{CoreURL: server.URL, LogLevel: "info"}, collector, identity)
	agentLink.HeartbeatInterval = 20 * time.Millisecond
	agentLink.MetricsInterval = 30 * time.Millisecond
	agentLink.RenewalInterval = time.Hour
	agentLink.BackoffBase = 10 * time.Millisecond
	agentLink.BackoffMax = 50 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- agentLink.Run(ctx)
	}()

	waitFor(t, func() bool {
		_, inventories, acks, _ := hub.counts()
		return inventories >= 1 && acks >= 1
	})

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

func TestLinkRejectsAServerFromAnotherCA(t *testing.T) {
	clientCA := newTestCA(t)
	identity := newTestIdentity(t, clientCA, "server-1")

	otherCA := newTestCA(t)
	server := newTLSTestServer(t, otherCA, &testHub{})

	collector := &fakeCollector{inventory: inventoryWith("vps-web-01"), metrics: &agentv1.Metrics{}}
	agentLink := New(config.Config{CoreURL: server.URL, LogLevel: "info"}, collector, identity)
	agentLink.HeartbeatInterval = time.Hour
	agentLink.MetricsInterval = time.Hour
	agentLink.RenewalInterval = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := agentLink.session(ctx); err == nil {
		t.Fatal("the link must reject a core whose certificate comes from another CA")
	}
}

// recordingActions answers every container action with err and remembers them.
type recordingActions struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (r *recordingActions) Do(_ context.Context, container string, action agentv1.ContainerActionType) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, action.String()+":"+container)
	return r.err
}

func (r *recordingActions) Logs(context.Context, string, int) (string, error) { return "", r.err }

func TestLinkRunsACommandAndReportsTheJob(t *testing.T) {
	hub := &testHub{command: &agentv1.CoreMsg{
		JobId: "job-42",
		Cmd: &agentv1.CoreMsg_ContainerAction{ContainerAction: &agentv1.ContainerAction{
			Container: "web",
			Action:    agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_RESTART,
		}},
	}}
	url := newTestServer(t, hub)

	actions := &recordingActions{}
	agentLink := NewInsecure(config.Config{CoreURL: url, LogLevel: "info"}, &fakeCollector{
		inventory: inventoryWith("vps-test"),
		metrics:   &agentv1.Metrics{Hostname: "vps-test"},
	})
	agentLink.jobs = jobs.New(actions)
	agentLink.HeartbeatInterval = time.Hour
	agentLink.MetricsInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = agentLink.Run(ctx) }()

	waitFor(t, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		return len(hub.jobEvents) >= 2
	})

	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.jobEvents[0].GetState() != agentv1.JobState_JOB_STATE_RUNNING ||
		hub.jobEvents[1].GetState() != agentv1.JobState_JOB_STATE_SUCCEEDED ||
		hub.jobEvents[1].GetJobId() != "job-42" {
		t.Errorf("job events = %v", hub.jobEvents)
	}
	actions.mu.Lock()
	defer actions.mu.Unlock()
	if len(actions.calls) != 1 || actions.calls[0] != "CONTAINER_ACTION_TYPE_RESTART:web" {
		t.Errorf("actions = %v", actions.calls)
	}
}

// changingActions changes the inventory the collector reports, as a real action
// changes the containers.
type changingActions struct {
	collector *fakeCollector
}

func (c changingActions) Do(context.Context, string, agentv1.ContainerActionType) error {
	changed := inventoryWith("vps-test")
	changed.Containers = []*agentv1.Container{{Id: "abcdefghijkl", Name: "web", State: "exited"}}
	c.collector.setInventory(changed)
	return nil
}

func (c changingActions) Logs(context.Context, string, int) (string, error) { return "", nil }

func TestLinkResendsTheInventoryRightAfterAJob(t *testing.T) {
	hub := &testHub{command: &agentv1.CoreMsg{
		JobId: "job-7",
		Cmd: &agentv1.CoreMsg_ContainerAction{ContainerAction: &agentv1.ContainerAction{
			Container: "web",
			Action:    agentv1.ContainerActionType_CONTAINER_ACTION_TYPE_STOP,
		}},
	}}
	url := newTestServer(t, hub)

	collector := &fakeCollector{inventory: inventoryWith("vps-test"), metrics: &agentv1.Metrics{Hostname: "vps-test"}}
	agentLink := NewInsecure(config.Config{CoreURL: url, LogLevel: "info"}, collector)
	agentLink.jobs = jobs.New(changingActions{collector: collector})
	// The periodic tick is far away: only the end of the job can trigger the resend.
	agentLink.HeartbeatInterval = time.Hour
	agentLink.MetricsInterval = time.Hour
	agentLink.InventoryInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = agentLink.Run(ctx) }()

	waitFor(t, func() bool { return hub.inventoryCount() >= 2 })
}

// fixedStats reports the same sample every time.
type fixedStats struct {
	mu    sync.Mutex
	calls int
}

func (f *fixedStats) Stats(context.Context) *agentv1.ContainerStats {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return &agentv1.ContainerStats{Items: []*agentv1.ContainerStat{{Id: "abcdefghijkl", CpuPercent: 12.5, MemUsedBytes: 1024}}}
}

func TestLinkSendsContainerStatsOnTheirOwnTimer(t *testing.T) {
	hub := &testHub{}
	url := newTestServer(t, hub)
	source := &fixedStats{}

	agentLink := NewInsecure(config.Config{CoreURL: url, LogLevel: "info"}, &fakeCollector{
		inventory: inventoryWith("vps-test"),
		metrics:   &agentv1.Metrics{Hostname: "vps-test"},
	})
	agentLink.stats = source
	agentLink.HeartbeatInterval = time.Hour
	agentLink.MetricsInterval = time.Hour
	agentLink.InventoryInterval = time.Hour
	agentLink.StatsInterval = 20 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = agentLink.Run(ctx) }()

	waitFor(t, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		return len(hub.stats) >= 2
	})

	hub.mu.Lock()
	defer hub.mu.Unlock()
	if got := hub.stats[0].GetItems(); len(got) != 1 || got[0].GetId() != "abcdefghijkl" || got[0].GetCpuPercent() != 12.5 {
		t.Errorf("stats = %v", hub.stats[0])
	}
}

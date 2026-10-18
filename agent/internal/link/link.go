// Package link keeps the outbound connection from talaia-agent to talaia-core:
// it sends the inventory, then heartbeats and metrics, and reconnects with
// exponential backoff and jitter when the stream drops.
package link

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"golang.org/x/net/http2"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/catlinux/Talaia/agent/internal/collect"
	"github.com/catlinux/Talaia/agent/internal/config"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
	"github.com/catlinux/Talaia/proto/gen/go/agentv1connect"
)

// streamResetAfter is the stream lifetime after which the backoff delay is
// reset to its base value.
const streamResetAfter = 30 * time.Second

// Link maintains one outbound stream to the core.
type Link struct {
	cfg       config.Config
	collector collect.Collector
	client    agentv1connect.AgentHubClient

	// Intervals and backoff bounds are fields so tests can use milliseconds.
	HeartbeatInterval time.Duration
	MetricsInterval   time.Duration
	BackoffBase       time.Duration
	BackoffMax        time.Duration

	now func() time.Time
}

// New returns a Link that sends heartbeats every 10 s and metrics every 15 s.
func New(cfg config.Config, collector collect.Collector) *Link {
	// Phase 0 has no TLS: the stream runs over plaintext HTTP/2 (h2c). Phase 1
	// replaces this with an mTLS client.
	httpClient := &http.Client{Transport: newH2CTransport()}
	transport := connecthttp.NewTransport(httpClient, cfg.CoreURL)

	return &Link{
		cfg:               cfg,
		collector:         collector,
		client:            agentv1connect.NewAgentHubClient(connect.NewClient(transport)),
		HeartbeatInterval: 10 * time.Second,
		MetricsInterval:   15 * time.Second,
		BackoffBase:       time.Second,
		BackoffMax:        60 * time.Second,
		now:               time.Now,
	}
}

// Run connects with the default settings and blocks until ctx is cancelled.
func Run(ctx context.Context, cfg config.Config, collector collect.Collector) error {
	return New(cfg, collector).Run(ctx)
}

// Run keeps the stream alive, reconnecting with backoff, until ctx is
// cancelled.
func (l *Link) Run(ctx context.Context) error {
	backoff := l.BackoffBase

	for {
		started := l.now()
		err := l.session(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			slog.Warn("core link lost", "error", err)
		}
		if l.now().Sub(started) >= streamResetAfter {
			backoff = l.BackoffBase
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(jitter(backoff)):
		}

		backoff = min(backoff*2, l.BackoffMax)
	}
}

// session runs a single stream until it fails or ctx is cancelled.
func (l *Link) session(ctx context.Context) error {
	stream, err := l.client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("connect to core: %w", err)
	}
	defer stream.Close()

	inventory, err := l.collector.Inventory()
	if err != nil {
		return fmt.Errorf("collect inventory: %w", err)
	}
	if err := stream.Send(inventoryMsg(inventory)); err != nil {
		return fmt.Errorf("send inventory: %w", err)
	}
	slog.Info("connected to core", "core_url", l.cfg.CoreURL, "hostname", inventory.GetHostname())

	received := make(chan error, 1)
	go func() {
		received <- l.receive(stream)
	}()

	heartbeat := time.NewTicker(l.HeartbeatInterval)
	defer heartbeat.Stop()
	metrics := time.NewTicker(l.MetricsInterval)
	defer metrics.Stop()

	var seq uint64
	for {
		select {
		case <-ctx.Done():
			return nil

		case err := <-received:
			if err == nil || errors.Is(err, io.EOF) {
				return errors.New("core closed the stream")
			}
			return fmt.Errorf("receive from core: %w", err)

		case <-heartbeat.C:
			seq++
			heartbeatMsg := &agentv1.AgentMsg{
				Msg: &agentv1.AgentMsg_Heartbeat{
					Heartbeat: &agentv1.Heartbeat{Seq: seq, SentAt: timestamppb.Now()},
				},
			}
			if err := stream.Send(heartbeatMsg); err != nil {
				return fmt.Errorf("send heartbeat: %w", err)
			}
			slog.Debug("heartbeat sent", "seq", seq)

		case <-metrics.C:
			percent, err := l.collector.CPUPercent()
			if err != nil {
				slog.Warn("collect cpu percent", "error", err)
				continue
			}
			metricsMsg := &agentv1.AgentMsg{
				Msg: &agentv1.AgentMsg_Metrics{
					Metrics: &agentv1.Metrics{
						SampledAt:  timestamppb.Now(),
						Hostname:   inventory.GetHostname(),
						CpuPercent: percent,
					},
				},
			}
			if err := stream.Send(metricsMsg); err != nil {
				return fmt.Errorf("send metrics: %w", err)
			}
			slog.Debug("metrics sent", "cpu_percent", percent)
		}
	}
}

// receive reads the core messages until the stream ends.
func (l *Link) receive(stream agentv1connect.AgentHubConnectClientStream) error {
	for {
		msg, err := stream.Receive()
		if err != nil {
			return err
		}
		if ack := msg.GetHeartbeatAck(); ack != nil {
			slog.Debug("heartbeat acknowledged", "seq", ack.GetSeq())
		}
	}
}

func inventoryMsg(inventory *agentv1.Inventory) *agentv1.AgentMsg {
	return &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_Inventory{Inventory: inventory}}
}

// newH2CTransport speaks HTTP/2 without TLS (h2c). It is temporary: phase 1
// replaces it with mTLS.
func newH2CTransport() *http2.Transport {
	return &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, address string, _ *tls.Config) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, address)
		},
	}
}

// jitter returns a delay between half of backoff and backoff, to avoid
// reconnecting every agent at the same instant.
func jitter(backoff time.Duration) time.Duration {
	if backoff <= 0 {
		return 0
	}
	half := int64(backoff / 2)
	return time.Duration(half + rand.Int64N(half+1))
}

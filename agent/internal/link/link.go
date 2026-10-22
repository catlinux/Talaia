// Package link keeps the outbound connection from talaia-agent to talaia-core:
// it sends the inventory, then heartbeats and metrics, and reconnects with
// exponential backoff and jitter when the stream drops.
package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"connectrpc.com/connect/v2"
	"connectrpc.com/connect/v2/connecthttp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/catlinux/Talaia/agent/internal/apt"
	"github.com/catlinux/Talaia/agent/internal/collect"
	"github.com/catlinux/Talaia/agent/internal/config"
	"github.com/catlinux/Talaia/agent/internal/enroll"
	"github.com/catlinux/Talaia/agent/internal/inventory"
	"github.com/catlinux/Talaia/agent/internal/jobs"
	"github.com/catlinux/Talaia/agent/internal/stacks"
	"github.com/catlinux/Talaia/agent/internal/terminal"
	"github.com/catlinux/Talaia/agent/internal/update"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
	"github.com/catlinux/Talaia/proto/gen/go/agentv1connect"
)

// streamResetAfter is the stream lifetime after which the backoff delay is
// reset to its base value.
const streamResetAfter = 30 * time.Second

// ContainerStatsSource samples the use of the running containers. It returns nil
// when there is nothing to report, for example without Docker.
type ContainerStatsSource interface {
	Stats(ctx context.Context) *agentv1.ContainerStats
}

// Link maintains one outbound stream to the core.
type Link struct {
	cfg       config.Config
	collector collect.Collector
	identity  *enroll.Identity
	client    agentv1connect.AgentHubClient
	// jobs runs the typed commands the core sends.
	jobs *jobs.Executor
	// apt keeps the pending package updates used by the inventory and the job.
	apt *apt.Checker
	// terminals runs the interactive shells; its sessions end with each stream.
	terminals *terminal.Manager
	// stats samples the running containers; nil leaves it out.
	stats ContainerStatsSource

	// Intervals and backoff bounds are fields so tests can use milliseconds.
	HeartbeatInterval time.Duration
	MetricsInterval   time.Duration
	InventoryInterval time.Duration
	// StatsInterval is how often the use of the containers is sampled and sent.
	StatsInterval   time.Duration
	RenewalInterval time.Duration
	BackoffBase     time.Duration
	BackoffMax      time.Duration

	now func() time.Time
}

// New returns a Link that reaches the core over TLS with mutual authentication,
// using the identity of the agent.
func New(
	cfg config.Config,
	collector collect.Collector,
	identity *enroll.Identity,
	checkers ...*apt.Checker,
) *Link {
	var checker *apt.Checker
	if len(checkers) > 0 {
		checker = checkers[0]
	}
	if identity == nil {
		return NewInsecure(cfg, collector, checker)
	}
	return newLink(cfg, collector, identity, newTLSTransport(identity, coreHost(cfg.CoreURL)), checker)
}

// NewInsecure returns a Link for insecure development mode: plaintext HTTP/2
// and no certificate.
func NewInsecure(cfg config.Config, collector collect.Collector, checkers ...*apt.Checker) *Link {
	var checker *apt.Checker
	if len(checkers) > 0 {
		checker = checkers[0]
	}
	return newLink(cfg, collector, nil, newH2CTransport(), checker)
}

func newLink(
	cfg config.Config,
	collector collect.Collector,
	identity *enroll.Identity,
	transport *http.Transport,
	checker *apt.Checker,
) *Link {
	httpClient := &http.Client{Transport: transport}
	rpcTransport := connecthttp.NewTransport(httpClient, cfg.CoreURL)

	return &Link{
		cfg:               cfg,
		collector:         collector,
		identity:          identity,
		client:            agentv1connect.NewAgentHubClient(connect.NewClient(rpcTransport)),
		jobs:              newExecutor(cfg, identity, checker),
		apt:               checker,
		terminals:         terminal.New(terminal.NewDockerBackend(), terminal.DefaultLimits()),
		HeartbeatInterval: 10 * time.Second,
		MetricsInterval:   15 * time.Second,
		InventoryInterval: 60 * time.Second,
		StatsInterval:     15 * time.Second,
		stats:             inventory.NewDocker(),
		RenewalInterval:   time.Hour,
		BackoffBase:       time.Second,
		BackoffMax:        60 * time.Second,
		now:               time.Now,
	}
}

// Run connects with the default settings and blocks until ctx is cancelled.
func Run(
	ctx context.Context,
	cfg config.Config,
	collector collect.Collector,
	identity *enroll.Identity,
	checkers ...*apt.Checker,
) error {
	return New(cfg, collector, identity, checkers...).Run(ctx)
}

// Run keeps the stream alive, reconnecting with backoff, until ctx is
// cancelled. It also renews the certificate in the background.
func (l *Link) Run(ctx context.Context) error {
	go l.renewLoop(ctx)
	if l.apt != nil {
		go l.apt.Run(ctx)
	}

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
	defer func() { _ = stream.Close() }()

	inventory, err := l.collector.Inventory()
	if err != nil {
		return fmt.Errorf("collect inventory: %w", err)
	}
	applyAptSummary(inventory, l.apt)
	if err := stream.Send(inventoryMsg(inventory)); err != nil {
		return fmt.Errorf("send inventory: %w", err)
	}
	sentInventory := inventory
	slog.Info("connected to core", "core_url", l.cfg.CoreURL, "hostname", inventory.GetHostname())

	// Job events are queued here and sent from this loop, the only place that
	// writes to the stream.
	// A job that outlives the stream drops its events instead of blocking.
	streamCtx, endStream := context.WithCancel(ctx)
	defer endStream()
	events := make(chan *agentv1.AgentMsg, 32)
	emit := func(msg *agentv1.AgentMsg) {
		select {
		case events <- msg:
		case <-streamCtx.Done():
		}
	}

	received := make(chan error, 1)
	go func() {
		received <- l.receive(ctx, stream, emit)
	}()

	heartbeat := time.NewTicker(l.HeartbeatInterval)
	defer heartbeat.Stop()
	metrics := time.NewTicker(l.MetricsInterval)
	defer metrics.Stop()
	inventoryTicker := time.NewTicker(l.InventoryInterval)
	defer inventoryTicker.Stop()
	statsTicker := time.NewTicker(l.StatsInterval)
	defer statsTicker.Stop()
	// Sampling takes seconds, so it runs on its own and never overlaps itself.
	var sampling atomic.Bool

	// sendInventoryIfChanged collects the inventory and sends it when it differs
	// from the last one. A failed collection is logged and retried later.
	sendInventoryIfChanged := func() error {
		current, err := l.collector.Inventory()
		if err != nil {
			slog.Warn("collect inventory", "error", err)
			return nil
		}
		applyAptSummary(current, l.apt)
		if inventoryUnchanged(current, sentInventory) {
			return nil
		}
		if err := stream.Send(inventoryMsg(current)); err != nil {
			return fmt.Errorf("send inventory: %w", err)
		}
		sentInventory = current
		slog.Debug("inventory changed, sent")
		return nil
	}

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

		case event := <-events:
			if err := stream.Send(event); err != nil {
				return fmt.Errorf("send job event: %w", err)
			}
			// A finished job probably changed the containers: tell the core now
			// instead of at the next tick.
			if jobFinished(event) {
				if err := sendInventoryIfChanged(); err != nil {
					return err
				}
			}

		case <-statsTicker.C:
			if l.stats != nil && sampling.CompareAndSwap(false, true) {
				go func() {
					defer sampling.Store(false)
					if sample := l.stats.Stats(streamCtx); sample != nil {
						emit(&agentv1.AgentMsg{Msg: &agentv1.AgentMsg_ContainerStats{ContainerStats: sample}})
					}
				}()
			}

		case <-inventoryTicker.C:
			if err := sendInventoryIfChanged(); err != nil {
				return err
			}

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
			sample, err := l.collector.Metrics()
			if err != nil {
				slog.Warn("collect metrics", "error", err)
				continue
			}
			metricsMsg := &agentv1.AgentMsg{
				Msg: &agentv1.AgentMsg_Metrics{Metrics: sample},
			}
			if err := stream.Send(metricsMsg); err != nil {
				return fmt.Errorf("send metrics: %w", err)
			}
			slog.Debug("metrics sent", "cpu_percent", sample.GetCpuPercent())
		}
	}
}

// jobFinished reports whether msg is the final event of a job.
func jobFinished(msg *agentv1.AgentMsg) bool {
	state := msg.GetJobEvent().GetState()
	return state == agentv1.JobState_JOB_STATE_SUCCEEDED || state == agentv1.JobState_JOB_STATE_FAILED
}

// receive reads the core messages until the stream ends.
func (l *Link) receive(ctx context.Context, stream agentv1connect.AgentHubConnectClientStream, emit func(*agentv1.AgentMsg)) error {
	// A session cannot outlive the stream that opened it: the core would no
	// longer know it.
	if l.terminals != nil {
		defer l.terminals.CloseAll()
	}
	for {
		msg, err := stream.Receive()
		if err != nil {
			return err
		}
		if ack := msg.GetHeartbeatAck(); ack != nil {
			slog.Debug("heartbeat acknowledged", "seq", ack.GetSeq())
			continue
		}
		if l.terminals != nil && l.terminals.Handle(ctx, msg, emit) {
			continue
		}
		l.jobs.Handle(ctx, msg, emit)
	}
}

// inventoryUnchanged compares two inventories ignoring the human-readable
// container status ("Up 5 minutes"), which changes every minute and would
// otherwise make the agent resend an identical inventory on every tick.
func inventoryUnchanged(a, b *agentv1.Inventory) bool {
	return proto.Equal(withoutContainerStatus(a), withoutContainerStatus(b))
}

func withoutContainerStatus(inventory *agentv1.Inventory) *agentv1.Inventory {
	if inventory == nil {
		return nil
	}
	clone, ok := proto.Clone(inventory).(*agentv1.Inventory)
	if !ok {
		return inventory
	}
	for _, container := range clone.GetContainers() {
		container.Status = ""
	}
	return clone
}

func inventoryMsg(inventory *agentv1.Inventory) *agentv1.AgentMsg {
	return &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_Inventory{Inventory: inventory}}
}

// applyAptSummary copies the last apt snapshot into the inventory message.
func applyAptSummary(inventory *agentv1.Inventory, checker *apt.Checker) {
	if inventory == nil || checker == nil {
		return
	}
	summary := checker.Summary()
	if summary == nil {
		return
	}
	aptSummary := &agentv1.AptSummary{
		Upgradable:     uint32(summary.Upgradable),
		Security:       uint32(summary.Security),
		RebootRequired: summary.RebootRequired,
	}
	if !summary.CheckedAt.IsZero() {
		aptSummary.CheckedAt = timestamppb.New(summary.CheckedAt)
	}
	if !summary.ListsUpdatedAt.IsZero() {
		aptSummary.ListsUpdatedAt = timestamppb.New(summary.ListsUpdatedAt)
	}
	inventory.Apt = aptSummary
}

// newH2CTransport speaks HTTP/2 without TLS (h2c). It is only for insecure
// development mode.
func newH2CTransport() *http.Transport {
	var protocols http.Protocols
	protocols.SetUnencryptedHTTP2(true)
	return &http.Transport{Protocols: &protocols}
}

// newTLSTransport speaks HTTP/2 over TLS with the identity of the agent.
func newTLSTransport(identity *enroll.Identity, serverName string) *http.Transport {
	return &http.Transport{
		TLSClientConfig:   identity.TLSConfig(serverName),
		ForceAttemptHTTP2: true,
	}
}

// coreHost returns the hostname of the core URL, which is the TLS server name.
func coreHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// renewLoop renews the certificate when less than a third of its lifetime
// remains. A failure is logged and retried on the next tick; the stream is not
// dropped.
func (l *Link) renewLoop(ctx context.Context) {
	if l.identity == nil {
		return
	}

	ticker := time.NewTicker(l.RenewalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !l.identity.NeedsRenewal(l.now()) {
				continue
			}
			if err := enroll.Renew(ctx, l.identity, l.cfg.CoreURL); err != nil {
				slog.Warn("certificate renewal failed", "error", err)
				continue
			}
			slog.Info("certificate renewed", "server_id", l.identity.ServerID(), "not_after", l.identity.NotAfter())
		}
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

// newExecutor builds the executor of a real agent: container actions, stacks,
// Docker resources and agent updates, all on the local Docker.
func newExecutor(cfg config.Config, identity *enroll.Identity, checker *apt.Checker) *jobs.Executor {
	docker := jobs.NewDockerActions()
	return jobs.New(docker).
		WithStacks(stacks.NewDefault().WithProjects(inventory.NewDocker())).
		WithResources(docker).
		WithUpdates(update.New(update.Options{CoreURL: cfg.CoreURL, Identity: identity})).
		WithApt(checker)
}

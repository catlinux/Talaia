package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/catlinux/Talaia/core/internal/hub"
	"github.com/catlinux/Talaia/core/internal/store"
	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

type fakeServerLister []store.Server

func (f fakeServerLister) ListServers(context.Context) ([]store.Server, error) { return f, nil }

type fakeConnected []hub.AgentInfo

func (f fakeConnected) Connected() []hub.AgentInfo { return f }

func TestHubSourceMergesEnrolledAndConnectedServers(t *testing.T) {
	lastSeen := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	newer := lastSeen.Add(time.Minute)
	servers := fakeServerLister{
		{ID: "live", GroupID: "group-a", LastSeen: lastSeen},
		{ID: "offline", LastSeen: lastSeen},
		{ID: "revoked", RevokedAt: lastSeen},
		{ID: "removed", RemovedAt: lastSeen},
	}
	agents := fakeConnected{{
		ServerID: "live", LastSeen: newer, MetricsAt: newer, LastCPUPercent: 12.5,
		MemUsedBytes: 20, MemTotalBytes: 100, DiskUsedBytes: 30, DiskTotalBytes: 100,
		ContainerList: []*agentv1.Container{{Name: "web", State: "running", Health: "healthy", ComposeProject: "shop"}},
	}}
	states, err := NewHubSource(agents, servers).Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 3 {
		t.Fatalf("states=%d, want three non-removed servers", len(states))
	}
	got := map[string]ServerState{}
	for _, state := range states {
		got[state.ID] = state
	}
	live := got["live"]
	if !live.Connected || !live.HasMetrics || live.LastSeen != newer || live.CPUPercent != 12.5 || live.MemUsed != 20 || live.DiskTotal != 100 {
		t.Fatalf("live state=%+v", live)
	}
	if len(live.Containers) != 1 || live.Containers[0] != (ContainerState{Name: "web", State: "running", Health: "healthy", Stack: "shop"}) {
		t.Fatalf("live containers=%+v", live.Containers)
	}
	if got["offline"].Connected || got["offline"].LastSeen != lastSeen {
		t.Fatalf("offline state=%+v", got["offline"])
	}
	if !got["revoked"].Revoked {
		t.Fatalf("revoked state=%+v", got["revoked"])
	}
	if _, ok := got["removed"]; ok {
		t.Fatal("removed server was included")
	}
}

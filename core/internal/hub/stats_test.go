package hub

import (
	"testing"

	agentv1 "github.com/catlinux/Talaia/proto/gen/go"
)

func TestContainerStatsAreKeptPerContainer(t *testing.T) {
	h := New(newFakeServerStore(enrolledServer("server-1", "aa")))
	stream, _ := startStreamedAgent(t, h, "server-1", "aa")

	stream.in <- &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_ContainerStats{ContainerStats: &agentv1.ContainerStats{
		Items: []*agentv1.ContainerStat{
			{Id: "aaaaaaaaaaaa", CpuPercent: 12.5, MemUsedBytes: 100},
			{Id: "bbbbbbbbbbbb", CpuPercent: 1, MemUsedBytes: 200},
		},
	}}}

	waitFor(t, func() bool {
		agents := h.Connected()
		return len(agents) == 1 && len(agents[0].ContainerStats) == 2
	})

	agent := h.Connected()[0]
	if got := agent.ContainerStats["aaaaaaaaaaaa"]; got.GetCpuPercent() != 12.5 || got.GetMemUsedBytes() != 100 {
		t.Errorf("stats of the first container = %v", got)
	}
	if agent.StatsAt.IsZero() {
		t.Error("the time of the sample was not recorded")
	}

	// A new sample replaces the previous one, so a container that stopped is gone.
	stream.in <- &agentv1.AgentMsg{Msg: &agentv1.AgentMsg_ContainerStats{ContainerStats: &agentv1.ContainerStats{
		Items: []*agentv1.ContainerStat{{Id: "aaaaaaaaaaaa", CpuPercent: 3}},
	}}}
	waitFor(t, func() bool {
		agents := h.Connected()
		return len(agents) == 1 && len(agents[0].ContainerStats) == 1
	})
}

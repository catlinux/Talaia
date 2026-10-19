package hub

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"connectrpc.com/connect/v2"

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
			Inventory: &agentv1.Inventory{Hostname: hostname, AgentVersion: "1.2.3"},
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

func TestServeRejectsFirstMessageThatIsNotInventory(t *testing.T) {
	h := New()
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
	h := New()
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

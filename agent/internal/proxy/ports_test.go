package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakePublished struct {
	ports map[uint16]struct{}
	err   error
}

func (f fakePublished) Published(context.Context) (map[uint16]struct{}, error) {
	return f.ports, f.err
}

func TestPickSkipsListeningSocketsAndDockerPorts(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:20000")
	if err != nil {
		t.Fatalf("listen on 20000: %v", err)
	}
	defer func() { _ = listener.Close() }()

	picker := NewPicker(fakePublished{ports: map[uint16]struct{}{20001: {}}})
	port, err := picker.Pick(context.Background())
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if port.Host != ExternalHost || port.Number != 20002 {
		t.Fatalf("port = %+v, want 127.0.0.1:20002", port)
	}
	if port.Address() != "127.0.0.1:20002" {
		t.Errorf("address = %q", port.Address())
	}
}

func TestPickAlwaysProbesLoopback(t *testing.T) {
	var addresses []string
	picker := NewPicker(nil)
	picker.listen = func(network, address string) (net.Listener, error) {
		if network != "tcp4" {
			t.Errorf("network = %q, want tcp4", network)
		}
		addresses = append(addresses, address)
		return net.Listen(network, address)
	}

	port, err := picker.Pick(context.Background())
	if err != nil {
		t.Fatalf("Pick: %v", err)
	}
	if port.Number != PortRangeStart {
		t.Errorf("port = %d, want %d", port.Number, PortRangeStart)
	}
	if len(addresses) == 0 {
		t.Fatal("Pick did not probe a port")
	}
	for _, address := range addresses {
		if !strings.HasPrefix(address, "127.0.0.1:") {
			t.Errorf("probed %q, want a 127.0.0.1 address", address)
		}
	}
}

func TestPickFailsWhenDockerPortsCannotBeRead(t *testing.T) {
	picker := NewPicker(fakePublished{err: errors.New("no docker")})
	if _, err := picker.Pick(context.Background()); err == nil {
		t.Fatal("Pick accepted an unreadable Docker port list")
	}
}

func TestPickFailsWhenThereIsNoFreePort(t *testing.T) {
	picker := NewPicker(nil)
	picker.listen = func(string, string) (net.Listener, error) {
		return nil, errors.New("busy")
	}
	if _, err := picker.Pick(context.Background()); err == nil {
		t.Fatal("Pick found a port although every probe failed")
	}
}

func TestPickStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	picker := NewPicker(nil)
	if _, err := picker.Pick(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestDockerPortsReadsPublishedTCPPorts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" || r.URL.Query().Get("all") != "1" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = w.Write([]byte(`[
			{"Ports":[{"PublicPort":20001,"Type":"tcp"},{"PublicPort":20002,"Type":"tcp"}]},
			{"Ports":[{"PublicPort":20003,"Type":"udp"},{"PublicPort":0,"Type":"tcp"}]}
		]`))
	}))
	defer server.Close()

	ports, err := NewDockerPortsWithClient(server.URL, server.Client()).Published(context.Background())
	if err != nil {
		t.Fatalf("Published: %v", err)
	}
	for _, port := range []uint16{20001, 20002} {
		if _, ok := ports[port]; !ok {
			t.Errorf("port %d is missing", port)
		}
	}
	if _, ok := ports[20003]; ok {
		t.Error("a UDP port was included")
	}
}

func TestSystemPortsFree(t *testing.T) {
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	if (SystemPorts{}).Free(port) {
		t.Errorf("port %d is reported free while it is in use", port)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if !(SystemPorts{}).Free(port) {
		t.Errorf("port %d is not free after closing it", port)
	}
}

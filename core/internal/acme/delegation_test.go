package acme

import (
	"context"
	"crypto/tls"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/challenge/dns01"

	"github.com/catlinux/Talaia/core/internal/acmedns"
)

func TestStorageDirSeparatesDirectories(t *testing.T) {
	parse := func(raw string) *url.URL {
		t.Helper()
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	production := storageDir("/data", parse("https://acme-v02.api.letsencrypt.org/directory"))
	staging := storageDir("/data", parse("https://acme-staging-v02.api.letsencrypt.org/directory"))
	pebble := storageDir("/data", parse("https://localhost:14000/dir"))

	if production == staging {
		t.Fatal("staging and production share the storage")
	}
	if production != filepath.Join("/data", "acme", "acme-v02.api.letsencrypt.org") {
		t.Errorf("production = %s", production)
	}
	if pebble != filepath.Join("/data", "acme", "localhost_14000") {
		t.Errorf("pebble = %s", pebble)
	}
	if odd := storageDir("/data", &url.URL{Host: "../.."}); strings.Contains(odd, "..") {
		t.Errorf("a directory host escaped the storage: %s", odd)
	}
}

func TestDelegationNeedsItsSettings(t *testing.T) {
	opts := testOptions(t, &fakeIssuer{})
	opts.Provider = ProviderDelegation
	if _, err := New(opts, tls.Certificate{}); err == nil {
		t.Fatal("the delegation provider started without a challenge directory")
	}
	opts.ChallengeDir = t.TempDir()
	opts.DNSCheckAddress = "acme-dns:5353"
	if _, err := New(opts, tls.Certificate{}); err != nil {
		t.Fatalf("new: %v", err)
	}
}

func TestDelegationProviderPublishesWhatTheServerServes(t *testing.T) {
	dir := t.TempDir()
	reader := acmedns.NewReader(dir)
	server, err := acmedns.NewServer(acmedns.Config{
		Hosts:  []string{"panel.example.com"},
		Values: reader.Values,
		Rate:   1000,
		Burst:  1000,
		Logger: testLogger(),
	})
	if err != nil {
		t.Fatal(err)
	}
	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := packetConn.LocalAddr().String()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		_ = packetConn.Close()
		t.Skipf("the TCP port of %s is taken: %v", address, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, packetConn, listener) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})

	provider, options, err := newDelegationProvider(Options{ChallengeDir: dir, DNSCheckAddress: address})
	if err != nil {
		t.Fatal(err)
	}
	if len(options) != 1 {
		t.Fatalf("options = %d, want the precheck", len(options))
	}
	timeout, interval := provider.(*delegationProvider).Timeout()
	if timeout <= interval {
		t.Fatalf("timeout %v, interval %v", timeout, interval)
	}

	const keyAuth = "token.thumbprint"
	value := dns01.GetChallengeInfo("panel.example.com", keyAuth).Value
	name := acmedns.ChallengeName("panel.example.com")

	if servesValue(context.Background(), address, name, value) {
		t.Fatal("a value was served before it was presented")
	}
	if err := provider.Present("panel.example.com", "token", keyAuth); err != nil {
		t.Fatalf("present: %v", err)
	}
	if !eventually(func() bool { return servesValue(context.Background(), address, name, value) }) {
		t.Fatal("the DNS server does not serve the presented value")
	}
	if err := provider.CleanUp("panel.example.com", "token", keyAuth); err != nil {
		t.Fatalf("clean up: %v", err)
	}
	if !eventually(func() bool { return !servesValue(context.Background(), address, name, value) }) {
		t.Fatal("the DNS server still serves a cleaned value")
	}
}

func TestServesValueToleratesAnUnreachableServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	// Nothing listens on the discard port of loopback.
	if servesValue(ctx, "127.0.0.1:9", acmedns.ChallengeName("panel.example.com"), "x") {
		t.Fatal("an unreachable server served a value")
	}
}

// eventually retries check for a couple of seconds: the reader looks at the
// file at most twice a second.
func eventually(check func() bool) bool {
	for range 40 {
		if check() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

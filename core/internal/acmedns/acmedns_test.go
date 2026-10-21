package acmedns

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

const (
	host      = "talaia.example.com"
	challenge = "_acme-challenge.talaia.example.com."
	valueA    = "LoqXcYV8q5ONbJQxbmR7SCTNo3tiAXDfowyjxAjEuX0"
	valueB    = "1JRXmQEYH0DhSh2pcqkd8GbxojZiGxN8bLwtDVJCcY4"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fixedValues(values ...string) func(string) ([]string, time.Time) {
	return func(name string) ([]string, time.Time) {
		if name == challenge {
			return values, time.Unix(1_800_000_000, 0)
		}
		return nil, time.Time{}
	}
}

func newTestServer(t *testing.T, values func(string) ([]string, time.Time)) *Server {
	t.Helper()
	server, err := NewServer(Config{Hosts: []string{host, "192.0.2.10"}, Values: values, Logger: testLogger()})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server
}

func question(name string, qtype uint16) *dns.Msg {
	query := new(dns.Msg)
	query.SetQuestion(name, qtype)
	return query
}

func TestWriterAndReader(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	reader := NewReader(dir)
	reader.interval = 0

	if values, _ := reader.Values(challenge); len(values) != 0 {
		t.Fatalf("values without a file = %v", values)
	}
	if err := writer.Add("_ACME-Challenge.Talaia.Example.com", valueA, time.Hour); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := writer.Add(challenge, valueB, time.Hour); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Adding the same value again does not duplicate it.
	if err := writer.Add(challenge, valueA, time.Hour); err != nil {
		t.Fatalf("add again: %v", err)
	}
	values, modified := reader.Values(challenge)
	if strings.Join(values, ",") != valueA+","+valueB || modified.IsZero() {
		t.Fatalf("values = %v, modified %v", values, modified)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, FileName))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != fileMode {
			t.Errorf("mode = %v, want %v", info.Mode().Perm(), os.FileMode(fileMode))
		}
	}

	if err := writer.Remove(challenge, valueA); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// The file may keep its modification time within the same tick on some
	// filesystems: force the reader to see the change.
	reader.modified = time.Time{}
	if values, _ := reader.Values(challenge); len(values) != 1 || values[0] != valueB {
		t.Fatalf("values after remove = %v", values)
	}
	if err := writer.Remove(challenge, "not-there"); err != nil {
		t.Fatalf("removing a missing value: %v", err)
	}
}

func TestWriterRejectsBadInput(t *testing.T) {
	writer := NewWriter(t.TempDir())
	for _, value := range []string{"", "has space", "quote\"", strings.Repeat("a", maxValueLength+1)} {
		if err := writer.Add(challenge, value, time.Hour); err == nil {
			t.Errorf("value %q was accepted", value)
		}
	}
	if err := writer.Add("bad..name", valueA, time.Hour); err == nil {
		t.Error("an invalid name was accepted")
	}
	for i := range maxRecords {
		if err := writer.Add(challenge, fmt.Sprintf("value%02d", i), time.Hour); err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
	}
	if err := writer.Add(challenge, "onemore", time.Hour); err == nil {
		t.Error("more than the maximum of records was accepted")
	}
}

func TestExpiredValuesAreNotServed(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	writer := NewWriter(dir)
	writer.now = func() time.Time { return now }
	reader := NewReader(dir)
	reader.interval = 0
	reader.now = func() time.Time { return now }

	if err := writer.Add(challenge, valueA, time.Minute); err != nil {
		t.Fatal(err)
	}
	if values, _ := reader.Values(challenge); len(values) != 1 {
		t.Fatalf("values = %v", values)
	}
	now = now.Add(2 * time.Minute)
	if values, _ := reader.Values(challenge); len(values) != 0 {
		t.Fatalf("an expired value is still served: %v", values)
	}
}

func TestReaderKeepsValuesOnABrokenFile(t *testing.T) {
	dir := t.TempDir()
	writer := NewWriter(dir)
	reader := NewReader(dir)
	reader.interval = 0
	if err := writer.Add(challenge, valueA, time.Hour); err != nil {
		t.Fatal(err)
	}
	if values, _ := reader.Values(challenge); len(values) != 1 {
		t.Fatalf("values = %v", values)
	}
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	if values, _ := reader.Values(challenge); len(values) != 1 {
		t.Fatalf("a broken file dropped the values: %v", values)
	}
}

func TestNewServerNeedsADNSName(t *testing.T) {
	if _, err := NewServer(Config{Hosts: []string{"192.0.2.10", "[2001:db8::1]"}, Values: fixedValues()}); err == nil {
		t.Fatal("a server without DNS names was created")
	}
	if _, err := NewServer(Config{Hosts: []string{host}}); err == nil {
		t.Fatal("a server without values was created")
	}
}

func TestAnswers(t *testing.T) {
	server := newTestServer(t, fixedValues(valueA, valueB))

	tests := []struct {
		name          string
		query         *dns.Msg
		rcode         int
		authoritative bool
		answers       int
		authority     int
	}{
		{name: "txt", query: question(challenge, dns.TypeTXT), rcode: dns.RcodeSuccess, authoritative: true, answers: 2},
		{name: "mixed case", query: question("_ACME-challenge.TALAIA.example.COM.", dns.TypeTXT), rcode: dns.RcodeSuccess, authoritative: true, answers: 2},
		{name: "soa", query: question(challenge, dns.TypeSOA), rcode: dns.RcodeSuccess, authoritative: true, answers: 1},
		{name: "ns", query: question(challenge, dns.TypeNS), rcode: dns.RcodeSuccess, authoritative: true, answers: 1},
		{name: "a is nodata", query: question(challenge, dns.TypeA), rcode: dns.RcodeSuccess, authoritative: true, authority: 1},
		{name: "any is nodata", query: question(challenge, dns.TypeANY), rcode: dns.RcodeSuccess, authoritative: true, authority: 1},
		{name: "below is nxdomain", query: question("x."+challenge, dns.TypeTXT), rcode: dns.RcodeNameError, authoritative: true, authority: 1},
		{name: "the host itself is refused", query: question(host+".", dns.TypeA), rcode: dns.RcodeRefused},
		{name: "another zone is refused", query: question("example.org.", dns.TypeTXT), rcode: dns.RcodeRefused},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.query.RecursionDesired = true
			reply := server.answer(tt.query)
			if reply == nil {
				t.Fatal("no reply")
			}
			if reply.Rcode != tt.rcode || reply.Authoritative != tt.authoritative ||
				len(reply.Answer) != tt.answers || len(reply.Ns) != tt.authority {
				t.Fatalf("reply = rcode %d aa %t answers %d authority %d", reply.Rcode, reply.Authoritative, len(reply.Answer), len(reply.Ns))
			}
			if reply.RecursionAvailable {
				t.Error("the server offers recursion")
			}
			if len(reply.Extra) != 0 {
				t.Errorf("unexpected additional records: %v", reply.Extra)
			}
		})
	}

	reply := server.answer(question("_ACME-challenge.TALAIA.example.COM.", dns.TypeTXT))
	if reply.Answer[0].Header().Name != "_ACME-challenge.TALAIA.example.COM." {
		t.Errorf("the answer does not keep the case of the question: %s", reply.Answer[0].Header().Name)
	}
	ns := server.answer(question(challenge, dns.TypeNS)).Answer[0].(*dns.NS)
	if ns.Ns != host+"." {
		t.Errorf("NS = %s", ns.Ns)
	}
	soa := server.answer(question(challenge, dns.TypeSOA)).Answer[0].(*dns.SOA)
	if soa.Serial != 1_800_000_000 || soa.Minttl != answerTTL {
		t.Errorf("SOA = %v", soa)
	}
}

func TestOddQueries(t *testing.T) {
	server := newTestServer(t, fixedValues(valueA))

	notify := question(challenge, dns.TypeSOA)
	notify.Opcode = dns.OpcodeNotify
	if reply := server.answer(notify); reply.Rcode != dns.RcodeNotImplemented {
		t.Errorf("notify rcode = %d", reply.Rcode)
	}

	chaos := question(challenge, dns.TypeTXT)
	chaos.Question[0].Qclass = dns.ClassCHAOS
	if reply := server.answer(chaos); reply.Rcode != dns.RcodeRefused {
		t.Errorf("chaos rcode = %d", reply.Rcode)
	}

	edns1 := question(challenge, dns.TypeTXT)
	edns1.SetEdns0(4096, false)
	edns1.IsEdns0().SetVersion(1)
	if reply := server.answer(edns1); reply.Rcode != dns.RcodeBadVers {
		t.Errorf("EDNS version 1 rcode = %d", reply.Rcode)
	}

	answer := new(dns.Msg)
	answer.SetQuestion(challenge, dns.TypeTXT)
	answer.Response = true
	if reply := server.answer(answer); reply != nil {
		t.Error("the server answered a response")
	}
}

func startServer(t *testing.T, server *Server) string {
	t.Helper()
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
	// Wait until it answers.
	client := &dns.Client{Timeout: time.Second}
	for range 50 {
		if _, _, err := client.Exchange(question(challenge, dns.TypeSOA), address); err == nil {
			return address
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the DNS server did not start")
	return ""
}

func TestServeOverUDPAndTCPWithTruncation(t *testing.T) {
	values := make([]string, 0, maxRecords)
	for i := range maxRecords {
		values = append(values, fmt.Sprintf("%s%02d", valueA[:40], i))
	}
	server := newTestServer(t, fixedValues(values...))
	server.limiter = newLimiter(1000, 1000, time.Now)
	address := startServer(t, server)

	udp := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	reply, _, err := udp.Exchange(question(challenge, dns.TypeTXT), address)
	if err != nil {
		t.Fatalf("UDP query: %v", err)
	}
	if !reply.Truncated {
		t.Fatalf("a %d-value answer over plain UDP was not truncated", len(values))
	}

	tcp := &dns.Client{Net: "tcp", Timeout: 2 * time.Second}
	reply, _, err = tcp.Exchange(question(challenge, dns.TypeTXT), address)
	if err != nil {
		t.Fatalf("TCP query: %v", err)
	}
	if reply.Truncated || len(reply.Answer) != len(values) {
		t.Fatalf("TCP answer: truncated %t, %d answers", reply.Truncated, len(reply.Answer))
	}

	refused, _, err := udp.Exchange(question("example.org.", dns.TypeA), address)
	if err != nil {
		t.Fatalf("UDP query outside the zone: %v", err)
	}
	if refused.Rcode != dns.RcodeRefused || len(refused.Answer)+len(refused.Ns)+len(refused.Extra) != 0 {
		t.Fatalf("outside the zone: %v", refused)
	}
}

func TestRateLimitDropsTheExcess(t *testing.T) {
	server := newTestServer(t, fixedValues(valueA))
	server.limiter = newLimiter(1, 3, time.Now)
	address := startServer(t, server)

	client := &dns.Client{Net: "udp", Timeout: 300 * time.Millisecond}
	answered, dropped := 0, 0
	for range 8 {
		if _, _, err := client.Exchange(question(challenge, dns.TypeTXT), address); err != nil {
			dropped++
		} else {
			answered++
		}
	}
	// startServer spent one token on its readiness query.
	if answered > 3 || dropped == 0 {
		t.Fatalf("answered %d, dropped %d: the excess was not dropped", answered, dropped)
	}
}

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	l := newLimiter(10, 2, func() time.Time { return now })
	a := netip.MustParseAddr("198.51.100.7")
	sameNetwork := netip.MustParseAddr("198.51.100.200")
	other := netip.MustParseAddr("203.0.113.9")

	if !l.allow(a) || !l.allow(sameNetwork) {
		t.Fatal("the burst was not allowed")
	}
	if l.allow(a) {
		t.Fatal("a /24 went over its burst")
	}
	if !l.allow(other) {
		t.Fatal("another network was limited")
	}
	now = now.Add(100 * time.Millisecond)
	if !l.allow(a) {
		t.Fatal("the bucket did not refill")
	}
	if l.allow(netip.Addr{}) {
		t.Fatal("an unknown source was allowed")
	}

	v6 := netip.MustParseAddr("2001:db8:1:2::1")
	v6Same := netip.MustParseAddr("2001:db8:1:2:ffff::1")
	if sourcePrefix(v6) != sourcePrefix(v6Same) {
		t.Fatal("two addresses of one /56 got different buckets")
	}
}

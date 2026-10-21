package main

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/miekg/dns"
)

const (
	testResolver = "192.0.2.53:53"
	testAuth     = "198.51.100.1:53"
	testHost     = "talaia.example.com."
	testChal     = "_acme-challenge.talaia.example.com."
)

// fakeZone answers like public resolvers and the authoritative servers of
// example.com would, with knobs for what can be wrong.
type fakeZone struct {
	address     string // A of the panel host; empty means none
	nsTarget    string // NS of the challenge name at the parent; empty means none
	serverRcode int    // rcode resolvers get for the challenge SOA
}

func rr(t *testing.T, text string) dns.RR {
	t.Helper()
	record, err := dns.NewRR(text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	return record
}

func (z fakeZone) exchange(t *testing.T) exchangeFunc {
	return func(_ context.Context, query *dns.Msg, server string) (*dns.Msg, error) {
		q := query.Question[0]
		name := strings.ToLower(q.Name)
		reply := new(dns.Msg)
		reply.SetReply(query)
		soaExample := rr(t, "example.com. 300 IN SOA ns1.example.net. hostmaster.example.com. 1 7200 3600 1209600 300")

		switch server {
		case testResolver:
			switch {
			case name == testHost && q.Qtype == dns.TypeA:
				if z.address != "" {
					reply.Answer = []dns.RR{rr(t, testHost+" 300 IN A "+z.address)}
				}
			case name == testHost && q.Qtype == dns.TypeAAAA:
				reply.Ns = []dns.RR{soaExample}
			case name == testHost && q.Qtype == dns.TypeSOA:
				reply.Ns = []dns.RR{soaExample}
			case name == "example.com." && q.Qtype == dns.TypeNS:
				reply.Answer = []dns.RR{rr(t, "example.com. 300 IN NS ns1.example.net.")}
			case name == "ns1.example.net." && q.Qtype == dns.TypeA:
				reply.Answer = []dns.RR{rr(t, "ns1.example.net. 300 IN A 198.51.100.1")}
			case name == testChal && q.Qtype == dns.TypeSOA:
				reply.Rcode = z.serverRcode
				if z.serverRcode == dns.RcodeSuccess {
					reply.Answer = []dns.RR{rr(t, testChal+" 10 IN SOA talaia.example.com. hostmaster.talaia.example.com. 1 3600 600 86400 10")}
				}
			default:
				reply.Rcode = dns.RcodeNameError
			}
		case testAuth:
			if query.RecursionDesired {
				t.Error("the authoritative server was asked to recurse")
			}
			if name == testChal && q.Qtype == dns.TypeNS && z.nsTarget != "" {
				reply.Ns = []dns.RR{rr(t, testChal+" 3600 IN NS "+z.nsTarget)}
			} else {
				reply.Rcode = dns.RcodeNameError
				reply.Ns = []dns.RR{soaExample}
			}
		default:
			return nil, errors.New("unreachable")
		}
		return reply, nil
	}
}

func runCheck(t *testing.T, zone fakeZone, expected string) (bool, string) {
	t.Helper()
	checker := &delegationChecker{exchange: zone.exchange(t), resolvers: []string{"192.0.2.99:53", testResolver}}
	var out strings.Builder
	var want netip.Addr
	if expected != "" {
		want = netip.MustParseAddr(expected)
	}
	ok := checker.check(context.Background(), &out, "Talaia.Example.com", want)
	return ok, out.String()
}

func TestCheckDelegation(t *testing.T) {
	good := fakeZone{address: "203.0.113.5", nsTarget: testHost, serverRcode: dns.RcodeSuccess}

	tests := []struct {
		name     string
		zone     fakeZone
		expected string
		ok       bool
		lines    []string
	}{
		{name: "everything in place", zone: good, expected: "203.0.113.5", ok: true,
			lines: []string{"a-record: ok 203.0.113.5", "aaaa-record: none", "delegation: ok", "dns-server: ok"}},
		{name: "no A record", zone: fakeZone{nsTarget: testHost, serverRcode: dns.RcodeSuccess},
			lines: []string{"a-record: missing"}},
		{name: "A record to another address", zone: good, expected: "203.0.113.9",
			lines: []string{"a-record: other 203.0.113.5 (expected 203.0.113.9)"}},
		{name: "no delegation", zone: fakeZone{address: "203.0.113.5", serverRcode: dns.RcodeNameError},
			lines: []string{"delegation: missing (create an NS record _acme-challenge.talaia.example.com pointing to talaia.example.com.)", "dns-server: not delegated yet"}},
		{name: "delegation elsewhere", zone: fakeZone{address: "203.0.113.5", nsTarget: "ns.other.example.", serverRcode: dns.RcodeServerFailure},
			lines: []string{"delegation: other ns.other.example."}},
		{name: "port 53 closed", zone: fakeZone{address: "203.0.113.5", nsTarget: testHost, serverRcode: dns.RcodeServerFailure},
			lines: []string{"delegation: ok", "dns-server: unreachable (open UDP and TCP port 53"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, out := runCheck(t, tt.zone, tt.expected)
			if ok != tt.ok {
				t.Errorf("ok = %t, want %t\n%s", ok, tt.ok, out)
			}
			for _, line := range tt.lines {
				if !strings.Contains(out, line) {
					t.Errorf("output lacks %q:\n%s", line, out)
				}
			}
		})
	}
}

func TestCheckDelegationWithoutResolvers(t *testing.T) {
	checker := &delegationChecker{
		exchange: func(context.Context, *dns.Msg, string) (*dns.Msg, error) {
			return nil, &net.OpError{Op: "read", Err: errors.New("timeout")}
		},
		resolvers: []string{testResolver},
	}
	var out strings.Builder
	if checker.check(context.Background(), &out, "talaia.example.com", netip.Addr{}) {
		t.Fatal("the check passed without any resolver")
	}
	if !strings.Contains(out.String(), "a-record: unknown") || !strings.Contains(out.String(), "dns-server: unreachable") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestRunACMEUsage(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := runACME(nil, &stdout, &stderr); code != 2 {
		t.Errorf("no subcommand = %d, want 2", code)
	}
	if code := runACME([]string{"check-delegation"}, &stdout, &stderr); code != 2 {
		t.Errorf("no hostname = %d, want 2", code)
	}
	if code := runACME([]string{"check-delegation", "--hostname", "x.example.com", "--address", "nope"}, &stdout, &stderr); code != 2 {
		t.Errorf("bad address = %d, want 2", code)
	}
}

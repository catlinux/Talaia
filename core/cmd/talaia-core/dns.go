package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/miekg/dns"

	"github.com/catlinux/Talaia/core/internal/acmedns"
	"github.com/catlinux/Talaia/core/internal/config"
)

// publicResolvers answer the delegation checks: they see the zone the way
// Let's Encrypt will.
var publicResolvers = []string{"1.1.1.1:53", "8.8.8.8:53", "9.9.9.9:53"}

// runDNS serves the DNS-01 challenges of the panel. It runs in its own
// container, without the database or the secrets of the core, and only reads
// the challenge file the core writes.
func runDNS(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("dns", flag.ContinueOnError)
	flags.SetOutput(stderr)
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "usage: talaia-core dns (settings come from TALAIA_PUBLIC_HOSTNAMES, TALAIA_ACME_CHALLENGE_DIR and TALAIA_DNS_LISTEN)")
		return 2
	}

	level := slog.LevelInfo
	if value := os.Getenv("TALAIA_LOG_LEVEL"); value != "" {
		if err := level.UnmarshalText([]byte(value)); err != nil {
			fmt.Fprintf(stderr, "dns: invalid TALAIA_LOG_LEVEL %q\n", value)
			return 2
		}
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	dir := strings.TrimSpace(os.Getenv("TALAIA_ACME_CHALLENGE_DIR"))
	if dir == "" {
		dir = config.DefaultACMEChallengeDir
	}
	listen := strings.TrimSpace(os.Getenv("TALAIA_DNS_LISTEN"))
	if listen == "" {
		listen = ":5353"
	}
	hosts := strings.Split(os.Getenv("TALAIA_PUBLIC_HOSTNAMES"), ",")

	reader := acmedns.NewReader(dir)
	server, err := acmedns.NewServer(acmedns.Config{
		Hosts:  hosts,
		NS:     os.Getenv("TALAIA_DNS_NS"),
		Values: reader.Values,
		Logger: logger,
	})
	if err != nil {
		fmt.Fprintf(stderr, "dns: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("starting the ACME DNS server", "listen", listen, "names", server.Names(), "version", version)
	if err := server.ListenAndServe(ctx, listen); err != nil {
		logger.Error("the ACME DNS server stopped", "error", err)
		return 1
	}
	return 0
}

func runACME(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "check-delegation" {
		fmt.Fprintln(stderr, "usage: talaia-core acme check-delegation --hostname <name> [--address <public IP>]")
		return 2
	}
	flags := flag.NewFlagSet("acme check-delegation", flag.ContinueOnError)
	flags.SetOutput(stderr)
	hostname := flags.String("hostname", "", "DNS name of the panel")
	address := flags.String("address", "", "public IPv4 address the name must point to (optional)")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || strings.TrimSpace(*hostname) == "" {
		fmt.Fprintln(stderr, "usage: talaia-core acme check-delegation --hostname <name> [--address <public IP>]")
		return 2
	}
	var expected netip.Addr
	if strings.TrimSpace(*address) != "" {
		parsed, err := netip.ParseAddr(strings.TrimSpace(*address))
		if err != nil {
			fmt.Fprintf(stderr, "acme check-delegation: invalid --address %q\n", *address)
			return 2
		}
		expected = parsed
	}

	checker := &delegationChecker{exchange: exchangeUDPThenTCP, resolvers: publicResolvers}
	if checker.check(context.Background(), stdout, strings.TrimSpace(*hostname), expected) {
		return 0
	}
	return 1
}

// exchangeFunc sends one query to one server.
type exchangeFunc func(ctx context.Context, query *dns.Msg, server string) (*dns.Msg, error)

func exchangeUDPThenTCP(ctx context.Context, query *dns.Msg, server string) (*dns.Msg, error) {
	client := &dns.Client{Net: "udp", Timeout: 4 * time.Second}
	reply, _, err := client.ExchangeContext(ctx, query, server)
	if err == nil && reply.Truncated {
		client.Net = "tcp"
		reply, _, err = client.ExchangeContext(ctx, query, server)
	}
	return reply, err
}

// delegationChecker tells the installer, in plain lines, what is right and
// what is missing in the zone: the A record of the panel, the NS record that
// delegates the challenge name, and whether the DNS server answers through it.
type delegationChecker struct {
	exchange  exchangeFunc
	resolvers []string
}

// resolve asks the public resolvers in turn until one answers.
func (c *delegationChecker) resolve(ctx context.Context, name string, qtype uint16) (*dns.Msg, error) {
	query := new(dns.Msg)
	query.SetQuestion(dns.Fqdn(name), qtype)
	query.RecursionDesired = true
	var lastErr error
	for _, resolver := range c.resolvers {
		reply, err := c.exchange(ctx, query, resolver)
		if err == nil {
			return reply, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no resolver configured")
	}
	return nil, lastErr
}

func addressesOf(reply *dns.Msg) []string {
	var found []string
	for _, rr := range reply.Answer {
		switch record := rr.(type) {
		case *dns.A:
			found = append(found, record.A.String())
		case *dns.AAAA:
			found = append(found, record.AAAA.String())
		}
	}
	slices.Sort(found)
	return slices.Compact(found)
}

func (c *delegationChecker) check(ctx context.Context, out io.Writer, hostname string, expected netip.Addr) bool {
	host := acmedns.CanonicalName(hostname)
	challenge := acmedns.ChallengeName(hostname)
	ok := true

	// 1. The name of the panel points to this server.
	if reply, err := c.resolve(ctx, host, dns.TypeA); err != nil {
		fmt.Fprintf(out, "a-record: unknown (no public resolver answered: %v)\n", err)
		ok = false
	} else if addresses := addressesOf(reply); len(addresses) == 0 {
		fmt.Fprintf(out, "a-record: missing (create an A record for %s)\n", strings.TrimSuffix(host, "."))
		ok = false
	} else if expected.IsValid() && !slices.Contains(addresses, expected.String()) {
		fmt.Fprintf(out, "a-record: other %s (expected %s)\n", strings.Join(addresses, ","), expected)
		ok = false
	} else {
		fmt.Fprintf(out, "a-record: ok %s\n", strings.Join(addresses, ","))
	}
	if reply, err := c.resolve(ctx, host, dns.TypeAAAA); err == nil {
		if addresses := addressesOf(reply); len(addresses) > 0 {
			fmt.Fprintf(out, "aaaa-record: present %s (the challenge server answers on IPv4 only; remove the AAAA record or Let's Encrypt may fail)\n", strings.Join(addresses, ","))
		} else {
			fmt.Fprintln(out, "aaaa-record: none")
		}
	}

	// 2. The zone delegates the challenge name to the panel host. Asked to the
	// authoritative servers of the zone, so a closed port 53 here does not
	// hide a record that is right.
	delegated := c.checkDelegation(ctx, out, host, challenge)
	ok = ok && delegated

	// 3. Through that delegation, the DNS server of this machine answers.
	if reply, err := c.resolve(ctx, challenge, dns.TypeSOA); err != nil {
		fmt.Fprintf(out, "dns-server: unreachable (no public resolver answered: %v)\n", err)
		ok = false
	} else {
		switch {
		case reply.Rcode == dns.RcodeSuccess && answersSOA(reply, challenge):
			fmt.Fprintln(out, "dns-server: ok")
		case reply.Rcode == dns.RcodeServerFailure:
			fmt.Fprintln(out, "dns-server: unreachable (open UDP and TCP port 53 to this server, also in the firewall of the provider)")
			ok = false
		case reply.Rcode == dns.RcodeNameError:
			fmt.Fprintln(out, "dns-server: not delegated yet (the resolvers still see no NS record; DNS changes can take a few minutes)")
			ok = false
		default:
			fmt.Fprintf(out, "dns-server: unexpected answer %s\n", dns.RcodeToString[reply.Rcode])
			ok = false
		}
	}
	return ok
}

func answersSOA(reply *dns.Msg, name string) bool {
	for _, rr := range reply.Answer {
		if soa, isSOA := rr.(*dns.SOA); isSOA && acmedns.CanonicalName(soa.Hdr.Name) == name {
			return true
		}
	}
	return false
}

// checkDelegation finds the zone of host, asks one of its authoritative
// servers for the NS record of the challenge name and says what it found.
func (c *delegationChecker) checkDelegation(ctx context.Context, out io.Writer, host, challenge string) bool {
	zone := c.zoneOf(ctx, host)
	if zone == "" {
		fmt.Fprintln(out, "delegation: unknown (could not find the DNS zone of the name)")
		return false
	}
	servers := c.authoritativeServers(ctx, zone)
	if len(servers) == 0 {
		fmt.Fprintf(out, "delegation: unknown (could not reach the name servers of %s)\n", strings.TrimSuffix(zone, "."))
		return false
	}

	query := new(dns.Msg)
	query.SetQuestion(challenge, dns.TypeNS)
	query.RecursionDesired = false
	for _, server := range servers {
		reply, err := c.exchange(ctx, query, server)
		if err != nil {
			continue
		}
		var targets []string
		for _, section := range [][]dns.RR{reply.Answer, reply.Ns} {
			for _, rr := range section {
				if ns, isNS := rr.(*dns.NS); isNS && acmedns.CanonicalName(ns.Hdr.Name) == challenge {
					targets = append(targets, acmedns.CanonicalName(ns.Ns))
				}
			}
		}
		switch {
		case len(targets) == 0:
			fmt.Fprintf(out, "delegation: missing (create an NS record %s pointing to %s)\n",
				strings.TrimSuffix(challenge, "."), host)
			return false
		case !slices.Contains(targets, host):
			fmt.Fprintf(out, "delegation: other %s (it must point to %s)\n", strings.Join(targets, ","), host)
			return false
		default:
			fmt.Fprintln(out, "delegation: ok")
			return true
		}
	}
	fmt.Fprintf(out, "delegation: unknown (the name servers of %s did not answer)\n", strings.TrimSuffix(zone, "."))
	return false
}

// zoneOf returns the zone that holds host: the owner of the SOA record a
// resolver returns for it, in the answer or as the proof of a negative one.
func (c *delegationChecker) zoneOf(ctx context.Context, host string) string {
	reply, err := c.resolve(ctx, host, dns.TypeSOA)
	if err != nil {
		return ""
	}
	for _, section := range [][]dns.RR{reply.Answer, reply.Ns} {
		for _, rr := range section {
			if soa, isSOA := rr.(*dns.SOA); isSOA {
				return acmedns.CanonicalName(soa.Hdr.Name)
			}
		}
	}
	return ""
}

// authoritativeServers returns host:53 addresses of the name servers of zone.
func (c *delegationChecker) authoritativeServers(ctx context.Context, zone string) []string {
	reply, err := c.resolve(ctx, zone, dns.TypeNS)
	if err != nil {
		return nil
	}
	var servers []string
	for _, rr := range reply.Answer {
		ns, isNS := rr.(*dns.NS)
		if !isNS {
			continue
		}
		addresses, err := c.resolve(ctx, ns.Ns, dns.TypeA)
		if err != nil {
			continue
		}
		for _, address := range addressesOf(addresses) {
			servers = append(servers, net.JoinHostPort(address, "53"))
		}
		if len(servers) >= 4 {
			break
		}
	}
	return servers
}

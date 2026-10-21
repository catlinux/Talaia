package acme

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/challenge"
	"github.com/go-acme/lego/v4/challenge/dns01"
	"github.com/miekg/dns"

	"github.com/catlinux/Talaia/core/internal/acmedns"
)

const (
	// delegationTimeout bounds the wait for the DNS server to serve a value.
	delegationTimeout = 2 * time.Minute
	// delegationInterval is how often that wait asks again.
	delegationInterval = 2 * time.Second
)

// storageDir is where the account and the certificate of one ACME directory
// live: <dataDir>/acme/<host of the directory>. An account registered with
// the staging directory is not valid in production, so they never mix.
func storageDir(dataDir string, directory *url.URL) string {
	var b strings.Builder
	for _, r := range strings.ToLower(directory.Host) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	name := strings.Trim(b.String(), ".")
	if name == "" {
		name = "default"
	}
	return filepath.Join(dataDir, directoryName, name)
}

// delegationProvider publishes DNS-01 values for Talaia's own DNS server
// (talaia-core dns), which the zone delegates _acme-challenge.<host> to.
type delegationProvider struct {
	writer *acmedns.Writer
}

func newDelegationProvider(opts Options) (challenge.Provider, []dns01.ChallengeOption, error) {
	dir := strings.TrimSpace(opts.ChallengeDir)
	address := strings.TrimSpace(opts.DNSCheckAddress)
	if dir == "" || address == "" {
		return nil, nil, errors.New("the delegation provider needs a challenge directory and the address of the DNS server")
	}
	provider := &delegationProvider{writer: acmedns.NewWriter(dir)}
	// The delegation is static, so there is no propagation to wait for: the
	// value is live once Talaia's DNS server serves it. Asking that server on
	// the internal network does not depend on the machine reaching its own
	// public address, which some providers do not allow.
	check := dns01.WrapPreCheck(func(domain, _, value string, _ dns01.PreCheckFunc) (bool, error) {
		return servesValue(context.Background(), address, acmedns.ChallengeName(domain), value), nil
	})
	return provider, []dns01.ChallengeOption{check}, nil
}

// Present publishes the value of one challenge. The name is the challenge name
// of the domain itself, never one reached through a CNAME: the zone delegates
// exactly that name.
func (p *delegationProvider) Present(domain, _, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)
	if err := p.writer.Add(acmedns.ChallengeName(domain), info.Value, acmedns.DefaultLifetime); err != nil {
		return fmt.Errorf("publish the DNS-01 value: %w", err)
	}
	return nil
}

// CleanUp withdraws the value of one challenge.
func (p *delegationProvider) CleanUp(domain, _, keyAuth string) error {
	info := dns01.GetChallengeInfo(domain, keyAuth)
	if err := p.writer.Remove(acmedns.ChallengeName(domain), info.Value); err != nil {
		return fmt.Errorf("withdraw the DNS-01 value: %w", err)
	}
	return nil
}

// Timeout is how long lego waits for the precheck to pass, and how often it
// asks.
func (p *delegationProvider) Timeout() (time.Duration, time.Duration) {
	return delegationTimeout, delegationInterval
}

// servesValue asks the DNS server at address for the TXT values of name and
// reports whether value is among them. A failure to ask counts as "not yet":
// lego asks again until the timeout.
func servesValue(ctx context.Context, address, name, value string) bool {
	query := new(dns.Msg)
	query.SetQuestion(name, dns.TypeTXT)
	query.RecursionDesired = false
	client := &dns.Client{Net: "udp", Timeout: 3 * time.Second}
	reply, _, err := client.ExchangeContext(ctx, query, address)
	if err != nil {
		return false
	}
	if reply.Truncated {
		client.Net = "tcp"
		if reply, _, err = client.ExchangeContext(ctx, query, address); err != nil {
			return false
		}
	}
	for _, answer := range reply.Answer {
		if txt, ok := answer.(*dns.TXT); ok && strings.Join(txt.Txt, "") == value {
			return true
		}
	}
	return false
}

var _ challenge.ProviderTimeout = (*delegationProvider)(nil)

package checks

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/catlinux/Talaia/core/internal/netguard"
	"github.com/catlinux/Talaia/core/internal/store"
)

type Store interface {
	ListEnabledAlertChecks(context.Context) ([]store.AlertCheck, error)
	InsertAlertCheckResult(context.Context, store.AlertCheckResult) error
	DeleteCheckResultsBefore(context.Context, time.Time) (int64, error)
	GetAllowPrivateTargets(context.Context) (bool, error)
}
type Ticker interface {
	C() <-chan time.Time
	Stop()
}
type Clock interface {
	Now() time.Time
	NewTicker(time.Duration) Ticker
}
type realTicker struct{ *time.Ticker }

func (t realTicker) C() <-chan time.Time { return t.Ticker.C }

type realClock struct{}

func (realClock) Now() time.Time                   { return time.Now() }
func (realClock) NewTicker(d time.Duration) Ticker { return realTicker{time.NewTicker(d)} }

type ClientFactory func(netguard.HTTPClientOptions, *x509.CertPool) *http.Client
type Options struct {
	Store         Store
	OnResult      func(store.AlertCheckResult)
	Clock         Clock
	Concurrency   int
	RootCAs       *x509.CertPool
	ClientFactory ClientFactory
	Jitter        func(time.Duration) time.Duration
}
type Runner struct {
	store       Store
	onResult    func(store.AlertCheckResult)
	clock       Clock
	concurrency int
	roots       *x509.CertPool
	factory     ClientFactory
	jitter      func(time.Duration) time.Duration
	mu          sync.Mutex
	private     bool
	privateAt   time.Time
	active      map[string]bool
	reload      chan struct{}
}

func New(o Options) *Runner {
	if o.Clock == nil {
		o.Clock = realClock{}
	}
	if o.Concurrency <= 0 {
		o.Concurrency = 8
	}
	f := o.ClientFactory
	if f == nil {
		f = func(opts netguard.HTTPClientOptions, roots *x509.CertPool) *http.Client {
			c := netguard.NewHTTPClient(opts)
			if tr, ok := c.Transport.(*http.Transport); ok {
				tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
			}
			return c
		}
	}
	return &Runner{store: o.Store, onResult: o.OnResult, clock: o.Clock, concurrency: o.Concurrency, roots: o.RootCAs, factory: f, jitter: o.Jitter, active: map[string]bool{}, reload: make(chan struct{}, 1)}
}
func (r *Runner) Reload() {
	select {
	case r.reload <- struct{}{}:
	default:
	}
}
func (r *Runner) allowPrivate(ctx context.Context) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.clock.Now()
	if !r.privateAt.IsZero() && now.Sub(r.privateAt) < 30*time.Second {
		return r.private
	}
	v, err := r.store.GetAllowPrivateTargets(ctx)
	if err == nil {
		r.private = v
	}
	r.privateAt = now
	return r.private
}

func (r *Runner) Run(ctx context.Context) error {
	if r.store == nil {
		return fmt.Errorf("checks runner requires store")
	}
	checks, err := r.store.ListEnabledAlertChecks(ctx)
	if err != nil {
		return fmt.Errorf("load checks: %w", err)
	}
	sem := make(chan struct{}, r.concurrency)
	next := map[string]time.Time{}
	now := r.clock.Now()
	_ = r.purge(ctx, now)
	for _, c := range checks {
		d := interval(c)
		jitter := time.Duration(0)
		if r.jitter != nil {
			jitter = r.jitter(d)
		}
		next[c.ID] = now.Add(jitter)
	}
	refresh := r.clock.NewTicker(time.Minute)
	defer refresh.Stop()
	tick := r.clock.NewTicker(time.Second)
	defer tick.Stop()
	purgeAt := now.Add(24 * time.Hour)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-r.reload:
			checks = r.reloadOrKeep(ctx, next, checks)
		case <-refresh.C():
			checks = r.reloadOrKeep(ctx, next, checks)
		case <-tick.C():
			now = r.clock.Now()
			if !now.Before(purgeAt) {
				_ = r.purge(ctx, now)
				purgeAt = now.Add(24 * time.Hour)
			}
			for _, c := range checks {
				due, ok := next[c.ID]
				if !ok {
					next[c.ID] = now
					due = now
				}
				if now.Before(due) || r.isActive(c.ID) {
					continue
				}
				select {
				case sem <- struct{}{}:
					r.setActive(c.ID, true)
					next[c.ID] = now.Add(interval(c))
					go func(ch store.AlertCheck) {
						defer func() { <-sem; r.setActive(ch.ID, false) }()
						result := r.runOnce(ctx, ch)
						r.save(ctx, result)
					}(c)
				default:
				}
			}
		}
	}
}

// reloadOrKeep refreshes the list of checks and keeps running the current one
// when the database is briefly unavailable: a failed reload must not stop the
// monitoring for good.
func (r *Runner) reloadOrKeep(ctx context.Context, next map[string]time.Time, current []store.AlertCheck) []store.AlertCheck {
	checks, err := r.reloadChecks(ctx, next)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("reload checks", "error", err)
		}
		return current
	}
	return checks
}

func (r *Runner) reloadChecks(ctx context.Context, next map[string]time.Time) ([]store.AlertCheck, error) {
	cs, e := r.store.ListEnabledAlertChecks(ctx)
	if e != nil {
		return nil, e
	}
	now := r.clock.Now()
	present := map[string]bool{}
	for _, c := range cs {
		present[c.ID] = true
		if _, ok := next[c.ID]; !ok {
			next[c.ID] = now
		}
	}
	for id := range next {
		if !present[id] {
			delete(next, id)
		}
	}
	return cs, nil
}
func interval(c store.AlertCheck) time.Duration {
	n := c.IntervalSeconds
	if n <= 0 {
		n = 60
	}
	if n < 30 {
		n = 30
	}
	return time.Duration(n) * time.Second
}
func (r *Runner) isActive(id string) bool { r.mu.Lock(); defer r.mu.Unlock(); return r.active[id] }
func (r *Runner) setActive(id string, v bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if v {
		r.active[id] = true
	} else {
		delete(r.active, id)
	}
}
func (r *Runner) purge(ctx context.Context, now time.Time) error {
	_, e := r.store.DeleteCheckResultsBefore(ctx, now.Add(-30*24*time.Hour))
	return e
}
func (r *Runner) save(ctx context.Context, v store.AlertCheckResult) {
	if err := r.store.InsertAlertCheckResult(ctx, v); err != nil {
		return
	}
	if r.onResult != nil {
		r.onResult(v)
	}
}
func (r *Runner) RunOnce(ctx context.Context, c store.AlertCheck) store.AlertCheckResult {
	v := r.runOnce(ctx, c)
	r.save(ctx, v)
	return v
}

func (r *Runner) runOnce(ctx context.Context, c store.AlertCheck) store.AlertCheckResult {
	start := r.clock.Now()
	result := store.AlertCheckResult{Time: start, CheckID: c.ID}
	timeout := 10 * time.Second
	cfg, e := parseConfig(c.Config)
	if e == nil {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	if e = ValidateCheck(c.Kind, c.Target, c.Config); e != nil {
		result.Error = "invalid target"
		result.LatencyMS = int(r.clock.Now().Sub(start).Milliseconds())
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	allowed := r.allowPrivate(ctx)
	allow := func() bool { return allowed }
	switch c.Kind {
	case "http":
		result = r.httpCheck(ctx, c, cfg, allow, timeout)
	case "tcp":
		conn, err := netguard.DialTCP(ctx, "tcp", c.Target, allow, timeout)
		if err != nil {
			result.Error = classify(err)
		} else {
			_ = conn.Close()
			result.OK = true
		}
	case "tls":
		result = r.tlsCheck(ctx, c, allow, timeout)
	}
	result.Time = start
	result.CheckID = c.ID
	result.LatencyMS = int(r.clock.Now().Sub(start).Milliseconds())
	return result
}
func (r *Runner) httpCheck(ctx context.Context, c store.AlertCheck, cfg config, allow func() bool, timeout time.Duration) store.AlertCheckResult {
	result := store.AlertCheckResult{}
	client := r.factory(netguard.HTTPClientOptions{AllowPrivate: allow, Timeout: timeout, ResponseHeaderTimeout: timeout}, r.roots)
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fmt.Errorf("too many redirects")
		}
		_, e := netguard.ValidateURL(req.URL.String())
		return e
	}
	var cert *time.Time
	if tr, ok := client.Transport.(*http.Transport); ok {
		tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: r.roots, InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) > 0 {
				t := cs.PeerCertificates[0].NotAfter
				cert = &t
			}
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("tls: certificate verification failed")
			}
			intermediates := x509.NewCertPool()
			for _, c := range cs.PeerCertificates[1:] {
				intermediates.AddCert(c)
			}
			_, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{DNSName: cs.ServerName, Roots: r.roots, Intermediates: intermediates})
			return err
		}}
	}
	req, e := http.NewRequestWithContext(ctx, cfg.Method, c.Target, nil)
	if e != nil {
		result.Error = "invalid target"
		return result
	}
	resp, e := client.Do(req)
	if e != nil {
		result.Error = classify(e)
		if cert != nil {
			result.CertNotAfter = cert
		}
		if strings.Contains(e.Error(), "too many redirects") {
			result.Error = "too many redirects"
		}
		return result
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = netguard.ReadLimited(resp.Body, 64<<10)
	result.StatusCode = &resp.StatusCode
	if cert != nil {
		result.CertNotAfter = cert
	} else if strings.HasPrefix(strings.ToLower(c.Target), "https:") && resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		t := resp.TLS.PeerCertificates[0].NotAfter
		result.CertNotAfter = &t
	}
	result.OK = expected(resp.StatusCode, cfg.ExpectedCodes)
	if !result.OK {
		result.Error = "unexpected status " + strconv.Itoa(resp.StatusCode)
	}
	return result
}
func expected(status int, codes []string) bool {
	for _, s := range codes {
		p := strings.Split(s, "-")
		lo, _ := strconv.Atoi(p[0])
		hi := lo
		if len(p) > 1 {
			hi, _ = strconv.Atoi(p[1])
		}
		if status >= lo && status <= hi {
			return true
		}
	}
	return false
}
func (r *Runner) tlsCheck(ctx context.Context, c store.AlertCheck, allow func() bool, timeout time.Duration) store.AlertCheckResult {
	result := store.AlertCheckResult{}
	host, _, _ := net.SplitHostPort(c.Target)
	conn, e := netguard.DialTCP(ctx, "tcp", c.Target, allow, timeout)
	if e != nil {
		result.Error = classify(e)
		return result
	}
	defer func() { _ = conn.Close() }()
	var cert *time.Time
	tc := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: r.roots, ServerName: host, InsecureSkipVerify: true, VerifyConnection: func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) == 0 {
			return fmt.Errorf("tls: certificate verification failed")
		}
		t := cs.PeerCertificates[0].NotAfter
		cert = &t
		intermediates := x509.NewCertPool()
		for _, c := range cs.PeerCertificates[1:] {
			intermediates.AddCert(c)
		}
		_, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{DNSName: host, Roots: r.roots, Intermediates: intermediates})
		return err
	}})
	e = tc.HandshakeContext(ctx)
	result.CertNotAfter = cert
	if e != nil {
		if errors.Is(e, x509.UnknownAuthorityError{}) || strings.Contains(strings.ToLower(e.Error()), "certificate") {
			result.Error = "tls: certificate verification failed"
		} else {
			result.Error = "tls: handshake failed"
		}
		return result
	}
	result.OK = true
	return result
}
func classify(err error) string {
	if strings.Contains(strings.ToLower(err.Error()), "certificate") || strings.Contains(strings.ToLower(err.Error()), "x509:") {
		return "tls: certificate verification failed"
	}
	if errors.Is(err, netguard.ErrBlockedAddress) {
		return "blocked address"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() || errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var op *net.OpError
	if errors.As(err, &op) {
		if strings.Contains(strings.ToLower(op.Err.Error()), "refused") {
			return "connection refused"
		}
		var dns *net.DNSError
		if errors.As(op.Err, &dns) {
			return "dns: lookup failed"
		}
	}
	return "connection failed"
}

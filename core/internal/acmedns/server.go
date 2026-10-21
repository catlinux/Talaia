package acmedns

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	// answerTTL is short: a value only matters while one issuance runs.
	answerTTL = 10
	// nsTTL is the TTL of the NS record of the delegated name.
	nsTTL = 300
	// maxUDPSize is the largest UDP answer, as the DNS flag day 2020 advises.
	maxUDPSize = 1232

	defaultRate  = 20
	defaultBurst = 40
	maxTCPConns  = 64
	tcpTimeout   = 5 * time.Second
	maxBuckets   = 100_000
)

// Config configures a Server.
type Config struct {
	// Hosts are the DNS names of the panel. The server answers for
	// _acme-challenge.<host> of each and refuses everything else.
	Hosts []string
	// NS is the name the zone delegates to (the panel host itself); it goes in
	// the NS and SOA answers. Empty means the first DNS name of Hosts.
	NS string
	// Values gives the live TXT values of a challenge name.
	Values func(name string) ([]string, time.Time)
	// Rate and Burst limit the queries per second of one source network; zero
	// means 20 and 40.
	Rate, Burst float64
	Logger      *slog.Logger
}

// Server is an authoritative DNS server for the delegated challenge names
// only. It never recurses, answers in a few hundred bytes at most and drops
// what exceeds the per-source rate, so it cannot be used as an amplifier.
type Server struct {
	names   map[string]bool
	ns      string
	mbox    string
	values  func(name string) ([]string, time.Time)
	limiter *limiter
	logger  *slog.Logger
}

func isAddress(host string) bool {
	_, err := netip.ParseAddr(strings.Trim(strings.TrimSpace(host), "[]"))
	return err == nil
}

// NewServer validates cfg and returns the server.
func NewServer(cfg Config) (*Server, error) {
	names := map[string]bool{}
	firstHost := ""
	for _, host := range cfg.Hosts {
		host = strings.TrimSpace(host)
		if host == "" || isAddress(host) {
			continue
		}
		name := ChallengeName(host)
		if _, ok := dns.IsDomainName(name); !ok {
			return nil, fmt.Errorf("invalid host %q", host)
		}
		names[name] = true
		if firstHost == "" {
			firstHost = host
		}
	}
	if len(names) == 0 {
		return nil, errors.New("the DNS server needs at least one DNS name in the public hostnames")
	}
	if cfg.Values == nil {
		return nil, errors.New("the DNS server needs a source of challenge values")
	}
	ns := strings.TrimSpace(cfg.NS)
	if ns == "" {
		ns = firstHost
	}
	ns = CanonicalName(ns)
	if _, ok := dns.IsDomainName(ns); !ok {
		return nil, fmt.Errorf("invalid name server %q", ns)
	}
	rate, burst := cfg.Rate, cfg.Burst
	if rate <= 0 {
		rate = defaultRate
	}
	if burst <= 0 {
		burst = defaultBurst
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		names:   names,
		ns:      ns,
		mbox:    "hostmaster." + ns,
		values:  cfg.Values,
		limiter: newLimiter(rate, burst, time.Now),
		logger:  logger,
	}, nil
}

// Names returns the challenge names the server answers for.
func (s *Server) Names() []string {
	names := make([]string, 0, len(s.names))
	for name := range s.names {
		names = append(names, name)
	}
	return names
}

// ServeDNS answers one query.
func (s *Server) ServeDNS(w dns.ResponseWriter, query *dns.Msg) {
	if !s.limiter.allow(remoteIP(w.RemoteAddr())) {
		// No answer at all: a spoofed source gets nothing reflected.
		_ = w.Close()
		return
	}
	reply := s.answer(query)
	if reply == nil {
		_ = w.Close()
		return
	}

	size := dns.MinMsgSize
	if _, tcp := w.RemoteAddr().(*net.TCPAddr); tcp {
		size = dns.MaxMsgSize
	} else if opt := query.IsEdns0(); opt != nil {
		size = min(max(int(opt.UDPSize()), dns.MinMsgSize), maxUDPSize)
	}
	reply.Truncate(size)
	if err := w.WriteMsg(reply); err != nil {
		s.logger.Debug("write a DNS answer", "error", err)
	}
}

// answer builds the reply to query, or nil when nothing should be sent.
func (s *Server) answer(query *dns.Msg) *dns.Msg {
	if query.Response || len(query.Question) != 1 {
		return nil
	}
	reply := new(dns.Msg)
	reply.SetReply(query)
	reply.RecursionAvailable = false
	if opt := query.IsEdns0(); opt != nil {
		if opt.Version() != 0 {
			reply.Rcode = dns.RcodeBadVers
			reply.SetEdns0(maxUDPSize, false)
			return reply
		}
		reply.SetEdns0(maxUDPSize, false)
	}
	if query.Opcode != dns.OpcodeQuery {
		reply.Rcode = dns.RcodeNotImplemented
		return reply
	}

	question := query.Question[0]
	name := CanonicalName(question.Name)
	if question.Qclass != dns.ClassINET {
		reply.Rcode = dns.RcodeRefused
		return reply
	}
	apex, below := s.zoneOf(name)
	if apex == "" {
		reply.Rcode = dns.RcodeRefused
		return reply
	}

	reply.Authoritative = true
	values, modified := s.values(apex)
	soa := s.soa(apex, modified)
	if below {
		// Nothing exists under the challenge name.
		reply.Rcode = dns.RcodeNameError
		reply.Ns = []dns.RR{soa}
		return reply
	}

	switch question.Qtype {
	case dns.TypeTXT:
		for _, value := range values {
			reply.Answer = append(reply.Answer, &dns.TXT{
				Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: answerTTL},
				Txt: []string{value},
			})
		}
	case dns.TypeSOA:
		soa.Hdr.Name = question.Name
		reply.Answer = []dns.RR{soa}
	case dns.TypeNS:
		reply.Answer = []dns.RR{&dns.NS{
			Hdr: dns.RR_Header{Name: question.Name, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: nsTTL},
			Ns:  s.ns,
		}}
	}
	if len(reply.Answer) == 0 {
		// NODATA, also for ANY (RFC 8482 allows a minimal answer).
		reply.Ns = []dns.RR{soa}
	}
	return reply
}

// zoneOf returns the delegated name that name belongs to, and whether name is
// below it rather than the name itself.
func (s *Server) zoneOf(name string) (string, bool) {
	if s.names[name] {
		return name, false
	}
	for apex := range s.names {
		if strings.HasSuffix(name, "."+apex) {
			return apex, true
		}
	}
	return "", false
}

func (s *Server) soa(apex string, modified time.Time) *dns.SOA {
	serial := uint32(1)
	if !modified.IsZero() && modified.Unix() > 0 {
		serial = uint32(modified.Unix())
	}
	return &dns.SOA{
		Hdr:     dns.RR_Header{Name: apex, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: answerTTL},
		Ns:      s.ns,
		Mbox:    s.mbox,
		Serial:  serial,
		Refresh: 3600,
		Retry:   600,
		Expire:  86400,
		Minttl:  answerTTL,
	}
}

// ListenAndServe serves UDP and TCP on addr until ctx ends.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	packetConn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("listen on UDP %s: %w", addr, err)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		_ = packetConn.Close()
		return fmt.Errorf("listen on TCP %s: %w", addr, err)
	}
	return s.Serve(ctx, packetConn, listener)
}

// acceptQuery lets through only what this server answers: a plain query with
// one question and at most an EDNS record. Everything else gets no answer at
// all, not even the error the DNS library would send before the rate limit.
func acceptQuery(dh dns.Header) dns.MsgAcceptAction {
	const qr = 1 << 15
	if dh.Bits&qr != 0 || int(dh.Bits>>11)&0xF != dns.OpcodeQuery {
		return dns.MsgIgnore
	}
	if dh.Qdcount != 1 || dh.Ancount != 0 || dh.Nscount != 0 || dh.Arcount > 1 {
		return dns.MsgIgnore
	}
	return dns.MsgAccept
}

// Serve answers on the given connections until ctx ends.
func (s *Server) Serve(ctx context.Context, packetConn net.PacketConn, listener net.Listener) error {
	udp := &dns.Server{PacketConn: packetConn, Handler: s, UDPSize: maxUDPSize, MsgAcceptFunc: acceptQuery}
	tcp := &dns.Server{
		Listener:      &limitListener{Listener: listener, slots: make(chan struct{}, maxTCPConns)},
		Handler:       s,
		MsgAcceptFunc: acceptQuery,
		ReadTimeout:   tcpTimeout,
		WriteTimeout:  tcpTimeout,
		IdleTimeout:   func() time.Duration { return tcpTimeout },
		MaxTCPQueries: 1,
	}

	errs := make(chan error, 2)
	go func() { errs <- udp.ActivateAndServe() }()
	go func() { errs <- tcp.ActivateAndServe() }()
	go s.limiter.sweep(ctx, time.Minute)

	var serveErr error
	select {
	case <-ctx.Done():
	case err := <-errs:
		serveErr = fmt.Errorf("serve DNS: %w", err)
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = udp.ShutdownContext(shutdownCtx)
	_ = tcp.ShutdownContext(shutdownCtx)
	return serveErr
}

func remoteIP(addr net.Addr) netip.Addr {
	var ip net.IP
	switch a := addr.(type) {
	case *net.UDPAddr:
		ip = a.IP
	case *net.TCPAddr:
		ip = a.IP
	default:
		return netip.Addr{}
	}
	parsed, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}
	}
	return parsed.Unmap()
}

// limiter is a token bucket per source network: /24 for IPv4, /56 for IPv6.
type limiter struct {
	rate, burst float64
	now         func() time.Time

	mu      sync.Mutex
	buckets map[netip.Prefix]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(rate, burst float64, now func() time.Time) *limiter {
	return &limiter{rate: rate, burst: burst, now: now, buckets: map[netip.Prefix]*bucket{}}
}

func sourcePrefix(ip netip.Addr) netip.Prefix {
	bits := 56
	if ip.Is4() {
		bits = 24
	}
	prefix, err := ip.Prefix(bits)
	if err != nil {
		return netip.Prefix{}
	}
	return prefix
}

func (l *limiter) allow(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	key := sourcePrefix(ip)
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		// Bound the memory a scan from many networks can take.
		if len(l.buckets) >= maxBuckets {
			return false
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep forgets the buckets that refilled, every interval until ctx ends.
func (l *limiter) sweep(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := l.now()
			l.mu.Lock()
			for key, b := range l.buckets {
				if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
					delete(l.buckets, key)
				}
			}
			l.mu.Unlock()
		}
	}
}

// limitListener accepts at most cap(slots) TCP connections at once.
type limitListener struct {
	net.Listener
	slots chan struct{}
}

func (l *limitListener) Accept() (net.Conn, error) {
	l.slots <- struct{}{}
	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &limitConn{Conn: conn, release: func() { <-l.slots }}, nil
}

type limitConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

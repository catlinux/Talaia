package netguard

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const maxBodyBytes = 64 << 10

// HTTPClientOptions configures outbound HTTP checks while keeping private-network policy dynamic.
type HTTPClientOptions struct {
	AllowPrivate          func() bool
	Timeout               time.Duration
	ResponseHeaderTimeout time.Duration
}

// NewHTTPClient creates an HTTP client that bypasses environment proxies and uses the guarded dialer.
func NewHTTPClient(opts HTTPClientOptions) *http.Client {
	timeout := opts.Timeout
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 10 * time.Second
	}
	headerTimeout := opts.ResponseHeaderTimeout
	if headerTimeout <= 0 || headerTimeout > 10*time.Second {
		headerTimeout = 10 * time.Second
	}
	dialer := NewDialer(opts.AllowPrivate, timeout)
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		DisableKeepAlives:     true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ResponseHeaderTimeout: headerTimeout,
	}
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fmt.Errorf("too many redirects")
		}
		if _, err := ValidateURL(req.URL.String()); err != nil {
			return err
		}
		return nil
	}}
}

// ValidateURL accepts only credential-free HTTP destinations with an explicit host.
// Numeric alternate IPv4 spellings are rejected because URL parsers and resolvers disagree on them.
func ValidateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme")
	}
	if u.User != nil {
		return nil, fmt.Errorf("URL credentials are not allowed")
	}
	if u.Host == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("URL host is required")
	}
	host := u.Hostname()
	if strings.Contains(u.Host, "[") && strings.Contains(u.Host, "]") {
		ip, err := netip.ParseAddr(host)
		if err != nil || !ip.Is6() {
			return nil, fmt.Errorf("invalid bracketed IPv6 host")
		}
		return u, nil
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.Is4() {
		return u, nil
	}
	if looksNumericIP(host) {
		return nil, fmt.Errorf("non-canonical numeric IP host")
	}
	return u, nil
}

// ReadLimited reads at most 64 KiB, even when a larger limit is requested.
func ReadLimited(r io.Reader, max int64) ([]byte, error) {
	if max < 0 || max > maxBodyBytes {
		max = maxBodyBytes
	}
	return io.ReadAll(io.LimitReader(r, max))
}

// looksNumericIP reports whether the host would be read as an IPv4 address by
// some parsers: like browsers do, a host whose last label is a decimal or 0x
// hexadecimal number is treated as an address (2130706433, 0x7f.1, 017700000001).
func looksNumericIP(host string) bool {
	host = strings.TrimSuffix(host, ".")
	last := host[strings.LastIndex(host, ".")+1:]
	if last == "" {
		return false
	}
	if len(last) > 2 && (last[:2] == "0x" || last[:2] == "0X") {
		last = last[2:]
		for _, r := range last {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
				return false
			}
		}
		return true
	}
	for _, r := range last {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

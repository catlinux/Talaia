// Package netguard validates outbound destinations at the point of connection.
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"time"
)

// NewDialer creates a TCP dialer whose Control hook checks each resolved socket address,
// preventing DNS rebinding and fallback attempts from bypassing the destination policy.
func NewDialer(allowPrivate func() bool, timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: func(network, address string, _ syscall.RawConn) error {
		if network != "tcp" && network != "tcp4" && network != "tcp6" {
			return fmt.Errorf("unsupported network %q", network)
		}
		addrPort, err := netip.ParseAddrPort(address)
		if err != nil {
			return fmt.Errorf("parse outbound address: %w", err)
		}
		permitted := allowPrivate != nil && allowPrivate()
		return Check(addrPort.Addr(), permitted)
	}}
}

// DialTCP opens a guarded TCP or TLS-check connection using the resolved IP policy.
func DialTCP(ctx context.Context, network, address string, allowPrivate func() bool, timeout time.Duration) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("unsupported network %q", network)
	}
	return NewDialer(allowPrivate, timeout).DialContext(ctx, network, address)
}

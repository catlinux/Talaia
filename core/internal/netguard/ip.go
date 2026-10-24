package netguard

import (
	"fmt"
	"net/netip"
)

// ErrBlockedAddress identifies an address rejected by the outbound network guard.
var ErrBlockedAddress = fmt.Errorf("outbound address blocked")

var (
	metadataIPv4 = netip.MustParseAddr("169.254.169.254")
	metadataIPv6 = netip.MustParseAddr("fd00:ec2::254")
	unspecified4 = netip.MustParsePrefix("0.0.0.0/8")
	linkLocal4   = netip.MustParsePrefix("169.254.0.0/16")
	linkLocal6   = netip.MustParsePrefix("fe80::/10")
	private4     = []netip.Prefix{
		netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"), netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
	}
	private6    = []netip.Prefix{netip.MustParsePrefix("::1/128"), netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("2001:db8::/32")}
	nat64Prefix = netip.MustParsePrefix("64:ff9b::/96")
)

// Check rejects special-purpose destinations before an outbound connection is made.
// The caller's private-network policy never overrides metadata or link-local blocks.
func Check(ip netip.Addr, allowPrivate bool) error {
	if !ip.IsValid() || ip.Zone() != "" {
		return blocked("invalid or zoned address")
	}
	if nat64Prefix.Contains(ip) {
		a := ip.As16()
		ip = netip.AddrFrom4([4]byte{a[12], a[13], a[14], a[15]})
	}
	if ip.Is6() {
		a := ip.As16()
		var embedded netip.Addr
		if a[0] == 0x20 && a[1] == 0x02 { // 6to4 embeds the IPv4 address in bits 16–48.
			embedded = netip.AddrFrom4([4]byte{a[2], a[3], a[4], a[5]})
		} else if a[0] == 0x20 && a[1] == 0x01 && a[2] == 0 && a[3] == 0 { // Teredo stores the client address inverted.
			embedded = netip.AddrFrom4([4]byte{^a[12], ^a[13], ^a[14], ^a[15]})
		}
		if embedded.IsValid() {
			if err := Check(embedded, allowPrivate); err != nil {
				return err
			}
		}
	}
	ip = ip.Unmap()
	if ip == metadataIPv4 || ip == metadataIPv6 {
		return blocked("cloud metadata")
	}
	if ip.IsUnspecified() {
		return blocked("unspecified address")
	}
	if unspecified4.Contains(ip) {
		return blocked("unspecified IPv4")
	}
	if ip.IsMulticast() {
		return blocked("multicast")
	}
	if ip == netip.MustParseAddr("255.255.255.255") {
		return blocked("broadcast")
	}
	if linkLocal4.Contains(ip) || linkLocal6.Contains(ip) {
		return blocked("link-local")
	}
	if ip.Is4() && ip.IsPrivate() || ip.Is6() && ip.IsPrivate() {
		if !allowPrivate {
			return blocked("private address")
		}
	}
	for _, prefix := range private4 {
		if prefix.Contains(ip) {
			if !allowPrivate || prefix == netip.MustParsePrefix("192.0.2.0/24") || prefix == netip.MustParsePrefix("198.51.100.0/24") || prefix == netip.MustParsePrefix("203.0.113.0/24") {
				return blocked("special-purpose IPv4")
			}
		}
	}
	for _, prefix := range private6 {
		if prefix.Contains(ip) {
			if !allowPrivate || prefix == netip.MustParsePrefix("2001:db8::/32") {
				return blocked("special-purpose IPv6")
			}
		}
	}
	return nil
}

func blocked(reason string) error { return fmt.Errorf("%w: address is %s", ErrBlockedAddress, reason) }

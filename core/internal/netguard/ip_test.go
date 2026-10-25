package netguard

import (
	"errors"
	"net/netip"
	"testing"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		ip             string
		allow, blocked bool
	}{
		{"0.1.2.3", false, true}, {"::", false, true}, {"224.0.0.1", false, true}, {"ff02::1", false, true},
		{"255.255.255.255", false, true}, {"169.254.0.0", true, true}, {"169.254.169.254", true, true}, {"fe80::1", true, true},
		{"fd00:ec2::254", true, true}, {"::ffff:169.254.169.254", true, true}, {"127.0.0.1", false, true}, {"127.0.0.1", true, false},
		{"10.0.0.1", false, true}, {"::ffff:10.0.0.1", false, true}, {"10.0.0.1", true, false},
		{"100.64.0.0", false, true}, {"100.127.255.255", false, true}, {"100.128.0.0", false, false},
		{"172.15.255.255", false, false}, {"172.16.0.0", false, true}, {"172.31.255.255", true, false}, {"172.32.0.0", false, false},
		{"192.168.1.1", false, true}, {"192.0.0.1", false, true}, {"192.0.2.1", true, true}, {"198.18.0.1", false, true},
		{"198.51.100.1", true, true}, {"203.0.113.1", true, true}, {"240.0.0.1", false, true},
		{"fc00::1", false, true}, {"fc00::1", true, false}, {"2001:db8::1", true, true}, {"64:ff9b::7f00:1", false, true},
		{"2002:0a00:0001::1", false, true}, {"2001:0000::f5ff:fffe", false, true},
		{"::7f00:1", true, true}, {"::a00:1", true, true}, {"::1", true, false},
		{"100::1", true, true}, {"2001:1::1", true, true}, {"64:ff9b:1::1", true, true}, {"fec0::1", true, true}, {"3fff::1", true, true},
		{"2001:4860:4860::8888", false, false},
		{"93.184.216.34", false, false}, {"2606:4700:4700::1111", false, false},
	}
	if err := Check(netip.MustParseAddr("fe80::1%eth0"), true); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("zoned address error=%v", err)
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip, err := netip.ParseAddr(tt.ip)
			if err != nil {
				t.Fatal(err)
			}
			err = Check(ip, tt.allow)
			if got := errors.Is(err, ErrBlockedAddress); got != tt.blocked {
				t.Fatalf("Check() error=%v blocked=%v, want %v", err, got, tt.blocked)
			}
		})
	}
}

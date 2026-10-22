package webpush

import (
	"net/netip"
	"testing"
)

func TestPublicAddress(t *testing.T) {
	for address, public := range map[string]bool{
		"142.250.184.170":       true, // a push service
		"2a00:1450:4003:80e::a": true,
		"127.0.0.1":             false,
		"::1":                   false,
		"::ffff:127.0.0.1":      false, // IPv4 loopback written as IPv6
		"10.1.2.3":              false,
		"172.16.0.1":            false,
		"192.168.1.1":           false,
		"169.254.169.254":       false, // cloud metadata
		"100.64.0.1":            false, // shared address space
		"fd00::1":               false, // unique local
		"fe80::1":               false, // link-local
		"0.0.0.0":               false,
		"224.0.0.1":             false, // multicast
	} {
		if got := publicAddress(netip.MustParseAddr(address)); got != public {
			t.Errorf("%s: public %v, want %v", address, got, public)
		}
	}
}

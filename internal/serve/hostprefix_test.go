package serve

import (
	"net"
	"reflect"
	"testing"
)

func TestIPv6PrefixesKeepsOnlyGlobalOnes(t *testing.T) {
	cidr := func(s string) net.Addr {
		ip, n, err := net.ParseCIDR(s)
		if err != nil {
			t.Fatal(err)
		}
		n.IP = ip
		return n
	}
	got := ipv6Prefixes([]net.Addr{
		cidr("127.0.0.1/8"), cidr("192.168.1.5/24"), cidr("::1/128"), cidr("fe80::1/64"),
		cidr("fd12:3456::1/64"), cidr("::ffff:10.0.0.1/120"),
		cidr("2001:db8:1::5/64"), cidr("2001:db8:1::6/64"), cidr("2001:db8:2:0:1::7/56"),
		&net.UnixAddr{Name: "x", Net: "unix"},
	})
	want := []string{"2001:db8:1::/64", "2001:db8:2::/56"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ipv6Prefixes = %v, want %v", got, want)
	}
}

func TestIPv6PrefixesAreBounded(t *testing.T) {
	var addrs []net.Addr
	for i := range 40 {
		addrs = append(addrs, &net.IPNet{IP: net.ParseIP("2001:db8::").To16(), Mask: net.CIDRMask(64, 128)})
		addrs[i].(*net.IPNet).IP[7] = byte(i)
	}
	if got := ipv6Prefixes(addrs); len(got) != maxHostPrefixes {
		t.Errorf("got %d prefixes, want %d", len(got), maxHostPrefixes)
	}
}

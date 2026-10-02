package serve

import (
	"fmt"
	"net"
	"net/netip"
)

// maxHostPrefixes bounds how many prefixes are passed to a sidecar.
const maxHostPrefixes = 16

// minHostPrefixBits is the widest prefix runtime.Prepare accepts; a wider
// one an interface reports is dropped here so it cannot make Prepare refuse
// every spec.
const minHostPrefixBits = 16

// HostIPv6Prefixes lists the global IPv6 prefixes of the host's own interfaces
// for the egress proxy to refuse (design §7.2, issue #124). It fails open: when
// the interfaces cannot be read, or some prefixes are dropped or over the cap,
// it returns what it has and says so through logf (which may be nil).
func HostIPv6Prefixes(logf func(string, ...any)) []string {
	return hostIPv6Prefixes(net.InterfaceAddrs, logf)
}

func hostIPv6Prefixes(list func() ([]net.Addr, error), logf func(string, ...any)) []string {
	addrs, err := list()
	var warns []string
	if err != nil {
		warns = append(warns, fmt.Sprintf("cannot read the host's interfaces (%v)", err))
	}
	out, w := ipv6Prefixes(addrs)
	warns = append(warns, w...)
	if logf != nil {
		for _, m := range warns {
			logf("host IPv6 prefixes: %s; the egress proxy still refuses the private ranges and the prefixes it got", m)
		}
	}
	return out
}

// ipv6Prefixes takes each global-unicast IPv6 address with its network prefix
// as the interface reports it, skipping loopback, link-local, unique-local and
// IPv4 addresses, without duplicates and at most maxHostPrefixes of them. A
// prefix wider than /16 (a /0 included) is dropped, and so is the excess over
// the cap; each of those is reported in the warnings.
func ipv6Prefixes(addrs []net.Addr) (out, warns []string) {
	seen := map[netip.Prefix]bool{}
	dropped := 0
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(n.IP)
		if !ok || ip.Is4() || ip.Is4In6() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
			continue
		}
		ones, bits := n.Mask.Size()
		if bits != 128 {
			continue
		}
		if ones < minHostPrefixBits {
			warns = append(warns, fmt.Sprintf("dropped %s/%d: wider than /%d", ip, ones, minHostPrefixBits))
			continue
		}
		p := netip.PrefixFrom(ip, ones).Masked()
		if seen[p] {
			continue
		}
		seen[p] = true
		if len(out) == maxHostPrefixes {
			dropped++
			continue
		}
		out = append(out, p.String())
	}
	if dropped > 0 {
		warns = append(warns, fmt.Sprintf("kept %d prefixes and dropped %d more over the cap", maxHostPrefixes, dropped))
	}
	return out, warns
}

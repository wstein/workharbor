package serve

import (
	"net"
	"net/netip"
)

// maxHostPrefixes bounds how many prefixes are passed to a sidecar.
const maxHostPrefixes = 16

// HostIPv6Prefixes lists the global IPv6 prefixes of the host's own interfaces
// for the egress proxy to refuse (design §7.2, issue #124). It returns nothing
// when the interfaces cannot be read.
func HostIPv6Prefixes() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	return ipv6Prefixes(addrs)
}

// ipv6Prefixes takes each global-unicast IPv6 address with its network prefix
// as the interface reports it, skipping loopback, link-local, unique-local and
// IPv4 addresses, without duplicates and at most maxHostPrefixes of them.
func ipv6Prefixes(addrs []net.Addr) []string {
	var out []string
	seen := map[netip.Prefix]bool{}
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
		p := netip.PrefixFrom(ip, ones).Masked()
		if ones == 0 || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p.String())
		if len(out) == maxHostPrefixes {
			break
		}
	}
	return out
}

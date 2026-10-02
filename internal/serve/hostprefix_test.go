package serve

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/runtime"
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
	got, warns := ipv6Prefixes([]net.Addr{
		cidr("127.0.0.1/8"), cidr("192.168.1.5/24"), cidr("::1/128"), cidr("fe80::1/64"),
		cidr("fd12:3456::1/64"), cidr("::ffff:10.0.0.1/120"),
		cidr("2001:db8:1::5/64"), cidr("2001:db8:1::6/64"), cidr("2001:db8:2:0:1::7/56"),
		&net.UnixAddr{Name: "x", Net: "unix"},
	})
	want := []string{"2001:db8:1::/64", "2001:db8:2::/56"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ipv6Prefixes = %v, want %v", got, want)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v, want none", warns)
	}
}

func TestIPv6PrefixesAreBounded(t *testing.T) {
	var addrs []net.Addr
	for i := range 40 {
		addrs = append(addrs, &net.IPNet{IP: net.ParseIP("2001:db8::").To16(), Mask: net.CIDRMask(64, 128)})
		addrs[i].(*net.IPNet).IP[7] = byte(i)
	}
	got, warns := ipv6Prefixes(addrs)
	if len(got) != maxHostPrefixes {
		t.Errorf("got %d prefixes, want %d", len(got), maxHostPrefixes)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "dropped 24 more") {
		t.Errorf("warnings = %v, want one naming 24 dropped", warns)
	}
}

func TestIPv6PrefixesDropsWhatPrepareWouldRefuse(t *testing.T) {
	mk := func(ip string, ones, bits int) net.Addr {
		return &net.IPNet{IP: net.ParseIP(ip), Mask: net.CIDRMask(ones, bits)}
	}
	got, warns := ipv6Prefixes([]net.Addr{
		mk("2001:db8::1", 8, 128), mk("2001:db8::1", 0, 128), mk("10.1.2.3", 8, 32),
		mk("2001:db8:7::1", 64, 128),
	})
	if want := []string{"2001:db8:7::/64"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(warns) != 2 || !strings.Contains(warns[0], "wider than /16") {
		t.Errorf("warnings = %v, want two about prefixes wider than /16", warns)
	}
	// What is kept is accepted by Prepare: no availability failure.
	spec := runtime.Spec{
		Image: "img", Owner: "wh", Network: runtime.Network{Name: "n", Internal: true},
		ReadOnlyRoot: true, CapDrop: []string{"ALL"},
		Egress: &runtime.Egress{Image: "img", Proxy: "/p", DenyPrefixes: got},
	}
	if _, err := runtime.Prepare(runtime.PrepareOptions{FS: runtime.OSFS{}}, spec); err != nil {
		var se *runtime.SpecError
		if errors.As(err, &se) {
			for _, p := range se.Problems {
				if strings.Contains(p, "prefix") {
					t.Errorf("Prepare refused a kept prefix: %v", p)
				}
			}
		}
	}
}

func TestHostIPv6PrefixesLogsAndFailsOpen(t *testing.T) {
	var logs []string
	logf := func(f string, a ...any) { logs = append(logs, fmt.Sprintf(f, a...)) }
	got := hostIPv6Prefixes(func() ([]net.Addr, error) { return nil, errors.New("boom") }, logf)
	if len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "boom") || !strings.Contains(logs[0], "still refuses") {
		t.Errorf("logs = %v, want one naming the error", logs)
	}
	logs = nil
	got = hostIPv6Prefixes(func() ([]net.Addr, error) {
		return []net.Addr{&net.IPNet{IP: net.ParseIP("2001:db8:1::5"), Mask: net.CIDRMask(64, 128)}}, nil
	}, logf)
	if !reflect.DeepEqual(got, []string{"2001:db8:1::/64"}) || len(logs) != 0 {
		t.Errorf("got %v, logs %v", got, logs)
	}
}

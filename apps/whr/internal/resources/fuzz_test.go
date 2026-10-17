// SPDX-License-Identifier: EUPL-1.2

package resources

import "testing"

func FuzzParseCPU(f *testing.F) {
	for _, s := range []string{"", "2", "2.5", "0.001", "0", "-1", "1e3", "NaN", "9223372036854775.807", "9223372036854775.808", "２", "2.5000"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := ParseCPU(s)
		if err != nil {
			if classOf(t, err) == "" {
				t.Fatal("rejection without class")
			}
			return
		}
		if v <= 0 || int64(v) > MaxCanonical {
			t.Fatalf("accepted %q out of range: %d", s, v)
		}
		back, err := ParseCPU(v.String())
		if err != nil || back != v {
			t.Fatalf("round trip %q -> %q -> %d, %v", s, v.String(), back, err)
		}
	})
}

func FuzzParseMemory(f *testing.F) {
	for _, s := range []string{"", "4GiB", "1024MiB", "0MiB", "4", "4GB", "-4GiB", "1e3GiB", "8796093022207MiB", "8796093022208MiB", "８GiB", "4.5GiB"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := ParseMemory(s)
		if err != nil {
			if classOf(t, err) == "" {
				t.Fatal("rejection without class")
			}
			return
		}
		if v <= 0 || int64(v) > MaxCanonical || int64(v)%MiB != 0 {
			t.Fatalf("accepted %q out of range: %d", s, v)
		}
		back, err := ParseMemory(v.String())
		if err != nil || back != v {
			t.Fatalf("round trip %q -> %q -> %d, %v", s, v.String(), back, err)
		}
	})
}

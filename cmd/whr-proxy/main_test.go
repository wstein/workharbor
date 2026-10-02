package main

import "testing"

func TestParsePrefixes(t *testing.T) {
	got, err := parsePrefixes("2001:db8:1::/64, 2001:db8:2::5/48,")
	if err != nil || len(got) != 2 || got[0].String() != "2001:db8:1::/64" || got[1].String() != "2001:db8:2::/48" {
		t.Errorf("parsePrefixes = %v, %v", got, err)
	}
	if got, err := parsePrefixes(""); err != nil || len(got) != 0 {
		t.Errorf("empty = %v, %v", got, err)
	}
	if _, err := parsePrefixes("not-a-prefix"); err == nil {
		t.Error("a bad prefix was accepted")
	}
}

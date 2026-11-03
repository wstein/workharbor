// SPDX-License-Identifier: EUPL-1.2
package wsname

import "testing"

func TestContainerNameDerivation(t *testing.T) {
	got, err := ContainerName("cubing", "")
	if err != nil || got != "whr-cubing" {
		t.Fatalf("got %q, %v; want whr-cubing", got, err)
	}
}

func TestContainerNameOverrideWins(t *testing.T) {
	got, err := ContainerName("cubing", "custom_box.1")
	if err != nil || got != "custom_box.1" {
		t.Fatalf("got %q, %v; want override", got, err)
	}
	if _, err := ContainerName("cubing", "bad name"); err == nil {
		t.Fatal("override with a space must be refused as a container name")
	}
}

func TestInvalidNamesRefused(t *testing.T) {
	for _, n := range []string{"", "-a", "a-", "Cubing", "a b", "a_b", "a/b", "ä", "a.b", string(make([]byte, 65))} {
		if Valid(n) {
			t.Errorf("Valid(%q) = true", n)
		}
		if _, err := ContainerName(n, ""); err == nil {
			t.Errorf("ContainerName(%q) accepted", n)
		}
	}
	long := ""
	for i := 0; i < 64; i++ {
		long += "a"
	}
	if !Valid(long) || !Valid("a") || !Valid("a-b1") {
		t.Error("valid names refused")
	}
}

func TestSelectorCandidates(t *testing.T) {
	if got := DerivedFromSelector("cubing"); got != "whr-cubing" {
		t.Fatalf("got %q", got)
	}
	if got := DerivedFromSelector("Not Valid"); got != "" {
		t.Fatalf("invalid selector derived %q", got)
	}
}

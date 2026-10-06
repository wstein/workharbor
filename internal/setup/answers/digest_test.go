package answers

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wstein/workharbor/internal/doctor"
)

func fakeChecks() []doctor.Check {
	return []doctor.Check{
		{Name: "alpha", Phase: doctor.PhaseUser, Title: "Alpha", Fix: &doctor.Fix{
			Cmds: []doctor.Cmd{{Argv: []string{"mkdir", "-p", "/x"}}, {Argv: []string{"chmod", "700", "/x"}}},
		}},
		{Name: "beta", Phase: doctor.PhaseUser, Fix: &doctor.Fix{
			Do: func(context.Context, doctor.Prompter) error { return nil }, Desc: "write the configuration",
		}},
		{Name: "gamma", Phase: doctor.PhaseHost, Fix: &doctor.Fix{
			Cmds:  []doctor.Cmd{{Argv: []string{"pmset", "-a", "sleep", "0"}, Sudo: true}},
			Build: func(context.Context, doctor.Prompter) ([]doctor.Cmd, error) { return nil, nil },
		}},
	}
}

func TestFixDigestsArePinned(t *testing.T) {
	var b strings.Builder
	for _, c := range fakeChecks() {
		b.WriteString(c.Name + " " + FixDigest(c) + "\n")
	}
	golden(t, "fix-digests.txt", []byte(b.String()))
}

func TestDigestInputFieldOrderIsBytewiseSorted(t *testing.T) {
	typ := reflect.TypeOf(digestInput{})
	var tags []string
	for i := 0; i < typ.NumField(); i++ {
		tags = append(tags, typ.Field(i).Tag.Get("json"))
	}
	if !sort.StringsAreSorted(tags) {
		t.Fatalf("fields not in sorted tag order: %v", tags)
	}
}

func TestDigestChanges(t *testing.T) {
	base := func() doctor.Check {
		return doctor.Check{Name: "s", Phase: doctor.PhaseUser, Title: "T", Fix: &doctor.Fix{
			Cmds: []doctor.Cmd{{Argv: []string{"a", "b"}}, {Argv: []string{"c"}}}, Desc: "d",
		}}
	}
	ref := FixDigest(base())
	if FixDigest(base()) != ref {
		t.Fatal("not deterministic")
	}
	changes := map[string]func(*doctor.Check){
		"argv element":   func(c *doctor.Check) { c.Fix.Cmds[0].Argv[1] = "x" },
		"second command": func(c *doctor.Check) { c.Fix.Cmds[1].Argv = []string{"cc"} },
		"extra argv":     func(c *doctor.Check) { c.Fix.Cmds[1].Argv = append(c.Fix.Cmds[1].Argv, "z") },
		"sudo":           func(c *doctor.Check) { c.Fix.Cmds[0].Sudo = true },
		"desc":           func(c *doctor.Check) { c.Fix.Desc = "e" },
		"do presence":    func(c *doctor.Check) { c.Fix.Do = func(context.Context, doctor.Prompter) error { return nil } },
		"build presence": func(c *doctor.Check) {
			c.Fix.Build = func(context.Context, doctor.Prompter) ([]doctor.Cmd, error) { return nil, nil }
		},
		"step":  func(c *doctor.Check) { c.Name = "t" },
		"phase": func(c *doctor.Check) { c.Phase = doctor.PhaseHost },
		"split argv": func(c *doctor.Check) {
			c.Fix.Cmds = []doctor.Cmd{{Argv: []string{"a"}}, {Argv: []string{"b", "c"}}}
		},
	}
	for name, f := range changes {
		c := base()
		f(&c)
		if FixDigest(c) == ref {
			t.Errorf("%s did not change the digest", name)
		}
	}
	same := map[string]func(*doctor.Check){
		"title": func(c *doctor.Check) { c.Title = "other" },
		"guide": func(c *doctor.Check) { c.Fix.Guide = "read this" },
		"open":  func(c *doctor.Check) { c.Fix.Open = "x-apple.systempreferences:" },
	}
	for name, f := range same {
		c := base()
		f(&c)
		if FixDigest(c) != ref {
			t.Errorf("%s changed the digest", name)
		}
	}
}

func TestEligibleRules(t *testing.T) {
	do := func(context.Context, doctor.Prompter) error { return nil }
	cases := []struct {
		name string
		c    doctor.Check
		ok   bool
	}{
		{"user do", doctor.Check{Name: "x", Phase: doctor.PhaseUser, Fix: &doctor.Fix{Do: do}}, true},
		{"user cmds", doctor.Check{Name: "x", Phase: doctor.PhaseUser, Fix: &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"a"}}}}}, true},
		{"host", doctor.Check{Name: "x", Phase: doctor.PhaseHost, Fix: &doctor.Fix{Do: do}}, false},
		{"shared", doctor.Check{Name: "x", Fix: &doctor.Fix{Do: do}}, false},
		{"no fix", doctor.Check{Name: "x", Phase: doctor.PhaseUser}, false},
		{"guided", doctor.Check{Name: "x", Phase: doctor.PhaseUser, Fix: &doctor.Fix{Guide: "g", Open: "o"}}, false},
		{"sudo", doctor.Check{Name: "x", Phase: doctor.PhaseUser, Fix: &doctor.Fix{Cmds: []doctor.Cmd{{Argv: []string{"a"}, Sudo: true}}}}, false},
		{"sudo in second", doctor.Check{Name: "x", Phase: doctor.PhaseUser, Fix: &doctor.Fix{Do: do, Cmds: []doctor.Cmd{{Argv: []string{"a"}}, {Argv: []string{"b"}, Sudo: true}}}}, false},
		{"drop-admin", doctor.Check{Name: "drop-admin", Phase: doctor.PhaseUser, Fix: &doctor.Fix{Do: do}}, false},
	}
	for _, c := range cases {
		ok, reason := Eligible(c.c)
		if ok != c.ok || (!ok && reason == "") {
			t.Errorf("%s: got %v %q", c.name, ok, reason)
		}
	}
}

func TestEligibilityOverTheRealChecks(t *testing.T) {
	eligible := map[string]bool{}
	seen := map[string]bool{}
	for _, c := range doctor.Checks(doctor.Deps{ConfigPath: "x/config.json", Account: "workharbor"}) {
		seen[c.Name] = true
		ok, _ := Eligible(c)
		if !ok {
			continue
		}
		eligible[c.Name] = true
		if c.Phase == doctor.PhaseHost {
			t.Errorf("%s: a host step is eligible", c.Name)
		}
		if c.Fix.Do == nil && c.Fix.Build == nil && len(c.Fix.Cmds) == 0 {
			t.Errorf("%s: a guided step is eligible", c.Name)
		}
		for _, cmd := range c.Fix.Cmds {
			if cmd.Sudo {
				t.Errorf("%s: a sudo step is eligible", c.Name)
			}
		}
		if c.Name == "drop-admin" {
			t.Errorf("drop-admin is eligible")
		}
		if d := FixDigest(c); len(d) != 64 {
			t.Errorf("%s: digest %q", c.Name, d)
		}
	}
	for _, want := range []string{"config-dir", "api-token"} {
		if !eligible[want] {
			t.Errorf("%s should be eligible", want)
		}
	}
	if !seen["drop-admin"] {
		t.Error("drop-admin is not in the walked list: the deny-list assertion is vacuous")
	}
	if len(eligible) < 2 {
		t.Fatal("vacuous")
	}
}

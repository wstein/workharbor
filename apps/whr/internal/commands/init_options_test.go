// SPDX-License-Identifier: EUPL-1.2
package commands

import (
	"reflect"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/scaffold"
)

func mappedInit(t *testing.T, spelling, flags []string) (scaffold.Options, error) {
	t.Helper()
	a := &app{colorFlag: "auto"}
	a.build()
	c, _, err := a.root.Find(spelling)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.ParseFlags(flags); err != nil {
		t.Fatal(err)
	}
	return initOptions(c)
}
func TestInitProposalWizardFlagParity(t *testing.T) {
	name, shell := "demo", "nu"
	direct := scaffold.Options{Name: &name, HumanShell: &shell, DryRun: true, Overwrite: []string{"one,two.json", "workharbor.json"}}
	s := scaffold.Snapshot{Directory: "workspace", Entries: map[string]scaffold.Entry{".devcontainer": {Kind: "directory"}, ".devcontainer/devcontainer.json": {Kind: "file", Data: []byte(`{"image":"debian:bookworm"}`)}}}
	want, err := scaffold.Build(s, direct)
	if err != nil {
		t.Fatal(err)
	}
	for _, spelling := range [][]string{{"workspace", "init"}, {"ws", "init"}, {"init"}} {
		o, err := mappedInit(t, spelling, []string{"--name=demo", "--human-shell=nu", "--dry-run", "--overwrite=one,two.json", "--overwrite=workharbor.json", "--json"})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(o, direct) {
			t.Errorf("typed options differ for %v: got %+v, want %+v", spelling, o, direct)
		}
		got, err := scaffold.Build(s, o)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Files(), want.Files()) || !reflect.DeepEqual(got.Preview(), want.Preview()) {
			t.Errorf("flag/wizard model differs for %v", spelling)
		}
	}
}
func TestInitModes(t *testing.T) {
	for _, tc := range []struct {
		flags []string
		code  int
	}{{nil, 64}, {[]string{"--json"}, 64}, {[]string{"--non-interactive"}, 70}, {[]string{"--non-interactive", "--yes"}, 0}, {[]string{"--dry-run"}, 0}} {
		_, err := mappedInit(t, []string{"init"}, tc.flags)
		if int(exitcode.FromError(err)) != tc.code {
			t.Errorf("%v: %v, want code%d", tc.flags, err, tc.code)
		}
	}
}

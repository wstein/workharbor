// SPDX-License-Identifier: EUPL-1.2

package commands_test

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/commands"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/version"
)

func TestCommandTableMatchesContract(t *testing.T) {
	c := loadContract(t)
	table := commands.Table()
	if len(table) != len(c.Commands) {
		t.Fatalf("table has %d commands, contract has %d", len(table), len(c.Commands))
	}
	for i, want := range c.Commands {
		got := table[i]
		w := commands.Entry{
			ID: want.ID, Path: want.Path, Short: want.Short, Increment: want.Increment, Needs: want.Needs,
			Writes: want.Writes, JSON: want.JSON, BlockedOn: want.BlockedOn, Flags: want.Flags,
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("entry %d differs:\n got  %+v\n want %+v", i, got, w)
		}
	}
	if !reflect.DeepEqual(commands.GroupAliases(), c.GroupAliases) {
		t.Errorf("group aliases = %v, contract %v", commands.GroupAliases(), c.GroupAliases)
	}
	if c.Freeze.TestFile != "apps/whr/internal/commands/table_test.go" {
		t.Errorf("contract names freeze file %q", c.Freeze.TestFile)
	}
	assertCurrentTestIDs(t, c.Freeze.TestIDs)
}

func TestCommandTableAliasRules(t *testing.T) {
	table := commands.Table()
	groups := map[string]bool{}
	topLevel := map[string]bool{}
	verbGroups := map[string]map[string]bool{}
	ids := map[string]bool{}
	for _, e := range table {
		if ids[e.ID] {
			t.Errorf("duplicate id %q", e.ID)
		}
		ids[e.ID] = true
		if strings.Join(e.Path, ".") != e.ID {
			t.Errorf("id %q is not the dotted path %v", e.ID, e.Path)
		}
		if len(e.Path) == 1 {
			topLevel[e.Path[0]] = true
			continue
		}
		groups[e.Path[0]] = true
		verb := e.Path[len(e.Path)-1]
		if verbGroups[verb] == nil {
			verbGroups[verb] = map[string]bool{}
		}
		verbGroups[verb][e.Path[0]] = true
	}
	seen := map[string]string{}
	for _, e := range table {
		if e.Short == nil {
			continue
		}
		spelling := strings.Join(e.Short, " ")
		if other, dup := seen[spelling]; dup {
			t.Errorf("short form %q is used by %s and %s", spelling, other, e.ID)
		}
		seen[spelling] = e.ID
		if len(e.Short) == 1 && (topLevel[e.Short[0]] || (groups[e.Short[0]] && e.Short[0] != "service")) {
			t.Errorf("%s: short form %q shadows a group or top-level command", e.ID, e.Short[0])
		}
		if !reflect.DeepEqual(e.Short, e.Path[len(e.Path)-len(e.Short):]) {
			t.Errorf("%s: short form %v is not a suffix of the path %v", e.ID, e.Short, e.Path)
		}
		verb := e.Path[len(e.Path)-1]
		// "service status" keeps its service prefix, so it is not a bare status.
		if len(e.Short) == 1 && len(verbGroups[verb]) > 1 && e.Path[0] != "workspace" {
			t.Errorf("%s: verb %q exists in %d groups and has a short form outside workspace", e.ID, verb, len(verbGroups[verb]))
		}
	}
	// ls, show, status, stop exist in several groups: only workspace may shorten them.
	for _, e := range table {
		verb := e.Path[len(e.Path)-1]
		if len(verbGroups[verb]) > 1 && e.Path[0] != "workspace" && len(e.Short) == 1 {
			t.Errorf("%s has a bare short form", e.ID)
		}
	}
	// ws is an alias of the group workspace only.
	aliases := commands.GroupAliases()
	if len(aliases) != 1 || !reflect.DeepEqual(aliases["workspace"], []string{"ws"}) {
		t.Errorf("group aliases = %v, want only ws for workspace", aliases)
	}
	for group, list := range aliases {
		for _, a := range list {
			if groups[a] || topLevel[a] || seen[a] != "" {
				t.Errorf("alias %q of %s collides with another name", a, group)
			}
		}
	}
	// Every entry carries one of the declared values.
	for _, e := range table {
		if e.Increment != "first" && e.Increment != "later" {
			t.Errorf("%s: increment %q", e.ID, e.Increment)
		}
		if (e.Increment == "later") != (e.BlockedOn != "") {
			t.Errorf("%s: blocked_on must be set exactly for later commands", e.ID)
		}
		if commands.Describe(e.ID) == "" {
			t.Errorf("%s has no help description", e.ID)
		}
	}
}

// spellings returns every way to type the command: canonical, group alias and short form.
func spellings(e commands.Entry) [][]string {
	out := [][]string{e.Path}
	if e.Path[0] == "workspace" {
		out = append(out, append([]string{"ws"}, e.Path[1:]...))
	}
	if e.Short != nil {
		out = append(out, e.Short)
	}
	return out
}

func TestAliasInvocationEquivalent(t *testing.T) {
	handlers := stubHandlers()
	for _, e := range commands.Table() {
		if e.ID == "help" || e.ID == "completion" || e.ID == "version" {
			continue // built-ins without aliases
		}
		for _, mode := range [][]string{nil, {"--json"}} {
			var base *result
			for _, sp := range spellings(e) {
				args := append(append([]string{}, sp...), mode...)
				args = append(args, "extra-arg")
				got := run(t, handlers, args...)
				wantCode := 0
				if e.JSON == "rejected" && len(mode) > 0 {
					wantCode = 64 // --json is a structured usage error for ssh
				}
				if got.code != wantCode {
					t.Fatalf("%v: exit %d, stderr %q", args, got.code, got.err)
				}
				if base == nil {
					base = &got
					continue
				}
				if got != *base {
					t.Errorf("%s: %v differs from canonical spelling:\n got  %+v\n want %+v", e.ID, args, got, *base)
				}
			}
		}
	}
	// With the production handlers every alias refuses identically, too.
	for _, e := range commands.Table() {
		if e.Increment == "first" && (e.ID == "version" || e.ID == "help" || e.ID == "completion") {
			continue
		}
		var base *result
		for _, sp := range spellings(e) {
			got := run(t, nil, append(append([]string{}, sp...), "--json")...)
			if base == nil {
				base = &got
			} else if got != *base {
				t.Errorf("%s: %v differs from the canonical spelling", e.ID, sp)
			}
		}
	}
}

func TestJSONCommandFieldIsCanonical(t *testing.T) {
	handlers := stubHandlers()
	for _, e := range commands.Table() {
		if e.ID == "help" || e.ID == "completion" {
			continue // json is rejected; covered below
		}
		for _, sp := range spellings(e) {
			for _, h := range []map[string]commands.Handler{handlers, nil} {
				args := append(append([]string{}, sp...), "--json")
				got := run(t, h, args...)
				env := decodeEnvelope(t, got.stdout)
				if env["command"] != e.ID {
					t.Errorf("%v: command = %v, want %q", args, env["command"], e.ID)
				}
			}
		}
	}
	for _, args := range [][]string{{"--json", "ws", "ls"}, {"ws", "--json", "ls"}, {"--json", "ls"}, {"--color", "never", "--json", "ls"}} {
		if env := decodeEnvelope(t, run(t, nil, args...).stdout); env["command"] != "workspace.ls" {
			t.Errorf("%v: command = %v", args, env["command"])
		}
	}
	for _, args := range [][]string{{"--json", "nonsense"}, {"--json", "workspace", "nonsense"}, {"--json", "--bogus"}} {
		env := decodeEnvelope(t, run(t, nil, args...).stdout)
		if env["ok"] != false || env["exit_code"] != float64(64) {
			t.Errorf("%v: %v", args, env)
		}
	}
	// Unsupported --json is a structured usage error naming the canonical id.
	for _, id := range [][]string{{"help"}, {"completion", "bash"}} {
		got := run(t, nil, append(id, "--json")...)
		env := decodeEnvelope(t, got.stdout)
		if got.code != 64 || env["command"] != id[0] || env["ok"] != false {
			t.Errorf("%v --json: exit %d, envelope %v", id, got.code, env)
		}
		if !strings.HasPrefix(got.err, "whr: ") {
			t.Errorf("%v --json: stderr %q", id, got.err)
		}
	}
}

func TestHelpShowsShortFormHint(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"workspace", "ls", "--help"}, []string{"Short form: whr ls"}},
		{[]string{"ws", "ls", "-h"}, []string{"Short form: whr ls"}},
		{[]string{"help", "workspace", "validate"}, []string{"Short form: whr validate"}},
		{[]string{"host", "service", "install", "--help"}, []string{"Short form: whr service install"}},
		{[]string{"host", "ls", "--help"}, []string{"Short form: none"}},
		{[]string{"auth", "login", "--help"}, []string{"Short form: whr login"}},
	} {
		got := run(t, nil, tc.args...)
		if got.code != 0 || got.err != "" {
			t.Errorf("%v: exit %d, stderr %q", tc.args, got.code, got.err)
		}
		for _, want := range tc.want {
			if !strings.Contains(got.stdout, want) {
				t.Errorf("%v: help lacks %q:\n%s", tc.args, want, got.stdout)
			}
		}
	}
	root := run(t, nil)
	for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"help"}} {
		got := run(t, nil, args...)
		if got.code != 0 || got.stdout != root.stdout || got.err != "" {
			t.Errorf("%v differs from bare whr (exit %d)", args, got.code)
		}
	}
	for _, want := range []string{"version", "completion", "help", "Not yet available", "workspace ls", "(short: ls)", "--json", "--color", "--no-color"} {
		if !strings.Contains(root.stdout, want) {
			t.Errorf("root help lacks %q:\n%s", want, root.stdout)
		}
	}
	// Available commands move out of the "not yet available" block.
	all := run(t, stubHandlers())
	if strings.Contains(all.stdout, "Not yet available") {
		t.Errorf("help still lists unavailable commands when every command has a handler:\n%s", all.stdout)
	}
	// Help is data: it goes to stdout.
	if root.err != "" {
		t.Errorf("help wrote to stderr: %q", root.err)
	}
}

func TestCompletionKnowsBothForms(t *testing.T) {
	complete := func(h map[string]commands.Handler, args ...string) []string {
		got := run(t, h, append([]string{"__complete"}, args...)...)
		if got.code != 0 {
			t.Fatalf("__complete %v: exit %d: %s", args, got.code, got.err)
		}
		var names []string
		for _, line := range strings.Split(got.stdout, "\n") {
			if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "Completion ended") {
				continue
			}
			names = append(names, strings.Split(line, "\t")[0])
		}
		sort.Strings(names)
		return names
	}
	handlers := stubHandlers()
	root := complete(handlers, "")
	for _, want := range []string{"workspace", "ws", "host", "auth", "op", "agent", "version", "completion", "help",
		"ls", "show", "create", "start", "stop", "rebuild", "rm", "shell", "console", "ssh", "open", "init", "validate",
		"setup", "doctor", "offboard", "service", "login", "logout"} {
		if !contains(root, want) {
			t.Errorf("root completion lacks %q: %v", want, root)
		}
	}
	for _, none := range []string{"status", "wait", "attach", "send"} {
		if contains(root, none) {
			t.Errorf("root completion offers %q, which has no short form", none)
		}
	}
	// Every canonical spelling is completed under its group; ws completes like workspace.
	for _, e := range commands.Table() {
		if len(e.Path) < 2 {
			continue
		}
		parent := e.Path[:len(e.Path)-1]
		for _, p := range [][]string{parent, append([]string{"ws"}, parent[1:]...)} {
			if p[0] == "ws" && e.Path[0] != "workspace" {
				continue
			}
			if got := complete(handlers, append(append([]string{}, p...), "")...); !contains(got, e.Path[len(e.Path)-1]) {
				t.Errorf("%v completion lacks %q: %v", p, e.Path[len(e.Path)-1], got)
			}
		}
	}
	if got := complete(handlers, "service", ""); !reflect.DeepEqual(got, []string{"install", "status", "uninstall"}) {
		t.Errorf("service completion = %v", got)
	}
	if got := complete(handlers, "w"); !reflect.DeepEqual(got, []string{"workspace", "ws"}) {
		t.Errorf("prefix w = %v, want both spellings of the group", got)
	}
	// Production handlers: unavailable commands are not offered.
	prod := complete(nil, "")
	for _, want := range []string{"version", "completion", "help", "doctor", "host", "validate", "workspace", "ws", "init", "ls", "rm", "shell"} {
		if !contains(prod, want) {
			t.Errorf("production completion lacks %q: %v", want, prod)
		}
	}
	for _, none := range []string{"setup", "service"} {
		if contains(prod, none) {
			t.Errorf("production completion offers unavailable %q: %v", none, prod)
		}
	}

	// completion scripts for the three shells.
	for _, shell := range []string{"bash", "zsh", "fish"} {
		got := run(t, nil, "completion", shell)
		if got.code != 0 || got.err != "" || !strings.Contains(got.stdout, "__complete") || !strings.Contains(got.stdout, "whr") {
			t.Errorf("completion %s: exit %d, stderr %q, %d bytes", shell, got.code, got.err, len(got.stdout))
		}
	}
	for _, args := range [][]string{{"completion"}, {"completion", "powershell"}, {"completion", "bash", "zsh"}} {
		if got := run(t, nil, args...); got.code != 64 || got.stdout != "" || !strings.HasPrefix(got.err, "whr: ") {
			t.Errorf("%v: exit %d stdout %q stderr %q", args, got.code, got.stdout, got.err)
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func TestLaterCommandsRefuseWithoutBackend(t *testing.T) {
	spy := baseHandlers()
	contract := loadContract(t)
	for _, e := range commands.Table() {
		if e.Increment != "later" {
			continue
		}
		if strings.TrimSpace(contract.BlockedOnValues[e.BlockedOn]) == "" {
			t.Errorf("%s: blocked_on %q has no contract description", e.ID, e.BlockedOn)
		}
		if strings.TrimSpace(commands.WaitsFor(e.ID)) == "" {
			t.Errorf("%s: refusal has no explanation of its prerequisite", e.ID)
		}
		for _, sp := range spellings(e) {
			extras := [][]string{nil, {"some-name"}, {"--cpus", "2", "--memory", "1GiB"}, {"--bogus"}}
			// FR-066 parses its own local flags now. Unknown flags retain exit
			// 64 via Cobra; known flags without --local retain owner_api refusal.
			if e.ID == "workspace.shell" {
				extras = [][]string{nil, {"some-name"}, {"--local=false"}}
			}
			if e.ID == "workspace.rm" {
				extras = [][]string{nil, {"some-name"}, {"--role", "temp"}}
			}
			if e.ID == "workspace.ls" {
				extras = [][]string{nil, {"some-name"}, {"--role", "primary"}}
			}
			for _, extra := range extras {
				args := append(append([]string{}, sp...), extra...)
				got := run(t, spy, args...)
				if got.code != 64 || got.stdout != "" {
					t.Errorf("%v: exit %d, stdout %q; want 64 and no data", args, got.code, got.stdout)
				}
				line := strings.TrimSuffix(got.err, "\n")
				if !strings.HasPrefix(line, "whr: "+e.ID+" is not available") || strings.Contains(line, "\n") {
					t.Errorf("%v: stderr %q is not a one-line refusal naming %s", args, got.err, e.ID)
				}
				if !strings.Contains(line, commands.WaitsFor(e.ID)) {
					t.Errorf("%v: refusal does not name what it waits for (%q): %q", args, commands.WaitsFor(e.ID), line)
				}
			}
		}
	}
	// Init is now available, but unattended writes still require --yes.
	for _, sp := range [][]string{{"workspace", "init"}, {"ws", "init"}, {"init"}} {
		got := run(t, nil, append(append([]string{}, sp...), "--non-interactive")...)
		if got.code != 70 || got.stdout != "" || !strings.Contains(got.err, "--yes") {
			t.Errorf("available init %v must name missing consent: %+v", sp, got)
		}
	}
	// Once a handler exists the command runs, and a handler is never invoked for a refused JSON mode.
	called := 0
	h := stubHandlers()
	h["workspace.validate"] = func(context.Context, commands.Call) (commands.Result, error) {
		called++
		return commands.Result{Human: "ok\n"}, nil
	}
	if got := run(t, h, "validate"); got.code != 0 || called != 1 {
		t.Errorf("validate with handler: exit %d, called %d", got.code, called)
	}
}

func TestInitFlagsMatchContract(t *testing.T) {
	var init contractEntry
	for _, e := range loadContract(t).Commands {
		if e.ID == "workspace.init" {
			init = e
		}
	}
	if len(init.Flags) == 0 {
		t.Fatal("contract lists no flags for workspace.init")
	}
	for _, sp := range [][]string{{"workspace", "init"}, {"ws", "init"}, {"init"}} {
		help := run(t, nil, append(append([]string{}, sp...), "--help")...)
		for _, flag := range init.Flags {
			if !strings.Contains(help.stdout, flag) {
				t.Errorf("%v --help lacks %s:\n%s", sp, flag, help.stdout)
			}
		}
		// Every contract flag parses; the stub avoids applying unrelated fixture inputs.
		for _, flag := range init.Flags {
			args := append(append([]string{}, sp...), flag)
			if !isBoolFlag(flag) {
				args = append(args, "value")
			}
			got := run(t, stubHandlers(), args...)
			if got.code != 0 {
				t.Errorf("%v: exit %d stderr %q, want parsed flags delivered to the stub", args, got.code, got.err)
			}
		}
		for _, flags := range [][]string{{"--overwrite=workharbor.json"}, {"--overwrite=workharbor.json", "--overwrite=AGENTS.md"}} {
			got := run(t, stubHandlers(), append(append([]string{}, sp...), flags...)...)
			if got.code != 0 {
				t.Errorf("%v %v: want parsed flags delivered to the stub, got %+v", sp, flags, got)
			}
		}
		missing := run(t, nil, append(append([]string{}, sp...), "--overwrite")...)
		if missing.code != 64 || !strings.Contains(missing.err, "flag needs an argument") {
			t.Errorf("%v --overwrite: want missing path usage error, got %+v", sp, missing)
		}
		bogus := run(t, nil, append(append([]string{}, sp...), "--bogus")...)
		if bogus.code != 64 || !strings.Contains(bogus.err, "unknown flag") || !strings.Contains(bogus.err, "workspace.init") {
			t.Errorf("%v --bogus: exit %d stderr %q", sp, bogus.code, bogus.err)
		}
	}
	// workspace.create ships no flags in wave 1a (first_increment_part: none).
	if got := run(t, nil, "workspace", "create", "--help"); strings.Contains(got.stdout, "--cpus") {
		t.Errorf("workspace create must not advertise --cpus yet:\n%s", got.stdout)
	}
}

func isBoolFlag(flag string) bool {
	switch flag {
	case "--non-interactive", "--yes", "--dry-run", "--no-agents-block", "--json":
		return true
	}
	return false
}

func TestUnknownCommandHint(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"wokspace"}, `did you mean "whr workspace"?`},
		{[]string{"worksapce", "ls"}, `did you mean "whr workspace"?`},
		{[]string{"lss"}, `did you mean "whr ls"?`},
		{[]string{"workspace", "lss"}, `did you mean "whr workspace ls"?`},
		{[]string{"ws", "valdate"}, `did you mean "whr workspace validate"?`},
		{[]string{"host", "service", "instal"}, `did you mean "whr host service install"?`},
		{[]string{"vrsion"}, `did you mean "whr version"?`},
		// "lsh" is one edit from both ls and ssh: the first in table order wins, deterministically.
		{[]string{"ws", "lsh"}, `did you mean "whr workspace ls"?`},
		{[]string{"ws", "zzzzzzzz"}, ``},
		{[]string{"zzzzzzzzzz"}, ``},
	}
	for _, tc := range cases {
		got := run(t, nil, tc.args...)
		if got.code != 64 || got.stdout != "" {
			t.Errorf("%v: exit %d stdout %q", tc.args, got.code, got.stdout)
		}
		lines := strings.Split(strings.TrimSuffix(got.err, "\n"), "\n")
		if !strings.HasPrefix(lines[0], "whr: unknown command ") || strings.Contains(lines[0], "did you mean") {
			t.Errorf("%v: first line %q is not a bare unknown-command error", tc.args, lines[0])
		}
		if tc.want != "" {
			if len(lines) != 2 || !strings.HasPrefix(lines[1], "  hint: ") || !strings.Contains(lines[1], tc.want) {
				t.Errorf("%v: stderr %q lacks the hint line %q", tc.args, got.err, tc.want)
			}
		} else if len(lines) != 1 {
			t.Errorf("%v: unexpected hint in %q", tc.args, got.err)
		}
	}
	// A hostile word is quoted, not echoed raw.
	got := run(t, nil, "bad\x1b]0;pwned\x07\nwhr: ok")
	if got.code != 64 || strings.ContainsAny(got.err, "\x1b\x07") || strings.Count(got.err, "\n") > 2 {
		t.Errorf("hostile unknown command: exit %d stderr %q", got.code, got.err)
	}
	env := decodeEnvelope(t, run(t, nil, "--json", "bad\x1b\u202e").stdout)
	if env["command"] != "unknown" {
		t.Errorf("command = %v, want unknown", env["command"])
	}
}

func TestGlobalFlagsAndVersion(t *testing.T) {
	human := run(t, nil, "version")
	if human.code != 0 || human.stdout != version.Current().Human() || human.err != "" {
		t.Fatalf("version: %+v", human)
	}
	if flag := run(t, nil, "--version"); flag != human {
		t.Errorf("--version differs from version: %+v", flag)
	}
	if flag := run(t, nil, "--version", "--json"); flag != run(t, nil, "version", "--json") {
		t.Errorf("--version --json differs from version --json")
	}
	if got := run(t, nil, "--version", "setup"); got.code != 64 || got.stdout != "" {
		t.Errorf("--version setup: %+v", got)
	}
	if got := run(t, nil, "--version", "extra"); got.code != 64 || got.err != "whr: --version takes no other arguments\n" {
		t.Errorf("--version extra: %+v", got)
	}
	if got := run(t, nil, "version", "extra"); got.code != 64 || got.stdout != "" {
		t.Errorf("version extra: %+v", got)
	}
	for _, args := range [][]string{{"--color", "sometimes", "version"}, {"--color"}, {"--bogus"}, {"version", "--bogus"}} {
		if got := run(t, nil, args...); got.code != 64 || got.stdout != "" || !strings.HasPrefix(got.err, "whr: ") {
			t.Errorf("%v: %+v", args, got)
		}
	}
	for _, args := range [][]string{{"--color", "always", "version"}, {"--color=never", "version"}, {"--no-color", "version"}} {
		if got := run(t, nil, args...); got != human {
			t.Errorf("%v changed the version output: %+v", args, got)
		}
	}
}

func TestHandlerErrorsMapToExitCodes(t *testing.T) {
	h := stubHandlers()
	fail := func(err error) {
		h["workspace.ls"] = func(context.Context, commands.Call) (commands.Result, error) { return commands.Result{}, err }
	}
	for _, tc := range []struct {
		err  error
		want int
		name string
	}{
		{exitcode.Errorf(exitcode.PathTargetAbsent, "no such directory"), 65, "not_found"},
		{exitcode.Errorf(exitcode.PathWrongState, "invalid"), 68, "conflict"},
		{context.Canceled, 72, "interrupted"},
		{context.DeadlineExceeded, 71, "timeout"},
		{context.Canceled, 72, "interrupted"},
		{errString("boom"), 74, "internal"},
	} {
		fail(tc.err)
		got := run(t, h, "ls", "--json")
		env := decodeEnvelope(t, got.stdout)
		errBody, _ := env["error"].(map[string]any)
		if got.code != tc.want || env["exit_code"] != float64(tc.want) || errBody["code"] != tc.name {
			t.Errorf("%v: exit %d, envelope %v", tc.err, got.code, env)
		}
		if !strings.HasPrefix(got.err, "whr: ") {
			t.Errorf("%v: stderr %q", tc.err, got.err)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestRootFlagErrorHasNoDoubledPrefix(t *testing.T) {
	got := run(t, nil, "--bogus")
	if !strings.HasPrefix(got.err, "whr: unknown flag: --bogus\n") {
		t.Errorf("stderr = %q", got.err)
	}
	if got := run(t, nil, "version", "--bogus"); !strings.HasPrefix(got.err, "whr: version: unknown flag") {
		t.Errorf("stderr = %q", got.err)
	}
}

func TestHumanErrorsAreBounded(t *testing.T) {
	for _, tc := range []struct {
		name, unit string
		min        int // minimum message runes (placeholders may stop short of the limit)
	}{{"ascii", "x", 1024}, {"accent", "\u00e9", 1024}, {"control", "\x01", 1016}, {"bidi", "\u202e", 1016}} {
		huge := strings.Repeat(tc.unit, 100000)
		for _, args := range [][]string{{"--" + huge}, {"--color", huge, "version"}, {"completion", huge}} {
			got := run(t, nil, args...)
			line := strings.SplitN(got.err, "\n", 2)[0]
			n := utf8.RuneCountInString(strings.TrimPrefix(line, "whr: "))
			if got.code != 64 || n > 1024 || n < tc.min || (tc.min == 1024 && n != 1024) {
				t.Errorf("%s %.12q: exit %d, message of %d runes (want %d..1024)", tc.name, args[0], got.code, n, tc.min)
			}
			if len(got.err) > 9000 {
				t.Errorf("%s: stderr %d bytes", tc.name, len(got.err))
			}
			js := run(t, nil, append(append([]string{}, args...), "--json")...)
			if len(js.stdout) > 9000 || utf8.RuneCountInString(strings.SplitN(js.err, "\n", 2)[0]) > 5+1024 {
				t.Errorf("%s: JSON run wrote %d stdout bytes, stderr line of %d runes", tc.name, len(js.stdout), len(strings.SplitN(js.err, "\n", 2)[0]))
			}
		}
	}
	// The echoed unknown word is bounded by maxEcho, not only by the line limit.
	huge := strings.Repeat("x", 100000)
	want := "whr: unknown command \"" + strings.Repeat("x", 64) + "...\"\n"
	if got := run(t, nil, huge); got.err != want {
		t.Errorf("unknown word echo = %q, want %q", got.err, want)
	}
}

func TestScanJSONForms(t *testing.T) {
	isJSON := func(args ...string) bool { return strings.HasPrefix(run(t, nil, args...).stdout, "{") }
	for _, args := range [][]string{{"ls", "--json"}, {"ls", "--json=true"}, {"ls", "--json=T"}, {"ls", "--json=TRUE"}, {"ls", "--json=1"}, {"ls", "--json=True"}, {"ls", "--json=t"}} {
		if !isJSON(args...) {
			t.Errorf("%v is not JSON mode", args)
		}
	}
	for _, args := range [][]string{{"ls", "--json=false"}, {"ls", "--json=0"}, {"ls", "--", "--json"}, {"ls", "--no-json"}} {
		if got := run(t, nil, args...); isJSON(args...) || got.code != 64 {
			t.Errorf("%v: JSON mode or wrong exit: %+v", args, got)
		}
	}
	// A flag error before --json is parsed is still an envelope.
	for _, form := range []string{"--json", "--json=true", "--json=T", "--json=TRUE", "--json=True", "--json=t", "--json=1"} {
		if !isJSON("version", "--bogus", form) {
			t.Errorf("a flag error before %s is not an envelope", form)
		}
	}
	if isJSON("version", "--bogus", "--", "--json") {
		t.Error("--json after the -- terminator must not select JSON mode")
	}
	if !isJSON("version", "--bogus", "--json") || !isJSON("--bogus", "--json") {
		t.Error("flag errors with --json must be envelopes")
	}
}

func TestFlagErrorKeepsHintAndCommandID(t *testing.T) {
	got := run(t, nil, "version", "--bogus")
	if got.err != "whr: version: unknown flag: --bogus\n  hint: run whr version --help\n" {
		t.Errorf("stderr = %q", got.err)
	}
	if env := decodeEnvelope(t, run(t, nil, "version", "--bogus", "--json").stdout); env["command"] != "version" {
		t.Errorf("flag error command = %v, want the canonical id", env["command"])
	}
	if env := decodeEnvelope(t, run(t, nil, "--bogus", "--json").stdout); env["command"] != "unknown" {
		t.Errorf("root flag error command = %v, want unknown", env["command"])
	}
}

func TestSuggestionThresholds(t *testing.T) {
	hint := func(args ...string) bool { return strings.Contains(run(t, nil, args...).err, "did you mean") }
	for _, ok := range [][]string{{"ws", "lx"}, {"ws", "creat"}, {"ws", "crxate"}, {"ws", "rbuild"}} {
		if !hint(ok...) {
			t.Errorf("%v: expected a hint (distance within 2)", ok)
		}
	}
	for _, none := range [][]string{{"ws", "creatxyz"}, {"ws", "l"}, {"ws", "q"}, {"ws", "xxxxxx"}} {
		if hint(none...) {
			t.Errorf("%v: unexpected hint", none)
		}
	}
}
func TestJSONWithHelpAndGroupsKeepsExitCode(t *testing.T) {
	for _, args := range [][]string{{"--json"}, {"ws", "--json"}, {"host", "--json"}, {"host", "service", "--json"}, {"--help", "--json"},
		{"ls", "--help", "--json"}, {"--json", "-h"}, {"help", "ls", "--json"}, {"service", "--json"}} {
		got := run(t, stubHandlers(), args...)
		env := decodeEnvelope(t, got.stdout)
		if got.code != 64 || env["exit_code"] != float64(64) || env["ok"] != false {
			t.Errorf("%v: exit %d, envelope %v", args, got.code, env)
		}
	}
}

func TestContextCancellationAndDeadline(t *testing.T) {
	observe := func(ctx context.Context, _ commands.Call) (commands.Result, error) {
		select {
		case <-ctx.Done():
			return commands.Result{}, ctx.Err()
		default:
			return commands.Result{Human: "ran\n"}, nil
		}
	}
	h := stubHandlers()
	h["workspace.ls"] = observe
	exec := func(ctx context.Context, args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := commands.Execute(ctx, args, commands.Env{Stdout: &out, Stderr: &errOut}, h)
		return code, out.String()
	}
	if code, _ := exec(context.Background(), "ls"); code != 0 {
		t.Fatalf("live context: exit %d", code)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if code, out := exec(cancelled, "ls", "--json"); code != 72 || !strings.Contains(out, `"code":"interrupted"`) {
		t.Errorf("cancelled context: exit %d stdout %q, want 72 interrupted", code, out)
	}
	expired, stop := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer stop()
	if code, out := exec(expired, "ws", "ls", "--json"); code != 71 || !strings.Contains(out, `"code":"timeout"`) {
		t.Errorf("expired deadline: exit %d stdout %q, want 71 timeout", code, out)
	}
}

func TestCompletionOfArgumentsIsNotFiles(t *testing.T) {
	for _, h := range []map[string]commands.Handler{nil, stubHandlers()} {
		got := run(t, h, "__complete", "completion", "")
		for _, shell := range []string{"bash", "zsh", "fish"} {
			if !strings.Contains(got.stdout, shell+"\n") && !strings.Contains(got.stdout, shell+"\t") {
				t.Errorf("completion argument lacks %s: %q", shell, got.stdout)
			}
		}
		if !strings.Contains(got.stdout, ":4\n") {
			t.Errorf("completion argument allows file completion: %q", got.stdout)
		}
		if after := run(t, h, "__complete", "completion", "bash", ""); after.stdout != ":4\n" {
			t.Errorf("completion after the shell argument: %q", after.stdout)
		}
		for _, args := range [][]string{{"ws", ""}, {"workspace", "ls", ""}, {"ls", ""}, {"help", ""}, {"version", ""}, {"host", "service", ""}} {
			got := run(t, h, append([]string{"__complete"}, args...)...)
			if !strings.Contains(got.stdout, ":4\n") && !strings.Contains(got.stdout, ":0\n") {
				t.Errorf("%v: %q", args, got.stdout)
			}
			if strings.Contains(got.stdout, ":0\n") || strings.Contains(got.stdout, ":1\n") {
				t.Errorf("%v falls back to file completion: %q", args, got.stdout)
			}
		}
	}
}

// assertCurrentTestIDs checks names against the actual runnable declarations in
// this contract's fixed test file, without keeping a second name registry.
func assertCurrentTestIDs(t *testing.T, ids []string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "table_test.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	for _, node := range file.Decls {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Type.Params.NumFields() != 1 || (fn.Type.Results != nil && fn.Type.Results.NumFields() != 0) || (fn.Type.TypeParams != nil && fn.Type.TypeParams.NumFields() != 0) {
			continue
		}
		pointer, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		selector, ok := pointer.X.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		pkg, ok := selector.X.(*ast.Ident)
		if ok && pkg.Name == "testing" && selector.Sel.Name == "T" && strings.HasPrefix(fn.Name.Name, "Test") {
			declared[fn.Name.Name] = true
		}
	}
	if len(ids) == 0 {
		t.Error("contract has no current test IDs")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !declared[id] {
			t.Errorf("contract test ID %q has no runnable declaration in table_test.go", id)
		}
		if seen[id] {
			t.Errorf("duplicate contract test ID %q", id)
		}
		seen[id] = true
	}
}

// FR-066 is a local-only amendment: remote metadata and every other deferred
// command remain blocked. Source and contract must change together.
func TestLocalLsContractAmendment(t *testing.T) {
	for _, e := range commands.Table() {
		if e.ID == "workspace.ls" {
			if strings.Join(e.Flags, ",") != "--local,--role,--label,--json" || e.BlockedOn != "owner_api" || e.Needs != "control_plane" || e.Writes != "none" {
				t.Fatalf("unexpected local/remote amendment: %+v", e)
			}
			got := run(t, nil, "ls", "--help")
			for _, flag := range []string{"--local", "--role", "--label", "owner_api", "5 s"} {
				if !strings.Contains(got.stdout, flag) {
					t.Errorf("help lacks %s", flag)
				}
			}
			return
		}
	}
	t.Fatal("workspace.ls missing")
}

func TestLocalShellContractAmendment(t *testing.T) {
	found := 0
	for _, e := range commands.Table() {
		if e.ID == "workspace.shell" || e.ID == "workspace.console" {
			found++
			if strings.Join(e.Flags, ",") != "--local" || e.JSON != "events" || e.BlockedOn != "owner_api" || e.Needs != "control_plane" || e.Writes != "none" {
				t.Fatalf("local mode changed remote contract: %+v", e)
			}
		}
	}
	if found != 2 {
		t.Fatal("shell/console registry entries missing")
	}
}

func TestLocalStageCommandContractAndAlias(t *testing.T) {
	var entry *commands.Entry
	for _, e := range commands.Table() {
		if e.ID == "workspace.stage" {
			copy := e
			entry = &copy
		}
	}
	want := []string{"--local", "--source", "--smoke", "--dry-run", "--trust", "--yes", "--json"}
	if entry == nil || !reflect.DeepEqual(entry.Flags, want) || len(entry.Short) != 0 || entry.Needs != "none" || entry.Writes != "local" {
		t.Fatalf("stage command entry: %+v", entry)
	}
	if commands.DefaultHandlers()["workspace.stage"] == nil {
		t.Fatal("production stage handler absent")
	}
	for _, group := range []string{"workspace", "ws"} {
		handlers := baseHandlers()
		called := false
		handlers["workspace.stage"] = func(context.Context, commands.Call) (commands.Result, error) {
			called = true
			return commands.Result{Value: stageCLIResult()}, nil
		}
		got := run(t, handlers, group, "stage", "--local", "--source", "/source", "--dry-run", "primary", "--json")
		if got.code != 0 || !called {
			t.Fatalf("dispatch: %+v called=%t", got, called)
		}
		envelope := decodeEnvelope(t, got.stdout)
		if envelope["command"] != "workspace.stage" {
			t.Fatalf("canonical command: %v", envelope)
		}
	}
}

func TestLocalHomeCopyCommandContract(t *testing.T) {
	want := map[string][]string{
		"workspace.home.clone":  {"--local", "--to", "--exclude", "--include-credentials", "--dry-run", "--trust", "--yes", "--json"},
		"workspace.home.backup": {"--local", "--to", "--dry-run", "--trust", "--yes", "--json"},
	}
	for _, e := range commands.Table() {
		flags, ok := want[e.ID]
		if !ok {
			continue
		}
		delete(want, e.ID)
		if !reflect.DeepEqual(e.Flags, flags) || e.Short != nil || e.Needs != "none" || e.Writes != "local" || e.JSON != "envelope" || e.Increment != "first" {
			t.Errorf("%s entry: %+v", e.ID, e)
		}
		if commands.DefaultHandlers()[e.ID] == nil {
			t.Errorf("%s has no production handler", e.ID)
		}
		help := run(t, nil, append(append([]string{}, e.Path...), "--help")...)
		for _, f := range append(flags, "--trust", "fingerprint", "new volume") {
			if !strings.Contains(help.stdout, f) {
				t.Errorf("%s help lacks %s:\n%s", e.ID, f, help.stdout)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing entries %v", want)
	}
}

// SPDX-License-Identifier: EUPL-1.2

package exitcode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/output"
)

type contractFile struct {
	SchemaVersion int `json:"schema_version"`
	CLIRange      struct {
		Min      int   `json:"min"`
		Max      int   `json:"max"`
		Reserved []int `json:"reserved_unassigned"`
	} `json:"cli_range"`
	Codes []struct {
		Code     int    `json:"code"`
		Name     string `json:"name"`
		Category string `json:"category"`
	} `json:"codes"`
	Remote struct {
		Range struct {
			Min int `json:"min"`
			Max int `json:"max"`
		} `json:"passthrough_range"`
		Kinds        []string `json:"json_exit_event_kind"`
		TransportLos int      `json:"transport_loss"`
	} `json:"remote_status"`
	Freeze struct {
		TestIDs []string `json:"test_ids"`
	} `json:"freeze"`
}

func loadContract(t *testing.T) contractFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "cli", "exit-codes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c contractFile
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestExitTableMatchesContract(t *testing.T) {
	c := loadContract(t)
	table := exitcode.Table()
	if len(table) != len(c.Codes) {
		t.Fatalf("table has %d entries, contract has %d", len(table), len(c.Codes))
	}
	for i, want := range c.Codes {
		got := table[i]
		if got.Code != want.Code || got.Name != want.Name || got.Category != want.Category {
			t.Errorf("entry %d = %+v, contract = %+v", i, got, want)
		}
	}
	if exitcode.RemoteMin != c.Remote.Range.Min || exitcode.RemoteMax != c.Remote.Range.Max {
		t.Errorf("remote passthrough range %d-%d, contract %d-%d", exitcode.RemoteMin, exitcode.RemoteMax, c.Remote.Range.Min, c.Remote.Range.Max)
	}
	if int(exitcode.Interrupted) != c.Remote.TransportLos {
		t.Errorf("transport loss code %d, contract %d", exitcode.Interrupted, c.Remote.TransportLos)
	}
	if len(c.Remote.Kinds) != 2 || c.Remote.Kinds[0] != string(exitcode.KindCLI) || c.Remote.Kinds[1] != string(exitcode.KindRemote) {
		t.Errorf("exit event kinds %v do not match %q and %q", c.Remote.Kinds, exitcode.KindCLI, exitcode.KindRemote)
	}
}

func TestExitTableCodesUniqueAndInRange(t *testing.T) {
	c := loadContract(t)
	seen := map[int]bool{}
	names := map[string]bool{}
	for _, e := range exitcode.Table() {
		if seen[e.Code] || names[e.Name] {
			t.Errorf("duplicate entry %+v", e)
		}
		seen[e.Code], names[e.Name] = true, true
		if e.Code != 0 && (e.Code < c.CLIRange.Min || e.Code > c.CLIRange.Max) {
			t.Errorf("code %d outside %d-%d", e.Code, c.CLIRange.Min, c.CLIRange.Max)
		}
	}
	for _, r := range c.CLIRange.Reserved {
		if seen[r] {
			t.Errorf("reserved code %d is assigned", r)
		}
		if _, ok := exitcode.Name(r); ok {
			t.Errorf("Name(%d) resolves a reserved code", r)
		}
	}
	for _, id := range c.Freeze.TestIDs {
		if id == "" {
			t.Error("empty freeze id")
		}
	}
}

func TestEveryExitPathMapsToCode(t *testing.T) {
	// SC-012 stays open until T017: this covers the paths that exist today.
	want := map[exitcode.Path]exitcode.Code{
		exitcode.PathBadArguments:           64,
		exitcode.PathUnsupportedJSON:        64,
		exitcode.PathUnavailableCommand:     64,
		exitcode.PathUnknownCommand:         64,
		exitcode.PathTargetAbsent:           65,
		exitcode.PathSessionInterfaceAbsent: 65,
		exitcode.PathNoCredential:           66,
		exitcode.PathNotAuthorized:          67,
		exitcode.PathConflict:               68,
		exitcode.PathWrongState:             68,
		exitcode.PathIdempotencyMismatch:    68,
		exitcode.PathIncompatibleVersion:    69,
		exitcode.PathHumanDecisionRefused:   70,
		exitcode.PathHumanDecisionMissing:   70,
		exitcode.PathDeadlineExceeded:       71,
		exitcode.PathUserInterrupt:          72,
		exitcode.PathTransportLost:          72,
		exitcode.PathPartialFailure:         73,
		exitcode.PathUnclassified:           74,
	}
	for path, code := range want {
		if got := path.Code(); got != code {
			t.Errorf("%s maps to %d, want %d", path, got, code)
		}
		if _, ok := exitcode.Name(int(path.Code())); !ok {
			t.Errorf("%s maps to code %d missing from the table", path, path.Code())
		}
	}
	if len(want) != len(exitcode.Paths()) {
		t.Errorf("test covers %d paths, package declares %d", len(want), len(exitcode.Paths()))
	}
	// A missing interface is not-found, an incompatible one is 69 (ADR 0013).
	if exitcode.PathSessionInterfaceAbsent.Code() == exitcode.PathIncompatibleVersion.Code() {
		t.Error("missing and incompatible interface share a code")
	}
}

func TestFromErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		want exitcode.Code
	}{
		{nil, 0},
		{exitcode.Errorf(exitcode.PathBadArguments, "bad flag"), 64},
		{fmt.Errorf("wrapped: %w", exitcode.Errorf(exitcode.PathConflict, "busy")), 68},
		{context.DeadlineExceeded, 71},
		{fmt.Errorf("op: %w", context.Canceled), 72},
		{errors.New("boom"), 74},
	}
	for _, tc := range cases {
		if got := exitcode.FromError(tc.err); got != tc.want {
			t.Errorf("FromError(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

func TestRemoteStatusPassthrough0_1_255(t *testing.T) {
	// Table level only: the interactive commands arrive with T014 and later.
	for _, status := range []int{0, 1, 2, 64, 72, 126, 127, 130, 255} {
		got, err := exitcode.Remote(status)
		if err != nil {
			t.Fatalf("Remote(%d): %v", status, err)
		}
		if got.Code != status || got.Kind != exitcode.KindRemote {
			t.Errorf("Remote(%d) = %+v, want the status unchanged with kind remote", status, got)
		}
	}
	for _, status := range []int{-1, 256} {
		if _, err := exitcode.Remote(status); err == nil {
			t.Errorf("Remote(%d) accepted an out-of-range status", status)
		}
	}
	lost := exitcode.TransportLost()
	if lost.Code != 72 || lost.Kind != exitcode.KindCLI {
		t.Errorf("TransportLost() = %+v, want 72 with kind cli", lost)
	}
}

func TestCLIFailureAnnouncedOnStderrAndKindCLI(t *testing.T) {
	// A CLI failure prints a whr: line in human and JSON mode and is a CLI
	// status; a remote status prints no whr: line and is marked remote.
	for _, asJSON := range []bool{false, true} {
		var out, errOut bytes.Buffer
		w := &output.Writer{Out: &out, Err: &errOut, JSON: asJSON}
		code := w.Failure("workspace.ls", exitcode.Errorf(exitcode.PathUnavailableCommand, "not available"))
		if code != exitcode.Usage {
			t.Fatalf("json=%v: code %d", asJSON, code)
		}
		if !strings.HasPrefix(errOut.String(), "whr: not available\n") {
			t.Errorf("json=%v: stderr %q lacks the whr: line", asJSON, errOut.String())
		}
		if asJSON {
			var env struct {
				ExitCode int `json:"exit_code"`
			}
			if err := json.Unmarshal(out.Bytes(), &env); err != nil || env.ExitCode != 64 {
				t.Errorf("envelope %q: %v", out.String(), err)
			}
		}
		if got := exitcode.CLI(code); got.Kind != exitcode.KindCLI {
			t.Errorf("CLI status kind = %q", got.Kind)
		}
	}
	remote, err := exitcode.Remote(64)
	if err != nil || remote.Kind != exitcode.KindRemote || remote.Code != 64 {
		t.Fatalf("Remote(64) = %+v, %v", remote, err)
	}
}

// SPDX-License-Identifier: EUPL-1.2

package sandbox_test

import (
	"errors"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/runtime"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/sandbox"
)

func rec(id string, state sandbox.State, ret sandbox.Retention, res sandbox.ResultsState) sandbox.Record {
	return sandbox.Record{ID: sandbox.ID(id), Workspace: "w1", Profile: runtime.ProfileUntrustedJob, Provider: "fake", State: state, Retention: ret, Results: res}
}

func TestKeepThenCleanKeeps(t *testing.T) {
	a := rec("sbx-aaaaaaaaaaaaaaaa", sandbox.StateFinished, sandbox.RetentionEphemeral, sandbox.ResultsExported)
	b := rec("sbx-bbbbbbbbbbbbbbbb", sandbox.StateFailed, sandbox.RetentionEphemeral, sandbox.ResultsNone)
	c := rec("sbx-cccccccccccccccc", sandbox.StateFinished, sandbox.RetentionEphemeral, sandbox.ResultsUnexported)
	d := rec("sbx-dddddddddddddddd", sandbox.StateRunning, sandbox.RetentionEphemeral, sandbox.ResultsNone)
	e := rec("sbx-eeeeeeeeeeeeeeee", sandbox.StateDiscarded, sandbox.RetentionEphemeral, sandbox.ResultsNone)

	// Before keep, clean takes finished or failed ephemeral instances with no unexported results.
	got := sandbox.CleanCandidates([]sandbox.Record{a, b, c, d, e})
	if len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("clean candidates %+v", got)
	}

	kept, err := sandbox.Keep(a)
	if err != nil || kept.Retention != sandbox.RetentionKept {
		t.Fatalf("keep: %+v, %v", kept, err)
	}
	if a.Retention != sandbox.RetentionEphemeral {
		t.Error("Keep must not modify its argument")
	}
	// keep is idempotent and implies nothing else: state and results unchanged.
	again, err := sandbox.Keep(kept)
	if err != nil || again != kept || kept.State != a.State || kept.Results != a.Results {
		t.Errorf("keep not idempotent or changed other fields: %+v, %v", again, err)
	}

	// After keep, clean leaves the instance.
	got = sandbox.CleanCandidates([]sandbox.Record{kept, b})
	if len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("a kept instance must survive clean: %+v", got)
	}
	if sandbox.Cleanable(kept) {
		t.Error("kept instance is cleanable")
	}
	// keep blocks clean, not discard.
	if err := sandbox.CheckDiscard(kept, sandbox.DiscardOptions{}); err != nil {
		t.Errorf("a kept instance can be discarded: %v", err)
	}

	// Keep is refused once discarding has begun.
	for _, s := range []sandbox.State{sandbox.StateDiscarding, sandbox.StateDiscarded, sandbox.StateCleanupIncomplete} {
		_, err := sandbox.Keep(rec("sbx-aaaaaaaaaaaaaaaa", s, sandbox.RetentionEphemeral, sandbox.ResultsNone))
		if code, _ := runtime.RefusalCodeOf(err); code != runtime.RefusedInvalidState {
			t.Errorf("keep in %q: %v", s, err)
		}
	}
	// A retention outside the model is not accepted.
	if sandbox.Retention("forever").Valid() || !sandbox.RetentionKept.Valid() || !sandbox.RetentionEphemeral.Valid() {
		t.Error("retention validity")
	}

	// Lookup of an unknown id is not found (exit 65 later), not a refusal.
	if _, err := sandbox.Lookup([]sandbox.Record{a}, "sbx-zzzzzzzzzzzzzzzz"); !errors.Is(err, sandbox.ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
	if r, err := sandbox.Lookup([]sandbox.Record{a, b}, b.ID); err != nil || r.ID != b.ID {
		t.Errorf("lookup: %+v %v", r, err)
	}
}

func TestDiscardRefusesUnexportedResults(t *testing.T) {
	type row struct {
		name  string
		rec   sandbox.Record
		opts  sandbox.DiscardOptions
		check func(error) bool
	}
	isCode := func(c runtime.RefusalCode) func(error) bool {
		return func(err error) bool { got, ok := runtime.RefusalCodeOf(err); return ok && got == c }
	}
	isNil := func(err error) bool { return err == nil }
	needsHuman := func(err error) bool { return errors.Is(err, sandbox.ErrConfirmationRequired) }
	r := func(s sandbox.State, res sandbox.ResultsState) sandbox.Record {
		return rec("sbx-aaaaaaaaaaaaaaaa", s, sandbox.RetentionEphemeral, res)
	}
	rows := []row{
		{"exported results", r(sandbox.StateFinished, sandbox.ResultsExported), sandbox.DiscardOptions{}, isNil},
		{"no results", r(sandbox.StateReady, sandbox.ResultsNone), sandbox.DiscardOptions{}, isNil},
		{"failed with no results", r(sandbox.StateFailed, sandbox.ResultsNone), sandbox.DiscardOptions{}, isNil},
		{"unexported results", r(sandbox.StateFinished, sandbox.ResultsUnexported), sandbox.DiscardOptions{}, isCode(runtime.RefusedUnexportedResults)},
		{"unexported results of a failed instance", r(sandbox.StateFailed, sandbox.ResultsUnexported), sandbox.DiscardOptions{}, isCode(runtime.RefusedUnexportedResults)},
		{"unexported but confirmed", r(sandbox.StateFinished, sandbox.ResultsUnexported), sandbox.DiscardOptions{ConfirmResultsDeletion: true}, isNil},
		{"unexported, unattended, unconfirmed", r(sandbox.StateFinished, sandbox.ResultsUnexported), sandbox.DiscardOptions{Unattended: true}, needsHuman},
		{"unexported, unattended, confirmed", r(sandbox.StateFinished, sandbox.ResultsUnexported), sandbox.DiscardOptions{Unattended: true, ConfirmResultsDeletion: true}, isNil},
		{"unattended with nothing to lose", r(sandbox.StateFinished, sandbox.ResultsExported), sandbox.DiscardOptions{Unattended: true}, isNil},
		{"running cannot be discarded", r(sandbox.StateRunning, sandbox.ResultsNone), sandbox.DiscardOptions{ConfirmResultsDeletion: true}, isCode(runtime.RefusedInvalidState)},
		{"preparing cannot be discarded", r(sandbox.StatePreparing, sandbox.ResultsNone), sandbox.DiscardOptions{}, isCode(runtime.RefusedInvalidState)},
		{"discarded cannot be discarded again", r(sandbox.StateDiscarded, sandbox.ResultsNone), sandbox.DiscardOptions{}, isCode(runtime.RefusedInvalidState)},
		{"discarding cannot be restarted", r(sandbox.StateDiscarding, sandbox.ResultsNone), sandbox.DiscardOptions{}, isCode(runtime.RefusedInvalidState)},
		{"cleanup_incomplete retries", r(sandbox.StateCleanupIncomplete, sandbox.ResultsNone), sandbox.DiscardOptions{}, isNil},
		{"state is checked before results", r(sandbox.StateRunning, sandbox.ResultsUnexported), sandbox.DiscardOptions{Unattended: true}, isCode(runtime.RefusedInvalidState)},
	}
	for _, x := range rows {
		t.Run(x.name, func(t *testing.T) {
			if err := sandbox.CheckDiscard(x.rec, x.opts); !x.check(err) {
				t.Errorf("CheckDiscard = %v", err)
			}
		})
	}
	// A results state outside the model is treated as unexported (fail closed).
	odd := r(sandbox.StateFinished, "mystery")
	if err := sandbox.CheckDiscard(odd, sandbox.DiscardOptions{}); err == nil {
		t.Error("an unknown results state must not allow discard")
	}
	if sandbox.Cleanable(odd) {
		t.Error("an unknown results state must not be cleanable")
	}
}

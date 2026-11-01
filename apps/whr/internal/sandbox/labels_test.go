// SPDX-License-Identifier: EUPL-1.2

package sandbox_test

import (
	"errors"
	"maps"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/runtime"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/sandbox"
)

func goodLabels() sandbox.OwnershipLabels {
	return sandbox.OwnershipLabels{
		ID:                "sbx-aaaaaaaaaaaaaaaa",
		Workspace:         "ws-1",
		Profile:           runtime.ProfileUntrustedJob,
		Provider:          "fake",
		Retention:         sandbox.RetentionEphemeral,
		Created:           time.Date(2026, 10, 31, 12, 30, 5, 0, time.UTC),
		GitProfile:        "1:0123abcd",
		SharedGitMetadata: sandbox.SharedGitNone,
	}
}

func TestLabelsRoundTrip(t *testing.T) {
	l := goodLabels()
	m := l.Map()
	want := map[string]string{
		"io.workharbor.sandbox":                     "1",
		"io.workharbor.sandbox.id":                  "sbx-aaaaaaaaaaaaaaaa",
		"io.workharbor.sandbox.workspace":           "ws-1",
		"io.workharbor.sandbox.profile":             "untrusted_job",
		"io.workharbor.sandbox.provider":            "fake",
		"io.workharbor.sandbox.retention":           "ephemeral",
		"io.workharbor.sandbox.created":             "2026-10-31T12:30:05Z",
		"io.workharbor.sandbox.schema":              "1",
		"io.workharbor.sandbox.gitprofile":          "1:0123abcd",
		"io.workharbor.sandbox.shared_git_metadata": "none",
	}
	if !maps.Equal(m, want) {
		t.Fatalf("label map\n got %v\nwant %v", m, want)
	}
	keys := sandbox.LabelKeys()
	var wantKeys []string
	for k := range want {
		wantKeys = append(wantKeys, k)
	}
	sort.Strings(wantKeys)
	got := append([]string(nil), keys...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, wantKeys) {
		t.Errorf("LabelKeys %v, want %v", got, wantKeys)
	}

	back, err := sandbox.ParseLabels(m)
	if err != nil || back != l {
		t.Fatalf("round trip: %+v, %v", back, err)
	}

	// Other labels on the object are ignored; ours are still read.
	m["com.example.other"] = "x"
	if back, err := sandbox.ParseLabels(m); err != nil || back != l {
		t.Errorf("foreign labels beside ours: %+v, %v", back, err)
	}

	// A non-UTC time is written in UTC; sub-second precision is dropped.
	l.Created = time.Date(2026, 10, 31, 14, 30, 5, 999, time.FixedZone("x", 2*3600))
	if got := l.Map()["io.workharbor.sandbox.created"]; got != "2026-10-31T12:30:05Z" {
		t.Errorf("created = %q", got)
	}

	// Every profile and retention survives; cooperative is writable.
	for _, p := range []runtime.TrustProfile{runtime.ProfileDeveloper, runtime.ProfileCooperativeJob, runtime.ProfileUntrustedJob} {
		for _, r := range []sandbox.Retention{sandbox.RetentionEphemeral, sandbox.RetentionKept} {
			l := goodLabels()
			l.Profile, l.Retention = p, r
			if p != runtime.ProfileUntrustedJob {
				l.SharedGitMetadata = sandbox.SharedGitWritable
			}
			back, err := sandbox.ParseLabels(l.Map())
			if err != nil || back != l {
				t.Errorf("%s/%s: %+v, %v", p, r, back, err)
			}
		}
	}
}

// act is the shape of every mutating operation: the gate first, the
// mutation only when it passes.
func act(obj sandbox.Object, id sandbox.ID, mutate func()) error {
	if _, err := sandbox.CheckOwned(obj, id, sandbox.PrimaryEnvironment{}); err != nil {
		return err
	}
	mutate()
	return nil
}

func TestForeignObjectRefused(t *testing.T) {
	base := goodLabels().Map()
	mutate := func(f func(map[string]string)) map[string]string {
		m := maps.Clone(base)
		f(m)
		return m
	}
	cases := map[string]map[string]string{
		"no labels":           nil,
		"empty labels":        {},
		"only the marker":     {"io.workharbor.sandbox": "1"},
		"other labels":        {"com.docker.compose.project": "x"},
		"marker not 1":        mutate(func(m map[string]string) { m["io.workharbor.sandbox"] = "0" }),
		"marker true":         mutate(func(m map[string]string) { m["io.workharbor.sandbox"] = "true" }),
		"no marker":           mutate(func(m map[string]string) { delete(m, "io.workharbor.sandbox") }),
		"schema 2":            mutate(func(m map[string]string) { m["io.workharbor.sandbox.schema"] = "2" }),
		"no schema":           mutate(func(m map[string]string) { delete(m, "io.workharbor.sandbox.schema") }),
		"bad id":              mutate(func(m map[string]string) { m["io.workharbor.sandbox.id"] = "sbx-short" }),
		"no id":               mutate(func(m map[string]string) { delete(m, "io.workharbor.sandbox.id") }),
		"another id":          mutate(func(m map[string]string) { m["io.workharbor.sandbox.id"] = "sbx-bbbbbbbbbbbbbbbb" }),
		"bad profile":         mutate(func(m map[string]string) { m["io.workharbor.sandbox.profile"] = "root" }),
		"bad retention":       mutate(func(m map[string]string) { m["io.workharbor.sandbox.retention"] = "forever" }),
		"empty workspace":     mutate(func(m map[string]string) { m["io.workharbor.sandbox.workspace"] = "" }),
		"newline in provider": mutate(func(m map[string]string) { m["io.workharbor.sandbox.provider"] = "fake\nx" }),
		"offset time":         mutate(func(m map[string]string) { m["io.workharbor.sandbox.created"] = "2026-10-31T14:30:05+02:00" }),
		"junk time":           mutate(func(m map[string]string) { m["io.workharbor.sandbox.created"] = "yesterday" }),
		"no git profile":      mutate(func(m map[string]string) { delete(m, "io.workharbor.sandbox.gitprofile") }),
		"bad shared":          mutate(func(m map[string]string) { m["io.workharbor.sandbox.shared_git_metadata"] = "readonly" }),
		"untrusted with writable shared metadata": mutate(func(m map[string]string) { m["io.workharbor.sandbox.shared_git_metadata"] = "writable" }),
		"cooperative without shared flag":         mutate(func(m map[string]string) { m["io.workharbor.sandbox.profile"] = "cooperative_job" }),
	}
	for name, labels := range cases {
		t.Run(name, func(t *testing.T) {
			mutations := 0
			err := act(sandbox.Object{Kind: sandbox.KindContainer, Name: "c1", Labels: labels}, "sbx-aaaaaaaaaaaaaaaa", func() { mutations++ })
			if code, ok := runtime.RefusalCodeOf(err); !ok || code != runtime.RefusedForeignObject {
				t.Fatalf("want refused.foreign_object, got %v", err)
			}
			if mutations != 0 {
				t.Errorf("a foreign object was mutated %d times", mutations)
			}
			if _, perr := sandbox.ParseLabels(labels); name != "another id" && perr == nil {
				t.Errorf("ParseLabels accepted %v", labels)
			}
		})
	}

	// An owned object runs its mutation exactly once.
	n := 0
	if err := act(sandbox.Object{Kind: sandbox.KindVolume, Name: "v1", Labels: base}, "sbx-aaaaaaaaaaaaaaaa", func() { n++ }); err != nil || n != 1 {
		t.Errorf("owned object: err=%v mutations=%d", err, n)
	}
	// An owned object of this instance passes the gate and returns its labels.
	got, err := sandbox.CheckOwned(sandbox.Object{Kind: sandbox.KindVolume, Name: "v1", Labels: base}, "sbx-aaaaaaaaaaaaaaaa", sandbox.PrimaryEnvironment{})
	if err != nil || got != goodLabels() {
		t.Fatalf("owned object refused: %+v, %v", got, err)
	}
	if _, err := sandbox.CheckOwned(sandbox.Object{Kind: sandbox.KindVolume, Name: "v1", Labels: base}, "not-an-id", sandbox.PrimaryEnvironment{}); !errors.Is(err, sandbox.ErrInvalidID) {
		t.Errorf("an invalid target id is a usage error: %v", err)
	}
}

func TestPrimaryTargetRefused(t *testing.T) {
	primary := sandbox.PrimaryEnvironment{Objects: []sandbox.ObjectRef{
		{Kind: sandbox.KindContainer, Name: "ws-main"},
		{Kind: sandbox.KindVolume, Name: "ws-main-home"},
		{Kind: sandbox.KindDirectory, Name: "/work/repo"},
	}}
	id := sandbox.ID("sbx-aaaaaaaaaaaaaaaa")
	for _, obj := range []sandbox.Object{
		{Kind: sandbox.KindContainer, Name: "ws-main"},                                 // unlabelled primary
		{Kind: sandbox.KindVolume, Name: "ws-main-home", Labels: nil},                  // primary home volume
		{Kind: sandbox.KindDirectory, Name: "/work/repo"},                              // checkout
		{Kind: sandbox.KindContainer, Name: "ws-main", Labels: goodLabels().Map()},     // even a labelled lookalike
		{Kind: sandbox.KindDirectory, Name: "/work/repo/", Labels: goodLabels().Map()}, // trailing slash
	} {
		_, err := sandbox.CheckOwned(obj, id, primary)
		if code, ok := runtime.RefusalCodeOf(err); !ok || code != runtime.RefusedPrimaryTarget {
			t.Errorf("%+v: want refused.primary_target, got %v", obj, err)
		}
	}
	// The same name of another kind is not the primary object; it is just foreign.
	_, err := sandbox.CheckOwned(sandbox.Object{Kind: sandbox.KindVolume, Name: "ws-main"}, id, primary)
	if code, _ := runtime.RefusalCodeOf(err); code != runtime.RefusedForeignObject {
		t.Errorf("same name, other kind: %v", err)
	}
	// Our own labelled object passes.
	if _, err := sandbox.CheckOwned(sandbox.Object{Kind: sandbox.KindContainer, Name: "sbx-c", Labels: goodLabels().Map()}, id, primary); err != nil {
		t.Errorf("owned object refused: %v", err)
	}
}

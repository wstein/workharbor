// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNativeCandidatesQueriesNameAndRuntimeUnionIncludingForeignPartial(t *testing.T) {
	scope := policyFixture(Resources).Scope
	first, second := strings.Repeat("a", 64), strings.Repeat("c", 64)
	calls := [][]string{}
	p := &nativePort{scope: scope, check: func(context.Context) error { return nil }, query: func(_ context.Context, args []string) ([]byte, error) {
		calls = append(calls, append([]string{}, args...))
		if args[0] == "ps" {
			if strings.HasPrefix(args[len(args)-1], "name=") {
				return []byte(first + "\n"), nil
			}
			return []byte(first + "\n" + second + "\n"), nil
		}
		a, b := nativeContainerFixture(), nativeContainerFixture()
		b["Id"] = second
		b["Name"] = "/foreign"
		b["Config"].(map[string]any)["Labels"] = map[string]any{"workharbor.runtime": scope.RuntimeID}
		raw, _ := json.Marshal([]any{a, b})
		return raw, nil
	}}
	all, e := p.Candidates(context.Background(), scope)
	if e != nil || len(all) != 2 || all[1].ID != second || all[1].Name != "/foreign" {
		t.Fatal("union incomplete", all, e)
	}
	if len(calls) != 3 || !strings.HasPrefix(calls[0][len(calls[0])-1], "name=^") || calls[1][len(calls[1])-1] != "label=workharbor.runtime="+scope.RuntimeID || !reflect.DeepEqual(calls[2], []string{"inspect", "--type", "container", first, second}) {
		t.Fatal(calls)
	}
}
func TestNativeReadAuthorizationAndInspectionExactSet(t *testing.T) {
	scope := policyFixture(Resources).Scope
	calls := 0
	p := &nativePort{scope: scope, check: func(context.Context) error { return errors.New("denied") }, query: func(context.Context, []string) ([]byte, error) { calls++; return []byte(`[]`), nil }}
	if _, e := p.Candidates(context.Background(), scope); e == nil || calls != 0 {
		t.Fatal("unauthorized read")
	}
	if _, e := p.Information(context.Background()); e == nil || calls != 0 {
		t.Fatal("unauthorized info")
	}
	p.check = func(context.Context) error { return nil }
	p.query = func(_ context.Context, args []string) ([]byte, error) {
		if args[0] == "ps" {
			return []byte(strings.Repeat("a", 64) + "\n"), nil
		}
		return []byte(`[]`), nil
	}
	if _, e := p.Candidates(context.Background(), scope); e == nil {
		t.Fatal("missing inspected candidate treated absent")
	}
	foreign := scope
	foreign.OwnerID = "00000000-0000-4000-8000-000000000099"
	if _, e := p.Candidates(context.Background(), foreign); e == nil {
		t.Fatal("foreign scope read")
	}
}

func TestNativeInvokeReconstructsOnlyTypedScopeBoundReads(t *testing.T) {
	scope := policyFixture(Resources).Scope
	calls := [][]string{}
	p := &nativePort{scope: scope, check: func(context.Context) error { return nil }, query: func(_ context.Context, args []string) ([]byte, error) {
		calls = append(calls, args)
		return []byte("Docker version 29.8.2, build fixture\n"), nil
	}}
	raw, e := p.Invoke(context.Background(), Call{Scope: scope, Kind: "version"})
	if e != nil || len(raw) == 0 || !reflect.DeepEqual(calls, [][]string{{"-v"}}) {
		t.Fatal("typed read unavailable", e, calls)
	}
	for _, call := range []Call{{Scope: scope, Kind: "arbitrary"}, {Kind: "version"}} {
		if _, e = p.Invoke(context.Background(), call); e == nil {
			t.Fatal("unscoped/unknown call")
		}
	}
	if len(calls) != 1 {
		t.Fatal("unscoped call forwarded")
	}
}

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var at = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// The names on the wire are a contract with plugins: a rename is a breaking
// change, so they are written out here.
func TestEventWireNames(t *testing.T) {
	e := Event{
		Kind: EventUsage, At: at, SessionID: "s1",
		Usage: &Usage{
			Model: "m", Tokens: &TokenCounts{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4},
			Cost:    &Cost{MicroUSD: 5, Source: CostReported},
			Windows: []UsageWindow{{Name: WindowFiveHour, Utilization: 0.5, ResetsAt: at}},
		},
	}
	got, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"usage","at":"2026-10-01T12:00:00Z","session_id":"s1","usage":{"model":"m","tokens":{"input":1,"output":2,"cache_read":3,"cache_write":4},` +
		`"cost":{"micro_usd":5,"source":"reported"},` +
		`"windows":[{"name":"five_hour","utilization":0.5,"resets_at":"2026-10-01T12:00:00Z"}]}}`
	if string(got) != want {
		t.Errorf("event JSON\n got %s\nwant %s", got, want)
	}

	a := Event{Kind: EventApproval, At: at, Tool: "Bash", Input: "ls", Approval: &ApprovalRecord{ID: "a1", Allow: false, Reason: "no"}}
	got, _ = json.Marshal(a)
	want = `{"kind":"approval","at":"2026-10-01T12:00:00Z","tool":"Bash","input":"ls","approval":{"id":"a1","allow":false,"reason":"no"}}`
	if string(got) != want {
		t.Errorf("approval event JSON\n got %s\nwant %s", got, want)
	}
}

func TestWireTypesRoundTrip(t *testing.T) {
	values := []any{
		&Event{Kind: EventQuotaExhausted, At: at, ResetAt: at.Add(time.Hour)},
		&Result{Status: ResultQuotaExhausted, SessionID: "s1", ResetAt: at},
		&Capabilities{ContractVersion: ContractVersion, Headless: true, StructuredEvents: true, HostApprovals: true, ReportsUsage: true, AuthModes: []AuthMode{AuthAPIKey}},
		&StartSpec{EnvID: "e", Workdir: "/w", Prompt: "p", Auth: AuthSubscription, PermissionMode: PermissionDontAsk, AllowedTools: []string{"Read"}, ApprovalTimeout: time.Minute},
		&ApprovalRequest{ID: "a", Tool: "Bash", Input: "ls", Plan: true},
		&Approval{Allow: true, Reason: "ok"},
	}
	for _, v := range values {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		back := reflect.New(reflect.TypeOf(v).Elem()).Interface()
		if err := json.Unmarshal(raw, back); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(v, back) {
			t.Errorf("%T did not survive JSON:\n got %+v\nwant %+v (%s)", v, back, v, raw)
		}
	}
}

func TestZeroTimesAreLeftOut(t *testing.T) {
	raw, _ := json.Marshal(Result{Status: ResultCompleted, SessionID: "s"})
	if strings.Contains(string(raw), "reset_at") {
		t.Errorf("a zero reset time is on the wire: %s", raw)
	}
}

// The approver is a callback and cannot cross a process, so the spec leaves
// it out; an Approver in the spec must never break encoding.
func TestStartSpecLeavesTheApproverOffTheWire(t *testing.T) {
	spec := StartSpec{Auth: AuthSubscription, Approver: ApproverFunc(nil)}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "approver") {
		t.Errorf("the approver is on the wire: %s", raw)
	}
}

func TestEveryWireFieldHasAName(t *testing.T) {
	for _, v := range []any{Event{}, Result{}, Capabilities{}, StartSpec{}, ApprovalRequest{}, Approval{}, ApprovalRecord{}, Usage{}, Cost{}, UsageWindow{}} {
		typ := reflect.TypeOf(v)
		for i := range typ.NumField() {
			if f := typ.Field(i); f.Tag.Get("json") == "" {
				t.Errorf("%s.%s has no json tag", typ.Name(), f.Name)
			}
		}
	}
}

func TestErrorsCrossTheSeamAsCodes(t *testing.T) {
	sentinels := []error{ErrUnsupportedAuth, ErrUnsupported, ErrNoApprover, ErrNoSession, ErrNotRunning, ErrBadSpec}
	seen := map[string]error{}
	for _, want := range sentinels {
		code := Code(want)
		if code == "" || code == CodeUnknown {
			t.Fatalf("%v has no code", want)
		}
		if other, dup := seen[code]; dup {
			t.Fatalf("%v and %v share the code %q", want, other, code)
		}
		seen[code] = want

		if got := ErrorFor(code, ""); !errors.Is(got, want) {
			t.Errorf("ErrorFor(%q) = %v, want %v", code, got, want)
		}
		wrapped := fmt.Errorf("start: %w", want)
		if Code(wrapped) != code {
			t.Errorf("Code(wrapped %v) = %q, want %q", want, Code(wrapped), code)
		}
		detailed := ErrorFor(code, "the plugin said so")
		if !errors.Is(detailed, want) || !strings.Contains(detailed.Error(), "the plugin said so") {
			t.Errorf("ErrorFor(%q, detail) = %v", code, detailed)
		}
	}
	if Code(nil) != "" {
		t.Error(`Code(nil) must be ""`)
	}
	if Code(errors.New("boom")) != CodeUnknown {
		t.Error("an error outside the contract must have the unknown code")
	}
	if err := ErrorFor("from_the_future", "something new"); err == nil || Code(err) != CodeUnknown {
		t.Errorf("an unknown code must give a plain error, got %v", err)
	}
}

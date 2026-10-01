package claude

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/agent"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var clock = func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }

// parseAll feeds a whole stream and returns the events and the parser.
func parseAll(t *testing.T, stream []byte, dontAsk bool, allowed ...string) ([]agent.Event, *parser) {
	t.Helper()
	p := newParser(clock, dontAsk, allowed)
	return p.feed(stream), p
}

func golden(t *testing.T, name string, got any) {
	t.Helper()
	raw, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join("testdata", name+".golden.json")
	if *update {
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // a fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(raw) {
		t.Errorf("%s differs from its golden file (run with -update to see the diff in git):\n%s", name, raw)
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".jsonl")) //nolint:gosec // a fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGoldenStreams(t *testing.T) {
	tests := []struct {
		name    string
		dontAsk bool
		allowed []string
	}{
		{"finish", false, nil},
		{"tools", true, []string{"Read"}},
		{"auth", false, nil},
		{"quota", false, nil},
		{"partial", false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			events, p := parseAll(t, fixture(t, tc.name), tc.dontAsk, tc.allowed...)
			res, _ := p.outcome()
			golden(t, tc.name, map[string]any{"events": events, "result": res})
		})
	}
}

// Output arrives in chunks that end anywhere, even inside a multi-byte
// character, so the events must not depend on where they split.
func TestChunkBoundariesDoNotMatter(t *testing.T) {
	for _, name := range []string{"finish", "tools", "auth", "quota", "partial"} {
		stream := fixture(t, name)
		whole, _ := parseAll(t, stream, true, "Read")

		p := newParser(clock, true, []string{"Read"})
		var bytewise []agent.Event
		for i := range stream {
			bytewise = append(bytewise, p.feed(stream[i:i+1])...)
		}
		a, _ := json.Marshal(whole)
		b, _ := json.Marshal(bytewise)
		if string(a) != string(b) {
			t.Errorf("%s: byte-by-byte parsing differs from whole-stream parsing", name)
		}
	}
}

func kinds(events []agent.Event) []agent.EventKind {
	var out []agent.EventKind
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

// auth_expired comes from the error code on the assistant event, never from
// the result's subtype (which reads "success") or apiKeySource (spike #1).
func TestAuthExpiredFromTheErrorCode(t *testing.T) {
	events, p := parseAll(t, fixture(t, "auth"), false)
	if got := kinds(events); len(got) != 2 || got[0] != agent.EventAuthExpired || got[1] != agent.EventMessage {
		t.Errorf("kinds = %v, want auth_expired then the message", got)
	}
	res, ok := p.outcome()
	if !ok || res.Status != agent.ResultAuthExpired || res.SessionID == "" {
		t.Errorf("outcome = %+v, want auth_expired with a session ID, though the result says is_error with subtype success", res)
	}

	// The same result without the error code is an ordinary failure: the
	// subtype and is_error alone must not decide it.
	noCode := strings.ReplaceAll(string(fixture(t, "auth")), `"error":"authentication_failed",`, "")
	_, p = parseAll(t, []byte(noCode), false)
	if res, _ := p.outcome(); res.Status != agent.ResultFailed {
		t.Errorf("without the error code the status = %s, want failed", res.Status)
	}
	// And a login check by init.apiKeySource ("none") must not raise it.
	events, _ = parseAll(t, fixture(t, "finish"), false)
	for _, e := range events {
		if e.Kind == agent.EventAuthExpired {
			t.Error("a normal stream with apiKeySource none raised auth_expired")
		}
	}
}

func TestDontAskRecordsEachToolDecision(t *testing.T) {
	events, _ := parseAll(t, fixture(t, "tools"), true, "Read")
	var records []agent.ApprovalRecord
	for _, e := range events {
		if e.Kind == agent.EventApproval {
			records = append(records, *e.Approval)
		}
	}
	if len(records) != 2 {
		t.Fatalf("approval records = %+v, want one per tool use", records)
	}
	if r := records[0]; r.ID != "tu1" || !r.Allow || r.Reason != "on the allowlist" {
		t.Errorf("Read = %+v, want an allow from the allowlist", r)
	}
	if r := records[1]; r.ID != "tu2" || r.Allow {
		t.Errorf("Write = %+v, want a denial of tool use tu2", r)
	}
	// Without dontAsk the parser records nothing: manual approvals are the host's.
	events, _ = parseAll(t, fixture(t, "tools"), false)
	for _, e := range events {
		if e.Kind == agent.EventApproval {
			t.Error("an approval was recorded outside dontAsk")
		}
	}
}

func TestUsageEvent(t *testing.T) {
	events, p := parseAll(t, fixture(t, "finish"), false)
	var u *agent.Usage
	for _, e := range events {
		if e.Kind == agent.EventUsage {
			u = e.Usage
		}
	}
	if u == nil {
		t.Fatal("no usage event")
	}
	if err := u.Validate(); err != nil {
		t.Fatal(err)
	}
	if u.Model != "claude-haiku-4-5" || u.Cost == nil || u.Cost.MicroUSD != 12340 || u.Cost.Source != agent.CostReported {
		t.Errorf("usage = %+v", u)
	}
	if len(u.Windows) != 2 || u.Windows[0].Name != agent.WindowFiveHour || u.Windows[0].Utilization != 0.25 || !u.Windows[0].ResetsAt.Equal(time.Unix(1790000000, 0)) {
		t.Errorf("windows = %+v", u.Windows)
	}
	if res, _ := p.outcome(); res.Status != agent.ResultCompleted {
		t.Errorf("outcome = %+v", res)
	}
}

func TestQuotaFromAFullWindow(t *testing.T) {
	events, p := parseAll(t, fixture(t, "quota"), false)
	if got := kinds(events); got[1] != agent.EventQuotaExhausted {
		t.Errorf("kinds = %v, want quota_exhausted after the session event", got)
	}
	res, _ := p.outcome()
	if res.Status != agent.ResultQuotaExhausted || !res.ResetAt.Equal(time.Unix(1790000000, 0)) {
		t.Errorf("outcome = %+v", res)
	}
}

func TestDeltasAreNotEvents(t *testing.T) {
	events, _ := parseAll(t, fixture(t, "partial"), false)
	n := 0
	for _, e := range events {
		if e.Kind == agent.EventMessage {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d messages, want just the final one", n)
	}
}

func TestBadAndHugeLines(t *testing.T) {
	events, _ := parseAll(t, []byte("not json\n{\"type\":\"result\",\"is_error\":false}\n"), false)
	if len(events) != 1 || events[0].Kind != agent.EventError {
		t.Errorf("events = %+v, want one error for the bad line", events)
	}

	p := newParser(clock, false, nil)
	huge := strings.Repeat("x", maxLine+10)
	got := p.feed([]byte(huge))
	got = append(got, p.feed([]byte(huge+"\n"))...)
	got = append(got, p.feed([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"after"}]}}`+"\n"))...)
	if len(got) != 2 || got[0].Kind != agent.EventError || got[1].Text != "after" {
		t.Errorf("events = %+v, want one error for the huge line and the next line parsed", got)
	}
}

func TestUntrustedTextIsCapped(t *testing.T) {
	long := strings.Repeat("é", 5000)
	line, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"content": []map[string]any{{"type": "text", "text": long}}}})
	events, _ := parseAll(t, append(line, '\n'), false)
	if len(events) != 1 || len([]rune(events[0].Text)) != 2000 {
		t.Errorf("a long message was not capped to 2000 characters: %d", len([]rune(events[0].Text)))
	}
}

func TestResetTimeFormats(t *testing.T) {
	for in, want := range map[string]time.Time{
		`1790000000`:             time.Unix(1790000000, 0).UTC(),
		`1790000000000`:          time.Unix(1790000000, 0).UTC(),
		`"2026-10-01T15:00:00Z"`: time.Date(2026, 10, 1, 15, 0, 0, 0, time.UTC),
		`"tomorrow"`:             {},
		`null`:                   {},
	} {
		if got := parseTime(json.RawMessage(in)); !got.Equal(want) {
			t.Errorf("parseTime(%s) = %v, want %v", in, got, want)
		}
	}
}

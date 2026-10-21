//go:build linux || darwin

// SPDX-License-Identifier: EUPL-1.2
package plan

import (
	"encoding/json"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/wstein/workharbor-nxt/apps/whr/internal/hostsetup/profile"
	"strings"
	"testing"
)

func TestResultRedaction(t *testing.T) {
	canaries := []string{"/home/home-canary", "user-canary", "host-canary", "ENV_CANARY=secret", "git-value-canary"}
	p, e := Build(input())
	if e != nil {
		t.Fatal(e)
	}
	s := p.Steps[0]
	raw := ApplyResult{ExitCode: 68, Applied: canaries, Remaining: canaries, Steps: []StepOutcome{{Audit: profile.StepAudit{ID: s.ID, Item: canaries[1], Kind: canaries[2], Result: "blocked", Argv: canaries}, Reason: strings.Join(canaries, " "), Backups: []BackupAudit{{Path: canaries[0], SHA256: canaries[4]}}}}}
	result, e := ProjectResult(input(), p, raw)
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(result)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range canaries {
		if strings.Contains(string(b), c) {
			t.Fatal("leaked canary")
		}
	}
	var value any
	if e = json.Unmarshal(b, &value); e != nil {
		t.Fatal(e)
	}
	// Draft internal shape only; no command envelope or AP-6 failure placement.
	var draft any
	e = json.Unmarshal([]byte(`{"type":"object","additionalProperties":false,"required":["schema_version","ExitCode","Steps","Applied","Remaining"],"properties":{"schema_version":{"const":1},"ExitCode":{"enum":[0,64,68,70,71,72,73,74]},"Steps":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["ID","Item","Kind","Status","Reason"],"properties":{"ID":{"type":"string"},"Item":{"type":"string"},"Kind":{"type":"string"},"Status":{"enum":["applied","unchanged","blocked","failed","rolled_back","skipped","declined","opted_out"]},"Reason":{"type":"string"}}}},"Applied":{"type":"array","items":{"type":"string"}},"Remaining":{"type":"array","items":{"type":"string"}}}}`), &draft)
	if e != nil {
		t.Fatal(e)
	}
	c := jsonschema.NewCompiler()
	if e = c.AddResource("draft.json", draft); e != nil {
		t.Fatal(e)
	}
	schema, e := c.Compile("draft.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = schema.Validate(value); e != nil {
		t.Fatal(e)
	}
	if result.Steps[0].Reason != "" {
		t.Fatal("raw reason retained")
	}
}

func TestResultRejectsInvalidEngineStatus(t *testing.T) {
	// T006 Apply emits profile.StepAudit's closed protocol result set. A future
	// broken adapter must not be displayed as an ordinary blocked operation.
	p, err := Build(input())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ProjectResult(input(), p, ApplyResult{Steps: []StepOutcome{{Audit: profile.StepAudit{ID: p.Steps[0].ID, Result: "invalid-engine-state"}}}})
	if err == nil {
		t.Fatal("impossible engine status was silently remapped")
	}
}

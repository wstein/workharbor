// SPDX-License-Identifier: EUPL-1.2
package protocol_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	p "github.com/wstein/workharbor-nxt/apps/host_agent/internal/protocol"
	"math/big"
	"os"
	"strings"
	"testing"
)

func id(n string) string { return "00000000-0000-0000-0000-" + strings.Repeat("0", 12-len(n)) + n }
func cfg() map[string]any {
	return map[string]any{"repository_identity": "git@example.test:team/workspace.git", "commit": strings.Repeat("a", 40), "submodules": []any{}}
}
func command() map[string]any {
	return map[string]any{"schema_version": 1, "kind": "command", "command_id": id("5"), "owner_id": id("1"), "host_id": id("2"), "workspace_id": id("3"), "operation_id": id("4"), "idempotency_key": "create:1", "request_sha256": fixtures().RequestHash, "intent_version": 1, "request": map[string]any{"schema_version": 1, "workspace_id": id("3"), "action": "create", "parameters": map[string]any{"host_id": id("2"), "config": cfg()}}, "bindings": map[string]any{"target": nil, "candidate": map[string]any{"runtime_id": id("6"), "generation": 1}}, "execution_config": nil, "approval_ref": id("7")}
}

type vectors struct {
	RequestBytes     string `json:"request_bytes"`
	RequestHash      string `json:"request_sha256"`
	CommandCanonical string `json:"command_canonical"`
	CommandHash      string `json:"command_sha256"`
	Cases            []struct{ ID, Raw, Code, Disposition string }
}

func fixtures() vectors {
	raw, e := os.ReadFile("../../../../contracts/host-protocol/vectors.json")
	if e != nil {
		panic(e)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "dedc629e6989fdfaaad8053fe14a0680b692495fdfff88eb1f4e5a29c534d66d" {
		panic("fixture provenance changed")
	}
	var v vectors
	if e = json.Unmarshal(raw, &v); e != nil {
		panic(e)
	}
	return v
}
func context() p.Context {
	v := fixtures()
	return p.Context{Peer: &p.Peer{Kind: "control_plane", OwnerID: id("1"), HostID: id("2")}, Snapshot: &p.Snapshot{Command: command(), RequestBytes: []byte(v.RequestBytes), RequestHash: v.RequestHash, NativeBindings: map[string]any{"target": nil, "candidate": map[string]any{"id": id("6"), "generation": 1}}, CurrentIntent: 1, Status: "queued", Runtimes: map[string]p.Runtime{id("6"): {}}}}
}
func code(err error) string {
	if err == nil {
		return "ok"
	}
	return err.Error()
}
func result() map[string]any {
	m := command()
	delete(m, "request")
	delete(m, "execution_config")
	delete(m, "approval_ref")
	m["kind"] = "result"
	m["outcome"] = map[string]any{"status": "succeeded", "code": "ok"}
	return m
}
func observation() map[string]any {
	return map[string]any{"runtime_id": id("6"), "generation": 1, "observed_state": "running"}
}
func TestSharedRawVectors(t *testing.T) {
	for _, v := range fixtures().Cases {
		t.Run(v.ID, func(t *testing.T) {
			got, e := p.Decode([]byte(v.Raw), context())
			if code(e) != v.Code {
				t.Fatalf("%s: want %s got %s", v.ID, v.Code, code(e))
			}
			if e == nil && got.Disposition != v.Disposition {
				t.Fatalf("disposition %s", got.Disposition)
			}
		})
	}
}
func TestIndependentIdentityAnchors(t *testing.T) {
	v := fixtures()
	got, e := p.Validate(command(), context())
	if e != nil {
		t.Fatal(e)
	}
	if string(got.Canonical) != v.CommandCanonical {
		t.Fatal("whole command bytes")
	}
	sum := sha256.Sum256(got.Canonical)
	if hex.EncodeToString(sum[:]) != v.CommandHash {
		t.Fatal("command digest")
	}
	if bytes.Equal(got.Canonical, []byte(v.RequestBytes)) {
		t.Fatal("hash identities conflated")
	}
	if got.NativeBindings["candidate"].(map[string]any)["id"] != id("6") {
		t.Fatal("binding translation")
	}
}
func TestNativeClosedTypes(t *testing.T) {
	for _, bad := range []any{nil, true, 1, 1.0, []any{}, map[int]any{}, []byte("{}")} {
		if _, e := p.Validate(bad, context()); e == nil {
			t.Fatal("accepted native type")
		}
	}
	for _, bad := range []any{true, 1.0, "1", big.NewInt(0), new(big.Int).Lsh(big.NewInt(1), 63), uint64(1 << 63)} {
		m := command()
		m["intent_version"] = bad
		if _, e := p.Validate(m, context()); code(e) != "invalid_message" {
			t.Fatalf("bad counter %T %s", bad, code(e))
		}
	}
}
func TestTrustedScopeBeforeSnapshotDisclosure(t *testing.T) {
	bad := context()
	bad.Peer.OwnerID = id("9")
	bad.Snapshot.Command = map[string]any{"secret": "private-canary"}
	if _, e := p.Validate(command(), bad); code(e) != "unauthorized_scope" {
		t.Fatal("snapshot inspected before scope", code(e))
	}
	for _, peer := range []*p.Peer{nil, {Kind: "owner", OwnerID: id("1"), HostID: id("2")}, {Kind: "control_plane", OwnerID: id("9"), HostID: id("2")}} {
		c := context()
		c.Peer = peer
		c.Snapshot = nil
		if _, e := p.Validate(command(), c); code(e) != "unauthorized_scope" {
			t.Fatal(code(e))
		}
	}
	c := context()
	c.Snapshot = nil
	if _, e := p.Validate(command(), c); code(e) != "unauthorized_scope" {
		t.Fatal(code(e))
	}
}
func TestPendingReplayNeverPermitsEffect(t *testing.T) {
	c := context()
	c.Snapshot.CommandBytes = []byte(fixtures().CommandCanonical)
	g, e := p.Validate(command(), c)
	if e != nil || g.Disposition != "pending_lookup" {
		t.Fatal(g, e)
	}
	c.Snapshot.Status = "executing"
	g, e = p.Validate(command(), c)
	if e != nil || g.Disposition != "reconciliation_required" {
		t.Fatal(g, e)
	}
}
func TestCompletedReplayAfterNewerIntent(t *testing.T) {
	c := context()
	c.Snapshot.Status = "succeeded"
	c.Snapshot.CurrentIntent = 2
	c.Snapshot.CommandBytes = []byte(fixtures().CommandCanonical)
	g, e := p.Validate(command(), c)
	if e != nil || g.Disposition != "completed_lookup" {
		t.Fatal(g, e)
	}
}
func TestChangedReplayAndSameRuntimeStaleIntent(t *testing.T) {
	c := context()
	c.Snapshot.CommandBytes = []byte("changed")
	if _, e := p.Validate(command(), c); code(e) != "identity_conflict" {
		t.Fatal(code(e))
	}
	c = context()
	c.Snapshot.CurrentIntent = 2
	if _, e := p.Validate(command(), c); code(e) != "stale_intent" {
		t.Fatal(code(e))
	}
}
func TestResultsAndObservationDefaults(t *testing.T) {
	m := result()
	m["observation"] = observation()
	c := context()
	c.Peer.Kind = "host"
	c.Snapshot.Status = "executing"
	g, e := p.Validate(m, c)
	if e != nil {
		t.Fatal(e)
	}
	o := g.NativeResult["observation"].(map[string]any)
	if o["provider_instance_id"] != nil || o["effective_config"] != nil || o["deleted"] != false {
		t.Fatal("defaults")
	}
	delete(m, "observation")
	g, e = p.Validate(m, c)
	if e != nil || g.NativeResult["observation"] != nil {
		t.Fatal(g, e)
	}
	m["observation"] = nil
	if _, e = p.Validate(m, c); code(e) != "invalid_message" {
		t.Fatal(code(e))
	}
}
func TestMalformedObservationDoesNotBecomeNull(t *testing.T) {
	for _, bad := range []any{false, 0, "config", []any{}, map[string]any{}, map[string]any{"secret": "private-canary"}} {
		m := result()
		o := observation()
		o["effective_config"] = bad
		m["observation"] = o
		c := context()
		c.Peer.Kind = "host"
		c.Snapshot.Status = "executing"
		if _, e := p.Validate(m, c); code(e) != "invalid_message" {
			t.Fatal(code(e))
		}
	}
	for field, bad := range map[string]any{"provider_instance_id": false, "deleted": nil, "generation": 1.0} {
		m := result()
		o := observation()
		o[field] = bad
		m["observation"] = o
		c := context()
		c.Peer.Kind = "host"
		if _, e := p.Validate(m, c); code(e) != "invalid_message" {
			t.Fatal(code(e))
		}
	}
}
func TestResultScopeStatusAndTerminalImmutability(t *testing.T) {
	c := context()
	c.Peer.Kind = "host"
	m := result()
	if _, e := p.Validate(m, c); code(e) != "invalid_status" {
		t.Fatal(code(e))
	}
	c.Snapshot.Status = "executing"
	g, e := p.Validate(m, c)
	if e != nil {
		t.Fatal(e)
	}
	c.Snapshot.Status = "succeeded"
	c.Snapshot.CurrentIntent = 2
	c.Snapshot.TerminalResult = g.NativeResult
	g, e = p.Validate(m, c)
	if e != nil || g.Disposition != "completed_lookup" {
		t.Fatal(g, e)
	}
	m["outcome"] = map[string]any{"status": "failed", "code": "operation_failed"}
	if _, e = p.Validate(m, c); code(e) != "identity_conflict" {
		t.Fatal(code(e))
	}
}
func TestObservationBindingsConfigAndProviderImmutable(t *testing.T) {
	for _, variant := range []string{"runtime", "provider", "config", "deleted"} {
		m := result()
		o := observation()
		c := context()
		c.Peer.Kind = "host"
		c.Snapshot.Status = "executing"
		switch variant {
		case "runtime":
			o["runtime_id"] = id("9")
		case "provider":
			c.Snapshot.Runtimes[id("6")] = p.Runtime{ProviderID: "known"}
			o["provider_instance_id"] = "changed"
		case "config":
			bad := cfg()
			bad["commit"] = strings.Repeat("b", 40)
			o["effective_config"] = bad
		case "deleted":
			o["deleted"] = true
		}
		m["observation"] = o
		if _, e := p.Validate(m, c); e == nil {
			t.Fatal(variant)
		}
	}
}
func TestResultCommandOnlyFieldsRejected(t *testing.T) {
	for _, f := range []string{"request", "approval_ref", "execution_config"} {
		m := result()
		m[f] = nil
		c := context()
		c.Peer.Kind = "host"
		if _, e := p.Validate(m, c); code(e) != "invalid_message" {
			t.Fatal(code(e))
		}
	}
}
func TestBoundedNativeCanonicalBudgetAndRedaction(t *testing.T) {
	for _, bad := range []any{strings.Repeat("x", 1<<20), strings.Repeat("\\", 1<<19)} {
		m := command()
		m["request"].(map[string]any)["parameters"].(map[string]any)["config"].(map[string]any)["repository_identity"] = "git@example.test:" + bad.(string)
		if _, e := p.Validate(m, context()); code(e) != "transport_refused" {
			t.Fatal(code(e))
		}
	}
	m := command()
	m["secret"] = "private-canary"
	if _, e := p.Validate(m, context()); strings.Contains(code(e), "canary") {
		t.Fatal("leak")
	}
}
func TestRawLimitsAndNativeCycles(t *testing.T) {
	for _, raw := range [][]byte{append([]byte{255}, bytes.Repeat([]byte{' '}, 1<<20)...), bytes.Repeat([]byte{' '}, (1<<20)+1)} {
		if _, e := p.Decode(raw, context()); code(e) != "input_limit" {
			t.Fatal(code(e))
		}
	}
	m := map[string]any{}
	m["cycle"] = m
	if _, e := p.Validate(m, context()); e == nil {
		t.Fatal("cycle")
	}
}
func TestNoProviderEffectForRejectedOrLookup(t *testing.T) {
	calls := 0
	for _, v := range fixtures().Cases {
		if v.Code != "ok" {
			g, e := p.Decode([]byte(v.Raw), context())
			if e == nil && g.Disposition == "new_eligible" {
				calls++
			}
		}
	}
	gate := func(m any, c p.Context) {
		v, e := p.Validate(m, c)
		if e == nil && v.Disposition == "new_eligible" {
			calls++
		}
	}
	m := command()
	m["argv"] = []any{"sh"}
	gate(m, context())
	c := context()
	c.Snapshot.CommandBytes = []byte(fixtures().CommandCanonical)
	gate(command(), c)
	if calls != 0 {
		t.Fatal("effect gate")
	}
}

func TestAllLifecycleBranchesAndSameRuntimeNewIntent(t *testing.T) {
	raw, e := os.ReadFile("../../../../contracts/host-protocol/vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Actions []struct {
			Action   string
			Command  string          `json:"command_canonical"`
			Request  string          `json:"request_bytes"`
			Hash     string          `json:"request_sha256"`
			Bindings json.RawMessage `json:"native_bindings"`
		}
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	decode := func(raw []byte) map[string]any {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		var v any
		if e := d.Decode(&v); e != nil {
			t.Fatal(e)
		}
		var native func(any) any
		native = func(v any) any {
			switch x := v.(type) {
			case json.Number:
				n, e := x.Int64()
				if e != nil {
					t.Fatal(e)
				}
				return n
			case map[string]any:
				for k, a := range x {
					x[k] = native(a)
				}
			case []any:
				for i, a := range x {
					x[i] = native(a)
				}
			}
			return v
		}
		return native(v).(map[string]any)
	}
	for _, a := range f.Actions {
		t.Run(a.Action, func(t *testing.T) {
			c := context()
			m := decode([]byte(a.Command))
			c.Snapshot.Command = m
			c.Snapshot.RequestBytes = []byte(a.Request)
			c.Snapshot.RequestHash = a.Hash
			c.Snapshot.NativeBindings = decode(a.Bindings)
			c.Snapshot.CurrentIntent = 2
			c.Snapshot.Runtimes[id("6")] = p.Runtime{EffectiveConfig: cfg()}
			c.Snapshot.Runtimes[id("8")] = p.Runtime{}
			g, e := p.Decode([]byte(a.Command), c)
			if e != nil || g.Disposition != "new_eligible" {
				t.Fatal(g, e)
			}
			r := map[string]any{}
			for k, v := range m {
				r[k] = v
			}
			delete(r, "request")
			delete(r, "approval_ref")
			delete(r, "execution_config")
			r["kind"] = "result"
			r["outcome"] = map[string]any{"status": "succeeded", "code": "ok"}
			o := map[string]any{"runtime_id": id("6"), "generation": 1, "observed_state": "stopped", "effective_config": cfg()}
			if a.Action == "start" {
				o["observed_state"] = "running"
			}
			if a.Action == "rebuild" {
				o["runtime_id"] = id("8")
				o["generation"] = 2
				o["observed_state"] = "running"
			}
			if a.Action == "remove_runtime" {
				o["deleted"] = true
			}
			r["observation"] = o
			c.Peer.Kind = "host"
			c.Snapshot.Status = "executing"
			if _, e = p.Validate(r, c); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestExactCanonicalBudgetAndCounterMaximum(t *testing.T) {
	for _, text := range []string{strings.Repeat("x", (1<<20)-8), strings.Repeat("é", ((1<<20)-8)/2)} {
		if _, e := p.Validate(map[string]any{"x": text}, context()); code(e) != "invalid_message" {
			t.Fatal("exact budget", code(e))
		}
		if _, e := p.Validate(map[string]any{"x": text + "x"}, context()); code(e) != "transport_refused" {
			t.Fatal("over budget", code(e))
		}
	}
	c := context()
	m := command()
	m["intent_version"] = int64(9223372036854775807)
	c.Snapshot.Command = m
	c.Snapshot.CurrentIntent = 9223372036854775807
	if _, e := p.Validate(m, c); e != nil {
		t.Fatal(e)
	}
}

func TestNormalizedResultBudgetIncludesOptionalDefaults(t *testing.T) {
	m := result()
	o := observation()
	o["provider_instance_id"] = ""
	m["observation"] = o
	normalized := result()
	no := observation()
	no["provider_instance_id"] = ""
	no["effective_config"] = nil
	no["deleted"] = false
	normalized["observation"] = no
	base, e := json.Marshal(normalized)
	if e != nil {
		t.Fatal(e)
	}
	fill := (1 << 20) - len(base)
	c := context()
	c.Peer.Kind = "host"
	c.Snapshot.Status = "executing"
	o["provider_instance_id"] = strings.Repeat("x", fill)
	raw, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	g, e := p.Decode(raw, c)
	if e != nil || len(g.Canonical) != (1<<20) {
		t.Fatal("exact normalized budget", e)
	}
	o["provider_instance_id"] = strings.Repeat("x", fill+1)
	raw, e = json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	if len(raw) > 1<<20 {
		t.Fatal("regression fixture already oversize before defaults")
	}
	if _, e = p.Decode(raw, c); code(e) != "transport_refused" {
		t.Fatal("normalized default overhead", code(e))
	}
	if _, e = p.Validate(m, c); code(e) != "transport_refused" {
		t.Fatal("native normalized overhead", code(e))
	}
}
func TestWireObservationConfigMustAlreadyBeNormalized(t *testing.T) {
	for _, variant := range []string{"omitted", "unsorted"} {
		m := result()
		o := observation()
		config := cfg()
		if variant == "omitted" {
			delete(config, "submodules")
		} else {
			config["submodules"] = []any{map[string]any{"path": "z", "commit": strings.Repeat("a", 40)}, map[string]any{"path": "a", "commit": strings.Repeat("b", 40)}}
		}
		o["effective_config"] = config
		m["observation"] = o
		c := context()
		c.Peer.Kind = "host"
		c.Snapshot.Status = "executing"
		if _, e := p.Validate(m, c); code(e) != "invalid_message" {
			t.Fatal(variant, code(e))
		}
	}
}

func TestWorkspaceScopeUsesExpectedAssignment(t *testing.T) {
	m := command()
	m["workspace_id"] = id("9")
	m["request"].(map[string]any)["workspace_id"] = id("9")
	if _, e := p.Validate(m, context()); code(e) != "unauthorized_scope" {
		t.Fatal(code(e))
	}
}

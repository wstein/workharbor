// SPDX-License-Identifier: EUPL-1.2
// Package protocol validates the closed draft host contract without effects.
// Caller-supplied peer/snapshot types are not a cryptographic identity mechanism.
package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/wstein/workharbor-nxt/apps/api_client/wirejson"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"math/big"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const limit = 1 << 20

// Peer is supplied by the future authenticated admission layer, never a wire body.
type Peer struct{ Kind, OwnerID, HostID string }

// Runtime is an authoritative observation expectation, not provider state access.
type Runtime struct {
	ProviderID      any
	EffectiveConfig any
}

// Snapshot is a caller-supplied immutable operation expectation, not a ledger.
type Snapshot struct {
	Command        map[string]any
	RequestBytes   []byte
	RequestHash    string
	NativeBindings map[string]any
	CurrentIntent  int64
	Status         string
	CommandBytes   []byte
	TerminalResult map[string]any
	Runtimes       map[string]Runtime
}
type Context struct {
	Peer     *Peer
	Snapshot *Snapshot
}

// Validated carries only data and a disposition. Even new_eligible permits no effect.
type Validated struct {
	Message        map[string]any
	Canonical      []byte
	NativeBindings map[string]any
	NativeResult   map[string]any
	Disposition    string
}
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }
func fail(code string)         { panic(&Error{code}) }
func require(ok bool, code string) {
	if !ok {
		fail(code)
	}
}

const invalid = "invalid_message"

var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var key = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)
var commit = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
var common = []string{"schema_version", "kind", "command_id", "owner_id", "host_id", "workspace_id", "operation_id", "idempotency_key", "request_sha256", "intent_version", "bindings"}
var commandFields = append(append([]string{}, common...), "request", "execution_config", "approval_ref")
var states = map[string]bool{"pending": true, "provisioning": true, "running": true, "stopping": true, "stopped": true, "failed": true, "unknown": true}

func Decode(raw any, ctx Context) (*Validated, error) {
	v, e := wirejson.Parse(raw)
	if e != nil {
		if e.Error() == wirejson.CodeInputLimitExceeded {
			return nil, &Error{"input_limit"}
		}
		return nil, &Error{"invalid_json"}
	}
	if _, ok := v.(map[string]any); !ok {
		return nil, &Error{"invalid_json"}
	}
	return Validate(v, ctx)
}
func Validate(value any, ctx Context) (out *Validated, err error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*Error); ok {
				out = nil
				err = e
			} else {
				panic(r)
			}
		}
	}()
	budget(value, 0, limit)
	m := shape(value)
	budget(m, 0, limit)
	peer, s := ctx.Peer, ctx.Snapshot
	wantPeer := "control_plane"
	if m["kind"] == "result" {
		wantPeer = "host"
	}
	require(peer != nil && s != nil, "unauthorized_scope")
	require(peer.Kind == wantPeer && uuid.MatchString(peer.OwnerID) && uuid.MatchString(peer.HostID) && m["owner_id"] == peer.OwnerID && m["host_id"] == peer.HostID, "unauthorized_scope")
	budget(s.Command, 0, limit)
	expected := commandShape(s.Command)
	require(expected["owner_id"] == peer.OwnerID && expected["host_id"] == peer.HostID, "unauthorized_scope")
	require(m["workspace_id"] == expected["workspace_id"], "unauthorized_scope")
	for _, f := range common {
		if f != "kind" {
			require(equal(m[f], expected[f]), "identity_conflict")
		}
	}
	_, requestBytes, requestHash := normalizeRequest(peer.OwnerID, str(expected["idempotency_key"]), expected["request"])
	require(bytes.Equal(requestBytes, s.RequestBytes) && requestHash == s.RequestHash && expected["request_sha256"] == s.RequestHash, "identity_conflict")
	native := bindings(m["bindings"], "runtime_id")
	// Native bindings retain exact id/generation/role types, not loose numeric equality.
	require(nativeBindingsValid(s.NativeBindings) && equal(native, s.NativeBindings), "stale_runtime")
	canonical := canonical(m)
	out = &Validated{Message: m, Canonical: canonical, NativeBindings: native}
	if m["kind"] == "command" {
		require(equal(m, expected), "identity_conflict")
		if s.CommandBytes != nil {
			require(bytes.Equal(canonical, s.CommandBytes), "identity_conflict")
		}
		if terminal(s.Status) {
			out.Disposition = "completed_lookup"
			return out, nil
		}
		current(s, m)
		switch s.Status {
		case "queued":
			if s.CommandBytes == nil {
				out.Disposition = "new_eligible"
			} else {
				out.Disposition = "pending_lookup"
			}
		case "executing":
			out.Disposition = "reconciliation_required"
		default:
			fail("invalid_status")
		}
	} else {
		result := map[string]any{"outcome": m["outcome"], "observation": m["observation"]}
		out.NativeResult = result
		if terminal(s.Status) {
			require(equal(result, s.TerminalResult), "identity_conflict")
			out.Disposition = "completed_lookup"
			return out, nil
		}
		current(s, m)
		outcome := object(m["outcome"])
		require((s.Status == "queued" || s.Status == "executing") && (outcome["status"] != "succeeded" || s.Status == "executing"), "invalid_status")
		observe(result, native, expected, s)
		out.Disposition = "new_eligible"
	}
	return out, nil
}
func terminal(s string) bool { return s == "succeeded" || s == "failed" }
func current(s *Snapshot, m map[string]any) {
	require(s.CurrentIntent == counter(m["intent_version"]), "stale_intent")
}
func shape(v any) map[string]any {
	m := object(v)
	if n, ok := integer(m["schema_version"]); ok && n.Cmp(big.NewInt(1)) != 0 {
		fail("unsupported_version")
	}
	require(counter(m["schema_version"]) == 1, invalid)
	switch m["kind"] {
	case "command":
		return commandShape(m)
	case "result":
		fields(m, append(append([]string{}, common...), "outcome"), []string{"observation"})
		commonShape(m)
		o := object(m["outcome"])
		fields(o, []string{"status", "code"}, nil)
		require((o["status"] == "succeeded" && o["code"] == "ok") || (o["status"] == "failed" && o["code"] == "operation_failed"), invalid)
		out := copyMap(m)
		if observation, present := m["observation"]; present {
			out["observation"] = observationShape(observation)
		}
		return out
	default:
		fail(invalid)
	}
	return nil
}
func commandShape(v any) map[string]any {
	m := object(v)
	fields(m, commandFields, nil)
	require(counter(m["schema_version"]) == 1 && m["kind"] == "command", invalid)
	commonShape(m)
	normalized, _, _ := normalizeRequest(str(m["owner_id"]), str(m["idempotency_key"]), m["request"])
	delete(normalized, "owner_id")
	require(equal(normalized, m["request"]), invalid)
	require(normalized["workspace_id"] == m["workspace_id"], invalid)
	action := str(normalized["action"])
	parameters := object(normalized["parameters"])
	b := object(m["bindings"])
	switch action {
	case "create":
		require(parameters["host_id"] == m["host_id"] && b["target"] == nil && b["candidate"] != nil, invalid)
	case "rebuild":
		require(b["candidate"] != nil, invalid)
	case "start":
		require(b["target"] != nil && b["candidate"] == nil, invalid)
	default:
		require(b["candidate"] == nil, invalid)
	}
	if action == "start" {
		require(equal(normalizeConfig(m["execution_config"]), m["execution_config"]), invalid)
	} else {
		require(m["execution_config"] == nil, invalid)
	}
	if action == "create" || action == "rebuild" {
		require(uuid.MatchString(str(m["approval_ref"])), invalid)
	} else {
		require(m["approval_ref"] == nil, invalid)
	}
	return copyMap(m)
}
func commonShape(m map[string]any) {
	for _, f := range []string{"command_id", "owner_id", "host_id", "workspace_id", "operation_id"} {
		require(uuid.MatchString(str(m[f])), invalid)
	}
	require(key.MatchString(str(m["idempotency_key"])) && hash.MatchString(str(m["request_sha256"])), invalid)
	counter(m["intent_version"])
	bindings(m["bindings"], "runtime_id")
}
func bindings(v any, idField string) map[string]any {
	b := object(v)
	fields(b, []string{"target", "candidate"}, nil)
	out := map[string]any{}
	for _, role := range []string{"target", "candidate"} {
		v := b[role]
		if v == nil {
			out[role] = nil
			continue
		}
		r := object(v)
		fields(r, []string{idField, "generation"}, nil)
		require(uuid.MatchString(str(r[idField])), invalid)
		out[role] = map[string]any{"id": r[idField], "generation": counter(r["generation"])}
	}
	return out
}
func nativeBindingsValid(v any) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, known := r.(*Error); !known {
				panic(r)
			}
			ok = false
		}
	}()
	bindings(v, "id")
	return true
}
func observationShape(v any) map[string]any {
	o := object(v)
	fields(o, []string{"runtime_id", "generation", "observed_state"}, []string{"provider_instance_id", "effective_config", "deleted"})
	require(uuid.MatchString(str(o["runtime_id"])) && states[str(o["observed_state"])], invalid)
	counter(o["generation"])
	provider := o["provider_instance_id"]
	if provider != nil {
		str(provider)
	}
	deleted := any(false)
	if value, exists := o["deleted"]; exists {
		b, ok := value.(bool)
		require(ok, invalid)
		deleted = b
	}
	effective := o["effective_config"]
	if effective != nil {
		normalized := normalizeConfig(effective)
		require(equal(effective, normalized), invalid)
		effective = normalized
	}
	out := copyMap(o)
	out["provider_instance_id"] = provider
	out["effective_config"] = effective
	out["deleted"] = deleted
	return out
}
func observe(result, native, command map[string]any, s *Snapshot) {
	if result["observation"] == nil {
		return
	}
	o := object(result["observation"])
	identity := map[string]any{"id": o["runtime_id"], "generation": o["generation"]}
	role := ""
	for _, r := range []string{"target", "candidate"} {
		if equal(native[r], identity) {
			role = r
		}
	}
	require(role != "", "stale_runtime")
	runtime, ok := s.Runtimes[str(o["runtime_id"])]
	require(ok, "stale_runtime")
	provider, effective := o["provider_instance_id"], o["effective_config"]
	if provider != nil && runtime.ProviderID != nil {
		require(equal(provider, runtime.ProviderID), "stale_runtime")
	}
	req := object(command["request"])
	action := str(req["action"])
	if effective != nil {
		if role == "target" {
			require(runtime.EffectiveConfig != nil, "stale_runtime")
		}
		if runtime.EffectiveConfig != nil {
			require(equal(effective, runtime.EffectiveConfig), "stale_runtime")
		}
		if role == "candidate" {
			require(equal(effective, object(req["parameters"])["config"]), "stale_runtime")
		}
		if action == "start" {
			require(equal(effective, command["execution_config"]), "stale_runtime")
		}
	}
	if o["deleted"] == true {
		require(action == "remove_runtime" && role == "target" && object(result["outcome"])["status"] == "succeeded" && o["observed_state"] == "stopped", invalid)
	}
}
func normalizeRequest(owner, k string, v any) (map[string]any, []byte, string) {
	require(uuid.MatchString(owner) && key.MatchString(k), invalid)
	r := object(v)
	fields(r, []string{"schema_version", "workspace_id", "action"}, []string{"parameters"})
	require(counter(r["schema_version"]) == 1 && uuid.MatchString(str(r["workspace_id"])), invalid)
	action := str(r["action"])
	raw, exists := r["parameters"]
	if !exists && (action == "start" || action == "stop" || action == "remove_runtime") {
		raw = map[string]any{}
	}
	parameters := object(raw)
	var normalized map[string]any
	switch action {
	case "start", "stop", "remove_runtime":
		fields(parameters, nil, nil)
		normalized = map[string]any{}
	case "create":
		fields(parameters, []string{"host_id", "config"}, nil)
		require(uuid.MatchString(str(parameters["host_id"])), invalid)
		normalized = map[string]any{"host_id": parameters["host_id"], "config": normalizeConfig(parameters["config"])}
	case "rebuild":
		fields(parameters, []string{"config"}, nil)
		normalized = map[string]any{"config": normalizeConfig(parameters["config"])}
	default:
		fail(invalid)
	}
	identity := map[string]any{"schema_version": int64(1), "owner_id": owner, "workspace_id": r["workspace_id"], "action": action, "parameters": normalized}
	b := canonical(identity)
	digest := sha256.Sum256(b)
	return identity, b, hex.EncodeToString(digest[:])
}
func normalizeConfig(v any) map[string]any {
	c := object(v)
	fields(c, []string{"repository_identity", "commit"}, []string{"submodules"})
	require(repository(str(c["repository_identity"])) && commit.MatchString(str(c["commit"])), invalid)
	list := []any{}
	if v, ok := c["submodules"]; ok {
		var valid bool
		list, valid = v.([]any)
		require(valid, invalid)
	}
	entries := make([]any, 0, len(list))
	seen := map[string]bool{}
	for _, v := range list {
		entry := object(v)
		fields(entry, []string{"path", "commit"}, nil)
		path := str(entry["path"])
		_, e := config.CanonicalPath(path)
		require(e == nil && commit.MatchString(str(entry["commit"])) && !seen[path], invalid)
		seen[path] = true
		entries = append(entries, map[string]any{"path": path, "commit": entry["commit"]})
	}
	sort.Slice(entries, func(i, j int) bool { return object(entries[i])["path"].(string) < object(entries[j])["path"].(string) })
	return map[string]any{"repository_identity": c["repository_identity"], "commit": c["commit"], "submodules": entries}
}

var urlShape = regexp.MustCompile(`(?is)^(https|ssh)://([^/]+)(/.*)$`)
var otherScheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*://`)
var scpShape = regexp.MustCompile(`(?s)^((?:[A-Za-z0-9._-]+@)?(?:\[[^\]]+\]|[^:]+)):(.+)$`)
var username = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var hostname = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*\.?$`)
var hostPort = regexp.MustCompile(`^([^:]+)(?::([0-9]+))?$`)
var ipv6Port = regexp.MustCompile(`^(\[[^\]]+\])(?::([0-9]+))?$`)

func repository(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r <= 32 || r == 127 || r == 0x85 || r == 0xa0 || r == 0x1680 || r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == '\\' || r == '?' || r == '#' {
			return false
		}
	}
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			if i+2 >= len(s) || !hexDigit(s[i+1]) || !hexDigit(s[i+2]) {
				return false
			}
			i += 2
		}
	}
	if m := urlShape.FindStringSubmatch(s); m != nil {
		return authority(m[2], strings.EqualFold(m[1], "ssh"), true)
	}
	if otherScheme.MatchString(s) {
		return false
	}
	m := scpShape.FindStringSubmatch(s)
	return m != nil && authority(m[1], true, false)
}
func hexDigit(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}
func authority(s string, user, ports bool) bool {
	parts := strings.Split(s, "@")
	if len(parts) == 2 {
		if !user || !username.MatchString(parts[0]) {
			return false
		}
		s = parts[1]
	} else if len(parts) != 1 {
		return false
	}
	re := hostPort
	if strings.HasPrefix(s, "[") {
		re = ipv6Port
	}
	m := re.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	host := m[1]
	if strings.HasPrefix(host, "[") {
		literal := host[1 : len(host)-1]
		if !strings.Contains(literal, ":") || net.ParseIP(literal) == nil {
			return false
		}
	} else if !hostname.MatchString(host) {
		return false
	}
	// An explicit port delimiter must have digits; the regex enforces this.
	if m[2] != "" {
		if !ports {
			return false
		}
		significant := strings.TrimLeft(m[2], "0")
		if len(significant) < 1 || len(significant) > 5 {
			return false
		}
		n, e := strconv.Atoi(significant)
		return e == nil && n <= 65535
	}
	return true
}
func object(v any) map[string]any { m, ok := v.(map[string]any); require(ok, invalid); return m }
func str(v any) string            { s, ok := v.(string); require(ok && utf8.ValidString(s), invalid); return s }
func fields(m map[string]any, required, optional []string) {
	allowed := map[string]bool{}
	for _, k := range required {
		_, exists := m[k]
		require(exists, invalid)
		allowed[k] = true
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for k := range m {
		require(allowed[k], invalid)
	}
}
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func canonical(v any) []byte {
	b, e := wirejson.CanonicalBytes(v)
	require(e == nil, invalid)
	return b
}
func equal(a, b any) bool { return bytes.Equal(canonical(a), canonical(b)) }
func counter(v any) int64 {
	n, ok := integer(v)
	require(ok && n.Sign() > 0 && n.IsInt64(), invalid)
	return n.Int64()
}
func integer(v any) (*big.Int, bool) {
	n := new(big.Int)
	switch x := v.(type) {
	case *big.Int:
		if x != nil {
			return x, true
		}
	case int:
		return n.SetInt64(int64(x)), true
	case int8:
		return n.SetInt64(int64(x)), true
	case int16:
		return n.SetInt64(int64(x)), true
	case int32:
		return n.SetInt64(int64(x)), true
	case int64:
		return n.SetInt64(x), true
	case uint:
		return n.SetUint64(uint64(x)), true
	case uint8:
		return n.SetUint64(uint64(x)), true
	case uint16:
		return n.SetUint64(uint64(x)), true
	case uint32:
		return n.SetUint64(uint64(x)), true
	case uint64:
		return n.SetUint64(x), true
	}
	return nil, false
}
func spend(left, n int) int {
	if n > left {
		fail("transport_refused")
	}
	return left - n
}

// budget checks exact generic native canonical size before any full serialization.
func budget(v any, depth, left int) int {
	switch x := v.(type) {
	case nil:
		return spend(left, 4)
	case bool:
		if x {
			return spend(left, 4)
		}
		return spend(left, 5)
	case string:
		if len(x) > left {
			fail("transport_refused")
		}
		require(utf8.ValidString(x), invalid)
		left = spend(left, 2)
		for _, r := range x {
			n := utf8.RuneLen(r)
			switch r {
			case '"', '\\', '\b', '\f', '\n', '\r', '\t':
				n = 2
			default:
				if r < 32 {
					n = 6
				}
			}
			left = spend(left, n)
		}
		return left
	case []any:
		require(depth < 128, invalid)
		left = spend(left, 2)
		for i, item := range x {
			if i > 0 {
				left = spend(left, 1)
			}
			left = budget(item, depth+1, left)
		}
		return left
	case map[string]any:
		require(depth < 128, invalid)
		left = spend(left, 2)
		i := 0
		for k, item := range x {
			if i > 0 {
				left = spend(left, 1)
			}
			left = budget(k, depth+1, left)
			left = spend(left, 1)
			left = budget(item, depth+1, left)
			i++
		}
		return left
	default:
		n, ok := integer(v)
		require(ok, invalid)
		require(n.BitLen() <= 14285, invalid)
		text := n.String()
		digits := strings.TrimPrefix(text, "-")
		require(len(digits) <= 4300, invalid)
		return spend(left, len(text))
	}
}

// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/wstein/workharbor-nxt/apps/api_client/wirejson"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/protocol"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/storage"
	"os"
	"strings"
	"testing"
)

func admissionFixture(t *testing.T) (map[string]any, protocol.Context, storage.Binding, config.Approval) {
	t.Helper()
	raw, e := os.ReadFile("../../../../../contracts/host-protocol/vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "dedc629e6989fdfaaad8053fe14a0680b692495fdfff88eb1f4e5a29c534d66d" {
		t.Fatal("upstream vector provenance")
	}
	var v struct {
		RequestBytes string `json:"request_bytes"`
		RequestHash  string `json:"request_sha256"`
		Command      string `json:"command_canonical"`
	}
	if json.Unmarshal(raw, &v) != nil {
		t.Fatal("vectors")
	}
	value, e := wirejson.Parse([]byte(v.Command))
	if e != nil {
		t.Fatal(e)
	}
	command := value.(map[string]any)
	owner := command["owner_id"].(string)
	host := command["host_id"].(string)
	workspace := command["workspace_id"].(string)
	candidate := command["bindings"].(map[string]any)["candidate"].(map[string]any)
	ctx := protocol.Context{Peer: &protocol.Peer{Kind: "control_plane", OwnerID: owner, HostID: host}, Snapshot: &protocol.Snapshot{Command: command, RequestBytes: []byte(v.RequestBytes), RequestHash: v.RequestHash, NativeBindings: map[string]any{"target": nil, "candidate": map[string]any{"id": candidate["runtime_id"], "generation": candidate["generation"]}}, CurrentIntent: 1, Status: "queued"}}
	d, e := config.DecodeDescriptor(command["request"].(map[string]any)["parameters"].(map[string]any)["config"])
	if e != nil {
		t.Fatal(e)
	}
	b := storage.Binding{OwnerID: owner, HostID: host, WorkspaceID: workspace, StorageID: "00000000-0000-0000-0000-000000000008", ReservationOperationID: command["operation_id"].(string), Initial: d}
	return command, ctx, b, config.Approval{ID: command["approval_ref"].(string), OwnerID: owner, Descriptor: d}
}
func TestNativeAdmissionEligibleDataNeedsExactApprovalStorageAndCurrentFence(t *testing.T) {
	command, ctx, b, approval := admissionFixture(t)
	validated, e := protocol.Validate(command, ctx)
	if e != nil || validated.Disposition != "new_eligible" {
		t.Fatal(validated, e)
	}
	_, e = Admit(context.Background(), command, ctx, b, approval, nil)
	errorCode(t, e, "forbidden")
	checks := 0
	plan, e := Admit(context.Background(), command, ctx, b, approval, func(context.Context) error { checks++; return nil })
	if e != nil || checks != 1 || plan.Scope().StorageID != b.StorageID || plan.Scope().AllocationOperationID != b.ReservationOperationID || plan.Descriptor().Commit != approval.Descriptor.Commit {
		t.Fatal(plan, e, checks)
	}
	for _, mutate := range []func(*storage.Binding, *config.Approval){func(b *storage.Binding, a *config.Approval) { a.Descriptor.Commit = strings.Repeat("b", 40) }, func(b *storage.Binding, a *config.Approval) { a.OwnerID = b.StorageID }, func(b *storage.Binding, a *config.Approval) { b.OwnerID = b.StorageID }, func(b *storage.Binding, a *config.Approval) { b.Initial.Commit = strings.Repeat("c", 40) }} {
		wrongB, wrongA := b, approval
		mutate(&wrongB, &wrongA)
		checks = 0
		if _, e = Admit(context.Background(), command, ctx, wrongB, wrongA, func(context.Context) error { checks++; return nil }); e == nil || checks != 0 {
			t.Fatal("foreign trust/storage reached current effect fence", e, checks)
		}
	}
	_, e = Admit(context.Background(), command, ctx, b, approval, func(context.Context) error { return errors.New("revoked") })
	if e == nil || e.Error() != "revoked" {
		t.Fatal(e)
	}
}
func TestNativeAdmissionReplayAndReconciliationNeverAuthorizeNewEffects(t *testing.T) {
	command, ctx, b, a := admissionFixture(t)
	validated, e := protocol.Validate(command, ctx)
	if e != nil {
		t.Fatal(e)
	}
	ctx.Snapshot.CommandBytes = validated.Canonical
	for _, status := range []string{"queued", "executing", "succeeded"} {
		ctx.Snapshot.Status = status
		calls := 0
		_, e = Admit(context.Background(), command, ctx, b, a, func(context.Context) error { calls++; return nil })
		errorCode(t, e, "operation_not_eligible")
		if calls != 0 {
			t.Fatal("lookup/reconciliation reached effect fence")
		}
	}
}

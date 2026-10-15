// SPDX-License-Identifier: EUPL-1.2
package docker

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/config"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/protocol"
	"github.com/wstein/workharbor-nxt/apps/host_agent/internal/storage"
)

// Admission is native preparation data with a separately supplied current fence.
// It adds no wire message, transport activation, lifecycle claim or trust grant.
// The lifecycle caller must recheck the fence before every filesystem/daemon effect.
type Admission struct {
	scope      Scope
	descriptor config.Descriptor
	binding    storage.Binding
	authorize  func(context.Context) error
}

func (a Admission) Scope() Scope { return a.scope }
func (a Admission) Descriptor() config.Descriptor {
	d, _ := config.DecodeDescriptor(a.descriptor.Map())
	return d
}
func (a Admission) Binding() storage.Binding {
	b := a.binding
	b.Initial, _ = config.DecodeDescriptor(b.Initial.Map())
	return b
}
func (a Admission) Check(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if a.authorize == nil {
		return fail("forbidden")
	}
	return a.authorize(ctx)
}

// Admit independently revalidates T007 data against its native authenticated
// Context, then matches exact owner approval and authoritative CP storage binding.
// new_eligible alone is insufficient: authorize must verify current host assignment,
// operation/intent/approval and held native fence. It is never derived from body IDs.
func Admit(ctx context.Context, command map[string]any, trusted protocol.Context, b storage.Binding, approval config.Approval, authorize func(context.Context) error) (Admission, error) {
	if authorize == nil {
		return Admission{}, fail("forbidden")
	}
	validated, e := protocol.Validate(command, trusted)
	if e != nil {
		return Admission{}, e
	}
	if validated.Disposition != "new_eligible" || validated.Message["kind"] != "command" {
		return Admission{}, fail("operation_not_eligible")
	}
	m := validated.Message
	request := m["request"].(map[string]any)
	action := request["action"]
	if action != "create" && action != "rebuild" {
		return Admission{}, fail("operation_not_eligible")
	}
	descriptor, e := config.DecodeDescriptor(request["parameters"].(map[string]any)["config"])
	if e != nil {
		return Admission{}, e
	}
	owner, host, workspace, operation := m["owner_id"].(string), m["host_id"].(string), m["workspace_id"].(string), m["operation_id"].(string)
	if e = config.MatchApproval(approval, owner, m["approval_ref"].(string), descriptor); e != nil {
		return Admission{}, e
	}
	if b.OwnerID != owner || b.HostID != host || b.WorkspaceID != workspace || !uuid.MatchString(b.StorageID) || !uuid.MatchString(b.ReservationOperationID) {
		return Admission{}, fail("storage_conflict")
	}
	initial, e := config.DecodeDescriptor(b.Initial.Map())
	if e != nil {
		return Admission{}, fail("storage_conflict")
	}
	if action == "create" && (b.ReservationOperationID != operation || b.ExistingHandle != "" || !reflect.DeepEqual(initial.Map(), descriptor.Map())) {
		return Admission{}, fail("storage_conflict")
	}
	candidate := validated.NativeBindings["candidate"].(map[string]any)
	generation, e := strconv.ParseUint(fmt.Sprint(candidate["generation"]), 10, 64)
	if e != nil {
		return Admission{}, fail("runtime_conflict")
	}
	scope := Scope{OwnerID: owner, HostID: host, WorkspaceID: workspace, RuntimeID: candidate["id"].(string), AllocationOperationID: operation, StorageID: b.StorageID, Generation: generation}
	if !scope.valid() {
		return Admission{}, fail("runtime_conflict")
	}
	b.Initial = initial
	a := Admission{scope: scope, descriptor: descriptor, binding: b, authorize: authorize}
	if e = a.Check(ctx); e != nil {
		return Admission{}, e
	}
	return a, nil
}

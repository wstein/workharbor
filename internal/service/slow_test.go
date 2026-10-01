package service

import (
	"context"

	"github.com/wstein/workharbor/internal/runtime"
)

// runtimeAdapter is the runtime the slow wrapper delegates to.
type runtimeAdapter = runtime.Adapter

// Exec fails the ready command until the clock has reached readyAt.
func (s *slowRuntime) Exec(ctx context.Context, id string, req runtime.ExecRequest) (runtime.ExecStream, error) {
	if s.clock.now.Before(s.readyAt) {
		return nil, runtime.ErrNotRunning
	}
	return s.runtimeAdapter.Exec(ctx, id, req)
}

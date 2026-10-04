package agenttest

import (
	"errors"
	"testing"

	"github.com/wstein/workharbor/internal/agent"
	"github.com/wstein/workharbor/internal/agent/codex"
)

func TestUnmeasuredCodexRefusesProduction(t *testing.T) {
	adapter := codex.New(nil, codex.Config{})
	capabilities := adapter.Capabilities()
	if capabilities.ContractVersion != agent.ContractVersion || capabilities.Mode() != agent.ModeUnsupported || capabilities.HostApprovals || capabilities.MidRunInstruction || capabilities.SessionResume || capabilities.ReportsUsage || len(capabilities.AuthModes) != 0 {
		t.Fatalf("unmeasured capabilities: %+v", capabilities)
	}
	for _, test := range []struct {
		name   string
		resume bool
	}{{"start", false}, {"resume", true}} {
		t.Run(test.name, func(t *testing.T) {
			var session agent.Session
			var err error
			if test.resume {
				session, err = adapter.Resume(t.Context(), agent.StartSpec{}, "native-thread")
			} else {
				session, err = adapter.Start(t.Context(), agent.StartSpec{})
			}
			if session != nil || !errors.Is(err, agent.ErrUnsupported) {
				t.Fatalf("unmeasured launch: %v, %v", session, err)
			}
		})
	}
}

package domain

import "testing"

func TestValidIntegrationBranchComparesTheAgentNamespaceWithoutCase(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"main", true},
		{"agentic/x", true},
		{"agents/x", true},
		{"myagent/x", true},
		{"release/agent", true},
		{"agent/x", false},
		{"Agent/x", false},
		{"AGENT/main", false},
		{"aGeNt/x", false},
		{"agent", false},
		{"Agent", false},
		{"AGENT", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidIntegrationBranch(tc.name); got != tc.want {
				t.Errorf("ValidIntegrationBranch(%q) = %v, want %v", tc.name, got, tc.want)
			}
			if got := InAgentNamespace(tc.name); got == tc.want {
				t.Errorf("InAgentNamespace(%q) = %v, want %v", tc.name, got, !tc.want)
			}
		})
	}
}

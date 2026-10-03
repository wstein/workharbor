package docscheck

import (
	"os"
	"testing"
)

// agentsMaxBytes is the cap Antigravity documents for its always-on rules
// (unverified: documented, not measured). AGENTS.md is read as one, so a longer
// file may lose its end for that lane (issue #233).
const agentsMaxBytes = 24000

// TestAgentsFileFitsTheRuleCap fails when AGENTS.md grows past the cap. Move
// procedure detail to the manual's Sessions and agents page, never a rule.
func TestAgentsFileFitsTheRuleCap(t *testing.T) {
	info, err := os.Stat("../../AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > agentsMaxBytes {
		t.Errorf("AGENTS.md is %d bytes, over the %d-byte cap; move procedure detail to docs/content/docs/manual/sessions-and-agents.md (issue #233)", info.Size(), agentsMaxBytes)
	}
}

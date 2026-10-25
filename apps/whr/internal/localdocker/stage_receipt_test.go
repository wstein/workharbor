//go:build unix

// SPDX-License-Identifier: EUPL-1.2
package localdocker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/exitcode"
)

func TestLocalStageReceiptTargetsAreBoundToDurableHashChain(t *testing.T) {
	s, e := openStageStore(filepath.Join(t.TempDir(), "state"), "daemon", "unix:///tmp/docker.sock")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	op := strings.Repeat("1", 32)
	if e = s.Begin(op, "sha256:"+strings.Repeat("a", 64)); e != nil {
		t.Fatal(e)
	}
	r := stageReceipt{OperationID: op, ToolManifest: map[string]stageToolIdentity{}, OutputInventory: []StageOutputEntry{}, OriginBinding: []StageRepositoryBinding{}, CandidateGitVersion: []stageCandidateGitVersion{}, CloneRepositories: []StageRepository{}, ExportedRefs: []StageResultRef{}, ImportedObjects: []StageGitObjectsTarget{}, ResultRefTransactions: []StageGitRefsTarget{}, ActionInventory: []StageEffect{{Action: "create_volume", Target: StageTarget{Resource: &StageResource{Kind: "volume", Name: stageString("whr-stage-home-original"), DaemonID: "daemon"}}, State: "planned"}}}
	if e = s.saveReceipt(&r, true); e != nil {
		t.Fatal(e)
	}
	if _, e = s.loadReceipt(op); e != nil {
		t.Fatal(e)
	}
	r.ActionInventory[0].Target.Resource.Name = stageString("whr-stage-home-substituted")
	b, _ := json.Marshal(r)
	if e = os.WriteFile(filepath.Join(s.path, op, "receipt.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.loadReceipt(op); exitcode.FromError(e) != exitcode.Conflict || !strings.Contains(e.Error(), "receipt hash-chain binding changed") {
		t.Fatalf("receipt target substitution accepted: %v", e)
	}
}

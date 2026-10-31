// SPDX-License-Identifier: EUPL-1.2

package runtime_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/runtime"
)

type refusalContract struct {
	SchemaVersion int    `json:"schema_version"`
	Contract      string `json:"contract"`
	ExitCode      int    `json:"exit_code"`
	ExitName      string `json:"exit_name"`
	Refusals      []struct {
		Code string `json:"code"`
		When string `json:"when"`
	} `json:"refusals"`
	Freeze struct {
		TestFile string   `json:"test_file"`
		TestIDs  []string `json:"test_ids"`
		Offline  bool     `json:"offline"`
	} `json:"freeze"`
}

func repoFile(t *testing.T, elem ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", "..", "..", "..", "specs", "008-workspace-sandbox"}, elem...)...)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func loadRefusalContract(t *testing.T) refusalContract {
	t.Helper()
	var c refusalContract
	if err := json.Unmarshal([]byte(repoFile(t, "contracts", "refusal-codes.json")), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// pinnedRefusals is the append-only history of the stable codes. A code is
// never reused or renamed; a new code is appended to this list and to the
// contract in the same change.
var pinnedRefusals = []string{
	"refused.profile_unsupported",
	"refused.kernel_isolation_unavailable",
	"refused.egress_enforcement_unavailable",
	"refused.credential_mediation_unavailable",
	"refused.host_tools_unavailable",
	"refused.git_config_profile_unavailable",
	"refused.git_config_single_file_mount",
	"refused.host_git_config_for_agent",
	"refused.shared_git_metadata_untrusted",
	"refused.host_socket_mount",
	"refused.tool_mount_writable",
	"refused.trust_acknowledgement_missing",
	"refused.copy_policy_secret_path",
	"refused.copy_policy_dirty_submodule",
	"refused.foreign_object",
	"refused.primary_target",
	"refused.unexported_results",
	"refused.invalid_state",
	"refused.git_env_override",
	"refused.cleanup_capability_insufficient",
	"refused.git_config_not_ready",
}

func TestRefusalCodesMatchContract(t *testing.T) {
	c := loadRefusalContract(t)
	if c.SchemaVersion != 1 || c.Contract != "whr-sandbox-refusals" {
		t.Fatalf("unexpected contract identity %d %q", c.SchemaVersion, c.Contract)
	}
	if c.ExitCode != 68 || c.ExitName != "conflict" {
		t.Fatalf("refusals map to 68 conflict, contract says %d %s", c.ExitCode, c.ExitName)
	}
	got := runtime.RefusalCodes()
	if len(got) != len(c.Refusals) {
		t.Fatalf("code has %d refusals, contract %d", len(got), len(c.Refusals))
	}
	for i, want := range c.Refusals {
		if string(got[i]) != want.Code {
			t.Errorf("refusal %d: code %q, contract %q", i, got[i], want.Code)
		}
		if want.When == "" {
			t.Errorf("contract refusal %q has no when text", want.Code)
		}
	}
	if c.Freeze.TestFile != "apps/whr/internal/runtime/refusal_test.go" || !c.Freeze.Offline {
		t.Errorf("freeze block moved: %+v", c.Freeze)
	}
	ids := map[string]bool{}
	for _, id := range c.Freeze.TestIDs {
		ids[id] = true
	}
	for _, id := range []string{"TestRefusalCodesMatchContract", "TestRefusalCodesUniqueAndStable"} {
		if !ids[id] {
			t.Errorf("freeze block does not name %s", id)
		}
	}
}

func TestRefusalCodesUniqueAndStable(t *testing.T) {
	shape := regexp.MustCompile(`^refused\.[a-z]+(_[a-z]+)*$`)
	seen := map[runtime.RefusalCode]bool{}
	codes := runtime.RefusalCodes()
	for _, code := range codes {
		if seen[code] {
			t.Errorf("duplicate code %q", code)
		}
		seen[code] = true
		if !shape.MatchString(string(code)) {
			t.Errorf("code %q has an unstable shape", code)
		}
		if !code.Valid() {
			t.Errorf("code %q is listed but not Valid", code)
		}
	}
	if len(codes) < len(pinnedRefusals) {
		t.Fatalf("codes were removed: %d < %d pinned", len(codes), len(pinnedRefusals))
	}
	for i, want := range pinnedRefusals {
		if string(codes[i]) != want {
			t.Errorf("pinned position %d changed: %q, want %q", i, codes[i], want)
		}
	}
	if runtime.RefusalCode("refused.made_up").Valid() || runtime.RefusalCode("").Valid() {
		t.Error("unlisted codes must not be Valid")
	}
	// RefusalCodes returns a copy; mutating it must not change the registry.
	codes[0] = "refused.mutated"
	if runtime.RefusalCodes()[0] != "refused.profile_unsupported" {
		t.Error("RefusalCodes exposes the registry")
	}
}

func TestRefusalErrorCarriesCode(t *testing.T) {
	var err error = runtime.NewRefusal(runtime.RefusedForeignObject)
	wrapped := fmt.Errorf("discard sbx-x: %w", err)
	code, ok := runtime.RefusalCodeOf(wrapped)
	if !ok || code != runtime.RefusedForeignObject {
		t.Fatalf("code not recoverable through wrapping: %q %v", code, ok)
	}
	var r runtime.Refusal
	if !errors.As(wrapped, &r) || r.Code != runtime.RefusedForeignObject {
		t.Fatal("errors.As does not find the Refusal")
	}
	if got := err.Error(); got != "refused.foreign_object" {
		t.Errorf("Error() = %q", got)
	}
	if _, ok := runtime.RefusalCodeOf(errors.New("other")); ok {
		t.Error("a plain error has no refusal code")
	}
}

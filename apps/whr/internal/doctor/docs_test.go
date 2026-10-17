// SPDX-License-Identifier: EUPL-1.2

package doctor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDoctorRemediationLinkResolves verifies the container-runtime page the
// socket remediation points to: it exists, recommends OrbStack and states that
// whr installs nothing, and every relative link in it resolves.
func TestDoctorRemediationLinkResolves(t *testing.T) {
	w := healthy()
	w.sockets = nil
	fix := statusOf(run(w), "container.socket").Remediation
	const rel = "docs/container-runtime.md"
	if !strings.Contains(fix, rel) {
		t.Fatalf("remediation does not name %s: %q", rel, fix)
	}
	root := filepath.Join("..", "..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	for _, want := range []string{"OrbStack", "never", "installs"} {
		if !strings.Contains(page, want) {
			t.Errorf("%s lacks %q", rel, want)
		}
	}
	for _, m := range strings.Split(page, "](")[1:] {
		target, _, _ := strings.Cut(m, ")")
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
			continue
		}
		target, _, _ = strings.Cut(target, "#")
		if _, err := os.Stat(filepath.Join(root, "docs", target)); err != nil {
			t.Errorf("%s links %q which does not resolve: %v", rel, target, err)
		}
	}
}

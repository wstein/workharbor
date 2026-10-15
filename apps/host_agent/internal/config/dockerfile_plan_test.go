// SPDX-License-Identifier: EUPL-1.2
package config

import (
	"strings"
	"testing"
)

func TestDockerfilePlanBindsSelectedStageToExternalBase(t *testing.T) {
	a := "ubuntu@sha256:" + strings.Repeat("a", 64)
	b := "debian@sha256:" + strings.Repeat("b", 64)
	raw := []byte("FROM " + a + " AS base\nRUN true\nFROM base AS app\nFROM " + b + " AS unused\n")
	for _, tt := range []struct{ target, base, stage string }{{"app", a, "app"}, {"", b, "unused"}} {
		p, e := AnalyzeDockerfile(raw, tt.target)
		if e != nil || p.SelectedBase != tt.base || p.Stage != tt.stage || len(p.Bases) != 2 {
			t.Fatal(p, e)
		}
	}
	for _, target := range []string{"missing", "0", "${TARGET}"} {
		_, e := AnalyzeDockerfile(raw, target)
		code(t, e, "unsupported_configuration")
	}
	p, e := AnalyzeDockerfile([]byte("FROM "+a+"\nRUN true\n"), "")
	if e != nil || !p.LastStageUnnamed || p.Stage != "dev_container_auto_added_stage_label" {
		t.Fatal(p, e)
	}
}

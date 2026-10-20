// SPDX-License-Identifier: EUPL-1.2

package commands

import (
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/workspacecfg"
)

func TestValidateRenderReportEscapesEachUntrustedField(t *testing.T) {
	const hostile = "name\x1b[31m\u202e"
	const inert = "name<U+001B>[31m<U+202E>"
	r := &workspacecfg.Report{
		Directory: hostile, ProviderAdmission: workspacecfg.ProviderAdmissionNotEvaluated,
		Counts: workspacecfg.Counts{Error: 1}, Findings: []workspacecfg.Finding{{
			Code: "devcontainer.missing", Severity: workspacecfg.SevError,
			Subject: hostile, Message: hostile, Remediation: hostile,
		}},
	}
	got := renderReport(r)
	if strings.ContainsAny(got, "\x1b\u202e") {
		t.Fatalf("raw terminal controls in report: %q", got)
	}
	for _, want := range []string{"Directory: " + inert + "\n", "devcontainer.missing " + inert + ": " + inert + "\n", "hint: " + inert + "\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing escaped field %q in %q", want, got)
		}
	}
}

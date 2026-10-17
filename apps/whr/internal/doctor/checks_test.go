// SPDX-License-Identifier: EUPL-1.2

package doctor_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wstein/workharbor-nxt/apps/whr/internal/doctor"
)

type checkContract struct {
	Statuses   []string `json:"statuses"`
	Thresholds struct {
		GitMinimum string `json:"git_minimum_version"`
		DiskFail   uint64 `json:"disk_fail_below_bytes"`
	} `json:"thresholds"`
	Sockets struct {
		Order []string `json:"order"`
	} `json:"socket_candidates"`
	Checks []struct {
		ID        string `json:"id"`
		Increment string `json:"increment"`
		Needs     string `json:"needs"`
		Skipped   string `json:"skipped"`
	} `json:"checks"`
	Freeze struct {
		TestFile string   `json:"test_file"`
		TestIDs  []string `json:"test_ids"`
	} `json:"freeze"`
}

func loadChecks(t *testing.T) checkContract {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "cli", "doctor-checks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c checkContract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestDoctorChecksMatchContract freezes ids, order, statuses, thresholds and
// socket candidates against contracts/cli/doctor-checks.json.
func TestDoctorChecksMatchContract(t *testing.T) {
	c := loadChecks(t)
	var ids []string
	for _, k := range c.Checks {
		ids = append(ids, k.ID)
	}
	if got := doctor.CatalogueIDs(); !reflect.DeepEqual(got, ids) {
		t.Errorf("catalogue ids %v, contract %v", got, ids)
	}
	r := doctor.Run(context.Background(), healthy().ports())
	var got []string
	for _, k := range r.Checks {
		got = append(got, k.ID)
	}
	if !reflect.DeepEqual(got, ids) {
		t.Errorf("report ids %v, contract %v", got, ids)
	}
	if !reflect.DeepEqual(doctor.Statuses(), c.Statuses) {
		t.Errorf("statuses %v, contract %v", doctor.Statuses(), c.Statuses)
	}
	if doctor.MinGitVersion != c.Thresholds.GitMinimum {
		t.Errorf("git minimum %q, contract %q", doctor.MinGitVersion, c.Thresholds.GitMinimum)
	}
	if doctor.MinFreeBytes != c.Thresholds.DiskFail {
		t.Errorf("disk threshold %d, contract %d", doctor.MinFreeBytes, c.Thresholds.DiskFail)
	}
	if want := c.Sockets.Order[1:]; !reflect.DeepEqual(doctor.CandidateOrder(), want) {
		t.Errorf("socket candidates %v, contract %v", doctor.CandidateOrder(), want)
	}
	if !strings.HasPrefix(c.Sockets.Order[0], "DOCKER_HOST (unix://") {
		t.Errorf("contract no longer starts with DOCKER_HOST: %q", c.Sockets.Order[0])
	}
	if c.Freeze.TestFile != "apps/whr/internal/doctor/checks_test.go" {
		t.Errorf("freeze file %q", c.Freeze.TestFile)
	}
	for _, id := range c.Freeze.TestIDs {
		if !strings.Contains(frozenSource(t), "func "+id+"(") {
			t.Errorf("frozen test %s is missing", id)
		}
	}
}

func frozenSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("checks_test.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestDoctorNeverPassesUnmeasured breaks every measurement in turn and also
// removes every port: nothing that could not be measured is pass.
func TestDoctorNeverPassesUnmeasured(t *testing.T) {
	boom := errors.New("boom")
	broken := map[string]func(w *world){
		"container.socket":       func(w *world) { w.sockets = map[string]error{"/var/run/docker.sock": boom} },
		"git.version":            func(w *world) { w.gitRunErr = boom },
		"disk.working_directory": func(w *world) { w.freeErr["/work/project"] = boom },
		"disk.container_storage": func(w *world) { w.freeErr["/var/lib/docker"] = boom },
		"platform.host":          func(w *world) { w.arch = "riscv64" },
	}
	for id, brk := range broken {
		w := healthy()
		brk(w)
		if got := statusOf(doctor.Run(context.Background(), w.ports()), id).Status; got == doctor.StatusPass {
			t.Errorf("%s passed with a broken measurement", id)
		}
	}
	// garbage git output is not a version
	w := healthy()
	w.gitOut = "not git at all"
	if got := statusOf(doctor.Run(context.Background(), w.ports()), "git.version").Status; got != doctor.StatusUnknown {
		t.Errorf("unparsable git output: %s", got)
	}
	// no ports at all: nothing passes except what needs no port
	r := doctor.Run(context.Background(), doctor.Ports{})
	for _, k := range r.Checks {
		if k.Status == doctor.StatusPass {
			t.Errorf("%s passed with no ports", k.ID)
		}
		if (k.Status == doctor.StatusSkipped || k.Status == doctor.StatusUnknown) && k.Reason == "" {
			t.Errorf("%s: %s without reason", k.ID, k.Status)
		}
		if k.Remediation == "" && k.Status != doctor.StatusPass {
			t.Errorf("%s: %s without remediation", k.ID, k.Status)
		}
	}
	if r.Verdict == doctor.VerdictOK {
		t.Error("verdict ok without measurements")
	}
}

// TestDoctorLaterChecksReportSkipped keeps the four later checks skipped with
// the contract's reasons, however healthy the machine is.
func TestDoctorLaterChecksReportSkipped(t *testing.T) {
	c := loadChecks(t)
	r := doctor.Run(context.Background(), healthy().ports())
	n := 0
	for _, k := range c.Checks {
		if k.Increment != "later" {
			continue
		}
		n++
		got := statusOf(r, k.ID)
		reason := strings.FieldsFunc(k.Skipped, func(r rune) bool { return r == ':' || r == ' ' })[0]
		if got.Status != doctor.StatusSkipped || got.Reason != reason || got.Remediation == "" {
			t.Errorf("%s: %+v, want skipped with reason %s", k.ID, got, reason)
		}
	}
	if n != 4 {
		t.Fatalf("contract has %d later checks, want 4", n)
	}
}

// SPDX-License-Identifier: EUPL-1.2
package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func traceFiles(t *testing.T, log *[]string, failRemove bool) {
	t.Helper()
	before := identityFiles
	ops := before
	ops.openDir = func(d string) (durableFile, error) {
		*log = append(*log, "opendir:"+d)
		return before.openDir(d)
	}
	ops.link = func(a, b string) error { *log = append(*log, "link"); return before.link(a, b) }
	ops.remove = func(p string) error {
		*log = append(*log, "remove")
		if failRemove {
			return os.ErrPermission
		}
		return before.remove(p)
	}
	identityFiles = ops
	t.Cleanup(func() { identityFiles = before })
}

func TestPrepareSyncsParentAfterMkdirAndRemovesTempBeforeDirectorySync(t *testing.T) {
	root := physicalTemp(t)
	dir := filepath.Join(root, "identity")
	var log []string
	traceFiles(t, &log, false)
	if _, e := Prepare(dir, "https://localhost:8443", ""); e != nil {
		t.Fatal(e)
	}
	want := []string{"opendir:" + root, "link", "remove", "opendir:" + dir}
	if len(log) != len(want) {
		t.Fatalf("order %v", log)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("order %v", log)
		}
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".identity-*"))
	if len(left) != 0 {
		t.Fatal("temp key copy remains")
	}
}

func TestPrepareRefusesWhenTempRemovalFails(t *testing.T) {
	var log []string
	traceFiles(t, &log, true)
	if _, e := Prepare(filepath.Join(physicalTemp(t), "identity"), "https://localhost:8443", ""); e == nil {
		t.Fatal("reported success without durable removal")
	}
}

func TestPrepareRefusesWhenParentSyncAfterMkdirFails(t *testing.T) {
	for _, stage := range []string{"parent_open", "parent_sync", "parent_close"} {
		dir := filepath.Join(physicalTemp(t), "identity")
		reset := failFiles(t, stage)
		s, e := Prepare(dir, "https://localhost:8443", "")
		reset()
		if s != nil || e == nil {
			t.Fatalf("%s: success without durable directory", stage)
		}
		if _, err := os.Lstat(filepath.Join(dir, "identity.json")); err == nil {
			t.Fatalf("%s: identity written before directory was durable", stage)
		}
	}
}

func TestPrepareRetrySyncsParentWhenDirectoryAlreadyExists(t *testing.T) {
	dir := filepath.Join(physicalTemp(t), "identity")
	reset := failFiles(t, "parent_sync")
	_, e := Prepare(dir, "https://localhost:8443", "")
	reset()
	if e == nil {
		t.Fatal("first attempt should fail")
	}
	var log []string
	traceFiles(t, &log, false)
	if _, e = Prepare(dir, "https://localhost:8443", ""); e != nil {
		t.Fatal(e)
	}
	if len(log) == 0 || log[0] != "opendir:"+filepath.Dir(dir) {
		t.Fatalf("retry skipped parent sync: %v", log)
	}
}

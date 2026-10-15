// SPDX-License-Identifier: EUPL-1.2
package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStorageInspectReturnsOriginalOwnerReadOnlyAcrossRebuild(t *testing.T) {
	s, b, source := fixture(t)
	h, e := s.Seed(context.Background(), b, source, allow)
	if e != nil {
		t.Fatal(e)
	}
	edited := []byte{0, 255, 7, 128}
	os.WriteFile(filepath.Join(h.Data, "binary"), edited, 0600)
	before, e := os.ReadDir(s.Root)
	if e != nil {
		t.Fatal(e)
	}
	b.ExistingHandle = h.Parent
	b.DataUID++
	b.DataGID++
	actual, found, e := s.Inspect(context.Background(), b, allow)
	if e != nil || !found || actual.Binding.DataUID != h.Binding.DataUID || actual.Binding.DataGID != h.Binding.DataGID || actual.Parent != h.Parent {
		t.Fatal("initial owner lost", actual, found, e)
	}
	after, _ := os.ReadDir(s.Root)
	if len(before) != len(after) {
		t.Fatal("inspection mutated store")
	}
	raw, e := os.ReadFile(filepath.Join(h.Data, "binary"))
	if e != nil || string(raw) != string(edited) {
		t.Fatal("retained bytes changed")
	}
}
func TestStorageInspectAbsentAndKnownMissingHaveDistinctOutcomes(t *testing.T) {
	s, b, _ := fixture(t)
	_, found, e := s.Inspect(context.Background(), b, allow)
	if e != nil || found {
		t.Fatal("new absent storage unavailable", found, e)
	}
	entries, _ := os.ReadDir(s.Root)
	if len(entries) != 0 {
		t.Fatal("inspection created lock/temp")
	}
	b.ExistingHandle = filepath.Join(s.Root, b.StorageID)
	_, found, e = s.Inspect(context.Background(), b, allow)
	assertCode(t, e, "storage_unavailable")
	if found {
		t.Fatal("known missing adopted")
	}
}
func TestStorageInspectForeignInitialBindingAndMarkerPreserved(t *testing.T) {
	s, b, source := fixture(t)
	h, e := s.Seed(context.Background(), b, source, allow)
	if e != nil {
		t.Fatal(e)
	}
	original, _ := os.ReadFile(filepath.Join(h.Parent, "seed.json"))
	b.ReservationOperationID = "00000000-0000-4000-8000-000000000099"
	if _, _, e = s.Inspect(context.Background(), b, allow); e == nil {
		t.Fatal("foreign initial binding")
	}
	raw, _ := os.ReadFile(filepath.Join(h.Parent, "seed.json"))
	if string(raw) != string(original) {
		t.Fatal("foreign final repaired")
	}
}

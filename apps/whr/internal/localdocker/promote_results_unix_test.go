// SPDX-License-Identifier: EUPL-1.2
//go:build unix

package localdocker

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLocalPromotionResultPreviewLeavesRetainedStateUntouched(t *testing.T) {
	captured, clone := fixtureStageResult(t)
	commitStageResult(t, clone, "candidate result")
	parent := filepath.Dir(clone)
	stamp := time.Unix(946684800, 0)
	if err := os.Chtimes(parent, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	plan, err := prepareStageResults(context.Background(), strings.Repeat("f", 32), captured.Repositories)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	after, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if !reflect.DeepEqual(names, got) || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatalf("result preview wrote retained operation state: before=%v after=%v", names, got)
	}
	if info, err := os.Stat(plan.root); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("result reader temporary root is not private")
	}
}

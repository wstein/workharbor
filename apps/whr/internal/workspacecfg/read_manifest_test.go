// SPDX-License-Identifier: EUPL-1.2

package workspacecfg

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadManifestUsesCheckManifestOnly(t *testing.T) {
	folder := t.TempDir()
	raw := []byte(`{"schema_version":2,"devcontainer_path":"missing.json","projects_path":"missing","bundles":[],"human_shell":"nu"}`)
	if err := os.WriteFile(filepath.Join(folder, ManifestFile), raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadManifest(context.Background(), folder)
	if err != nil {
		t.Fatal(err)
	}
	if want := CheckManifest(raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestReadManifestMissingAndCanceled(t *testing.T) {
	folder := t.TempDir()
	if _, err := ReadManifest(context.Background(), folder); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
	if _, err := ReadManifest(context.Background(), filepath.Join(folder, "missing")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing directory: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadManifest(ctx, folder); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %v", err)
	}
}

func TestManifestRejectedHumanShellDiagnostic(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{`"sh"`, `human_shell "sh" is not one of the allowed values`},
		{`null`, `human_shell must be a string (got null)`},
		{`false`, `human_shell must be a string (got boolean)`},
		{`7`, `human_shell must be a string (got number)`},
		{`[]`, `human_shell must be a string (got array)`},
		{`{}`, `human_shell must be a string (got object)`},
	} {
		raw := []byte(`{"schema_version":2,"devcontainer_path":"d.json","projects_path":"projects","bundles":[],"human_shell":` + tc.value + `}`)
		result := CheckManifest(raw)
		if len(result.Findings) != 1 || result.Findings[0].Code != "manifest.schema" || result.Findings[0].Message != tc.want || result.Manifest.HumanShell != "" {
			t.Fatalf("value %s: got %#v", tc.value, result)
		}
	}
}

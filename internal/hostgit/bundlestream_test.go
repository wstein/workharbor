package hostgit

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestStreamBundleCarriesTheCommitsAfterTheBase(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := newBundleRig(t)
	r.commit(2, 0)
	tip, err := r.repo.ImportBundle(ctx, "agent/docs", bytes.NewReader(r.raw()), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := r.repo.StreamBundle(ctx, &buf, r.base, tip, 1<<20); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "# v2 git bundle\n") && !strings.HasPrefix(buf.String(), "# v3 git bundle\n") {
		t.Fatalf("not a bundle: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "-"+r.base) {
		t.Errorf("the bundle does not name the prerequisite %s", r.base)
	}
	// the guest side takes it: its clone holds the prerequisite
	path := t.TempDir() + "/in.bundle"
	mustWrite(t, path, buf.Bytes())
	mustGit(t, r.env, r.guest, "bundle", "verify", path)
	// no ref is left behind
	if out, err := r.repo.Run(ctx, "for-each-ref", "refs/whr"); err != nil || len(out) != 0 {
		t.Errorf("refs left: %q %v", out, err)
	}
	if err := r.repo.StreamBundle(ctx, &buf, r.base, tip, 10); !errors.Is(err, ErrBundleTooLarge) {
		t.Errorf("over the cap: %v", err)
	}
	if err := r.repo.StreamBundle(ctx, &buf, "main", tip, 1<<20); err == nil {
		t.Error("a ref name was accepted for the base")
	}
}

func mustWrite(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

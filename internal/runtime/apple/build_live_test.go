//go:build applecontainer

package apple

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/runtime"
)

// TestBuildLive builds an image from a context directory with a Dockerfile
// outside it, a build argument and a COPY, then runs it (container 1.5.0).
func TestBuildLive(t *testing.T) {
	if _, err := exec.LookPath("container"); err != nil {
		t.Skip("the container CLI is not installed")
	}
	base := os.Getenv("WHR_TEST_IMAGE")
	if base == "" {
		base = "fedora"
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctxDir := filepath.Join(dir, "ctx")
	if err := os.MkdirAll(ctxDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctxDir, "hello.txt"), []byte("from the context\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dockerfile := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dockerfile, []byte("FROM "+base+"\nARG WHO=nobody\nCOPY hello.txt /hello.txt\nRUN echo \"$WHO\" > /who\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tag := "whtmp/build-live:" + strings.ToLower(filepath.Base(dir))
	a, err := New("wh-build-live", WithTemp("wh/runtime", "build-live"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	t.Cleanup(func() { _ = exec.Command("container", "image", "delete", tag).Run() }) //nolint:gosec,noctx // test cleanup of the image it built
	if _, err := a.Build(ctx, runtime.BuildSpec{Tag: tag, ContextDir: ctxDir, Dockerfile: dockerfile, Args: map[string]string{"WHO": "agent"}}); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(ctx, "container", "run", "--rm", tag, "cat", "/hello.txt", "/who").Output() //nolint:gosec // tag is built above
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if got := string(out); got != "from the context\nagent\n" {
		t.Errorf("the built image printed %q", got)
	}
}

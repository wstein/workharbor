//go:build applecontainer

package apple

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/wstein/workharbor/internal/runtime"
)

// TestMissingBuiltImageIsNeverFetchedLive measures rule 4a on container 1.5.0:
// Provision of a missing whr.invalid image fails with the build-it error
// before the CLI is asked to create anything, and `container create` itself
// (the thing the check guards) tries the network for a missing image.
func TestMissingBuiltImageIsNeverFetchedLive(t *testing.T) {
	if _, err := exec.LookPath("container"); err != nil {
		t.Skip("the container CLI is not installed")
	}
	a, err := New("wh-pull-live", WithTemp("wh/runtime", "pull-live"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	spec := baseSpec()
	spec.Owner = "wh-pull-live"
	spec.Image = runtime.BuiltImageHost + "whtmp-missing:none"
	spec.Network.Name = "whtmp-pull-net"
	start := time.Now()
	_, err = a.Provision(ctx, preparedIn(t, testHome(t), spec))
	if !errors.Is(err, runtime.ErrInvalidSpec) || !strings.Contains(err.Error(), "is not here") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("the refusal took %s: it reached the network", d)
	}
	out, err := exec.CommandContext(ctx, "container", "create", "--name", "whtmp-pull-probe", //nolint:gosec // fixed arguments
		"--label", "workharbor.temp=true", "--label", "workharbor.lane=wh/runtime", "--label", "workharbor.purpose=pull-live",
		spec.Image).CombinedOutput()
	t.Cleanup(func() { _ = exec.Command("container", "delete", "whtmp-pull-probe").Run() }) //nolint:gosec,noctx // test cleanup
	if err == nil || !strings.Contains(string(out), "failed to resolve") {
		t.Errorf("container create of a missing image: err %v, output %q", err, out)
	}
}

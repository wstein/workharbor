package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDSCL executes only a test-owned shell script, never the host's dscl.
type fakeDSCL struct{ path string }

func (r fakeDSCL) Output(ctx context.Context, argv ...string) ([]byte, error) {
	if argv[0] != "dscl" {
		return scripted{}.Output(ctx, argv...)
	}
	cmd := exec.CommandContext(ctx, r.path, argv[1:]...) //nolint:gosec // executes only the test-owned fake dscl
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && len(ee.Stderr) > 0 {
		err = fmt.Errorf("%w: %s", err, strings.TrimSpace(string(ee.Stderr)))
	}
	return out, err
}

func TestAccountFailureClassificationWithFakeDSCL(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       Status
		detail     string
	}{
		{"not found exit", "exit 56", Fail, "no user"},
		{"not found stdout", "echo 'record does not exist'; exit 70", Fail, "no user"},
		{"permission", "echo 'permission denied' >&2; exit 1", NotVerified, "exit status 1: permission denied"},
		{"unexpected stdout", "echo 'directory service unavailable'; exit 70", NotVerified, "directory service unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dscl")
			if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tc.body+"\n"), 0o700); err != nil { //nolint:gosec // executable test-owned script, with no secrets
				t.Fatal(err)
			}
			d := hostDeps(fakeDSCL{path})
			d.Account = "operator"
			got, detail := status(steps(t, d)["workharbor-user"])
			if got != tc.want || !strings.Contains(detail, tc.detail) {
				t.Fatalf("got %s %q, want %s containing %q", got, detail, tc.want, tc.detail)
			}
		})
	}
}

func TestFailureExitStatusDoesNotMatchPrefixes(t *testing.T) {
	for _, tc := range []struct {
		step, failure string
		want          Status
	}{
		{"workharbor-user", "exit status 56", Fail},
		{"workharbor-user", "exit status 560", NotVerified},
		{"workharbor-user", "exit status 560: unexpected", NotVerified},
		{"brew-packages", "exit status 1", Fail},
		{"brew-packages", "exit status 10", NotVerified},
		{"brew-packages", "exit status 137", NotVerified},
	} {
		t.Run(tc.step+"/"+tc.failure, func(t *testing.T) {
			d := hostDeps(failing{errors.New(tc.failure)})
			d.Account = "operator"
			got, detail := status(steps(t, d)[tc.step])
			if got != tc.want {
				t.Fatalf("got %s %q, want %s", got, detail, tc.want)
			}
			if got == NotVerified && !strings.Contains(detail, tc.failure) {
				t.Fatalf("failure hidden: %q", detail)
			}
		})
	}
}

func TestBrewStructuredExitStatusTakesPrecedenceOverDiagnostic(t *testing.T) {
	for _, code := range []string{"1", "10", "137"} {
		t.Run(code, func(t *testing.T) {
			err := exec.CommandContext(t.Context(), "sh", "-c", "exit "+code).Run() //nolint:gosec // fixed test exit statuses
			d := hostDeps(failing{fmt.Errorf("%w: diagnostic mentions exit status 1", err)})
			got, detail := status(steps(t, d)["brew-packages"])
			want := NotVerified
			if code == "1" {
				want = Fail
			}
			if got != want {
				t.Fatalf("got %s %q, want %s", got, detail, want)
			}
		})
	}
}

func TestLegacyLookupRetainsStdoutAndUncertainty(t *testing.T) {
	for _, tc := range []struct {
		body   string
		want   Status
		detail string
	}{
		{"echo 'record does not exist'; exit 70", Fail, "no user"},
		{"echo 'directory service unavailable'; exit 70", NotVerified, "directory service unavailable"},
	} {
		t.Run(tc.detail, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dscl")
			script := "#!/bin/sh\nif [ \"$3\" = /Users/workharbor ]; then exit 56; fi\n" + tc.body + "\n"
			if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // executable test-owned script, with no secrets
				t.Fatal(err)
			}
			c := steps(t, hostDeps(fakeDSCL{path}))["workharbor-user"]
			got, detail := status(c)
			if got != tc.want || !strings.Contains(detail, tc.detail) {
				t.Fatalf("got %s %q, want %s containing %q", got, detail, tc.want, tc.detail)
			}
			if got == NotVerified && len(c.Fix.Cmds) != 0 {
				t.Fatal("uncertain legacy lookup offers account creation")
			}
		})
	}
}

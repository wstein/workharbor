package scripts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodSHA = "0123456789abcdef0123456789abcdef01234567"

// fakeGH puts a gh on PATH that records its argv, one call per line, and answers
// call N with responses[N-1] (the last one repeats; a "!" response is an API
// error). Each response is the tab-separated run lines gh -q would print.
func fakeGH(t *testing.T, responses ...string) (bin, log string) {
	t.Helper()
	bin = t.TempDir()
	log = filepath.Join(bin, "gh.log")
	for i, r := range responses {
		if err := os.WriteFile(filepath.Join(bin, fmt.Sprintf("resp.%d", i+1)), []byte(r), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := `#!/bin/sh
echo "$*" >> "` + log + `"
n=$(wc -l < "` + log + `" | tr -d ' ')
last=` + fmt.Sprint(len(responses)) + `
[ "$n" -gt "$last" ] && n=$last
f="` + bin + `/resp.$n"
[ "$(cat "$f")" = "!" ] && { echo "boom" >&2; exit 1; }
cat "$f"
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil { //nolint:gosec // a fake executable
		t.Fatal(err)
	}
	return bin, log
}

func waitCI(t *testing.T, bin string, env ...string) (string, error) {
	t.Helper()
	base := []string{
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"SHA=" + goodSHA, "REPO=wstein/workharbor",
		"WAIT_INTERVAL_SECONDS=1", "WAIT_TIMEOUT_SECONDS=5", "NO_RUN_GRACE_SECONDS=3",
	}
	return bash(t, append(base, env...), "./wait-for-ci.sh")
}

func calls(t *testing.T, log string) int {
	t.Helper()
	b, err := os.ReadFile(log) //nolint:gosec // a test path
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "\n")
}

func run(id, status, conclusion string) string {
	return fmt.Sprintf("%s\t%s\t%s\thttps://github.com/wstein/workharbor/actions/runs/%s\n", id, status, conclusion, id)
}

func TestWaitForCISuccessAfterPolls(t *testing.T) {
	t.Parallel()
	bin, log := fakeGH(t, run("7", "queued", "-"), run("7", "in_progress", "-"), run("7", "completed", "success"))
	out, err := waitCI(t, bin)
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if n := calls(t, log); n != 3 {
		t.Errorf("gh called %d times, want 3", n)
	}
	if !strings.Contains(out, "ci passed") || !strings.Contains(out, "runs/7") {
		t.Errorf("output lacks the run: %s", out)
	}
	b, _ := os.ReadFile(log) //nolint:gosec // a test path
	if !strings.Contains(string(b), "head_sha="+goodSHA) || !strings.Contains(string(b), "event=push") {
		t.Errorf("query is not for the SHA and push event: %s", b)
	}
}

func TestWaitForCIFailsAtOnceOnNonSuccess(t *testing.T) {
	t.Parallel()
	for _, c := range []string{"failure", "cancelled", "timed_out", "skipped"} {
		bin, log := fakeGH(t, run("8", "completed", c))
		out, err := waitCI(t, bin)
		if err == nil || !strings.Contains(out, c) {
			t.Errorf("%s: err = %v, out = %s", c, err, out)
		}
		if n := calls(t, log); n != 1 {
			t.Errorf("%s: gh called %d times, want 1", c, n)
		}
	}
}

func TestWaitForCINewestCompletedRunDecides(t *testing.T) {
	t.Parallel()
	// Newest first: a green re-run after an older failure passes; the reverse fails.
	bin, _ := fakeGH(t, run("9", "completed", "success")+run("8", "completed", "failure"))
	if out, err := waitCI(t, bin); err != nil {
		t.Errorf("newest success: err = %v\n%s", err, out)
	}
	bin, _ = fakeGH(t, run("9", "completed", "failure")+run("8", "completed", "success"))
	if _, err := waitCI(t, bin); err == nil {
		t.Error("newest failure with an older success passed")
	}
	// A pending run keeps the wait going even when an older run failed.
	bin, log := fakeGH(t, run("9", "in_progress", "-")+run("8", "completed", "failure"), run("9", "completed", "success")+run("8", "completed", "failure"))
	if out, err := waitCI(t, bin); err != nil || calls(t, log) != 2 {
		t.Errorf("pending re-run: err = %v, calls = %d\n%s", err, calls(t, log), out)
	}
}

func TestWaitForCITimeout(t *testing.T) {
	t.Parallel()
	bin, log := fakeGH(t, run("7", "in_progress", "-"))
	out, err := waitCI(t, bin, "WAIT_TIMEOUT_SECONDS=3")
	if err == nil || !strings.Contains(out, "within 3s") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if n := calls(t, log); n != 4 { // polls at 0, 1, 2 and 3 seconds
		t.Errorf("gh called %d times, want 4", n)
	}
}

func TestWaitForCINoRunAfterGrace(t *testing.T) {
	t.Parallel()
	bin, log := fakeGH(t, "")
	out, err := waitCI(t, bin, "NO_RUN_GRACE_SECONDS=4")
	if err == nil || !strings.Contains(out, "no ci run") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if n := calls(t, log); n != 4 { // the production 15 s interval and 60 s grace are 4 polls too
		t.Errorf("gh called %d times, want 4", n)
	}
}

func TestWaitForCIRunAppearsWithinGrace(t *testing.T) {
	t.Parallel()
	bin, _ := fakeGH(t, "", "", run("7", "completed", "success"))
	if out, err := waitCI(t, bin); err != nil {
		t.Errorf("err = %v\n%s", err, out)
	}
}

func TestWaitForCIAPIErrorFails(t *testing.T) {
	t.Parallel()
	bin, _ := fakeGH(t, "!")
	if _, err := waitCI(t, bin); err == nil {
		t.Error("an API error passed")
	}
}

func TestWaitForCIValidatesInputBeforeGH(t *testing.T) {
	t.Parallel()
	for _, env := range [][]string{
		{"SHA=abc"},
		{"SHA=" + strings.ToUpper(goodSHA)},
		{"SHA=" + goodSHA + "; touch pwned"},
		{"SHA="},
		{"REPO=wstein"},
		{"REPO=a/b;c"},
		{"WAIT_INTERVAL_SECONDS=1s"},
		{"WAIT_TIMEOUT_SECONDS=-1"},
		{"NO_RUN_GRACE_SECONDS=$(id)"},
		{"WAIT_INTERVAL_SECONDS=0"},
	} {
		bin, log := fakeGH(t, run("7", "completed", "success"))
		out, err := waitCI(t, bin, env...)
		if err == nil {
			t.Errorf("%v accepted\n%s", env, out)
		}
		if n := calls(t, log); n != 0 {
			t.Errorf("%v: gh called %d times before validation", env, n)
		}
	}
}

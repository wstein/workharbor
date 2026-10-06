package scripts_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// #329: check-local runs no tests, so make land runs test-commitlint-consumers.
// These tests exercise the real target and the real land recipe against
// fixtures, never the real service package (its own fixtures are not under
// test here).

func makefileText(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// consumerTarget returns the Makefile lines of the real target and its variables.
func consumerTarget(t *testing.T) string {
	t.Helper()
	mk := makefileText(t)
	var out []string
	for _, line := range strings.Split(mk, "\n") {
		if strings.HasPrefix(line, "CONSUMER_PKGS :=") || strings.HasPrefix(line, "CONSUMER_TEST_TIMEOUT :=") {
			out = append(out, line)
		}
	}
	_, recipe, ok := strings.Cut(mk, "\ntest-commitlint-consumers:\n")
	if !ok || len(out) != 2 {
		t.Fatal("test-commitlint-consumers target or its variables missing")
	}
	recipe, _, _ = strings.Cut(recipe, "\n\n")
	return strings.Join(out, "\n") + "\ntest-commitlint-consumers:\n" + recipe + "\n"
}

func TestConsumerTargetDefinition(t *testing.T) {
	mk := consumerTarget(t)
	for _, want := range []string{
		"CONSUMER_TEST_TIMEOUT := 300s",
		"-timeout $(CONSUMER_TEST_TIMEOUT)",
		"GOENV=off GOFLAGS= go test",
		"-count=1",
		"./internal/commitlint ./cmd/commitlint ./internal/hostgit ./internal/serve ./internal/service",
	} {
		if !strings.Contains(mk, want) {
			t.Errorf("target lacks %q:\n%s", want, mk)
		}
	}
}

// consumerFixture is a module with the five consumer packages, each with one test.
func consumerFixture(t *testing.T, bodies map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	writeConsumerPackages(t, dir, bodies)
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(consumerTarget(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeConsumerPackages writes a go.mod and the five consumer packages into dir.
func writeConsumerPackages(t *testing.T, dir string, bodies map[string]string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"internal/commitlint", "cmd/commitlint", "internal/hostgit", "internal/serve", "internal/service"} {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o700); err != nil {
			t.Fatal(err)
		}
		body := bodies[p]
		src := "package x\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nvar _ = time.Second\n\nfunc TestX(t *testing.T) {\n" + body + "}\n"
		if err := os.WriteFile(filepath.Join(dir, p, "x_test.go"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func runConsumers(t *testing.T, dir string, args ...string) (string, time.Duration, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "make", append([]string{"-s", "test-commitlint-consumers"}, args...)...) //nolint:gosec // fixed make target, test-controlled arguments
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	start := time.Now()
	out, err := cmd.CombinedOutput()
	return string(out), time.Since(start), err
}

func TestConsumerTargetBlocksAFailingConsumer(t *testing.T) {
	dir := consumerFixture(t, map[string]string{"internal/hostgit": "\tt.Fatal(\"consumer broke\")\n"})
	out, _, err := runConsumers(t, dir)
	if err == nil || !strings.Contains(out, "consumer broke") {
		t.Fatalf("a failing consumer test must fail the target: %v\n%s", err, out)
	}
	good := consumerFixture(t, nil)
	if out, _, err := runConsumers(t, good); err != nil {
		t.Fatalf("passing consumers: %v\n%s", err, out)
	}
}

func TestConsumerTargetTimesOutAHang(t *testing.T) {
	dir := consumerFixture(t, map[string]string{"internal/service": "\ttime.Sleep(time.Minute)\n"})
	out, took, err := runConsumers(t, dir, "CONSUMER_TEST_TIMEOUT=2s")
	if err == nil || !strings.Contains(out, "panic: test timed out") {
		t.Fatalf("a hang must fail with a test timeout: %v\n%s", err, out)
	}
	if took > 45*time.Second {
		t.Fatalf("hang took %s to fail", took)
	}
}

func TestLandRunsAndHonoursTheConsumerGate(t *testing.T) {
	t.Run("runs between commitlint and the secret scan", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.stubChecks("check-local commitlint test-commitlint-consumers check-generated secrets-range:\n\t@echo $@ >> checks-ran\n")
		wt := r.topic("topic")
		if out, err := r.land(wt, nil); err != nil {
			t.Fatalf("land: %v\n%s", err, out)
		}
		logged, err := os.ReadFile(filepath.Join(wt, "checks-ran")) //nolint:gosec // fixed gate log in an isolated test repository
		if err != nil {
			t.Fatal(err)
		}
		if want := "check-local\ncommitlint\ntest-commitlint-consumers\nsecrets-range\n"; string(logged) != want {
			t.Fatalf("gate order = %q, want %q", logged, want)
		}
	})
	t.Run("a failing consumer blocks the land", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.stubChecks("check-local commitlint check-generated secrets-range:\n\t@echo $@ >> checks-ran\n" +
			"test-commitlint-consumers:\n\t@echo consumer-test-failed >&2; exit 1\n")
		wt := r.topic("topic")
		r.wantRefused(wt, "consumer-test-failed")
		logged, _ := os.ReadFile(filepath.Join(wt, "checks-ran")) //nolint:gosec // fixed gate log in an isolated test repository
		if strings.Contains(string(logged), "secrets-range") {
			t.Fatalf("later gates ran after the consumer failure: %q", logged)
		}
	})
	t.Run("ignores the caller's -i and MAKEFLAGS", func(t *testing.T) {
		r := newLandBranchRepo(t, false)
		r.stubChecks("check-local commitlint check-generated secrets-range:\n\t@:\n" +
			"test-commitlint-consumers:\n\t@echo consumer-test-failed >&2; exit 1\n")
		wt := r.topic("topic")
		base := r.git(r.dir, "rev-parse", "main")
		// The outer -i ignores the recipe's exit status, so only the output and main count.
		out, err := r.land(wt, []string{"MAKEFLAGS=i"}, "-i")
		if !strings.Contains(out, "consumer-test-failed") {
			t.Fatalf("want the consumer failure, got %v\n%s", err, out)
		}
		if got := r.git(r.dir, "rev-parse", "main"); got != base {
			t.Fatalf("main moved to %s", got)
		}
	})
	t.Run("the caller's go environment cannot turn the gate into a pass", func(t *testing.T) {
		envFile := filepath.Join(t.TempDir(), "goenv")
		if err := os.WriteFile(envFile, []byte("GOFLAGS=-run=NONE\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, env := range [][]string{
			{"GOFLAGS=-run=NONE"},
			{"GOFLAGS=-exec=true"},
			{"GOFLAGS=-skip=TestX"},
			{"GOENV=" + envFile},
		} {
			r := newLandBranchRepo(t, false)
			writeConsumerPackages(t, r.dir, map[string]string{"internal/hostgit": "\tt.Fatal(\"consumer broke\")\n"})
			r.git(r.dir, "add", ".")
			r.stubChecks("check-local commitlint check-generated secrets-range:\n\t@:\n" + consumerTarget(t))
			wt := r.topic("topic")
			base := r.git(r.dir, "rev-parse", "main")
			out, err := r.land(wt, env)
			if err == nil || !strings.Contains(out, "consumer broke") {
				t.Fatalf("%v: want the consumer failure, got %v\n%s", env, err, out)
			}
			if got := r.git(r.dir, "rev-parse", "main"); got != base {
				t.Fatalf("%v: main moved to %s", env, got)
			}
		}
	})
}

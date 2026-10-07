package scripts

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeTool is a command that appends to a counter file, prints msg and exits
// with the code of its call N (the last one repeats).
func fakeTool(t *testing.T, msg string, codes ...int) (tool, count string) {
	t.Helper()
	dir := t.TempDir()
	count = filepath.Join(dir, "count")
	var cases []string
	for i, c := range codes {
		cases = append(cases, strconv.Itoa(i+1)+") exit "+strconv.Itoa(c)+" ;;")
	}
	cases = append(cases, "*) exit "+strconv.Itoa(codes[len(codes)-1])+" ;;")
	script := "#!/bin/sh\necho x >> '" + count + "'\necho '" + msg + "'\n" +
		"n=$(wc -l < '" + count + "' | tr -d ' ')\ncase $n in\n" + strings.Join(cases, "\n") + "\nesac\n"
	tool = filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, []byte(script), 0o755); err != nil { //nolint:gosec // a fake executable
		t.Fatal(err)
	}
	return tool, count
}

func TestGovulncheckRetry(t *testing.T) {
	tests := []struct {
		name  string
		msg   string
		codes []int
		want  int
		fails bool
	}{
		{"pass first", "ok", []int{0}, 1, false},
		{"5xx then pass", "unexpected status 504 Gateway Timeout", []int{1, 0}, 2, false},
		{"network error then pass", "dial tcp: connection reset by peer", []int{1, 1, 0}, 3, false},
		{"5xx forever stops at 3", "HTTP 503", []int{1}, 3, true},
		{"vulnerability exit 3 never retried", "Vulnerability #1 status 504", []int{3}, 1, true},
		{"other error not retried", "build failed", []int{1}, 1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tool, count := fakeTool(t, tc.msg, tc.codes...)
			_, err := bash(t, []string{"PATH=" + os.Getenv("PATH"), "RETRY_PAUSE_SECONDS=0"}, "./govulncheck-retry.sh "+tool)
			if (err != nil) != tc.fails {
				t.Fatalf("err = %v, want failure %v", err, tc.fails)
			}
			b, _ := os.ReadFile(count) //nolint:gosec // test temp file
			if got := strings.Count(string(b), "x"); got != tc.want {
				t.Fatalf("runs = %d, want %d", got, tc.want)
			}
		})
	}
}

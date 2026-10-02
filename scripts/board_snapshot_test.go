package scripts

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const fakeBoard = `{"items":[
{"title":"b","status":"Todo","session":"wh/platform","priority":"P2","labels":["x"],"content":{"number":20,"type":"Issue","url":"https://example/20"}},
{"title":"a","status":"Todo","session":"wh/platform","priority":"P1","content":{"number":30,"type":"Issue","url":"https://example/30"}},
{"title":"c","status":"Todo","session":"wh/platform","priority":"P2","content":{"number":10,"type":"Issue","url":"https://example/10"}},
{"title":"d","status":"In progress","session":"wh/platform","priority":"P1","content":{"number":40,"type":"Issue","url":"https://example/40"}},
{"title":"e","status":"Todo","session":"wh/runtime","priority":"P1","content":{"number":50,"type":"Issue","url":"https://example/50"}}
],"totalCount":5}`

type board struct {
	bin, snap, log string
}

func newBoard(t *testing.T) board {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not installed")
	}
	b := board{bin: t.TempDir()}
	b.snap = filepath.Join(t.TempDir(), "cache", "board.json")
	b.log = filepath.Join(b.bin, "gh.log")
	data := filepath.Join(b.bin, "board.json")
	if err := os.WriteFile(data, []byte(fakeBoard), 0o600); err != nil {
		t.Fatal(err)
	}
	gh := `#!/bin/sh
echo "$*" >> "` + b.log + `"
sleep 0.3
if [ -n "$FAKE_GH_FAIL" ]; then echo "GraphQL: API rate limit exceeded" >&2; exit 1; fi
cat "` + data + `"
`
	if err := os.WriteFile(filepath.Join(b.bin, "gh"), []byte(gh), 0o755); err != nil { //nolint:gosec // a test fake
		t.Fatal(err)
	}
	return b
}

func (b board) run(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	return b.runEnv(t, nil, args...)
}

func (b board) runEnv(t *testing.T, env []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", append([]string{"board-snapshot.sh"}, args...)...) //nolint:gosec // a test script
	cmd.Env = append([]string{
		"PATH=" + b.bin + ":" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"WHR_BOARD_SNAPSHOT=" + b.snap,
	}, env...)
	var so, se strings.Builder
	cmd.Stdout, cmd.Stderr = &so, &se
	err = cmd.Run()
	return so.String(), se.String(), err
}

func (b board) calls(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(b.log) //nolint:gosec // a test path
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(strings.Split(strings.TrimSpace(string(data)), "\n"))
}

func (b board) age(t *testing.T, seconds int) {
	t.Helper()
	data, err := os.ReadFile(b.snap) //nolint:gosec // a test path
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	s["fetched_at"] = time.Now().Unix() - int64(seconds)
	out, _ := json.Marshal(s)
	if err := os.WriteFile(b.snap, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBoardSnapshotFreshMakesNoCall(t *testing.T) {
	b := newBoard(t)
	if _, se, err := b.run(t); err != nil {
		t.Fatalf("first run: %v %s", err, se)
	}
	if b.calls(t) != 1 {
		t.Fatalf("first run made %d calls, want 1", b.calls(t))
	}
	out, _, err := b.run(t)
	if err != nil || b.calls(t) != 1 {
		t.Fatalf("second run: err %v, calls %d, want 1", err, b.calls(t))
	}
	var s struct {
		FetchedAt int64            `json:"fetched_at"`
		Items     []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || s.FetchedAt == 0 || len(s.Items) != 5 {
		t.Fatalf("snapshot = %s (%v)", out, err)
	}
}

func TestBoardSnapshotStaleQueriesOnceWithModes(t *testing.T) {
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	b.age(t, 301)
	if _, se, err := b.run(t); err != nil || b.calls(t) != 2 {
		t.Fatalf("stale: err %v %s, calls %d, want 2", err, se, b.calls(t))
	}
	st, err := os.Stat(b.snap)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("file mode = %v, %v; want 0600", st, err)
	}
	if st, err := os.Stat(filepath.Dir(b.snap)); err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, %v; want 0700", st, err)
	}
	// A custom max age is honoured.
	b.age(t, 20)
	if _, _, err := b.runEnv(t, []string{"WHR_BOARD_MAX_AGE=10"}); err != nil || b.calls(t) != 3 {
		t.Fatalf("max age 10: err %v, calls %d, want 3", err, b.calls(t))
	}
}

func TestBoardSnapshotConcurrentCallsQueryOnce(t *testing.T) {
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	b.age(t, 400)
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, se, err := b.run(t); err != nil {
				t.Errorf("run: %v %s", err, se)
			}
		}()
	}
	wg.Wait()
	if b.calls(t) != 2 {
		t.Fatalf("calls = %d, want 2 (one first fill, one for three concurrent callers)", b.calls(t))
	}
}

func TestBoardSnapshotFailureKeepsOldFile(t *testing.T) {
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	b.age(t, 400)
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	out, se, err := b.runEnv(t, []string{"FAKE_GH_FAIL=1"})
	if err != nil {
		t.Fatalf("a stale copy must exit 0: %v", err)
	}
	if !strings.Contains(se, "stale") || out != string(before) {
		t.Fatalf("stderr %q, out kept = %v", se, out == string(before))
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if string(after) != string(before) {
		t.Fatal("the failed query changed the file")
	}
	// No file at all: exit 1.
	if err := os.Remove(b.snap); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.runEnv(t, []string{"FAKE_GH_FAIL=1"}); err == nil {
		t.Fatal("no snapshot and a failing query must fail")
	}
}

func TestBoardSnapshotRefreshForcesCall(t *testing.T) {
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.run(t, "--refresh"); err != nil || b.calls(t) != 2 {
		t.Fatalf("--refresh: err %v, calls %d, want 2", err, b.calls(t))
	}
}

func TestBoardSnapshotReaders(t *testing.T) {
	b := newBoard(t)
	out, se, err := b.run(t, "queue", "wh/platform")
	if err != nil {
		t.Fatalf("queue: %v %s", err, se)
	}
	var nums []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		nums = append(nums, strings.Split(l, "\t")[1])
	}
	if strings.Join(nums, " ") != "#30 #10 #20" {
		t.Fatalf("queue order = %v", nums)
	}
	out, _, err = b.run(t, "card", "40")
	if err != nil || !strings.HasPrefix(out, "#40\tIn progress\twh/platform\tP1\t") {
		t.Fatalf("card = %q, %v", out, err)
	}
	if b.calls(t) != 1 {
		t.Fatalf("readers made %d calls, want 1", b.calls(t))
	}
}

func TestBoardSnapshotRejectsBadEnv(t *testing.T) {
	b := newBoard(t)
	for _, env := range [][]string{
		{"WHR_BOARD_SNAPSHOT=relative/board.json"},
		{"WHR_BOARD_SNAPSHOT=/tmp/a\nb"},
		{"WHR_BOARD_MAX_AGE=soon"},
		{"WHR_BOARD_MAX_AGE=" + strconv.Itoa(-1)},
	} {
		if _, _, err := b.runEnv(t, env); err == nil {
			t.Errorf("%v accepted", env)
		}
	}
	if b.calls(t) != 0 {
		t.Fatal("a rejected setting must not call gh")
	}
}

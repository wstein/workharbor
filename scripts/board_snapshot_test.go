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
case "$1 $2" in
"project item-edit") exit 0 ;;
"project item-add") echo '{"id":"PVTI_x","title":"A new title","type":"Issue"}'; exit 0 ;;
esac
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
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
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
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
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
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
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

func (b board) lines(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(b.log) //nolint:gosec // a test path
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func (b board) card(t *testing.T, n string) string {
	t.Helper()
	out, se, err := b.run(t, "card", n)
	if err != nil {
		t.Fatalf("card %s: %v %s", n, err, se)
	}
	return strings.TrimSpace(out)
}

func TestBoardSnapshotMovePatchesCache(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if _, se, err := b.run(t, "move", "20", "In review"); err != nil {
		t.Fatalf("move: %v %s", err, se)
	}
	if _, se, err := b.run(t, "session", "20", "wh/review"); err != nil {
		t.Fatalf("session: %v %s", err, se)
	}
	if _, se, err := b.run(t, "priority", "20", "P3"); err != nil {
		t.Fatalf("priority: %v %s", err, se)
	}
	if got := b.card(t, "20"); !strings.HasPrefix(got, "#20\tIn review\twh/review\tP3\t") {
		t.Fatalf("card = %q", got)
	}
	l := b.lines(t)
	want := []string{
		"project item-edit 6 --owner wstein --url https://github.com/wstein/workharbor/issues/20 --field Status --value In review",
		"project item-edit 6 --owner wstein --url https://github.com/wstein/workharbor/issues/20 --field Session --value wh/review",
		"project item-edit 6 --owner wstein --url https://github.com/wstein/workharbor/issues/20 --field Priority --value P3",
	}
	if len(l) != 4 || strings.Join(l[1:], "\n") != strings.Join(want, "\n") {
		t.Fatalf("gh calls = %q", l)
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	var x, y struct {
		FetchedAt int64 `json:"fetched_at"`
	}
	_ = json.Unmarshal(before, &x)
	_ = json.Unmarshal(after, &y)
	if x.FetchedAt != y.FetchedAt {
		t.Fatal("a move changed fetched_at")
	}
}

func TestBoardSnapshotFailedMoveKeepsCache(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	for _, args := range [][]string{{"move", "20", "Blocked"}, {"session", "20", "Werner"}, {"priority", "20", "P1"}, {"add", "99"}} {
		_, se, err := b.runEnv(t, []string{"FAKE_GH_FAIL=1"}, args...)
		if err == nil || se == "" {
			t.Fatalf("%v: err %v, stderr %q; want a failure with a message", args, err, se)
		}
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if string(after) != string(before) {
		t.Fatal("a failed write changed the cache")
	}
}

func TestBoardSnapshotConcurrentMoves(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, n := range []string{"10", "20", "30", "40"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, se, err := b.run(t, "move", n, "Blocked"); err != nil {
				t.Errorf("move %s: %v %s", n, err, se)
			}
		}()
	}
	wg.Wait()
	for _, n := range []string{"10", "20", "30", "40"} {
		if got := b.card(t, n); !strings.Contains(got, "\tBlocked\t") {
			t.Errorf("card %s = %q, want Blocked", n, got)
		}
	}
}

func TestBoardSnapshotMoveWithoutCacheWritesOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	_, se, err := b.run(t, "move", "20", "Blocked")
	if err != nil || !strings.Contains(se, "cache") {
		t.Fatalf("err %v, stderr %q", err, se)
	}
	if _, err := os.Stat(b.snap); err == nil {
		t.Fatal("a move created a cache")
	}
	if len(b.lines(t)) != 1 {
		t.Fatalf("gh calls = %q, want only the edit", b.lines(t))
	}
	// A stale cache is not patched either.
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	b.age(t, 400)
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if _, _, err := b.run(t, "move", "20", "Blocked"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if string(after) != string(before) {
		t.Fatal("a stale cache was patched")
	}
}

func TestBoardSnapshotAdd(t *testing.T) {
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	if _, se, err := b.run(t, "add", "77"); err != nil {
		t.Fatalf("add: %v %s", err, se)
	}
	l := b.lines(t)
	if len(l) != 2 ||
		l[1] != "project item-add 6 --owner wstein --url https://github.com/wstein/workharbor/issues/77 --format json" {
		t.Fatalf("gh calls = %q", l)
	}
	if got := b.card(t, "77"); !strings.HasPrefix(got, "#77\t") || !strings.HasSuffix(got, "A new title") {
		t.Fatalf("card = %q", got)
	}
}

func TestBoardSnapshotWriteRejectsBadValues(t *testing.T) {
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	base := b.calls(t)
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	bad := [][]string{
		{"move", "", "Done"},
		{"move", "0", "Done"},
		{"move", "1 2", "Done"},
		{"move", "12\n3", "Done"},
		{"move", "$(id)", "Done"},
		{"move", "12;id", "Done"},
		{"move", "-1", "Done"},
		{"move", "12", "Merged"},
		{"move", "12", "Done; id"},
		{"move", "12", "$(id)"},
		{"move", "12", "Done\nTodo"},
		{"move", "12", "done"},
		{"move", "12"},
		{"move"},
		{"session", "12", "wh/nobody"},
		{"session", "12", "wh/platform extra"},
		{"session", "12", "`id`"},
		{"priority", "12", "P4"},
		{"priority", "12", "P1 "},
		{"priority", "12", "P1\nP2"},
		{"priority", "x", "P1"},
		{"add", "12 13"},
		{"add", "$(id)"},
		{"add", ""},
		{"add"},
	}
	for _, args := range bad {
		if _, _, err := b.run(t, args...); err == nil {
			t.Errorf("%q accepted", args)
		}
	}
	if b.calls(t) != base {
		t.Fatalf("a rejected value reached gh: %q", b.lines(t))
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if string(after) != string(before) {
		t.Fatal("a rejected value changed the cache")
	}
}

func TestBoardSnapshotMoveRefusesReviewGateStatuses(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	base := b.calls(t)
	for _, st := range []string{"Ready to push", "Done"} {
		_, se, err := b.run(t, "move", "20", st)
		if err == nil || !strings.Contains(se, "wh/review") || !strings.Contains(se, "gh project item-edit") {
			t.Fatalf("move %q: err %v, stderr %q; want a refusal naming wh/review and gh project item-edit", st, err, se)
		}
	}
	if b.calls(t) != base {
		t.Fatalf("a refused move made a gh call: %q", b.lines(t))
	}
	for _, st := range []string{"Todo", "In progress", "Blocked", "In review"} {
		if _, se, err := b.run(t, "move", "20", st); err != nil {
			t.Fatalf("move %q: %v %s", st, err, se)
		}
	}
}

func TestBoardSnapshotStaleLockTakeoverConcurrent(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: runs in the full suite (make test)")
	}
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(filepath.Dir(b.snap), ".board.lock")
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	old := strconv.FormatInt(time.Now().Unix()-1000, 10)
	if err := os.WriteFile(filepath.Join(lock, "ts"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	nums := []string{"10", "20", "30", "40", "50"}
	var wg sync.WaitGroup
	for _, n := range nums {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, se, err := b.run(t, "move", n, "Blocked"); err != nil {
				t.Errorf("move %s: %v %s", n, err, se)
			}
		}()
	}
	wg.Wait()
	for _, n := range nums {
		if got := b.card(t, n); !strings.Contains(got, "\tBlocked\t") {
			t.Errorf("card %s = %q, want Blocked (a write was lost)", n, got)
		}
	}
	if got := len(b.lines(t)); got != 1+len(nums) {
		t.Errorf("gh calls = %d, want %d", got, 1+len(nums))
	}
}

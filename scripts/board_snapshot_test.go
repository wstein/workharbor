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

const fakeNodes = `[
{"content":{"__typename":"Issue","number":20,"title":"b","url":"https://example/20","labels":{"nodes":[{"name":"x"}]}},"status":{"name":"Todo"},"session":{"name":"wh/platform"},"priority":{"name":"P2"}},
{"content":{"__typename":"Issue","number":30,"title":"a","url":"https://example/30"},"status":{"name":"Todo"},"session":{"name":"wh/platform"},"priority":{"name":"P1"}},
{"content":{"__typename":"Issue","number":10,"title":"c","url":"https://example/10"},"status":{"name":"Todo"},"session":{"name":"wh/platform"},"priority":{"name":"P2"}},
{"content":{"__typename":"Issue","number":40,"title":"d","url":"https://example/40"},"status":{"name":"In progress"},"session":{"name":"wh/platform"},"priority":{"name":"P1"}},
{"content":{"__typename":"Issue","number":50,"title":"e","url":"https://example/50"},"status":{"name":"Todo"},"session":{"name":"wh/runtime"},"priority":{"name":"P1"}}
]`

func page(nodes string, next string) string {
	if next == "" {
		return `{"data":{"node":{"items":{"pageInfo":{"hasNextPage":false,"endCursor":null},"nodes":` + nodes + `}}}}`
	}
	return `{"data":{"node":{"items":{"pageInfo":{"hasNextPage":true,"endCursor":"` + next + `"},"nodes":` + nodes + `}}}}`
}

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
	b.pages(t, map[string]string{"first": page(fakeNodes, "")})
	gh := `#!/bin/sh
if [ "$1 $2" = "api rate_limit" ]; then
	echo "rate_limit" >> "` + b.bin + `/rate.log"
	echo "${FAKE_RATE:-{\"resources\":{\"graphql\":{\"limit\":5000,\"remaining\":4900,\"reset\":1791032178}}}}"
	exit 0
fi
echo "$*" >> "` + b.log + `"
sleep 0.3
if [ -n "$FAKE_GH_FAIL" ]; then echo "GraphQL: API rate limit exceeded" >&2; exit 1; fi
case "$*" in
*addProjectV2ItemById*) echo '{"data":{}}'; exit 0 ;;
*updateProjectV2ItemFieldValue*) echo '{"data":{}}'; exit 0 ;;
*projectItems*) if [ -n "$FAKE_NOITEM" ] || case "$*" in *"n=99"*) true ;; *) false ;; esac; then echo '{"data":{"repository":{"issue":{"projectItems":{"nodes":[]}}}}}'; exit 0; fi; echo '{"data":{"repository":{"issue":{"projectItems":{"nodes":[{"id":"PVTI_other","project":{"id":"PVT_other"}},{"id":"PVTI_x","project":{"id":"PVT_kwHNjWrOAZVCuA"}}]}}}}}'; exit 0 ;;
*"fields(first"*) echo '{"data":{"node":{"fields":{"nodes":[{},{"id":"F_status","name":"Status","options":[{"id":"O_todo","name":"Todo"},{"id":"O_ip","name":"In progress"},{"id":"O_bl","name":"Blocked"},{"id":"O_ir","name":"In review"},{"id":"O_rp","name":"Ready to push"}]},{"id":"F_sess","name":"Session","options":[{"id":"O_s1","name":"wh/review"},{"id":"O_s2","name":"Werner"}]},{"id":"F_prio","name":"Priority","options":[{"id":"O_p1","name":"P1"},{"id":"O_p3","name":"P3"}]}]}}}}'; exit 0 ;;
*"items(first"*)
	cur=first
	for a in "$@"; do case "$a" in after=*) cur=${a#after=} ;; esac; done
	if [ "$FAKE_FAIL_PAGE" = "$cur" ]; then echo "GraphQL: secondary rate limit" >&2; exit 1; fi
	cat "` + b.bin + `/pages/$cur.json"; exit 0 ;;
*"issue(number"*) echo '{"data":{"repository":{"issue":{"id":"I_77","title":"A new title"}}}}'; exit 0 ;;
esac
echo '{}'
`
	if err := os.WriteFile(filepath.Join(b.bin, "gh"), []byte(gh), 0o755); err != nil { //nolint:gosec // a test fake
		t.Fatal(err)
	}
	return b
}

// pages writes the fake board's pages, named by the cursor that fetches them.
func (b board) pages(t *testing.T, p map[string]string) {
	t.Helper()
	dir := filepath.Join(b.bin, "pages")
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // a test dir
		t.Fatal(err)
	}
	for name, body := range p {
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.run(t, "--refresh"); err != nil || b.calls(t) != 2 {
		t.Fatalf("--refresh: err %v, calls %d, want 2", err, b.calls(t))
	}
}

func TestBoardSnapshotReaders(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	// 1 refresh, 1 field-ID query (cached), then lookup + mutation per write.
	if len(l) != 1+1+3*2 {
		t.Fatalf("gh calls = %d, want 8: %q", len(l), l)
	}
	joined := strings.Join(l, "\n")
	for _, w := range []string{"-f f=F_status -f o=O_ir", "-f f=F_sess -f o=O_s1", "-f f=F_prio -f o=O_p3", "-f i=PVTI_x", "-F n=20"} {
		if !strings.Contains(joined, w) {
			t.Fatalf("missing %q in %q", w, l)
		}
	}
	if strings.Contains(joined, "item-edit") || strings.Contains(joined, "item-list 6 --owner wstein --url") {
		t.Fatalf("the --url route is used: %q", l)
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	if len(b.lines(t)) != 3 {
		t.Fatalf("gh calls = %q, want fields, lookup and mutation", b.lines(t))
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
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	if _, se, err := b.run(t, "add", "77"); err != nil {
		t.Fatalf("add: %v %s", err, se)
	}
	l := b.lines(t)
	if len(l) != 3 || !strings.Contains(l[1], "issue(number") || !strings.Contains(l[2], "addProjectV2ItemById") ||
		!strings.Contains(l[2], "-f c=I_77") {
		t.Fatalf("gh calls = %q", l)
	}
	if got := b.card(t, "77"); !strings.HasPrefix(got, "#77\t") || !strings.HasSuffix(got, "A new title") {
		t.Fatalf("card = %q", got)
	}
}

func TestBoardSnapshotWriteRejectsBadValues(t *testing.T) {
	t.Parallel()
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
		{"add", "12", "x"},
		{"ready", "12", "13", "Done"},
		{"move", "12", "13", "Done"},
		{"move", "12", "13", "Merged"},
		{"move", "12", "x", "Todo"},
		{"session", "12", "13", "wh/nobody"},
		{"priority", "12", "0", "P1"},
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
	t.Parallel()
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
		if err == nil || !strings.Contains(se, "wh/review") || !strings.Contains(se, "board-snapshot.sh ready") {
			t.Fatalf("move %q: err %v, stderr %q; want a refusal naming wh/review and ready", st, err, se)
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
	t.Parallel()
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
	if got := len(b.lines(t)); got < 1+2*len(nums) || got > 1+3*len(nums) {
		t.Errorf("gh calls = %d, want %d to %d", got, 1+2*len(nums), 1+3*len(nums))
	}
}

func TestBoardSnapshotReadySetsReadyToPush(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	if _, se, err := b.run(t, "ready", "20"); err != nil {
		t.Fatalf("ready: %v %s", err, se)
	}
	if got := b.card(t, "20"); !strings.Contains(got, "\tReady to push\t") {
		t.Fatalf("card = %q", got)
	}
	if !strings.Contains(strings.Join(b.lines(t), "\n"), "-f o=O_rp") {
		t.Fatalf("gh calls = %q", b.lines(t))
	}
	for _, args := range [][]string{{"ready"}, {"ready", "20", "Done"}, {"ready", "x"}} {
		if _, _, err := b.run(t, args...); err == nil {
			t.Errorf("%q accepted", args)
		}
	}
}

func TestBoardSnapshotWritesNeverUseURLRouteAndCacheFields(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	for _, v := range []string{"Blocked", "Todo"} {
		if _, se, err := b.run(t, "move", "20", v); err != nil {
			t.Fatalf("move: %v %s", err, se)
		}
	}
	// Two writes: one field query (cached next to the snapshot), two lookups, two mutations.
	if got := len(b.lines(t)); got != 5 {
		t.Fatalf("gh calls = %d, want 5: %q", got, b.lines(t))
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(b.snap), "board-fields.json")); err != nil {
		t.Fatal(err)
	}
	// A card that is not on the board fails and says so.
	if _, se, err := b.runEnv(t, []string{"FAKE_NOITEM=1"}, "move", "20", "Todo"); err == nil || !strings.Contains(se, "not on the board") {
		t.Fatalf("err %v, stderr %q; want not on the board", err, se)
	}
}

func TestBoardSnapshotWarnsUnder20Percent(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	low := `{"resources":{"graphql":{"limit":5000,"remaining":900,"reset":1791032178}}}`
	out, se, err := b.runEnv(t, []string{"FAKE_RATE=" + low})
	if err != nil || !strings.Contains(se, "900 of 5000") || !strings.Contains(se, "resets at") {
		t.Fatalf("err %v, stderr %q; want a warning with the numbers and reset", err, se)
	}
	if strings.Contains(out, "warning") {
		t.Fatal("the warning went to stdout")
	}
	// Cached run: no GitHub call, no rate check, no warning.
	if _, se, _ := b.runEnv(t, []string{"FAKE_RATE=" + low}); se != "" {
		t.Fatalf("a cache hit warned: %q", se)
	}
	// Plenty left: silent.
	b.age(t, 400)
	if _, se, err := b.run(t); err != nil || se != "" {
		t.Fatalf("err %v, stderr %q; want silence at 98%%", err, se)
	}
	// Only rate_limit calls are free: they are not in the gh log.
	for _, l := range b.lines(t) {
		if strings.Contains(l, "rate_limit") {
			t.Fatalf("rate_limit logged as a counted call: %q", l)
		}
	}
}

func TestBoardSnapshotPagination(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	one := func(n int) string {
		return `[{"content":{"__typename":"Issue","number":` + strconv.Itoa(n) + `,"title":"t","url":"u"},"status":{"name":"Todo"},"session":{"name":"wh/platform"},"priority":{"name":"P1"}}]`
	}
	b.pages(t, map[string]string{"first": page(one(1), "c1"), "c1": page(one(2), "c2"), "c2": page(one(3), "")})
	out, se, err := b.run(t)
	if err != nil || se != "" {
		t.Fatalf("err %v, stderr %q", err, se)
	}
	var s struct {
		Items []struct {
			Number int
			Type   string
			Status string
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || len(s.Items) != 3 || s.Items[2].Number != 3 || s.Items[0].Type != "Issue" || s.Items[0].Status != "Todo" {
		t.Fatalf("snapshot = %s (%v)", out, err)
	}
	if b.calls(t) != 3 {
		t.Fatalf("calls = %d, want 3 pages", b.calls(t))
	}
	for _, l := range b.lines(t) {
		if strings.Contains(l, "item-list") {
			t.Fatalf("the board read still uses item-list: %q", l)
		}
	}
}

func TestBoardSnapshotFailingPageKeepsOldFile(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	b.age(t, 400)
	before, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	b.pages(t, map[string]string{"first": page(fakeNodes, "c1"), "c1": page(fakeNodes, "")})
	out, se, err := b.runEnv(t, []string{"FAKE_FAIL_PAGE=c1"})
	if err != nil || !strings.Contains(se, "stale") || out != string(before) {
		t.Fatalf("err %v, stderr %q, old snapshot kept = %v", err, se, out == string(before))
	}
	after, _ := os.ReadFile(b.snap) //nolint:gosec // a test path
	if string(after) != string(before) {
		t.Fatal("a failed page changed the file")
	}
}

func TestBoardSnapshotSeveralIssues(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"move", "10", "20", "30", "Blocked"},
		{"session", "10", "20", "Werner"},
		{"priority", "10", "20", "P3"},
		{"ready", "10", "20", "20"},
	} {
		if _, se, err := b.run(t, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, se)
		}
	}
	if got := b.card(t, "10"); !strings.HasPrefix(got, "#10\tReady to push\tWerner\tP3\t") {
		t.Fatalf("card 10 = %q", got)
	}
	if got := b.card(t, "30"); !strings.HasPrefix(got, "#30\tBlocked\t") {
		t.Fatalf("card 30 = %q", got)
	}
	if _, se, err := b.run(t, "add", "77", "78"); err != nil {
		t.Fatalf("add: %v %s", err, se)
	}
	if got := b.card(t, "78"); !strings.HasPrefix(got, "#78\t") {
		t.Fatalf("card 78 = %q", got)
	}
}

func TestBoardSnapshotSeveralIssuesPartialFailure(t *testing.T) {
	t.Parallel()
	b := newBoard(t)
	if _, _, err := b.run(t); err != nil {
		t.Fatal(err)
	}
	_, se, err := b.run(t, "move", "10", "99", "20", "Blocked")
	if err == nil || !strings.Contains(se, "#99") {
		t.Fatalf("err %v, stderr %q; want exit 1 naming #99", err, se)
	}
	for _, n := range []string{"10", "20"} {
		if got := b.card(t, n); !strings.Contains(got, "\tBlocked\t") {
			t.Errorf("card %s = %q, want Blocked despite the failure on #99", n, got)
		}
	}
}

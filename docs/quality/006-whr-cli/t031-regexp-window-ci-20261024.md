# PTY regexp window assertion: CI baseline maintenance

This is narrow CI baseline maintenance during T030 acceptance, not a start or
completion of wave1b/T031 feature acceptance. Only
`TestExpectRegexpWorkIsBounded` in
`apps/whr/internal/ptytest/ptytest_test.go` changes. Current review base:
`86008fab4b0131187c3d3b244fef94a48063b48b`. Initial source base was
`54c4b271745fe78800a523607dd00a9124d22f01`, with original test commit
`5a420b8167fe8d3d32d0ea24c49324e36e68b079`; the exact-window correction is
`1e3fe636d88271149cb676626609b86bc6a1f837`. Measured on 2026-10-24 with Go
`go1.27.2 linux/arm64`.

## Observed CI failure and source trace

The unchanged test failed in
[PR 249 bootstrap run 40132998458](https://github.com/wstein/workharbor/actions/runs/40132998458):
`regexp work 176572568 for 4247445 bytes of output: window not applied`.
The old assertion required aggregate Work to stay below 40 times transcript
length while ExpectRegexp was waiting on a live PTY.

Source inspection explains why that ratio is not the documented contract:

- `session.go` ExpectRegexp limits each invocation of its matcher to the last
  64 KiB of unconsumed visible text with `base := max(0, len(p)-regexpWindow)`.
- The expect loop can invoke that matcher again on every reader wake-up.
  PTY read chunking and scheduling change the number of repeated searches.
- Work also counts projection and screen drawing through advanceLocked.
  A fixed aggregate multiplier is therefore scheduling-sensitive even when
  every individual regexp search respects its window.

This is a false-positive test failure, not evidence of a new matcher defect.
The product code and its 64 KiB contract remain unchanged.

## Deterministic assertion and regression RED

The revised test retains the real 4 MiB flood fixture and PTY integration.
ExpectExit waits for both process exit and reader drain; the test checks that
at least 4 MiB were captured. Screen advances projection/drawing over that
complete transcript before recording Work. The subsequent ExpectRegexp must
return END and add matcher-only work of exactly 64 KiB. This
measures one search on a stable backlog, independent of read wake-up timing.
No threshold, sleep or timeout was increased, and no test hook was added.

Before the final green run, a temporary Go source overlay replaced only
`base := max(0, len(p)-regexpWindow)` in session.go with `base := 0`.
The revised real-PTY test compiled and failed:
`regexp matcher work 4194351: want exactly 65536 bytes for one search`
(rechecked after the exact-window review correction below).
This demonstrates sensitivity to loss of the window. Merely waiting for exit
while retaining the old 40-times multiplier would not reject that one full
backlog search. The exact count also rejects reduced windows and prevents an uncounted search
from satisfying the assertion.

Complete CI failure output, mutant source/overlay, compiled RED and focused
green logs are local under `/tmp/whr-pty-regexp-ci-w6dtndgp/`. To reproduce the
mutant, copy session.go to a temporary file, make the single replacement, map
its absolute source path through Go's JSON Replace overlay, and run the
focused ID with `go test -count=1 -overlay=<overlay.json> ./internal/ptytest
-run '^TestExpectRegexpWorkIsBounded$'`. No product mutation is retained.

## PR 252 F1: pin the documented window exactly

The [independent review](https://github.com/wstein/workharbor/pull/252#pullrequestreview-5640157788)
of head `2cadc76ef0c019664fdf81a54f3406648a6bc023` found that the initial
positive upper bound accepted a smaller window. The local reproduction changed
only session.go's `const regexpWindow = 64 << 10` to `32 << 10` in a temporary
source overlay: the initial assertion passed. Requiring the measured work to
equal 64 KiB then produced compiled semantic RED:
`regexp matcher work 32768: want exactly 65536 bytes for one search`.
The unchanged production implementation passes. The no-window mutant was also
rechecked and fails with work 4194351. No production source or timeout changes
are included.

The fixture has more than 4 MiB of stable unconsumed visible text after reader
drain and projection, so one matcher invocation searches exactly the documented
64 KiB window. Equality therefore detects shrinking as well as enlarging or
removing that window without reintroducing scheduling dependence.

Before the correction, the two agent-owned commits were rebased onto
`86008fab4b0131187c3d3b244fef94a48063b48b`. Both range-diff entries were equal
at intermediate head `1586a4ae05591aa680e0df46323a7bee8a2e7800`.
Complete surviving-before, shrink/no-window RED, focused green and check logs
are local under `/tmp/whr-pty-regexp-exact-fzz9vkh4/`. Reproduce the shrink probe
with the same overlay command above, replacing the window constant instead of
the base expression.

## Checks and remaining gates

From apps/whr with `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`:

```sh
go test -count=1 -v ./internal/ptytest -run '^(TestExpectRegexpWorkIsBounded|TestExpectRegexpSubmatches|TestExpectHandlesSplitSequencesAndRunes|TestExpectFindsNeedleCompletedByLaterOutput|TestPTYWorkCountsTheUnterminatedTail)$'
go vet ./internal/ptytest
```

Both passed. Source check `python3 scripts/check_repo.py`, targeted typos,
gofmt and `git diff --check` passed. Complete raw logs were saved before
returning bounded summaries.

No full suite, race, standalone build, full make, product/API/specification
change, task/progress ticks or publication is included. Formal wave1b/T031
acceptance remains after T030 merge, with its separate platform evidence and
review gates; this maintenance does not claim that acceptance or macOS
execution. Fresh PR/CI checks remain required.

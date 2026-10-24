# PTY regexp window assertion: CI baseline maintenance

This is narrow CI baseline maintenance during T030 acceptance, not a start or
completion of wave1b/T031 feature acceptance. Only
`TestExpectRegexpWorkIsBounded` in
`apps/whr/internal/ptytest/ptytest_test.go` changes. Base:
`54c4b271745fe78800a523607dd00a9124d22f01`; test commit:
`5a420b8167fe8d3d32d0ea24c49324e36e68b079`. Measured on 2026-10-24 with Go
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
return END and add positive matcher-only work no greater than 64 KiB. This
measures one search on a stable backlog, independent of read wake-up timing.
No threshold, sleep or timeout was increased, and no test hook was added.

Before the final green run, a temporary Go source overlay replaced only
`base := max(0, len(p)-regexpWindow)` in session.go with `base := 0`.
The revised real-PTY test compiled and failed:
`regexp matcher work 4194351: want 1..65536 bytes for one search`.
This demonstrates sensitivity to loss of the window. Merely waiting for exit
while retaining the old 40-times multiplier would not reject that one full
backlog search. The positive lower bound also prevents an uncounted search
from satisfying the assertion.

Complete CI failure output, mutant source/overlay, compiled RED and focused
green logs are local under `/tmp/whr-pty-regexp-ci-w6dtndgp/`. To reproduce the
mutant, copy session.go to a temporary file, make the single replacement, map
its absolute source path through Go's JSON Replace overlay, and run the
focused ID with `go test -count=1 -overlay=<overlay.json> ./internal/ptytest
-run '^TestExpectRegexpWorkIsBounded$'`. No product mutation is retained.

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

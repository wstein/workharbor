# Issue 106: bounded pending activation retries

2026-10-24. Owner-approved Package D maintenance of existing T008 identity/liveness;
no T009–T024 functionality or feature-task completion claim. Baseline
`c71890d27c3d415e50fe15610a6f8f044b313142`. Issue:
[#106](https://github.com/wstein/workharbor-nxt/issues/106).

An issued pending credential previously retried on every reconnect, including an
early socket failure. Successful heartbeats reset transport backoff, producing
roughly31s reconnect churn during prolonged activation failure. The
[enrollment contract](../../../specs/006-whr-cli/contracts/host-enrollment.md#live-expiry-and-reconnect)
now records immediate startup replay, then30s/60s/120s growth capped30min while
active remains valid and more than6h from expiry. Retry deadlines clamp to the6h
boundary; at/inside that boundary the cadence remains30s. Loop-local state is
keyed by pending identity ID, leaf and issuer, and resets after promotion or a
changed/removed pending identity. Heartbeats cannot reset it; early reconnects
cannot bypass it. Transport backoff, live validity monitoring, original staging
window and expired-active replay/refusal distinctions remain separate.

## Red and green evidence

Red regression commit `a3c74e552d300bbe0e33f301ac6ff24340ce123f` is reachable from
implementation commit `b2996ba8a570635d429ee9deaa8a8b1200b87439`.

- Before implementation, `go test ./apps/host_agent/internal/connection -run
  '^TestRunEarlyReconnectHonorsPendingActivationDue$' -count=1` exited1 with
  `early reconnect attempted activation 2 times, want 1 before retry due`.
- After implementation, `go test ./apps/host_agent/internal/connection -run
  '^(TestRunEarlyReconnectHonorsPendingActivationDue|TestRunRetriesFailedRenewalWhileConnected)$'
  -count=1` exited0. The existing first-failure test still enforces its30s upper
  bound; its mock now advances scheduling time to the due instant.
- `go test ./apps/host_agent/internal/connection -run
  '^(TestActivationRetry|TestRunHeartbeats|TestRunPromotion|TestRunCancellation|TestRunDoesNotStage)'
  -count=1` exited0.
- `go test ./apps/host_agent/internal/connection ./apps/host_agent/internal/identity
  -count=1` exited0 (connection29.837s; identity7.340s).
- The subsequent pending-issuer assertion extension passed with `go test
  ./apps/host_agent/internal/connection -run
  '^TestExpiredCountsAnExpiredIssuerWithValidLeaf$' -count=1` (exit0).
- `go vet ./apps/host_agent/internal/connection ./apps/host_agent/internal/identity`
  exited0. `gofmt -l` on both changed Go files produced no paths.
- `python3 scripts/check_repo.py` and `git diff --check` exited0.
- At the pristine implementation commit, `timeout 5s make check` exited124 during
  Python checks (`make: ... check ... Terminated`). This is incomplete, not a
  passing repository acceptance gate. The unaffected full Python suite was not
  repeated. Local raw output: `/tmp/whr-d106-make-check.log`.

## Coverage and limits

| Behavior | Evidence |
| --- | --- |
| First failure30s; growth through30min; repeated cap cannot overflow | `TestActivationRetryGrowthSaturatesAndResets` (10,000 capped failures) |
| Pending identity changes/removal reset state | `TestActivationRetryGrowthSaturatesAndResets` |
| Early reconnect honors due; heartbeat does not reset growth | `TestRunEarlyReconnectHonorsPendingActivationDue`, `TestRunHeartbeatsPreservePendingActivationGrowth` |
| Actual promotion clears retry deadline | `TestRunPromotionClearsPendingActivationDeadline` |
| Clamp before6h;30s at/inside boundary; expiry bound | `TestActivationRetryRenewalBoundaryAndExpiry` |
| Cancellation remains prompt; no staging outside6h | `TestRunCancellationDuringPendingRetry`, `TestRunDoesNotStageOutsideRenewalWindow` |
| Active/pending leaf expiry and active/pending issuer expiry | Existing recovery tests and extended `TestExpiredCountsAnExpiredIssuerWithValidLeaf` |
| Saved pending activation after active expiry; successful loopback promotion | Existing `TestRunAttemptsActivationOfPendingAfterActiveExpiry`, `TestRunRenewsOverLoopbackThenConnects` |
| Final refusals return recovery66;503/unknown4xx retry;403 reports/retries | Existing loopback recovery/refusal/security tests in full connection suite |
| Leaf/issuer validity watchdog and blocked socket expiry | Existing connection session/validity tests in full suite |

The scheduling seams model elapsed and UTC observations independently without
changing Go time internals. Production identity validity checks retain their real
clock checks. Pure scheduling tests avoid waiting minutes; Run integration tests
retain the normal transport timer. The session watchdog also checks the saved
renewal/retry deadline against elapsed and UTC observations (correction below).

Restart still makes one immediate saved-pending attempt, so this change does not
bound churn from an external service manager repeatedly restarting the process.
No persistent retry schema, new public dependency, control-plane or Mix change is
included. Local TLS/WebSocket loopback tests are not real-host evidence. No build,
race run, install, deployment, dispatch activation or real-host/browser run was
performed within this bounded authorization. Full `make check`, required CI and
independent exact-head review remain necessary before readiness; runtime evidence
remains a separate gate. Nothing was pushed or published by this author.


## Review correction: UTC transition into the renewal window

Independent review of `ccb71137caabbd977c7ca3c86845bef8917b6b74` found that a saved
retry deadline carrying Go monotonic time could remain30min away after UTC jumped
forward2h into the6h renewal window. Advancing test time with `Add` alone had moved
both clocks together and missed this case. No expiry-security failure was
reproduced; this was a retry-cadence contract gap.

Regression commit `38408d9edbd9859d7e4b9752d700dc825793f4b4` adds an independent wall observation seam and
`TestRunForwardWallClockJumpRestoresFastPendingRetry`. Before the correction,
`go test ./apps/host_agent/internal/connection -run
'^TestRunForwardWallClockJumpRestoresFastPendingRetry$' -count=1 -timeout=20s`
exited1 with `inside six-hour window retained retry=30m0s renews=7; want <=30s and
immediate attempt 8`. The test advances only UTC2h after seven failures while
leaving elapsed scheduling time fixed; it uses no unsafe time representation edits.

The correction makes a retry due when either elapsed or UTC time reaches its
saved deadline, and calculates the6h/expiry clamp from UTC remaining validity.
Reconnect therefore attempts activation and restores30s cadence in this case.
The existing session watchdog additionally observes that same saved deadline,
closing a connected socket when a forward UTC correction has passed it. Expiry
validation, recovery66 and security403 handling are unchanged. A backward UTC
correction cannot postpone a due elapsed deadline.

Post-correction commands:

- `go test ./apps/host_agent/internal/connection -run
  '^(TestRunForwardWallClockJumpRestoresFastPendingRetry|TestActivationRetry|TestRunHeartbeats|TestRunRetriesFailedRenewalWhileConnected|TestRunEarlyReconnect|TestRunPromotion)'
  -count=1 -timeout=30s` exited0 (14.221s).
- `go test ./apps/host_agent/internal/connection -run
  '^(TestRetryDeadlineReachedUsesEarlierClock|TestSessionWakesWhenUTCJumpPassesPendingRetryDeadline)$'
  -count=1 -timeout=15s` exited0 (1.199s). The loopback test advances the scheduling
  UTC observation after authenticated heartbeat and verifies session close before
  its4s cancellation deadline while the credential remains valid in real time.
- `go vet ./apps/host_agent/internal/connection ./apps/host_agent/internal/identity`
  and `python3 scripts/check_repo.py` exited0 again.

- `go test ./apps/host_agent/internal/connection -count=1` exited0 (36.212s),
  rechecking the full connection package because the watchdog predicate changed.
- `gofmt -l` on all three affected Go files produced no paths; `git diff --check`
  exited0.

These are injected-clock and local loopback results, not an actual host UTC
adjustment experiment. The earlier identity suite result remains applicable;
identity code is unchanged. The bounded `make check`
limitation above remains unresolved and is not waived by these targeted checks.

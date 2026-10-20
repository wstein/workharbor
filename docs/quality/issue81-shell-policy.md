# Issue 81 bounded shell-policy evidence

2026-10-15. Candidate implementation; full workspace integration remains unverified.
Scope: [Feature 007](../../specs/007-bundledock-shell-policy/spec.md) and the
[data-only policy contract](../../contracts/workspace-shell-policy.md).

## Source and review boundary

The candidate depends on the unmerged installable-pilot baseline
`832dd63000257bad01656154b639251d643f2a2d`. Its T009 source is `90ea176`;
this slice leaves the closed provider/config schemas and existing task markers
unchanged. The main-target draft must not imply the pilot prerequisites reached
main. Design PR [#82](https://github.com/wstein/workharbor/pull/82) was owner-merged;
its documentation and repository-rename CI fix are retained in this dependency
branch. The runtime policy is new follow-up work under the direct implementation
request; the historical issue's automatic closure does not prove integration.

An independent Sol design review cleared the concrete spec/plan/tasks and contract
after correcting share-only XDG fallback idempotence and requiring snippet-local
interactive guards. Independent implementation review found no code/security
findings at `c3ce22af433941b2899cb268f6925aabbfc8215b`; commit-message corrections
produced the identical source tree at `036e8e02345b7d307904659e1432deb496a0dd36`.
Both SHAs are pre-rebase; the review of the current rebased head is recorded on
PR [#83](https://github.com/wstein/workharbor/pull/83). Final exact-head
review/check evidence belongs to the PR handoff.

## Executed evidence

- Failure-first `go test ./internal/workspaceshell`: initial absent-API compilation
  failure, then a compiled refusal-only stub failed the intended successful planning,
  environment refusal, fragment and surrogate behavior cases. No failure was called
  passing. Implementation followed those observed failures.
- `GOCACHE=<task-local-cache> go test -count=1 ./internal/workspaceshell`: PASS.
- `GOCACHE=<task-local-cache> go vet ./internal/workspaceshell`: PASS.
- `gofmt -l internal/workspaceshell`: no output (formatted).
- Bash surrogate test: authored harmless fixture at a substituted temporary path;
  noninteractive mode did not source it, interactive mode did, and the prompt
  sentinel was absent with the planned deny environment.
- Manual `/bin/zsh -f -c` and `-ic` surrogate checks: same guard/sentinel outcomes.
- `git diff --check`: PASS at the source candidate.

The default Go build cache initially refused a fresh check under native sandbox
permissions. Repeating with a task-local writable GOCACHE passed; no permission
widening or missing check was concealed. Native Nushell is unavailable here.
Repository canonical checks are tracked separately at the final head in the PR;
no pending local run is claimed passing by this record.

## Remaining adoption and acceptance

This package has no caller in the current pilot: it deliberately supplies a pure
port for the future T013/T014 new-session launcher. A valid description/plan grants
no execution authorization and proves no real mount or artifact bytes. The future
resolver/attacher must establish immutable digest integrity, source provenance,
platform, ownership/trust and observed read-only layout. Actual supported image rc
paths, fixed `/opt` sourcing, all-four-shell vendor autoload, prompt-rendering
sentinels, noninteractive startup, Docker mount behavior and lifecycle/reconnect
acceptance remain unexecuted. Bash/zsh surrogate checks cannot satisfy those gates.

Always disabling BundleDock Starship avoids choosing the open shared-HOME trust
mode. It does not isolate same-user agents from user rc edits or existing prompts.
No carrier is built/published, attachment installed, shell launched for a user,
runtime/dispatch activated, or deployment performed by this policy slice.

## Current source observation (2026-10-20)

At reviewed baseline `ce3cfc83f766aa4f41be092f6b55dee1d98b2754`, policy
last-modification commit `b2b725eedfad22bc50e2d69563b38694becc2e25` is an ancestor
of main; policy and contract bytes match that ancestor. The original pilot object
is unavailable locally. The dated dependency account above is historical, not a
current requirement to obtain that pilot before continuing the pure port.

Offline local Go verification at that baseline passed the full policy package,
the isolated temporary-file Bash surrogate, focused vet, formatting (empty output)
and `git diff --check`. These source checks do not establish full repository
acceptance: `make check` was not run because its application/assets builds require
explicit authorization. T001 remains unchecked, and runtime/adoption acceptance
remains unverified.

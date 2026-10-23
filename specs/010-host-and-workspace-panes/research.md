# Research: Host and workspace panes

Date: 2026-10-23. Repository baseline: `e4caa8465e7abd183fcd87e8e8a9ffcd53ddbcaf`.
This is design research by source inspection, not Docker/herdr execution or acceptance evidence.
The seven answers in [spec Decisions](spec.md#decisions) are settled; research does not reopen them.

## Discovery before shell

**Decision:** P1 implements the FR-066 read-only slice of 006/T038 in `apps/whr/internal/localdocker/`; P2 reuses it.
**Rationale:** No localdocker package exists at baseline. `workspace.ls` and `workspace.shell` in
`apps/whr/internal/commands/entries.go` remain later/control-plane commands. The discovery dependency is real, not completed work.
006/T038 also owns destructive lifecycle behavior, which this plan neither implements nor marks complete.
**Alternatives considered:** Copy a container finder into shell, implement all of T038 first, or require the owner API.
Each either duplicates policy or exceeds the requested local-shell dependency.

## Socket and Docker boundary

**Decision:** Share/extract existing doctor socket rules into an internal reusable boundary in P1, preserving regression tests;
use a pinned explicit Unix endpoint for all Docker subprocesses. Keep read-only discovery behind a port and fake daemon/client.
**Rationale:** `internal/doctor/socket.go` already implements `DOCKER_HOST`, candidate order and a 500 ms per-dial cap;
`CandidateOrder` is exported but the resolver itself is private. A stable shared resolver must be created before reuse is claimed.
The FR-066 total budget is 5 seconds; socket selection, listing and preflight spend that one deadline.
**Alternatives considered:** Duplicated socket search, Docker contexts, or an Engine SDK. Context discovery is excluded by FR-002;
OQ-1 fixes the CLI choice. No new SDK dependency is justified.

## Terminal, lifetime and liveness

**Decision:** Give one Docker child direct terminal descriptors and wait for its exit. A nonzero status triggers exactly one bounded
liveness inspect. Keep local policy in planned `internal/localshell/`, with PTY tests in the existing test harness.
**Rationale:** A process replacement cannot run FR-008's post-exit check. Existing `internal/ptytest/` and
`cmd/whr/wizard_pty_test.go` supply the repository's test subprocess/PTY conventions. 010 FR-007 explicitly accepts orphan limits;
SC-009 separately tests host herdr persistence over SSH. There is no new process reaper or managed session.
**Alternatives considered:** Engine PTY relay, raw process replacement, cleanup daemon and prechecking a missing shell with a second
exec. They contradict the direct-stream/liveness design, exceed scope or reopen OQ-6.

## Environment evidence boundary

**Decision:** Verify both the Docker client's host-process environment and the additions to the container environment.
**Rationale:** SC-005's fake child must not inherit planted host tokens. Independently, FR-004 supplies three explicit container
`--env` values. Docker's container baseline environment is not a copy of the host environment. Tests must not assert that a real
login shell has only three variables, nor claim to scrub credentials already configured in that container.
**Alternatives considered:** Inherit all host variables and test only argv; that would not meet the fake-child canary requirement.

## Plain default migration

**Decision:** P3 changes omitted-field default application and existing schema/vectors/scaffold/wizard text together, while preserving
explicit values and live console state. Remote session creation stays a separate runtime dependency.
**Rationale:** `internal/scaffold/proposal.go`, `internal/wizard/wizard.go`, `contracts/workspace/workharbor.schema.json`,
`contracts/workspace/vectors.json` and `tests/test_workspace_manifest_vectors.py` contain existing herdr defaults/expectations.
010 FR-020 names additional contract/default text to reconcile. The owner API remains unavailable.
**Alternatives considered:** Rewrite existing manifests, mutate live consoles, remove explicit herdr, or implement a remote console here.
None is requested; the first two conflict with OQ-7 and accepted console lifetime rules.

## Plugin and evidence

**Decision:** Deliver P4 separately in `integrations/herdr/`, using public local CLI and fake-herdr/whr tests, plus a standalone SC-010
verifier. Keep the exact FR-021/021a glyph/hash/palette constants; record real terminal capability under SC-009.
**Rationale:** OQ-5/9/10 settle location and identity policy. Owner-verified tab labels and pane background are recorded in the spec;
clear/title persistence, notebook font/emoji width, detach behavior and SSH reconnect are not established by repository source checks.
**Contract prerequisite:** P1 lists all container states and exposes no running-state filter flag. US3's running-only picker and
FR-018's name-only read rule need reconciliation in the separately approved P4 scope. The concrete proposal is to permit reading the
existing public `state` field solely for eligibility, alongside `name`; no new flag or independent discovery. Implementation of that
picker stays gated until the amendment is accepted; P1–P3 and the seven settled decisions are unaffected.
**Alternatives considered:** Product-level herdr integration, mandatory prompt configuration, a new palette or background-only identity.
These reopen settled decisions. A failing SC-010 palette measurement is the specified reason to revisit palette values.

## Research result

No unanswered product-choice question remains for this plan. Runtime/native behavior remains explicitly unverified with a named owner
gate, rather than being treated as a planning clarification. Frozen contract edits, overall artifact approval and generated tasks
remain prerequisites of implementation, not research outcomes.

# Implementation Plan: Interactive shell for the container provider

Branch: `docs/012-plan-tasks` (PR target `main`) | Date: 2026-10-31
Spec: [spec.md](spec.md) | Tasks: [tasks.md](tasks.md)
Status: Draft for independent review and owner approval. The spec is itself a proposal ("nothing is implemented"); this plan
and the tasks approve nothing, bind no revision and authorize no source work. Approval needs an independent review CLEAR for the
exact head of spec, plan and tasks and explicit owner approval of that head, evidenced by a tracker or PR link (WORKFLOW.md).
Decisions D1 to D8 are the owner's as the spec records them; their tracker or PR link is still to be added by the spec.

## Summary

Add `whr sandbox shell <id>` for the container provider: one login shell with a pty inside a `ready`, owned instance, started
as a single container runtime client child that owns the terminal directly (as `whr shell --local` does, Feature 010), with
admission through the Feature 008 `Admit` and capability `interactive_shell=pty` reported only after conformance cases
S12-C1 to S12-C8 and the real-host gate SC-010 pass. Until then the provider keeps `interactive_shell=none` (FR-003).

## Technical Context

**Language/Version**: Go 1.27.2, module `apps/whr` (`.tool-versions`, `apps/whr/go.mod`); Python 3 repository checks.
**Dependencies**: standard library, existing Cobra registry, existing `golang.org/x/sys/unix`, the local `docker` command-line
client as a child process (Feature 010 OQ-1). No new module dependency and no Docker SDK. Whether the end-of-exec query needs the
engine API is open (research R-5); a direct engine API call over the selected Unix socket needs no SDK but is not decided here.
**Storage**: the instance record of Feature 008. Feature 008 defines no store, location or lock (spec FR-014); Feature 011
FR-009 (draft) names a local record store with an exclusive lock. See research R-1 and open question 2.
**Testing**: Go table and property tests; the fake provider and conformance suite of Feature 008 T008; fake Docker client and
daemon as in Feature 010; `internal/ptytest` for native terminal wiring. No real Docker, container, credential or host mutation
in any task except the owner gates T002 and T025.
**Target platform**: hosts with a local Docker-compatible runtime reached by a `unix://` socket (Feature 010 FR-002, Feature 009
FR-029 selection function). The spec states no further platform difference; see "Platform notes".
**Constraints**: stdin and stdout must be terminals; no `--json`; no command or agent flag; no start of a stopped container; no
recording of session bytes; exit codes only from the ADR 0013 table (no new code); `docker` addressed by full id with an explicit
endpoint and a fixed argument vector.
**Scale/Scope**: one instance, one session, once per instance (D5). No reattach, multiplexing, `exec`, VM backend or remote path.

## Guardrail check

Checked against the constitution principles as the 010 plan does (`.specify/memory/constitution.md`). No exception is proposed.

| Principle | Evidence in this plan |
| --- | --- |
| I Product scope | No AI runtime, queue or service; the command starts a login shell and takes no command (FR-020, Feature 006 FR-030). |
| II Portable configuration | Provider-neutral contract: `Admit`, refusal code and lifecycle name no provider kind (FR-022); Docker only as the runtime client. |
| III Reproducible Git workspaces and bundles | No workspace or bundle is changed; the exec plan is a fixed argument vector built from validated values, with no environment file and no operator-supplied environment (FR-008, FR-009). |
| IV Identity and host boundaries | No grant needed, local only, as the owner decided (D6); no host socket mount, no privileged option, no new mount or capability (FR-018); the unfiltered terminal channel is stated as not enforced and gated by confirmation for `untrusted_job` (FR-011, FR-023). |
| V Durable operations | Atomic `ready` to `running`, recorded failure reasons, bounded 5 s end-of-session query, honest orphan limits (FR-014 to FR-017, FR-024). |
| VI Repository as authority | Spec is authority; unsettled points are listed below, not decided here. Amendments to Features 006, 008, 010 text, ADR 0013 and ADR 0014 are owner-gated tasks. |
| VII Executable feedback | Every task names failing tests first; fakes cannot satisfy SC-010; absent real-host evidence keeps `interactive_shell=none`. |
| VIII Human control | Independent exact-head review, owner merge; registration, frozen contracts and the capability flip are owner gates (T019 to T022, T023, T026). |

## Dependencies on other features

Verified from the specs and the tree at `origin/main` f00885c. A task starts only with the prerequisite implementation and test
evidence; a checkbox is not evidence either way (Feature 006 T025 and T031 are unchecked while `internal/exitcode` and
`internal/ptytest` exist in the tree), so T001 re-verifies each row.

| Item | Evidence | Use / rule |
| --- | --- | --- |
| 008/T001, T002 (capability and refusal types, pure `Admit`) | `apps/whr/internal/runtime/` does not exist; [package-a-status](../../docs/quality/package-a-status-20261024.md) lists T001 to T017 as open | Prerequisite of T004. If absent, stop and ask; do not create `runtime/` from this feature. |
| 008/T003 (sandbox model: id, labels, state table, retention) | `apps/whr/internal/sandbox/` absent; audit lists T003 as absent | Prerequisite of T005, T008. Spec FR-014 adds a Feature 008 requirement (atomic start) that lands in the T003 package or an amendment of it. |
| 008/T004 (`ExitFor`), 006/T025 (exit table, envelope) | `ExitFor` absent; `internal/exitcode` and `internal/output` exist in the tree | SC-007: no new code, freeze test unchanged apart from the `sandbox shell` passthrough entry. |
| 008/T008, T009 (fake provider, conformance, second provider) | absent | Host S12-C1 to S12-C9 (T015). |
| 008/T013 (container provider over a `DockerAPI` port) | absent | `Shell` for the container provider is T016; blocked until T013 has evidence. |
| 008/T016, T017 (register `sandbox`, ADR 0014 amendment) | absent; both are owner gates scoped to the seven existing verbs | `shell` needs them extended (spec "Changes to existing documents"); T019, T023. |
| Feature 011 (`interactive_shell` field A1, `refused.interactive_unavailable`, owner decision 1) | PR #290 and #291 are merged (merge commit 7bacb00 is an ancestor of `origin/main`); `specs/011-vm-provider/spec.md` on main has Owner decisions 1, 9 and 14 (decision 1: the container provider stays `interactive_shell=none` until a separate feature lands, then becomes `pty` with status `enforced`). Feature 011 itself is still a draft, and amendment A1 is not applied to Feature 008: `specs/008-workspace-sandbox/` has no `interactive_shell`, and `refusal-codes.json` has no `refused.interactive_unavailable` | Spec FR-004 and "Changes to existing documents": the 011 target is owner decision 1 (and FR-003). If A1 is not applied first, the gated docs task T022 adds the field and the one code; library task T004 edits no contract. |
| Feature 010 `internal/localdocker` (client environment allowlist, 5 s budget; `ResolveEndpoint` wraps `doctor.ResolveLocalSocket` and so implements the pre-D-12 rule) and `internal/localshell` (TERM validation, exec plan, liveness inspect, exit classification) | present in the tree; P1/P2 merged per the 010 tasks and package-a-status; G-A/G-B real-host acceptance of 010 still open | Reuse the client environment allowlist, budget, TERM validation and exit classification by import or narrow extraction, not copy (as 010 did for `workspacecfg`); take the endpoint from the 009 selection function, not from `ResolveEndpoint`. Exec argv differs: detach keys disabled here, a fixed sequence there (FR-008). |
| Feature 009 FR-029, FR-030, FR-032 (single endpoint selection function; owner decisions D-12 to D-18, merged on main) | FR-029 names "the runtime-access step of Feature 012" as a consumer: order `--container-endpoint`, host-profile `container_endpoint`, `DOCKER_HOST`, `/var/run/docker.sock`; the first specified source is the only one used and is never replaced by a lower one. FR-030: the option is accepted by the later commands of Features 011 and 012; a malformed value exits 64. FR-032: no usable endpoint exits 68 with a `whr:` line naming the source and the remedies (the option, the config value, `whr setup`); ordinary commands never prompt or investigate. No code under `apps/whr` has `container-endpoint` or `container_endpoint`; the plan and tasks update for it is in draft PR #299 | Prerequisite of T008 and T016: the 009 selection function, not yet built. Cite PR #299; do not duplicate its tasks here. Conflict with spec 012 FR-007: see open question 9. |
| 006/T031 (PTY harness), 006/T027 (doctor socket rules) | `internal/ptytest`, `internal/doctor` exist | Native terminal wiring tests (T024). |
| 006 FR-006, FR-019, FR-022a, FR-047 text | spec "Changes to existing documents" | Owner-gated amendments (T021); FR-030 unchanged. |
| ADR 0013, `contracts/cli/exit-codes.json` | `remote_status.commands` lists `shell`, `console`, `ssh`, `agent attach` | Passthrough entry and killed-client exception (T020). |
| `docs/container-runtime.md` | Linux/macOS Docker runtime; Windows unsupported | Platform notes; not edited. |

## Project structure

```text
specs/012-container-interactive-shell/
  spec.md          # authoritative requirements and decisions D1 to D8
  plan.md          # this plan
  tasks.md         # test-first tasks, FR/SC/decision coverage, owner gates
  data-model.md    # record, Plan and outcome additions only (no new entity)
  research.md      # mechanism options and the five verification items
  quickstart.md    # offline checks and the SC-010 owner runbook
docs/quality/012-container-interactive-shell/   # evidence, created by T001/T002/T025 (not in this PR)

apps/whr/internal/                  # package names are proposals within ADR 0010
  runtime/                          # Shell on Provider, Admit rows, fake provider, conformance S12-C1..C9 (Feature 008 T001/T002/T008)
  sandbox/                          # record additions, atomic start, reconciliation (Feature 008 T003)
  sandbox/shellsession/             # new: order of checks, confirmation, exec plan, supervisor, end-of-session table
  runtime/docker/                   # container provider Shell, end-of-exec query adapter (Feature 008 T013)
  localdocker/, localshell/         # existing (010): endpoint, client environment, TERM validation; reused, not duplicated
  commands/                         # registration of `sandbox shell` (owner gate, extends 008/T016)
  ptytest/                          # existing test harness
```

No contract file under `contracts/cli/` and no ADR is edited by a library task; only the gated tasks may.

## Staged approach

Stage 0 (T001 to T003): approval binding and prerequisite audit; the owner-run verification of the five open runtime questions;
amendment proposal text. No code.
Stage 1 (T004 to T015): offline library behind ports with fakes; no command registered, no frozen contract changed (T004 edits none; the one contract append it may need is the gated T022), no real
Docker. Order is dependency order: admission (T004), record and atomic start (T005), Plan fields (T006), exec plan and
environment (T007), check order and ownership (T008), terminal gate (T009), confirmation (T010), notice and help (T011),
supervisor, commit point and signals (T012), end-of-session table (T013), reconciliation (T014, held until verification V5),
conformance wiring (T015). T004, T005 and T006 can start together; T009 and T011 are independent of their wave peers (different files, no prerequisite relation).
Stage 2 (T016 to T018): container provider `Shell` over the fake `DockerAPI`, the production end-of-exec query behind its port,
and the capability report that stays `none` until the gate. Needs 008/T013 and the T002 results.
Stage 3 (T019 to T024): owner-gated docs amendments (ADR 0014, ADR 0013 and `exit-codes.json`, Feature 006, Features 008 and
011 text), then registration of the command and the native terminal tests.
Stage 4 (T025 to T027): the SC-010 owner gate on a real host; the owner-gated flip of the capability report to `pty` with
status `enforced`; acceptance and handoff. Each task is one PR of at most about 1,500 changed lines, targeting `main`.

## Requirements to evidence

Full FR/SC/decision mapping is in [tasks.md](tasks.md#coverage). Test names there are planned, red first.

| Requirements | Task | Evidence location |
| --- | --- | --- |
| FR-004, FR-022; S12-C1; SC-001 | T004, T015 | `internal/runtime/admit_test.go`, conformance suite |
| FR-005, FR-014, FR-017; S12-C5; SC-005 | T005, T008 | `internal/sandbox/`, `internal/sandbox/shellsession/` |
| FR-008, FR-009, FR-018 to FR-020; S12-C2, S12-C3; SC-003 | T006, T007 | `internal/sandbox/shellsession/execplan_test.go` |
| FR-013, FR-015, FR-016; S12-C6, S12-C7; SC-007, SC-008 | T009, T012, T013 | `internal/sandbox/shellsession/` |
| FR-023; S12-C8; SC-012 | T010 | `internal/sandbox/shellsession/confirm_test.go` |
| FR-024; S12-C9; SC-013 | T014 (held on V5) | `internal/sandbox/reconcile_test.go` |
| FR-003; SC-002 | T018, T026 | capability report tests; flip PR |
| SC-010, FR-011/FR-012 live parts | T025 | `docs/quality/012-container-interactive-shell/` owner record |

## Risks and handling

| Risk | Handling |
| --- | --- |
| **PTY handling.** `whr` must not relay the stream: the client child owns the terminal (FR-010, FR-011). A `whr` that proxies or logs bytes breaks the safety statement. | One child with the real descriptors, no copy loop, no recording; test that `whr` writes nothing while the client runs (T011) and that no pty is requested for a non-terminal (T009). Resize is expected from the client (FR-012); unverified until V1 and SC-010; if SC-010 fails, own resize handling is a new owner decision, deferred. |
| **Detach key.** The runtime must be able to disable its detach sequence (FR-008); if not, no `pty`/`enforced`. | V2 in T002 before T016; T007 asserts the flag in the recorded vector; failing V2 blocks T026. |
| **Signals.** Client reaction to SIGINT, SIGTERM and SIGHUP after hand-over is unverified (FR-013). The SIGHUP rule is an author proposal. | V4 in T002; T012 tests `whr`'s own rules with a fake child; SC-010 covers the live part. Exit 72 and `interrupted` as the spec states; no claim that the in-container shell ended. |
| **Terminal restore.** Neither the spec nor Feature 010 requires `whr` to restore terminal modes, because it never puts the terminal in raw mode itself and the client owns the tty. If the client is killed while holding raw mode, the terminal can stay raw. | Not claimed. Spec states nothing on restore; plan adds no requirement. SC-010 should observe the terminal state after a killed client and record it; whether restore becomes a requirement is open question 5. |
| **Atomic start** (D8c) has no mechanism in Feature 008. | research R-1 options; T005 tests the contract (exactly one winner, no return to `ready`) against any mechanism; the choice is open question 2. |
| **End-of-exec and reconciliation queries** are unverified (V5). `docker exec` prints no exec id. | T002 decides; adapter in T017 sits behind a port; FR-003 cannot claim without the end-of-exec query (S12-C7 is in the gate), FR-024 can lag (T014, S12-C9 not a gate). |
| **Environment route.** Values must not appear in the argument vector or process list (FR-009). Route unverified (V3). | T007 asserts no value in argv with a fake route; SC-010 checks the process list on a real host. |
| **Unfiltered control sequences** from the instance act on the owner's terminal. Not preventable here. | Stated in help and "Not enforced"; confirmation for `untrusted_job` only (D1, FR-023); nothing is claimed beyond informed consent. |
| **Orphans and a killed `whr`.** A killed client can leave the shell running; a SIGKILLed `whr` leaves `running` with no transition (FR-017). | Honest limits, no reaper; `discard` after `failed`; reconciliation T014 when V5 allows. Tests of the stuck-`running` behavior in T005. |
| **Status ambiguity.** A shell status in 64 to 79 equals a CLI code. | Accepted by ADR 0013 for `ssh` and `shell --local`; `whr:` line only for CLI failures (T013). |
| **Spec drift.** Spec text still says PR #290 and PR #291 are unmerged; both are merged. Feature 011 is a draft and A1 is not applied to Feature 008; 008 has nothing built; Feature 009 D-12 to D-18 changed the endpoint rule after the spec text. | T001 re-checks each dependency at start; amendments only through owner-gated PRs; a stale statement in the spec is a question to the owner, not an edit here. |
| **Endpoint rule.** Spec 012 FR-007 accepts only `--confirm`, `--yes` and global options, but 009 FR-030 requires `--container-endpoint` on the later commands of Feature 012. | Not settled here (open question 9); the option test of T009 is held until the owner answers. |

## Platform notes

The spec states only these platform-relevant facts: the endpoint must be `unix://` (a `tcp://` or `ssh://` endpoint exits 68,
FR-005 step 5, edge cases); the docker client must be present; docker contexts are never used; no `sudo` is run. It makes no
Linux/macOS/Windows distinction beyond that. From other documents: Linux and macOS are Docker host platforms (Feature 010 target
platform, `docs/container-runtime.md`: macOS recommends OrbStack, Linux uses Docker Engine); Windows is unsupported until credential
and terminal evidence exist (`docs/container-runtime.md`). `internal/ptytest` has Linux and macOS back ends and a stub for others,
so native terminal tests (T024) run where it does. Differences in pty size propagation, signal delivery to the client and the
status of a killed client between Docker Engine on Linux and a macOS runtime are not stated by the spec and are not assumed:
SC-010 says "a real Docker host", and which hosts it covers is open question 4.

## Departure from the spec's verification order

The spec lists V1 to V5 under "Verification before the plan". This plan is written before they are run and schedules them as T002 after approval, with the outcomes binding later tasks (T014, T016, T017, T026). The owner accepts this order or asks for T002 to precede plan approval (open question 8).

## Plan decisions the spec assigns to the plan

Proposals the owner confirms with approval of this plan; each is also an open question.

1. Notice text (FR-010), one line on standard error only when it is a terminal, fixed bytes (`<id>` and `<profile>` escaped per Feature 006 FR-022): `whr: sandbox shell in <id> (<profile>; no grant; host container runtime access)`. Modeled on `whr: local shell in <name> (no grant; host Docker access; detach keys ...)` of Feature 010 FR-010; no detach keys exist to name. T011 tests these bytes.
2. Working directory per profile (Plan field, D3): the plan fixes the rule, not literal paths, because the mount targets belong to the Feature 008 Plan (008/T006, T011), which is not built: `developer` the target of the workspace bind mount; `cooperative_job` the target of the linked worktree mount; `untrusted_job` the target of the private copy mount; each an absolute path of at most 4096 bytes. Literal values are asserted by T006 against the 008 Plan once it exists (open question 10).
3. Conformance numbering: the cases keep their stable labels S12-C1 to S12-C9 as identifiers in the suite; no sequential numbers are assigned, because Feature 011 FR-025 appends cases to the same suite and its numbering is not fixed. T015 adds the labels; references in the spec stay valid.
4. Client binary disappears between step 5 and the launch: read as a failed start, exit 74, `failed`/`start_failed` (the spec's primary reading); the absent-client reading (exit 68) is not used. T012 tests this.

## Handoff file

No `HANDOFF.md` is added. 009 and 008 have one for delegating staged source work after approval; 010 has none, and this is a plan-stage change with no source work authorized. Add one when the owner authorizes source tasks.

## Measurement needs

No performance target exists. These measurements are needed, all by owner-run checks on a real host (T002, T025), none by CI:
V1 resize carriage; V2 detach disabling; V3 environment route and the process list; V4 client behavior on SIGHUP/SIGTERM/SIGINT
and its status for a killed client, a stopped and a removed container; V5 the read-only end-of-exec query and the reconciliation
query; behavior of the 5-second budget of FR-016 when the daemon stalls; terminal state after a killed client. Fake tests prove
`whr`'s rules only and must not be reported as these measurements.

## Open questions for the owner

Mirrors the PR body. None blocks reviewing the spec; items 2, 3, 4, 9 and 10 affect task scope. These are plan questions; the spec's own
open question 1 (exit 70 versus 72 for the confirmation, and 72 for a killed or lost client) is carried here unchanged as item 1.

1. Spec open question 1: exit 70 for a declined, unanswered or impossible confirmation and exit 72 for a killed or lost client
   stream (FR-015, FR-016, FR-023). T010, T012 and T013 test the spec text as written; a different answer is an ADR 0013 decision.
2. Mechanism and home of the instance record store for the atomic `ready` to `running` update (D8c) and the owner process id and
   start time (FR-024). Feature 008 defines none; Feature 011 FR-009 (draft) has a locked local record store. Proposal in research
   R-1: use the Feature 011 store if it lands first, otherwise a minimal store with a conditional update in 008/T003.
3. Landing order and ownership: Feature 011 is a draft and amendment A1 is not applied to Feature 008, whose T001 to T017 are unbuilt; the 009 selection function (draft PR #299) is unbuilt too. This feature's tasks depend on all three. Confirm that T022 (gated) may add `interactive_shell` and `refused.interactive_unavailable` to Feature 008 contracts only if 011 has not landed them first (FR-004).
4. Which real hosts must SC-010 cover (Linux Docker Engine, macOS with OrbStack, both)? The spec says "a real Docker host" only.
5. Terminal restore: should `whr` verify or restore the terminal mode after a killed client? The spec requires nothing; the plan
   adds nothing; this lists the risk only.
6. Whether the reconciliation (FR-024, T014) is built before V5 verifies a query. The plan holds it; it does not block FR-003.
7. Author proposals the spec still marks for confirmation and the plan tests as written: `WHR_SANDBOX_ID`, the SIGHUP rule, the
   failure `reason` field and values, the owner process id and start time, the `--confirm <id>` form, the widened trigger text.
8. Whether V1 to V5 (T002) may follow plan approval, as scheduled, or must precede it as the spec section title says.
9. Endpoint option conflict: spec 012 FR-007 accepts only `--confirm <id>`, `--yes` and global options, yet 009 FR-030 says `--container-endpoint` is accepted by the later commands of Feature 012 (malformed value: 64). Either `--container-endpoint` is added to FR-007's list or treated as a global option; the plan does not choose.
10. Confirm plan decisions 1 to 4 above (notice bytes, working directory rule, S12 label numbering, client-disappears reading), and supply the literal per-profile working directories once the 008 Plan exists.

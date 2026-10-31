# Implementation Plan: Host and workspace panes

**Branch**: `docs/010-oq11-tasks-20261023` | **Date**: 2026-10-23 | **Spec**: [spec.md](spec.md)
**Baseline**: `82136e51c82ccf62ceaf7491667e23f59035847a` (`main`, including merged OQ-11 and OQ-12).
**Status**: P1–P4 implementation packages are merged; [recorded evidence](../../docs/quality/010-host-and-workspace-panes/implementation.md)
binds their accepted scopes. G-A/G-B real-host acceptance and T031 closure remain open;
SC-007 runtime session creation remains outside the delivered configuration scope.
The owner requested this plan. [PR #212](https://github.com/wstein/workharbor/pull/212) merged the specification and
[PR #214](https://github.com/wstein/workharbor/pull/214) records seven owner decisions; [PR #216](https://github.com/wstein/workharbor/pull/216) adds the eighth, OQ-11, and SC-011. [PR #217](https://github.com/wstein/workharbor/pull/217) adds the ninth, OQ-12, and resolves picker eligibility. The spec retains its Draft/approval state separately from implemented package scope;
this plan does not invent revision-bound spec/plan approval, task approval, build authority or runtime activation.
Before implementation, bind the reviewed spec/plan/tasks revisions and owner approval links under [WORKFLOW](../../WORKFLOW.md).

## Summary

Deliver four ordered packages: **P1 `ls --local` (006 FR-066) → P2 `shell --local` → P3 console default `plain` →
P4 optional plugin in `integrations/herdr/`**. A follow-up package **P5** (endpoint selection of Feature 009 D-12 to D-18, tasks T032 to T034) changes only how P1 and P2 obtain the endpoint. P1 supplies shared discovery; P2 consumes it rather than implementing another
container finder. P3 reconciles existing schema, vectors and defaults. P4 consumes the public CLI without entering product dependencies.
The real-host owner gate **SC-009** is separate from offline acceptance and PR readiness.

## Technical Context

**Language/Version**: Go 1.27.2 for `apps/whr`; Python 3.14 for repository checks and the future standalone palette verifier;
existing shell/TOML conventions for plugin entrypoints. Versions come from `.tool-versions` and `apps/whr/go.mod`.
**Primary Dependencies**: existing Cobra command registry, standard Go library, existing `golang.org/x/sys/unix` for safe Unix file access,
local Docker CLI; optional host herdr only in P4. No new product module dependency or Docker SDK.
**Storage**: no session database or audit transcript; bounded read-only Docker metadata and host `workharbor.json` reads.
**Testing**: Go table/property tests, fake Docker daemon/client, existing `internal/ptytest`, schema vectors, Python repository checks;
separate plugin script/manifest checks. Native Linux/macOS PTY evidence and SC-009 remain distinct.
**Target Platform**: Linux and macOS Docker hosts reached directly or through the owner's SSH/VPN connection; Unix socket only.
**Project Type**: existing Go CLI plus an optional external multiplexer integration.
**Performance Goals**: one shared 5-second discovery/preflight budget; interactive shell has no discovery timeout;
after a nonzero Docker client status, at most one liveness inspect with its own 5-second budget (010 FR-004/008).
**Constraints**: one owner, explicit `--local`, no control plane/grant/credential access, no lifecycle mutations, no agent launch,
no implicit shell fallback, no terminal stream relay/filter/recording. Plugin is never required by `make check`.
**Scale/Scope**: one local daemon, multiple labelled containers, arbitrary separate shell panes; five cosmetic workspace glyph/tint
choices, with collisions accepted and full title text retaining identity.

## Constitution Check

Pre-research and post-design checks give the same result; no new constitutional exception is proposed.

| Principle | Design evidence / gate |
| --- | --- |
| I: scope and independence | No AI runtime, cloud service, queue or required multiplexer. P4 absent leaves P1–P3 usable. |
| II: portable configuration | Docker-only local adapter; reject unsupported endpoint schemes before Docker; preserve devcontainer semantics. |
| III: reproducible inputs | Read existing manifest without following links, bounded at 64 KiB; no configuration rewrite or provisioning. |
| IV: host boundaries | ADR 0015's explicit local capability relaxation only; no sudo, socket exposure, grant bypass for remote commands or host-shell command. Labels are a usability guard. |
| V: durable operations | No container/storage lifecycle operation is introduced. Fresh exec lifetime, detached/orphan process limits are explicit; no fake cleanup guarantee. |
| VI: authority | Nine decisions below are fixed inputs. Frozen contract edits are separate reviewed P1–P3 amendments, not changes made by this plan. |
| VII: executable evidence | Failing behavior tests first; fake-host checks cannot satisfy SC-009. Missing native/host evidence stays unverified. |
| VIII: human control | Per-package exact-head review and PR; owner merge, artifact approval, standalone builds and real-host operation remain separate gates. |

Implementation readiness remains conditional on recorded artifact approvals and generated task scope; research completion does not
satisfy that gate. Unknown runtime behavior is covered by SC-009, not an unresolved product choice.

## Fixed owner decisions — do not reopen

Source: [spec Decisions](spec.md#decisions), recorded 2026-10-23 and merged by [PR #214](https://github.com/wstein/workharbor/pull/214) , [PR #216](https://github.com/wstein/workharbor/pull/216) and [PR #217](https://github.com/wstein/workharbor/pull/217).
Retain these IDs in implementation tasks and review evidence. Changes require an explicit new owner decision and authoritative spec/ADR
amendment; an implementation difficulty does not silently turn a settled question into a new default.

| ID | Settled answer | Implementation consequence |
| --- | --- | --- |
| OQ-2 | Container name or ID only; no manifest/devcontainer name alias. | Exact container name wins, otherwise an unambiguous ID prefix of at least 12 hex characters; no name-prefix expansion. |
| OQ-5 | Plugin lives in `integrations/herdr/`, separately delivered and outside `make check`. | P4 has its own verification entrypoint; P1–P3 do not import or invoke it. |
| OQ-6 | Missing shell uses Docker's own 126/127 error/status. | Exactly one interactive exec; no availability exec, remediation rewrite, install or substituted shell. |
| OQ-7 | Missing `console_mode` means `plain`, including existing manifests. | Apply on new session creation; preserve explicit `herdr` and all live console state. |
| OQ-8 | No local `agent attach`. | Refuse `--local` there; session-contract/grant requirements remain unchanged. |
| OQ-9 | Persistent pane title and tab label with emoji identify pane kind. | Plugin sets both; owner sets them manually without it. Prompt markers remain BundleDock shell policy. |
| OQ-10 | Accept FR-021 glyphs and FR-021a palette, including measured limits. | Marker is default; background is optional priority-P3 functionality in delivery package P4. Change palette only if SC-010 verifier fails. |
| OQ-11 | Use the configured working directory with the exact fallback warning when no workspace-folder mount is determinable. | Preserve the byte-exact TTY-only notice; emit the escaped warning after the notice and before handover, also on redirected stderr. Invalid fallback exits 68 with no exec and no warning. |
| OQ-12 | Picker reads only public name and state; state is solely for running eligibility. | Offer only state exactly running, display the name only, add no CLI filter; stale/non-running shell selections still refuse 68. Approved and merged in PR #217. |

External decisions that amend FR-002: Feature 009 owner decisions D-12, D-13, D-16 and D-18 (2026-10-31, [Feature 009 FR-029 to FR-032](../009-host-setup-wizard/spec.md)) fix the endpoint order option `--container-endpoint`, config value `container_endpoint`, `DOCKER_HOST`, `/var/run/docker.sock`; contexts are never used by local mode; commands here never prompt, never investigate, exit 68 with a `whr:` line when no endpoint is usable and exit 64 for a malformed option value. They are already merged in the spec text of this feature and are not reopened here; the implementation follow-up is package P5 below. The merged P1 and P2 code still implements the previous order (`DOCKER_HOST`, then the doctor candidate list) until P5 lands.

Also preserve foreman choices OQ-1 (Docker CLI, not Engine API client) and OQ-4 (`WHR_LOCAL_WORKSPACE`, `WHR_LOCAL_MODE`);
OQ-3 was withdrawn. They are not additional undecided owner questions.

## Project Structure

```text
specs/010-host-and-workspace-panes/
├── spec.md                 # authoritative requirements and settled decisions
├── plan.md                 # this ordered implementation plan
├── research.md             # repository evidence and resolved design choices
├── data-model.md            # ephemeral discovery, exec and identity values
├── contracts/local-cli.md   # design boundary; not the frozen command table
├── quickstart.md            # offline validation and SC-009 owner runbook
└── tasks.md                # dependency-ordered implementation units and owner gates

apps/whr/internal/
├── doctor/                 # existing socket rules; share rather than copy
├── localdocker/            # implemented P1 discovery and Docker adapter
├── localshell/             # implemented P2 policy, exec plan and child supervision
├── commands/               # existing routing, command mirror and freeze tests
├── ptytest/                # existing test-only terminal harness
├── workspacecfg/           # existing safe manifest reads, bounds and shell validation
├── scaffold/               # existing P3 default application
└── wizard/                 # existing P3 prompt defaults
contracts/cli/              # gated P1/P2 table/result amendments; P3 default text
contracts/workspace/        # gated P3 schema and shared vectors
integrations/herdr/         # implemented optional P4 manifest, entrypoints, verifier, tests
```

Package names are implementation proposals within the existing module (ADR 0010). P1 owns Docker subprocess/socket details behind
testable ports; P2 owns interactive policy. P2 reuses `internal/workspacecfg` for the no-follow bounded manifest read and
`CheckManifest`/`Manifest.HumanShell` validation (`MaxManifestBytes`, 64 KiB; fish/bash/zsh/nu). Its filesystem reader is private today:
expose a narrow manifest-only reader or extract the shared helper without duplicating parsing/safe filesystem logic or invoking full
workspace validation/Git checks for shell startup. Preserve symlink, oversized-file and invalid-shell regression coverage.
Reuse existing terminal escaping and PTY support. Do not expose Docker details through
control-plane or browser interfaces.

## Ordered delivery packages

The package sequence is an owner requirement, including for command publication. Each package may use multiple atomic Conventional
Commits and focused PRs. Do not mark the broader 006/T038 complete after extracting only its FR-066 discovery slice.

| Package | Dependencies | Scope and concrete acceptance | Boundary of completion |
| --- | --- | --- | --- |
| P1: local discovery | 006/T025 command/exit foundation and 006/T027 doctor socket rules | Implement only the read-only `ls --local` slice of 006/T038: recognized labels, running AND stopped containers, roles/label filters, mounts/home/protected-volume metadata, escaped human output and schema-validated `localWorkspaceListResult`. Reuse/extract doctor's socket resolver; pin explicit Unix endpoint; share a 5 s deadline. Tests prove no remote endpoint, context command or lifecycle mutation. Review command/result amendments under 006 FR-051/052. | Discovery library and CLI are accepted independently; `rm`, `stage`, `promote`, `drift` and remaining 006/T038 acceptance stay pending. Shell work starts against this accepted public/library boundary. |
| P2: local shell | P1; 006/T031 PTY harness; merged FR-010/OQ-11 applied under SC-011 | Add `localshell` and explicit local command routing; select across all states then reject stopped/paused/restarting. Build FR-004 inspect/exec vectors from validated metadata, pass full ID and fixed detach keys, reuse `workspacecfg` for the no-follow 64-KiB manifest read and allowed `human_shell` validation; validate user/path/TERM. Preserve the configured-working-directory fallback and its warning; include `TestLocalShellWorkingDirFallbackWarnsBeforeHandover` and its no-fallback, redirected-stderr and invalid-directory cases. Preserve remote refusal; refuse `console --local` with guidance and local flags for ssh/agent attach. Tests cover terminal gating, no credentials, one exec, stream byte preservation, statuses and bounded liveness classification. | `shell --local` works without control plane/herdr. Missing-shell 126/127 remains delegated. Record no guarantee of cleanup after pane closure; no runtime/host activation follows. Run SC-009 base checkpoint only with owner authorization. |
| P3: default `plain` | P2 accepted; existing scaffold/wizard/schema support | Reconcile FR-020 files: manifest schema/default vectors, scaffold, wizard prompts, output-schema default text, relevant product/architecture text. Tests cover omitted field on new AND existing manifests, explicit `herdr`, explicit `plain`, retention of explicit values and unchanged live console state. Follow frozen-contract amendment gates. | Configuration defaults reconcile; do not implement the absent owner API or claim remote console persistence verified. Runtime session-creation integration remains a prerequisite for claiming that part of SC-007. |
| P4: optional herdr integration | P1–P3 accepted; owner approves separate plugin scope; FR-018/OQ-12 amendment already merged in PR #217 | In `integrations/herdr/`, implement overlay picker and tab/split launch from public `whr` commands, name validation, fixed argv/env, persistent glyph title AND tab label, optional per-pane OSC 11 tint. Add independent fake-whr/herdr script checks and SC-010 verifier; no product dependencies, installation or make-check requirement. | Optional plugin may be reviewed separately; full SC-009 plugin checkpoint must be owner-accepted before claiming verified end-to-end pane behavior. Never use plugin absence to block accepted P1–P3 scope. |
| P5: endpoint selection follow-up (Feature 009 D-12 to D-18) | P1 and P2 merged; Feature 009 T020 (selection function) accepted for T032; Feature 009 T028 (b) (flag lists, owner-gated) accepted for T033 | Consume the single selection function in `internal/localdocker/endpoint.go` and the local commands (`ls --local`, `shell --local`); add the `--container-endpoint` option (flag lists amended by Feature 009 T028 (b)); keep the shared 5 s budget and `--host=<endpoint>` on every Docker call; no prompt, no investigation, no context read; reconcile `research.md`, `contracts/local-cli.md`, `data-model.md` and the P1 record. | A separate small package: it reopens no checked task of P1 to P4 and changes no SC-009 gate. No control-plane access, lifecycle call or new exit code. |

P1's registry amendment must add the FR-066 `--local`, `--label` and `--role` interface and output schema without activating remote
`ls`. P2's amendment must distinguish local availability and JSON refusal from the still-unavailable remote shell/events contract;
removing an `owner_api` blocker globally is not sufficient.

P4's FR-018/US3 reconciliation is approved and merged in [PR #217](https://github.com/wstein/workharbor/pull/217),
commit `82136e51c82ccf62ceaf7491667e23f59035847a` (OQ-12). The picker reads only public `name` and `state`, uses `state` solely for
eligibility, offers only records whose state is exactly `running` and displays the name only. P1 has no new running-state CLI filter;
P2 still refuses stale/non-running selections with 68. The amendment prerequisite is satisfied, not an open owner question; normal
P4 package/artifact approval and prior-package merge gates remain. Tests must cover running versus every non-running/unknown state.

### P2 working-directory fallback and warning (OQ-11, SC-011)

The FR-005/FR-010 amendment is merged and owner-approved in [PR #216](https://github.com/wstein/workharbor/pull/216),
recorded at baseline `29ce650184b4015ff02759c0b07835c914ba4db1`. No fallback-warning amendment gate remains.
When no matching workspace-folder mount destination can be determined, validate the container's configured working directory and use
it as `<dir>`. After the byte-exact notice (only on terminal stderr) and before the child starts, write exactly this warning line,
with `<name>` and `<dir>` escaped per Feature 006 FR-022:

```text
whr: warning: no workspace folder mount in <name>; starting in <dir>
```

P2's red-first `TestLocalShellWorkingDirFallbackWarnsBeforeHandover` asserts the exact warning bytes and notice → warning → child-start
ordering, the fallback exec `--workdir`, redirected stderr (warning present, notice absent), matching mount (no warning), and invalid
fallback (exit 68, escaped refusal, no exec and no warning). Validate an absolute directory of at most 4096 bytes without NUL/newline;
cover boundary/hostile controls and bidirectional characters. Assert no fallback/notice output after handover and an unchanged raw
child stream. Carry this test through [tasks.md](tasks.md); it implements settled OQ-11 rather than requesting another decision.

### Process, environment and error handling

P2 must supervise the Docker child long enough to wait for status and perform the FR-008 liveness check; replacing `whr` with Docker
via process `exec` would prevent that check. Give the child direct terminal descriptors; do not proxy or log shell bytes.
Discovery/preflight errors get the prescribed `whr:` line; after handover only diagnosed transport loss may add one (FR-008).
On nonzero client exit, an inspect response proving daemon reachability preserves the client status even if the container has vanished;
connection failure/deadline maps to 72. Test reachable-daemon container-not-found separately from daemon loss.

Construct the Docker client environment from an explicit minimal host-process allowlist (executable search/runtime essentials only),
never a wholesale copy containing tokens. Separately assert that its `--env` additions to the CONTAINER are exactly TERM plus the two
WHR markers. Container baseline environment inherited by Docker is not host forwarding; SC-005 tests must distinguish those boundaries.
This plan does not promise removal of credentials already configured in the owner's container.

## Requirements and planned evidence

| Requirements | Package | Required evidence |
| --- | --- | --- |
| 006 FR-060/066; 010 FR-002/003 | P1, P2 | Socket-order regression, fake daemon discovery/filter/state/deadline tests; local result schema vectors; zero lifecycle/remote calls. |
| FR-002 (amended), scenario 8, SC-001; Feature 009 FR-029, FR-030, FR-032, SC-018, SC-020; CC-5, CC-11 to CC-13, CC-17, CC-23 | P5 (Feature 009 T020; T028 (b) for the option) | `TestSelectedColimaEndpointNoFallbackToStandardSocket` (CC-5), `TestLocalEndpointUsesSelectedEndpoint`, `TestLocalEndpointOptionConfigDockerHostStandardOrder`, `TestLocalEndpointNeverPromptsInvestigatesOrReadsContext`, `TestLocalEndpointNoUsableEndpointExits68WithSourceLine`, `TestLocalEndpointMalformedOptionExits64`, `TestLocalEndpointNonUnixOptionExits68BeforeDocker`, `TestLocalShellContainerEndpointPinnedAsHost` | `apps/whr/internal/localdocker`, `apps/whr/internal/commands` |
| FR-001, FR-004–010, FR-013–017; SC-001–006 | P2 | Exact argv, hostile metadata/property tests, manifest no-follow bounds, `TestLocalShellWorkingDirFallbackWarnsBeforeHandover` (warning timing/escaping, redirected stderr, no-fallback and invalid-directory cases), PTY input/output gating, env canaries, status 0/1/7/126/127/137/255 and transport/deadline tests. |
| FR-005/010; OQ-11; SC-011 | P2 | `TestLocalShellWorkingDirFallbackWarnsBeforeHandover`: exact escaped warning, notice/warning/child ordering, redirected stderr, matching-mount silence, invalid fallback 68 with no exec/no warning. |
| FR-011/012/020; SC-007 | P2 refusal, P3 defaults | Command freeze tests, schema/shared vectors, scaffold/wizard tests; live-console criterion remains pending until runtime consumer exists. |
| FR-018/019/021/021a; SC-008/010 | P4 | Standalone fake-whr/herdr tests, picker cancellation and invalid names, fixed-name hash table, palette verifier, one OSC 11 only when enabled. |
| SC-009 | Owner, after P2 and after P4 | Versioned real-host record: terminal keys, detach, exit, SSH reconnect/state, titles/tab labels, narrow emoji labels and pane-only tint. |

## SC-009 owner gate and evidence

Use [quickstart.md](quickstart.md#sc-009-real-host-owner-gate); no fake or native CI PTY run substitutes for this gate.
Record evidence under `docs/quality/010-host-and-workspace-panes/` in a reviewed follow-up, with exact tested product/plugin SHAs,
OS/architecture, Docker/herdr versions, notebook terminal/font, fixture/container identity, steps, expected/observed results and explicit
owner acceptance link. Capture only controlled fixture evidence; never session contents, host environment or credentials.

Checkpoint A follows P2: all five individual keys, fixed two-key detach, shell status, resize, manual persistent identity and SSH
reconnection to the same shell/working directory/running command. Checkpoint B follows P4: repeat A with plugin, then title AND tab
glyph persistence after `clear`, narrow-label readability and optional background affecting only the opened pane.
Until both applicable checkpoints are recorded, SC-009 remains **unverified**; a failed visual capability blocks its acceptance,
not a redesign of OQ-9/OQ-10 without owner approval. An omitted optional background run must be explicitly marked not tested;
it cannot establish background verification.

## Validation and handoff

For this documentation change: repository checker, diff whitespace check, independent exact-head review, then PR against `main`.
Run `make check` as required and report its actual outcome; do not translate a blocked, interrupted or missing-tool check to a pass.
Feature 010 is not in the current `ci_changes.py` prose allowlist, so do not assume docs-only CI. Do not widen that allowlist as part
of this plan. Associated PR CI may run the full accepted check set under workspace publication policy.

For future product packages: red-first affected Go/schema/PTY checks, formatting, build/test evidence under applicable authorization,
required CI and independent review of exact head. Run the plugin verifier separately from repository-required checks.
SC-009, whole-feature completion, artifact approval, PR readiness, merge and deployment are distinct gates. Execute [tasks.md](tasks.md) only after its artifact/merge gates, preserving the four package IDs and nine decision IDs; this plan creates no implementation task completion claims.

## Complexity Tracking

No additional database, Docker SDK, control-plane endpoint, session protocol or product dependency is needed. Optional plugin assets
and verification remain isolated. The post-exit inspect is required by the current spec; background/titles remain cosmetic and cannot
be treated as a security boundary or as globally unique by glyph alone.

# Implementation Plan: Workspace and sandbox execution model

Branch: `docs/spec008-workspace-sandbox` (PR target `main`, owner decision 2026-10-20) | Date: 2026-10-20
Spec: [spec.md](spec.md)
Status: proposed for independent review and owner approval; no product code exists for this feature.

## Approach

Library-first and offline. Go packages in `apps/whr/internal/` (ADR 0010; module `apps/whr`), no new dependency, standard
library only:

| Package | Content |
| --- | --- |
| `runtime/` | capability types, `Admit`, refusal codes, `Provider` interface, `Plan`, fake provider, conformance suite |
| `sandbox/` | ids, labels, state machine, retention, result manifest, error to exit mapping |
| `gitprofile/` | location and host-only-key check, agent profile render, preflight orchestration |
| `runtime/docker/` (Stage 2) | container provider over a `DockerAPI` port; copy policy and export planners |

Stage 1 registers no command and no frozen contract (`contracts/cli/*`) changes (FR-028, SC-008); Stage 2 registers the `sandbox` group only through T016, after the ADR 0014 amendment PR T017 is approved (OD-4). Real `git` runs only on
fixture repositories and configuration files in `t.TempDir()`; no Docker, network or credential is used (FR-032).

Backend neutrality (owner decision 2026-10-20): `Admit`, the state machine, labels, results and the conformance suite never
branch on provider kind. A microVM provider (own guest kernel, for example Apple container or Docker Sandboxes `sbx`) is a
later provider specification that adds a provider and its tests and changes no contract (SC-010).

## Dependencies on Feature 006 (stated, not duplicated)

| 006 item | Use | Rule |
| --- | --- | --- |
| T025 foundation (`exitcode`, `output`) | exit table and envelope types | prerequisite of 008 T004 |
| T038 local lifecycle (`localdocker`: 5 s bounded inspection, ownership gate, volume protection) | the `DockerAPI` port, label gate and `bundledock-*` protection | 008 Stage 2 reuses it; if T038 is not on `main`, stop and ask instead of reimplementing |
| T039 drift reporting | none; tool-mount drift stays there | no overlap: 008 only forbids writable tool mounts |
| T027 doctor catalogue, T016 | the read-only git-config check (OD-5, option B) would be a catalogue section | wiring is owner gate OG-5; T005 ships the library only |
| T015 host setup | none; FR-026's Mac-host step is not T015 | OD-5 (recommended B) keeps `setup` unchanged; T015 and FR-009 are not changed here |

## Staging for Codex

Stage 1 (T001 to T010): contracts, capability model, admission and refusal semantics, sandbox model, git-config profile
checks, fake provider and conformance tests. No Docker. Stage 2 (T011 to T016): container provider with private clone and
export, scratch home, ownership labels, cancellation and cleanup, fake-Docker tests only. T017 (docs-only ADR 0014 amendment PR, owner-gated) can run at any time after T010. Out of this increment, each its own
specification: real Docker execution and acceptance, host mutation (including the git configuration move), any microVM
provider, egress, credential mediation, host gateway, IDE verification, snapshots.

## Threat model

Scope: a single-owner workstation or devcontainer host; the primary adversary is a less-trusted process in an instance
(prompt-injected agent, compromised dependency), not a hostile local administrator. Claims are limited to what the canary
tests show for the stated provider and stand with their residual risks.

| Asset | developer | cooperative_job | untrusted_job |
| --- | --- | --- | --- |
| Primary refs, `.git/config`, hooks | writable by design | shared with the worktree (residual: refs, config and hooks are shared and writable) | not reachable; copy has generated config, no hooks from primary |
| Sibling workspaces | not mounted | not mounted | not mounted |
| Primary home | the workspace's own home, writable by design | not mounted; scratch home | not mounted; scratch home |
| Host global git configuration | mounted rw (owner decision). Residual: anything running there can write `credential.helper=!cmd`, `core.fsmonitor`, `core.sshCommand`, `core.hooksPath` or `alias.*`, which runs on the Mac host as the owner at the next git use; whr cannot detect an agent run here | not mounted | not mounted |
| Workspace container of ADR 0009 | not a sandbox; no host-config mount (FR-024) | | |
| Host credentials (keychain, SSH agent, tokens) | not provided by whr; the checkout's local config and the host global config apply | not provided by whr; credentials, `insteadOf` rules and hooks in the shared primary `.git/config` are visible and writable (acknowledged) | not provided by whr; a credential an external runner adds is exposed to the instance |
| Tool roots `/opt/bdock/<bundle>`, `bundledock-*` | read-only | read-only | read-only |
| Host Docker socket | never mounted | never mounted | never mounted |
| Network | unrestricted (no enforcement) | unrestricted | unrestricted; exfiltration over the network is not prevented |

Attacker capabilities per profile: `developer` is trusted and can do anything the user can inside the mount plan;
`cooperative_job` can write shared repository metadata (rewrite refs, add hooks that the human later runs); `untrusted_job`
can read and write everything in its copy and scratch home and use the network, but only the copy. Canary acceptance tests
(SC-005): the fake provider returns the `Plan`; fixtures name a canary path or value per asset; the test fails if the plan
mounts it writable, mounts it at all for jobs, or passes it in the environment. Residual risks common to all profiles: shared
kernel (container escape), no egress control, no credential mediation, a result patch can contain malicious code and must be
reviewed, copy-policy secret patterns are heuristics, a shared Docker daemon is privileged infrastructure.

## Requirements to evidence

Owner repository is `wstein/workharbor` for all rows except the Anchorage consumer evidence, which stays with the owner.

| Requirements | Prerequisite | Acceptance test (planned, red first) | Evidence location |
| --- | --- | --- | --- |
| FR-012 to FR-015, SC-001, SC-002 | 006/T025 | `TestAdmitTable`, `TestAdmitMicroVMNeverFallsBack`, `TestAdmitUnknownNeverSatisfies`, `TestRefusalCodesMatchContract` | `apps/whr/internal/runtime/*_test.go`; PR description |
| FR-002 to FR-006, FR-008, SC-006 | T001 | `TestStateTransitionTable`, `TestIDFormatAndUniqueness`, `TestForeignObjectRefused`, `TestKeepThenCleanKeeps` | `apps/whr/internal/sandbox/*_test.go` |
| FR-019 to FR-022, SC-003 | T001 | `TestDeveloperPlanMountsDirectory`, `TestSingleFileConfigMountRefused`, `TestHostOnlyKeysReportedWithoutValues`, `TestPreflightEBUSYBlocksReady` | `apps/whr/internal/gitprofile/*_test.go` |
| FR-023 to FR-025, FR-030, SC-004, SC-012 | T002 | `TestAgentProfileMatchesContract`, `TestAgentNeverMountsHostConfig`, `TestCanaryKeysPerEnvironment`, `TestCommitWithoutIdentityFails`, `TestExecScrubsGitControlEnvForJobs`, `TestDeveloperRefusesGitConfigEnv`, `TestWorkspaceContainerPlanHasNoHostConfigMount` | same |
| FR-009 to FR-011, FR-016, FR-027, SC-005 | T006, T007 | `TestCanaryAssetsAbsentUntrusted`, `TestCooperativeNeedsAcknowledgement`, `TestToolMountsReadOnly` | `apps/whr/internal/runtime/canary_test.go` |
| FR-007, SC-011 | T011 | `TestExportPatchAndManifest`, `TestExportParentRepoHostileConfigIgnored`, `TestExportNeverPushes`, `TestExportIgnoresInstanceGitConfig`, `TestExportFromFailedInstance`, `TestResultDirSymlinkSafe` | `apps/whr/internal/runtime/result/*_test.go` |
| FR-017, SC-007 | T013, 006/T038 | `TestCancelPreservesResults`, `TestCleanupOnlyTouchesLabelledObjects` | `apps/whr/internal/runtime/docker/*_test.go` |
| FR-028, FR-029, FR-035, SC-008 | T003 | `TestExitMappingMatchesADR0013`; per-PR `git diff --stat $(git merge-base origin/main HEAD) -- contracts/cli docs/adr` empty | `apps/whr/internal/sandbox/exit_test.go`; PR description |
| FR-014, FR-034, SC-010 | T009 | `TestConformanceSuiteSecondProvider`, `TestNoProviderKindBranchesInProductionCode` | `apps/whr/internal/runtime/conformance/` |
| FR-026 | OD-5 | amendment proposal (T010); a check command and its test follow the decision | spec 006 amendment PR (later) |
| Real Docker confirmation (EBUSY, mounts, cleanup) | owner authorization (OG-1) | out of this increment | owner-run record |
| Anchorage devcontainer integration | owner | `make devcontainer-test` in Anchorage | Anchorage workflow |

## Owner decisions

Resolved on 2026-10-20 and recorded in the spec: the git configuration profile, backend neutrality, the PR target (`main`), the
owner statement on the Mac host and the Docker VM, and OD-1 to OD-4 below.

| ID | Decision (owner, 2026-10-20) |
| --- | --- |
| OD-1 | Generic `sandbox exec` of an opaque argv is outside Feature 006 FR-030. Exit-status passthrough needs a separate small ADR 0013 amendment; `exec` stays proposal text and is not in Stage 1 or in the Stage 2 registration. |
| OD-2 | An additive optional `error.refusal` field in `contracts/cli/output.schema.json`, with a freeze test. Still an owner gate before any command registration. |
| OD-3 | Agent git profile v1 and the host-only catalogue, including the five extension keys, are accepted as proposed (v1 "accepted by owner 2026-10-20"); the per-key rationale tables stay. The first tests still verify behavior against real git. |
| OD-4 | A new `sandbox` group, option A. It needs an ADR 0014 amendment; commands are registered in Stage 2 only (T016). Stage 1 is library plus fake provider with no new command. |

### Open: OD-5

Which command carries the Mac-host git configuration check and the guided move. In Feature 006, `whr setup` is Linux/systemd
host enrollment (FR-009, T015) that mutates the host receiving the host agent; on macOS only `whr doctor` (read-only, already
runs there with OrbStack), `whr validate` and `whr init` exist. Option A: amend 006 so `setup` gains a Mac-host mode. Option B:
keep `setup` as is, add the read-only host git-config check as a section of `whr doctor`, and make the guided move (backup,
explicit confirmation, `--dry-run` as default) a separate step, proposed name `whr host git-config move` (ADR 0014 already has a
`host` group; the name is a proposal). **Recommendation: B**, because it changes no accepted 006 contract and keeps the report
separate from mutation. Proposed first Mac flow: install `whr`, `whr doctor`, `whr init <project>`, then optionally the guided
move. Stage 1 depends on OD-5 only for the read-only doctor section (T005 library; catalogue wiring is an owner gate, OG-5);
the move is a later specification.

A-3 (analysis): the earlier reading of T015 as a control-plane server is withdrawn; T015 mutates the Linux host it enrolls and
the owner statement concerns the Docker VM. No owner-decision note on T015 is needed beyond OD-5.

## Owner gates

OG-1 any real Docker run or Docker-dependent acceptance; OG-2 host mutation, including the git configuration move; OG-3 image
or documentation builds, release and publication; OG-4 changes to `contracts/cli/*`, ADR 0013 or ADR 0014; OG-5 doctor
catalogue or `whr setup` changes; OG-6 any change to Feature 006 FR-030 or Feature 006 task text.

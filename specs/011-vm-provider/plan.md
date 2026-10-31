# Implementation Plan: VM provider and cross-platform test evidence

**Branch**: `docs/011-plan-tasks` (PR target `main`) | **Date**: 2026-10-31 | **Spec**: [spec.md](spec.md)
**Baseline**: `main` at `f00885c6a6cc440225b6cf90b8f072a69e0265d9`.
**Status**: Draft, proposed for independent review and owner approval. Nothing here approves the spec, binds a revision or
authorizes source work (WORKFLOW.md). The spec is unchanged by this plan. Every value this plan chooses where the spec
delegates a choice is marked "proposal" and listed in [Open questions for the owner](#open-questions-for-the-owner).

## Summary

Six steps, in this order: (1) a cross-platform CI evidence path that needs no VM (FR-027 to FR-029); (2) the vendor-name
gate (FR-022); (3) the generic contract amendment A1 to Feature 008 (FR-001 to FR-005), then the VM provider on a
vendor-neutral driver port with a fake driver, offline (FR-006 to FR-026); (4) the macOS host surface in `whr setup` and
`whr doctor` (FR-031 to FR-041); (5) the first real driver and its support matrix; (6) owner-run real-VM evidence, which is
a gate and not a task. Library first and offline, then owner-gated registration. The default `make check` never needs a VM,
a hypervisor, a network or a backend (FR-026, SC-011).

## Technical Context

**Language/Version**: Go 1.27.2 for `apps/whr` (`apps/whr/go.mod`, `.tool-versions`); Python 3.14 for repository scripts.
**Primary Dependencies**: standard library and the module's existing `golang.org/x/sys/unix`. No new module dependency, no
VM-backend SDK in the product contract; a driver talks to its backend through an injected process-runner port, so its tests
use recorded output and no backend (ADR 0010 module layout).
**Storage**: a local record store for ownership and reservations (FR-008, FR-009) and the host profile records of Feature 009
(FR-034, FR-041). Location is a proposal (below). No registry, no upload (FR-007).
**Testing**: Go table and property tests over a recording fake driver with failure injection; the existing Feature 008
conformance suite; Python `unittest` for repository scripts; fake runners for package manager, image build and container
runtime (Feature 009 FR-023 style). The hostile-argv oracle uses the host's own POSIX shell as the declared guest shell
model, which runs on both CI platforms and needs no VM.
**Target Platform**: provider and driver library build on Linux and macOS (tests run on both, T008); the VM provider is
exercised for real only on the owner's Apple-silicon Mac (OD-2). The macOS host surface is macOS only; Linux `setup` and
`doctor --fix` are unchanged (FR-031, FR-033, FR-034, FR-037).
**Performance Goals**: `vm.*`, `container.*` and `vm.space` checks stay inside the existing shared 5 s doctor budget and make
only the read-only driver `probe` and `list` calls (FR-018, FR-032, FR-039, FR-041). No other numeric target.
**Constraints**: no VM-specific command (FR-019); no vendor name in gated text (FR-022); no image publish, upload, sign or
registry pull (FR-007); `Admit` stays pure (FR-009); frozen `contracts/cli/*`, ADR 0013/0014 and Feature 006/008/009 text
change only through owner-gated tasks.
**Scale/Scope**: one owner, one Mac, a handful of local clones; the concurrency limit comes from the driver or is unlimited
(decision 2).

## Constitution and guardrail check

Pre-research and post-design checks give the same result; no constitutional exception is proposed.

| Principle or rule | Design evidence / gate |
| --- | --- |
| I scope and independence | No AI runtime, cloud account or queue. CI evidence needs no provider and the provider needs no CI (OD-1). |
| II portable configuration | Capability negotiation stays mandatory and never approximates: no fallback for guest, architecture, base image or driver (FR-001, FR-006, FR-023). Provider details stay behind the driver port (FR-021). |
| III reproducible inputs | The Plan records the base image name and the driver-reported identifier (FR-006). Image definitions are readable user-owned files from a release template (FR-034). Credentials never in config, only references (FR-007). |
| IV host boundaries | Only owned instances are changed; no host virtualization, network, firewall or security setting (FR-017); ownership proven before every mutation (FR-008); no host socket or home mount (FR-014). Protections are stated with scope and are not a hostile-tenant claim. |
| V durable operations | Bounded, idempotent `Cancel` and `Remove`; results survive removal; killed runs leave a traceable reservation (FR-009, FR-011, FR-020). Data deletion needs the explicit confirmation of Feature 008 FR-008 (FR-033). |
| VI repository as authority | Decisions stay in the spec; unsettled points are in the open-questions list, not decided here. Amendments land through owner-gated docs tasks. |
| VII executable feedback | Every task names failing tests first; real-VM, real-build and real-host claims stay unverified until owner-run evidence (FR-026). Missing tools are blockers, not passes. |
| VIII human control | One reviewed PR per task, owner merge, no agent merge. Draft PR and exact-head review per WORKFLOW.md; new owner gates below. |
| AGENTS.md / WORKFLOW.md | Conventional Commits, atomic; US spelling; PR target `main` with linear history; `scripts/select_feature.py 011-vm-provider` before Spec Kit skills; no push to `main`. |
| ADR 0013 | Every new refusal maps to 68 like the existing ones; confirmation missing is 70; partial cleanup 73; deadline 71; no new exit code (spec "Changes to existing documents", Feature 008 FR-035). |
| FR-022 gate | This plan and `tasks.md` name no VM backend. The first driver is written `<first-driver>`; its identity is the informative note of the spec (OD-5). |

## Dependencies on other features

Verified against `main` at the baseline on 2026-10-31 (file listing of `apps/whr/internal/` and the 2026-10-24 status
reconciliation in [package-a-status-20261024.md](../../docs/quality/package-a-status-20261024.md)).

| Feature | What 011 needs | Evidence | Rule |
| --- | --- | --- | --- |
| 008 Stage 1 (T001 to T009) | `runtime/` capability types, `Admit`, refusal codes, fake provider, conformance suite, `sandbox/` labels and state, `ExitFor` | Spec Assumptions: "Admit, TestAdmitTable, the conformance suite and `apps/whr/internal/runtime/` do not exist yet". `main` has no `runtime/`, `sandbox/` or `gitprofile/`; status audit: no 008 task has implementation evidence | All library tasks T012 to T025 wait for the named 008 tasks. If absent, stop and ask; never reimplement. |
| 008 Stage 2 (T011 to T014) | copy policy (FR-014), result export (FR-020, FR-033), container provider that reports the FR-003 values (SC-002, SC-030), container cleanup (FR-033) | Spec FR-003, FR-014, FR-033; 008 tasks T011 to T014 | Same rule. T023 and T024 need T013. |
| 008 T016, T017 (command registration, ADR 0014 amendment) | the `sandbox` group that SC-019 and SC-030 count | Spec FR-019, FR-035 (commands `whr sandbox ...`) | T038 waits for them. No 011 task registers a command. |
| 008 contract and text | A1: FR-012, FR-034, FR-035, data model, `runtime-provider.md`, `refusal-codes.json` | Spec "Changes to existing documents" | Owner gate OG-8, docs only (T002). |
| 009 catalog, pkgmgr, profile, plan, fix, wizard | tap-sourced entries, new step kinds, profile records, `doctor --fix`, wizard offers (FR-031 to FR-041) | Main has `hostsetup/{catalogue,detect,dotfiles,native,pkgmgr,plan,profile}`; no `fix/`, `gitmove/` or wizard package; 009 T001 and T004 checked, T002, T003, T006 source-only, T005, T007, T010, T011 open (status audit) | Stage 4 waits for 009 T001, T003, T006, T007, T010. T011 registration is shared and owner-gated. |
| 009 D-12 to D-18 endpoint selection | one container endpoint for `container.image`, `container.leftovers`, the storage lookup and the image build (FR-039, FR-041, spec decision 23) | Spec FR-029 to FR-031 of Feature 009 exist; [009 tasks.md](../009-host-setup-wizard/tasks.md) has no task that mentions an endpoint and the code has no `container_endpoint` | Open question OQ-6. Until a 009 task exists, Stage 4 container tasks take the endpoint through an injected port and are not accepted against the real selection. |
| 010 `localdocker` (P1, merged) | bounded, endpoint-pinned, read-only Docker adapter and label ownership gate for container list, inspect and removal | No direct citation in the 011 spec; indirect: 009 FR-027 and FR-029 require every `docker` call to carry `--host=<endpoint>` (Feature 010 FR-002); 008 plan names `localdocker` the reused port for the container provider | Reuse, not duplicate, in T027 and T035. 010 `localshell` is not touched. |
| 006 (T025 exit/output, T027 doctor port, FR-060, `doctor-checks.json`) | exit table, envelope, doctor runner, six new check ids, budget lists | Spec "Changes to existing documents"; `contracts/cli/doctor-checks.json` has no `vm.*` or `container.image` id | Text edits are owner gates OG-4, OG-5, OG-6 (T003, T037). |
| 012 (draft, PR 297) | the shared `whr sandbox shell` command and an enforced `pty` for the container provider | Spec FR-016, decisions 1 and 14 | Not a prerequisite. The `pty` path is deferred (D-1). |
| ADR 0013, ADR 0014 | codes 64, 68, 70, 71, 72, 73; the `sandbox` group | ADR 0013 table; 008 T017 | No change by this plan. |
| BundleDock, Anchorage | none | Spec "Follow-up outside this feature" | No task edits those repositories. |

## Project structure

```text
specs/011-vm-provider/
|-- spec.md      # authoritative requirements and owner decisions (unchanged)
|-- plan.md      # this plan
`-- tasks.md     # dependency-ordered tasks and the FR/SC coverage table

scripts/                       # Stage 1, 2: check_skips.py, CI checks, vendor gate in check_repo.py
.github/workflows/check.yml    # Stage 1: platform matrix and aggregate
apps/whr/internal/
|-- runtime/                   # 008: Admit, capabilities, refusal codes, conformance (A1 edits, Stage 3)
|   |-- vm/                    # VM provider: select, ownership, records, reservations, plan, lifecycle
|   |   `-- fake/              # recording fake driver with failure injection (no vendor name)
|   `-- drivers/<first-driver>/  # Stage 5 only: adapter over a process-runner port
|-- doctor/                    # vm.* and container.* checks, disk.container_storage lookup (via 006/T027 port)
`-- hostsetup/                 # 009 packages extended: catalogue, pkgmgr, image/, fix/, wizard summary
docs/drivers/<first-driver>.md # support matrix, license note, base image recipe (adapter documentation)
docs/quality/011-vm-provider/  # platform baseline and owner-run result templates
```

Package names are proposals within the existing module (ADR 0010). Driver packages are the only allowed place for a backend
name (FR-022).

## Stages

| Stage | Tasks | Content | Starts when |
| --- | --- | --- | --- |
| 0 owner-gated docs | T001 to T004 | amendment proposals (exact before and after text), then A1 to Feature 008, Feature 006 and `container-runtime.md`, Feature 009 text | T001 immediately; the others per owner |
| 1 CI evidence | T005 to T009 | skip-record check, no-Windows check, macOS baseline measurement, platform matrix, injected-failure demonstration | immediately; independent of 008, 009 |
| 2 vendor gate | T010, T011 | FR-022 gate with fixtures, driver support-matrix check | immediately; before any driver code |
| 3 library, offline | T012 to T025 | A1 types and admission, driver port, fake driver, selection, ownership, reservations, lifecycle, conformance, boundaries | 008 Stage 1 merged, T002 merged |
| 4 commands and integration | T026 to T038 | doctor checks, catalog, tap steps, image library, wizard offers, readiness, cleanup, rebuild, registration (gated), parity | Stage 3 and the named 009 tasks |
| 5 first driver and evidence | T039 to T041 | `<first-driver>` adapter and matrix, recipe and license statements, acceptance | Stage 3, T011, verification below |

Each task is one pull request of at most about 1,500 changed lines (split when larger), targeting `main`. Real-VM, real-build and
real-host runs are owner gates, not tasks. Stages 1 and 2 are independent of the provider and can ship first; Stage 1 is the
authoritative cross-platform evidence path (OD-1).

## Plan-time values (spec delegates; all proposals, none measured)

| Value | Spec says | Proposal |
| --- | --- | --- |
| Runner images (FR-027: "pinned in the plan") | exact images pinned in the plan | `ubuntu-24.04` and `macos-15`, the labels the workflows already use; [releasing.md](../../docs/releasing.md) records `macos-15` as the native darwin/arm64 runner and states darwin/amd64 is not shipped. Confirm the architecture from the first job log (T007). No Windows runner (FR-029). |
| Aggregate records the head SHA (US1, SC-008) | "records the head SHA" | each job verifies and prints the tested head as the existing macOS jobs do; the aggregate job prints the SHA it gated |
| Reserved instance-name prefix (FR-008) | "a reserved prefix plus the instance id" | `whr-` followed by the Feature 008 id (`sbx-` plus 16 base32), one constant in `runtime/vm` |
| Local record store (FR-008, FR-009) | "local record store" with an exclusive lock | a directory below the Feature 009 state directory (`~/.local/state/whr/`, DP-1) with one lock file; OQ-1 |
| Bounded deadlines (FR-009, FR-010, FR-011) | "bounded", "deadline" | injected options, defaults: ready 300 s, graceful stop 30 s then force within 60 s total, reservation deadline ready plus stop plus 60 s margin. Unmeasured; tests use a fake clock |
| `image_build` deadline (spec: plan-time value, not 900 s) | plan-time value | no default set; the owner supplies it before T032 (OQ-7) |
| Skip record format (FR-028) | name platform, reason and tracking reference | one structured comment line per skip, parsed by `scripts/check_skips.py`; OQ-3 |

## Owner gates

Existing: OG-1 any real run on the owner's Mac or real Docker; OG-2 host mutation outside a temporary HOME; OG-3 image or
documentation builds and releases (a real image build is OG-3 evidence, spec FR-034); OG-4 `contracts/cli/*`, ADR 0013/0014;
OG-5 doctor catalog, `setup`, `container-runtime.md`; OG-6 Feature 006 text; OG-7 a new catalog entry; OG-8 Feature 008 text.
Proposed here, owner to confirm (OQ-8): OG-10 Feature 009 text, schema and contract changes (the spec calls them "owner-gated
like AP-1 to AP-9"); OG-11 any change to the required CI checks or the aggregate `check` job. An approval covers the one
operation named.

## Risks

| Risk | Effect | Mitigation |
| --- | --- | --- |
| Feature 008 is unbuilt | all library work is blocked; A1 regression rows need the unamended `Admit` first | order by dependency; T013 captures the container rows before A1 changes `Admit` (spec Assumptions) |
| Unknown macOS Python results | enabling the macOS job could turn the aggregate red on first merge | measure on a draft first (T007), classify (FR-028), then enable (T008); OQ-4 |
| Existing no-skip rules | `check_ci` rejects skips and the macOS jobs fail on any `SKIP`; FR-028 allows a recorded platform skip | reconcile in T005 and T008 with an exemption for a complete record only; OQ-3 |
| 009 gaps | endpoint selection, Colima and Docker catalog entries have spec text but no tasks; catalog count is 16 or 18 | tests assert the exact id set, not a literal count; OQ-6 |
| VM image build has no port | the driver port (FR-021) has no build operation, yet setup and `doctor --fix` build VM images | separate image-builder port proposed; OQ-2 |
| First backend maintenance and license unverified | the support matrix and driver choice may change | spec "Verification before the plan"; the port is the mitigation; T039 starts only after the check |
| Cross-platform skew in tests | fake-driver and file-lock tests may differ on macOS | T008 runs every Go package on both platforms; no test assumes Linux paths |
| Concurrency races | a flaky lock test hides a real race | tests drive the lock with a deterministic barrier and a fake clock and process-liveness port |
| Unattended approval | an unattended run could approve a modify-class step | every such step has a test that `--yes` alone records no call (SC-029, SC-032, SC-033) |
| Vendor name leaks | a name appears in gated text | T010 runs before driver code; this plan names none |

## Measurement needs (what the spec says is not measured)

| Item | Where the spec says so | How and when |
| --- | --- | --- |
| Python and Go results of this repository on macOS | Context 1: "no macOS evidence" | T007, first CI run on a draft branch; classify each failure (FR-028) |
| Runner architecture and run time of the macOS jobs | FR-027 pins the images in the plan | T007 job log (`uname -m`, durations); job timeouts set from it |
| Base image unchanged by a run (real image) | FR-006, SC-007 | owner-run: host digest of the image files before and after; offline suite proves nothing about a real image |
| Real-VM conformance, shell models, real readiness, real clone and export | FR-026, OD-2 | owner-run on the Mac under OG-1, recorded in `docs/quality/011-vm-provider/` |
| Real image builds and downloads | FR-034, FR-037, decision 19(d) | owner-run under OG-3; offline tests use fake runners (SC-029, SC-032) |
| Optional size and storage fields of the backend `probe` and `list` | FR-032 "size unknown" | T039 against recorded output; owner-run confirms |
| Container configuration file layout, runtime-home overrides, non-default layouts, file before first start | FR-041, 009 FR-028 (unmeasured list) | Feature 009 OG-1; T028 uses fakes and says "unknown" when absent |
| Docker context file layout | 009 FR-031 | Feature 009 OG-1; not read by 011 |
| First backend license text and maintenance status | spec "Verification before the plan" | owner check before T039; informs the matrix, not the design |
| Exact tap name of the VM tool source | spec informative notes: "verified at implementation" | T030 catalog entry, owner-gated OG-7 |
| Deadlines in the table above | not stated | owner-run observations; defaults change only by owner decision |
| Hosted-runner nested virtualization | Assumptions: not assumed | not measured; no CI job starts a VM |

## Evidence

Each task in [tasks.md](tasks.md) names its failing tests and its trace; the coverage table there maps every FR (FR-001 to
FR-041), SC (SC-001 to SC-036) and decision to tasks or to a deferral with its reason. Validation per PR:

```sh
python3 scripts/select_feature.py 011-vm-provider
python3 -I -B scripts/check_repo.py
git diff --check
(cd apps/whr && go vet ./... && go test ./...)          # code PRs only; no VM, network or Docker
git diff --stat $(git merge-base origin/main HEAD) -- contracts/cli docs/adr docs/container-runtime.md specs/006-whr-cli specs/008-workspace-sandbox specs/009-host-setup-wizard   # empty except the owner-gated tasks
```

## Deferred with reason

| ID | Item | Reason |
| --- | --- | --- |
| D-1 | FR-016 enforced `pty` path and the terminal-safety rules (second half of SC-014); the `whr sandbox shell` command | no provider reports `pty` as `enforced` in this increment; the command is the draft Feature 012; only the refusal is built here |
| D-2 | Real-VM conformance, base image digest, real image builds, real macOS runs (FR-006, FR-026, FR-034, FR-037 real parts) | owner-run evidence under OG-1 and OG-3, never required CI |
| D-3 | `whr doctor --deep`, Linux-host driver, Windows guest, remote hosts, snapshots, egress, credential mediation | "Not in this increment" and "Future work" in the spec |
| D-4 | CI for BundleDock and Anchorage and classification of their failing tests | separate repositories, owner follow-up |
| D-5 | Verdict rule for skipped checks (decision 22) | unchanged; T026 only tests that nothing changes |

## Open questions for the owner

Unsettled points. None is decided by this plan; where a proposal is given, tasks are written so a different answer changes
one task.

1. **OQ-1 Configuration source and record store.** Where is the chosen driver (FR-023, "by configuration or provider id"),
   the base image name, the guest account reference (FR-007) and the optional limit configured, and where does the local
   record store live? Proposal: the Feature 009 config and state directories. The draft Feature 012 (PR 297) asks the same
   record-store question; one answer should serve both.
2. **OQ-2 Who builds a VM image?** FR-034 and FR-037 build a VM image from a recipe, but the driver port (FR-021) has no
   build operation and the provider never builds (FR-007). Proposal: a separate image-builder port per backend, outside the
   driver port, with the recipe format in the driver documentation.
3. **OQ-3 Skip record.** Format and location of the FR-028 record and of the failure classification, and its relation to the
   existing rules that fail any skip (`scripts/check_ci.py` acceptance, the `--- SKIP` greps in `check.yml`). Proposal: a
   structured comment per skip; a complete record is the only exemption.
4. **OQ-4 Rollout and scope of the matrix.** FR-027 makes the aggregate require both platforms, but macOS Python results are
   unmeasured. Proposal: measure on a draft first (T007). Also confirm the Elixir and Node suites stay Linux-only (FR-027
   names Python and Go) and that no Intel macOS runner is wanted.
5. **OQ-5 Order with Feature 008.** The spec orders A1 after 008. Because no 008 code exists, should A1 instead be folded
   into 008 T001 and T002? Proposal: follow the spec (T013 captures the regression rows first).
6. **OQ-6 Feature 009 gaps.** Endpoint selection (D-12 to D-18) and the two container-runtime catalog entries have no 009 task.
   Who writes them, and in which order relative to T029 and T030 (the catalog has 16 or 18 entries depending on it)?
7. **OQ-7 Numeric values.** Confirm or replace the deadlines in the plan-time table, and supply the `image_build` deadline.
8. **OQ-8 New gates.** Confirm OG-10 and OG-11 above or map those changes to existing gates.
9. **OQ-9 Real driver in this feature.** The spec's first increment is the fake driver (Context 5). Confirm Stage 5 belongs
   to this feature's task list, starting only after the license and maintenance check.
10. **OQ-10 SC-024 scope.** "No current document declares Windows host support" can be automated only for workflows and
    scripts; documents are a review item. Confirm.

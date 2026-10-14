# Installable pilot execution record

Owner request: 2026-10-14, continue on `feat/installable-pilot` in the same worktree,
include Python-to-Go migration, independently review/fix each WI, PR after all WIs.
Latest owner instruction: while the owner is out of office, Sol subagents perform
intermediate planning/WI reviews and fixes; the owner reviews the final combined PR.
This supersedes the earlier intermediate human spec/plan-review workflow for this
authorized pilot execution. No historical human artifact approval is asserted, and
no pending intermediate human approval is treated as an execution blocker.
Status: independent Sol planning review cleared
`602ae81c63be52ccb2ee18aa9802a50d3e35260d`, and the internal-pilot/release ordering
correction cleared `0c2c135d8d82d8ed708095f7f1596760df38696b`. T001 scaffold is
accepted after independent Sol CLEAR of `d3d84c8361a1389292fad96d02a828ac9fabc2e3`.
T002 is implemented and under validation; its independent review remains pending.
No product runtime acceptance is claimed.
Baseline: `f02250fc75b1e72a1d949edaee128e31fffbaa53` (current design baseline).
The migration oracle remains `fd0019b7711438a2dbf7255ce3a43572d5a3a6fd`;
changing the design baseline does not change frozen Python behavior.
Authority: [Feature 001](../../../specs/001-workspace-lifecycle/spec.md),
[Feature 006 plan](../../../specs/006-whr-cli/plan.md),
[tasks](../../../specs/006-whr-cli/tasks.md),
[migration](../../../specs/006-whr-cli/migration.md).

## Progress

- Created and retained the pilot worktree; native permission approved branch switch.
- Read-only exact-baseline spec review identified a lifecycle/session retry ambiguity.
  Draft correction scopes durable retries to workspace lifecycle; uncertain agent
  input delivery is not automatically resent. Re-review pending.
- Official dependency research completed; proposal records concrete version/layout
  choices and identifies runtime-dependent transport/SSH decisions before those WIs.
- Pilot plan, migration inventory, data model, owner API design, tasks and validation
  guide drafted. No product code, task completion or live result claimed.
- Documentation checker and whitespace check passed before task-file generation;
  those historical checks do not establish acceptance of later revisions.
- Corrected the planning review finding: T011 requires baseline FR-022 human-text
  escaping, T014 requires the full baseline FR-022a stream filter and targeted
  hostile/split/unterminated tests, and T018 retains exhaustive corpus/fuzz and
  integration acceptance. `python3.14 scripts/check_repo.py` and
  `git diff --check` pass for this documentation correction; product runtime is
  not evaluated. Independent Sol re-review remains pending; final human review
  is reserved for the combined PR.

## Execution limitations

Native write approval was initially denied because the chat was scoped to SheetMill;
after explicit return/authorization, WorkHarbor writes were approved. No indirect
workaround was used. The configured Docker read-only probe did not complete and
was interrupted; this establishes no available host. No runtime was activated.
Signed release and live test/installation inputs are separate owner-controlled gates.

## T001 — pinned application scaffolds (candidate; review pending)

Input: planning clearance at `602ae81c63be52ccb2ee18aa9802a50d3e35260d`.
The measurements below describe original candidate
`3dd0b7825ae78c13d9e4dba3f9da683ff486f0f9`; the reviewed usage-exit correction
is recorded separately below.
Only T001 is implemented by this candidate; T002 and all Feature 001/006 task
completion markers remain pending. Independent review must evaluate the exact
commit containing this record before successor work.

- `.tool-versions`: Erlang/OTP 29.1.1 (read from the installed `OTP_VERSION`),
  Elixir 1.20.4/OTP 29, Go 1.27.2, Node 26.11.1 and Python 3.14.8.
- Official `phx_new` 1.8.15 generated `apps/control_plane` with Ecto/SQLite,
  binary IDs, HTML/LiveView/assets and the requested exclusions. Phoenix and
  Elixir constraints are exact; `mix.lock` freezes all actual direct/transitive
  dependencies, including Git asset revisions. Unused DNS clustering was removed
  for the initial single-instance SQLite design. Generated editor JSON was made
  strict JSON, and whitespace was normalized to satisfy inherited checks.
- Three Go 1.27.2 modules and root `go.work` establish the exported `apiclient`
  boundary and separate CLI/host-agent commands. The shared package deliberately
  has no transport yet. No external Go dependencies exist, so no `go.sum` or
  `go.work.sum` is needed. Help/version are explicitly scaffold output; unknown
  or future product commands fail with exit 1 and no success output. Refusal
  tests were authored first and failed on the absent `run` implementation.
- Phoenix format, warnings-as-errors application compile and `mix test` pass
  (5 tests). `mix deps.get --check-locked`, `mix assets.setup` and
  `mix assets.build` pass with Tailwind 4.3.3 and esbuild 0.25.4. Every Go module
  passes empty `gofmt -l .`, `go vet ./...`, `go build` and `go test ./...`;
  `apiclient` reports no test files because it has no implemented behavior.
  Command builds use `go build -o /private/tmp/workharbor-pilot-build/<module>/ ./...`;
  the library uses `go build ./...`. Both built entrypoints were exercised for
  `--help`, `--version` (exit 0) and `setup` (exit 1).
- The same application gates passed from a clean source snapshot under
  `/private/tmp/workharbor-t001-clean-eynnx4d7`, without copied dependencies,
  builds or databases (before final strict-JSON/whitespace-only normalization).
  Locked dependency fetching and asset-tool installation succeeded there.
  `python3.14 scripts/check_repo.py` and staged/working-tree whitespace checks
  pass for this candidate; inherited full `make check` is not claimed rerun here.
  T002 adds explicit CI/application gates and preserves inherited acceptance.
- Verification used darwin/arm64 only. `MIX_ARCHIVES` used the isolated approved
  generator directory; subsequent Mix state used `/private/tmp/workharbor-pilot-mix-home`.
  Go caches/build outputs used `/private/tmp`. No global tool configuration changed.
  Initial default-cache/build-path permission errors were corrected with explicit
  temporary cache/output locations; no denied native-write path was bypassed.
- Fresh dependency compilation emits a `phoenix_ecto` upstream xref deprecation;
  application warnings-as-errors compile still exits 0. No warning was suppressed.
  Initial SQLite creation logged a recovered database-lock message while all
  tests passed; subsequent working-tree and clean-source tests passed without it.
  Revisit dependency compatibility and SQLite startup if T002 fresh CI reproduces
  these messages; passing scaffold checks do not establish production durability.

No API, lifecycle/provider behavior, Python migration or harness change is included.
No web server, Docker host or live operation was activated. No build outputs or
SQLite artifacts are tracked. Real-host/browser/install/release acceptance remains
open, as does the final owner review of the combined PR.

## Internal-pilot acceptance ordering correction (review pending)

Independent Sol planning review found that T024's former all-SC-001–012 gate
required a human-signed release on `main` before the PR introducing the product
implementation. The owner requested an internal installable pilot and final human
PR review, with no release publication; that release prerequisite cannot precede
this handoff.

- T023 retains local archive/install/uninstall, checksums, SBOM, recorded build
  inputs/provenance and draft-only release machinery/policy tests.
- T024 requires SC-001–005 and SC-007–012 and all real-host, browser and
  five-workspace/eight-attachment load acceptance before the combined PR.
- The owner-authorized allowlisted human-signed release exercise and SC-006 remain
  a separate follow-up outside internal-pilot acceptance. Full Feature 006 remains
  open until SC-006 passes; release publication remains separately authorized.
- Plan, tasks and quickstart now agree on this boundary. No task is marked done.
  The T001 author record above and product source at
  `3dd0b7825ae78c13d9e4dba3f9da683ff486f0f9` are unchanged by this correction; its
  independent scaffold review remains separate from planning-delta review.
- `python3.14 scripts/check_repo.py` and `git diff --check` pass for this
  documentation correction. Independent Sol planning re-review remains pending.
  No runtime or signed-release result is claimed; full `make check` was not rerun
  by this documentation author.

## T001 review correction — CLI usage exit (re-review pending)

Independent Sol review of `3dd0b7825ae78c13d9e4dba3f9da683ff486f0f9`
returned NOT CLEAR with one P2 finding: the `whr` scaffold returned exit 1 for
unsupported arguments instead of ADR 0013 usage exit 64. The refusal test now
requires exactly 64, empty stdout and the `whr:` stderr prefix, including the
unsupported `--version setup` combination. The strengthened test first failed
with actual exit 1; the implementation then changed only `whr`'s refusal code.
Host-agent exit behavior remains unchanged because ADR 0013 governs `whr`.

Affected `whr` checks pass: empty `gofmt -l .`, `go vet ./...`,
`go build -o /private/tmp/workharbor-pilot-build/whr/ ./...` and `go test ./...`.
Built CLI refusal probes for `setup`, `ws create` and `--version setup` return
64 with empty stdout and a `whr:` stderr line; help/version still return 0.
`python3.14 scripts/check_repo.py` and staged/working-tree whitespace checks pass.
No full-suite rerun, runtime activation, task-completion marker or publication is
claimed. The exact corrective commit awaits the same independent Sol reviewer.

## T001 independent review clearance

Independent Sol re-review cleared exact corrective head
`d3d84c8361a1389292fad96d02a828ac9fabc2e3`. Feature 006 T001 is now checked;
T002 remains pending until its separate review. Original Feature 001 checkboxes
remain unchanged because their protected baseline fixture requires later legitimate
reconciliation; T001 evidence maps to 001 T001/T011 without a false completion claim.

## T002 — explicit application acceptance (candidate; review pending)

Prerequisite: independent Sol CLEAR of T001 at
`d3d84c8361a1389292fad96d02a828ac9fabc2e3`. Scope is application acceptance only,
including fixed typed gates, meaningful required test identity inventory, clean
dependency bootstrap, Makefile/CI equivalence and focused negative fixtures.
The helper and test-only ExUnit formatter are listed in the T002 path scope; they
keep executable argv out of manifest data and report actual ExUnit events without
adding product behavior. No Python product behavior is ported in this WI.

Schema 2 preserves all 492 inherited required Python IDs and the empty retained
failure list, and adds thirteen required Python controls, both existing Go command
refusal tests and all five existing Phoenix tests. The API client remains doc-only
with no invented required test; every actual Go test event must pass. Required ID
loss, failures, errors, skipped/excluded tests, duplicates, incomplete streams,
missing tools, version drift and failed setup/format/build/assets gates block.
Raw/qualified deadlines remain 600/1200 seconds; utility commands remain 300.

The application gate uses the exact `.tool-versions` pins and temporary isolated
HOME/Mix/Hex/Go state, with Go binaries/events outside the checkout. Phoenix's
dependency checkouts/build intermediates/assets/test SQLite files are generated
fresh in the disposable checkout to preserve its generated relative asset paths.
Hex 2.5.2 uses Mix's signed installer. Rebar 3.27.3 uses the official GitHub release
URL and SHA-512, with downloaded bytes also checked against GitHub's published
SHA-256 during authoring. CI pins checkout/setup-go/setup-node/setup-python and
setup-beam commits resolved through official GitHub tag APIs on 2026-10-14;
setup-beam's annotated v1 tag was resolved to its commit. CI contents permission
remains read-only, credentials are not persisted, and no runtime/release starts.

Preimplementation evidence: nine new application-control tests errored because
`scripts/application_checks.py` did not yet exist. After implementation, all nine
passed; the additional full-runner integration control also passed. Existing
`test_check_ci` ran 48 tests in 29.401s and passed with native process observation,
including the descendant-cleanup regression. Later controls and source checks are
recorded below after final candidate validation.

Intermediate application failures are retained honestly:

- Initial real gate failed because `go build -o <directory> ./...` cannot build
  the doc-only API client. The fixed library gate uses `go build ./...`; command
  modules keep external output directories.
- A source-only fresh snapshot downloaded pinned dependencies, then failed
  compiling telemetry because Mix CDN's Rebar 3.22.0 binary cannot load on OTP 29.
  Rebar 3.27.3 corrected that compatibility failure without weakening any gate.
- The next application run reached Phoenix format and rejected two missing blank
  lines in the new test-only formatter. The formatter was corrected.

Historical full baseline at `c597fa6` (before this WI): raw 411 tests passed in
426.531s; qualified 818 tests ran in 819.587s and was RED with two failure subcases
in `test_check_ci.AcceptanceTests.test_nested_check_processes_stop_before_disposable_checkout_cleanup`
because sandbox process observation denied `ps`. A direct isolated-file attempt
subsequently failed import before executing a test. The corrected trusted loader
with `scripts` on its import path passed that unchanged test's two subcases with
native permission in 2.301s. No baseline failure was reclassified, test/process
check weakened or unrelated production bug fixed. The final T002 full check uses
a pristine committed private clone and native process-observation permission.

T002 remains unchecked and awaits independent Sol review. No publication, runtime,
Docker/browser/install or release acceptance is claimed.

Before candidate commit, independent Sol review identified a P2 in Go event
classification: prefix matching could treat a nested package's test as owned by
its parent, rejecting a valid concurrent parent `[no test files]` skip. A new
regression first failed in both event orderings (child running and child already
passed). The classifier now tracks exact `(package, test)` ownership for running
and passed cases. This changes no skip policy and accepts no failed test.

Actual application gates now pass: both Go refusal tests, the API-client library
build with no test files, Phoenix format/warnings-as-errors compile, asset
installation/build and all five ExUnit cases. The source snapshot originally had
no copied dependencies or builds; intermediate setup failures above were corrected
in that snapshot. The canonical committed fresh-clone check remains the authoritative
clean-build/full-suite evidence and is pending at this candidate commit.

Final focused verification before the nested-package correction: 59 application
and CI-runner controls passed in 32.152s. `python3.14 scripts/check_repo.py` and
`git diff --check` passed. Linux CI, Docker/browser/install/release acceptance
remain unexecuted; CI uses the same native gate but is not claimed observed green.

After the nested-package correction, all ten application-control tests pass in
0.076s, including both previously failing ownership orderings. Repository and
whitespace checks pass. The existing CI job identity `bootstrap` is retained for
required-check continuity; the application gate is added to that same job.

## T002 canonical-run environment correction

The first canonical full run on `3900757138f84cf04838d7d3433f94d862ed2a83`
(tree `7538d7e7e99d2846903eeffb380bb5db2b33ffa3`) finished raw RED: 411 tests
in 448.736s, one failure and two errors, raw Make exit 2. All three were unchanged
Symphony tests whose literal `python3` subprocess selected macOS Python 3.9.6
instead of the primary selected Python 3.14.8; `prepare_symphony.py` failed import
at its `Path | None` annotation. No failing test was waived or reclassified.
The remaining qualified phase was interrupted after diagnosis, through SIGINT to
only the owned Make process group after native ps/lsof verified its exact private
clone cwd. The runner drained its active check group through normal cleanup;
the driver recorded signal exit -2 after 631.030s. No full green was claimed.

A red-first regression with an incompatible inherited `python3` sentinel failed
both normal/exception subcases. The existing isolated Python-cache lifecycle now
creates a private temporary `python3` symlink to the selected `sys.executable` and
prepends its directory to PATH, restoring PATH on success or exception. It adds no
flags, so ordinary CLI script-directory imports remain supported. This corrects
local/CI interpreter selection without global alias writes or legacy test edits.
Independent review also measured all four unchanged Symphony cases passing with
an equivalent temporary alias. The revised candidate requires fresh exact-head
review and a coherent canonical full run; the first raw RED remains evidence.

Before this behavior correction, the local T002 commit message was amended to add
truthful Codex co-author attribution under CrewBook commit hygiene. The initial
`3900757` and message-only successor `4ad64b97f6965357941844204aab6ecaff7b884b`
shared tree `7538d7e7e99d2846903eeffb380bb5db2b33ffa3`. A pristine clone of
`4ad64b9` passed source_context/source_identity/manifest/whitespace validation.
The interpreter correction changes the tree and receives separate fresh checks.

The correction's focused application/CI-runner/Symphony suite passed 65 tests
using the new isolated alias lifecycle, including all four unchanged Symphony
cases. The dedicated alias/cache/import controls passed three tests in 0.531s.
The first run's private clone lists only its original checkout after interruption,
confirming the disposable execution worktree was removed through normal cleanup.
Repository and whitespace checks pass. Canonical validation is pending on the
revised committed candidate; no prior red or interrupted run is called green.

## T002 real-checker fixture dependency correction

The second coherent full run at
`345972d46d79700604ebbefe63062c6914411fa1` (tree
`989a79890477121f5169741bab9b68ff37bd37b1`) completed with overall Make exit 2
after 1256.069s. Raw acceptance was GREEN: 411 tests in 431.761s, exit 0.
Qualified acceptance was RED: 831 tests in 787.641s, 19 failure records (including
subcases) and one error. Every failure/error belonged to existing readiness/source
fixtures that copied the real checker and its previous four source dependencies
but omitted the newly required `application_checks.py`. Strict source_identity
correctly rejected the incomplete fixtures before their expected positive paths.
No failure was qualified or waived. Actual fresh application gates subsequently
passed: both Go refusal tests, API-client doc-only build/no test files, Phoenix
format/warnings-as-errors compile, assets and all five ExUnit cases.

A focused pre-fix reproduction ran two affected cases and produced three failure
records in 11.059s (`source-readiness-blocked`). The author then audited all
`check_ci.py` references across scripts/tests, and independent Sol review confirmed
exactly two stale real-checker copy lists: `test_task_readiness.py` and
`test_task_source.py`. Their lists now include the required helper, without changing
any assertions, guard policy or source_identity behavior. `test_check_ci.py` already
carried the helper; publication tests use an independent fake checker port and need
no helper. The immutable historical BLOBS inventory remains unchanged. T002's path
scope now explicitly includes both fixture builders.

Complete direct CI/application controls after the fixture repair pass 61 tests in
34.649s. Complete affected readiness/source suites and the final revised canonical
run are required before clearance; the prior complete RED remains evidence.

Both complete affected suites now pass: 52 readiness/source cases in 345.657s,
including every previously failing positive path and all unchanged negative guards.
The 61 direct CI/application controls also passed after repair. Repository and
whitespace checks pass. The fixture-only correction is folded into the T002
candidate for fresh exact-head independent review and coherent full acceptance.

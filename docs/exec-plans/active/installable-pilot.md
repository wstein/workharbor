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
T002 is independently accepted at `bc18034164597aa841bea561cf6a882e9c0715e2`.
T003 is independently accepted at `191c62baf227f64122770244d62d289900bb4d8d`.
T004 is independently accepted at `6abb7419c77b3712a6ac276bc798b102503d3144`.
T005 foundation independently accepted at1189c81ba81b79a47a0485a9d5148fb8286c8eb0;
T006/T007/S001 are accepted prerequisites. T008 implementation and bounded
macOS/Linux transport are independently accepted at
64541c1fa8b1f0d9f353272a0ea8ee6888ea5f5c; documentation handoff review is separate.
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


## T002 final independent CLEAR and coherent acceptance

Independent Sol review cleared exact candidate
`bc18034164597aa841bea561cf6a882e9c0715e2`. Its final canonical
`make check PYTHON=/opt/homebrew/bin/python3.14` exited 0 in 1304.597s:
raw 411 tests GREEN in 413.799s; qualified 831 tests GREEN in 856.364s;
all three fresh Go format/vet/build/test gates passed (whr 1, host agent 1,
API client doc-only 0), followed by Phoenix format, warnings-as-errors compile,
assets and all five ExUnit cases. The same independent reviewer verified cleanup:
private execution worktree removed and owned check process group absent.
Logs: `/private/tmp/workharbor-t002-canonical-bc18034.log` and adjacent `.json`.
These local logs are execution evidence, not published artifacts.

The complete earlier RED/interrupted history above is retained: `3900757` raw
one failure/two errors from an old child interpreter, qualified interrupted;
`345972d` raw GREEN but qualified 19 failure records/one error from two stale
real-checker fixture copy lists, with actual application gates GREEN. Both causes
were fixed without waivers and the final coherent run passed. Feature 006 T002 is
now checked. Original Feature 001 task markers remain untouched. Linux CI and
real-host/browser/install/release acceptance remain unexecuted.

## T003 selected oracle contracts — author candidate, review pending

Prerequisite: exact T002 CLEAR above. Scope is only selected behavior inventory,
full pinned source snapshots/provenance, strict bounded self-contained vectors,
`scripts/generate_go_vectors.py`, meaningful negatives in `tests/test_go_vectors.py`,
their required IDs in `docs/development/ci-acceptance.json`, contract/harness links
and this evidence. The owner-authorized intermediate Sol review route permits the
reviewed profile choices; final owner review is reserved for the combined PR.
No Go product code, caller, dependency or runtime is changed.

Fifteen new controls were written before behavior: all 15 initially errored on the
absent generator (0.003s). The first implemented run passed 14 but exposed one test
setup inconsistency: changing only source_expected while labeling parity makes the
fixture invalid before candidate generation. The tamper control now changes both
expected digests equally, retaining a valid parity classification while proving
verification fails and only the explicit external candidate receives repaired
bytes. No validator was relaxed. All 15 then passed in 0.924s; initial 145 cases
reproduced byte-for-byte. Additional object/mixed-depth, decimal-chunk, Unicode
byte-size and option-domain cases are included in the final focused checks below.

Source snapshots exactly match commit
`fd0019b7711438a2dbf7255ce3a43572d5a3a6fd` and their original Git blob/SHA-256
identities. Only the JSON helper closure plus json/hashlib and the path helper
closure with no imports execute. Release provenance records CPython 3.14.8 and
explicit 4300-digit/1000-recursion settings. Actual macOS executable SHA-256
`1b5cb11d163e5ba34e2a05fc533c0ed89574d4425f9749a9d89405f651980e0d`
is per-run evidence, not a portable fixture precondition. No upstream archive
checksum or Linux reproduction is claimed. No source-linked resource is fetched.

Independent Sol anchors supplied before freezing cover -0, Unicode scalar key
ordering, literal HTML/U+2028/U+2029, boolean/integer separation and 4300 decimal
ones. Product depth 128, Unicode scalar paths and exact-boolean dot are explicit
restrictions with source-success/product-rejection pairs. Native oversized integers
reach the real canonical/digest oracle through bounded arithmetic/chunk construction.
The decimal limit covers parsing, direct canonicalization and digest equally.

T003 remains unchecked until independent review of the committed exact candidate.
T004 Go implementation/caller parity and all later runtime acceptance remain open.
The complete canonical T002 result is prior-revision evidence, not a full-suite
claim for this new candidate. No new runner behavior is introduced; focused
contract/manifest/source/repository checks are recorded separately below.


Final author focused checks for the T003 candidate:

- `python3.14 -I -B -m unittest discover -s tests -p test_go_vectors.py -v`:
  15 controls GREEN in 0.967s after the complete 159-case inventory.
- `python3.14 -I -B scripts/generate_go_vectors.py`: all 159 cases reproduced
  byte-for-byte. The self-contained fixture is 163,819 bytes: 78 parse, 18 direct
  canonical, 17 direct digest and 46 path cases. Fourteen explicit restriction
  pairs retain source success/product rejection; 145 cases retain parity.
- Trusted checker `load_manifest`/`collect`/`validate_collection`: 520 required
  IDs present among 846 discovered tests. All 505 prior required IDs are retained
  in their original order; retained-failure and application inventories are
  unchanged. This is collection validation, not an execution of those 846 tests.
- Both source snapshots compared byte-for-byte with `git show` at the pinned
  original commit. Both added Python files compile through builtin `compile`
  without writing bytecode. Original Feature 001 tasks diff is empty.
- `python3.14 -I -B scripts/check_repo.py` and `git diff --check` pass.
  Staged checks and post-commit source identity are checked during handoff.

Native write permission was requested for the authorized WorkHarbor paths; no
sandbox workaround or global configuration change was used. Standalone candidate
mode and default read-only mode are exercised by the focused controls. No runtime
Python/AI orchestration dependency was added to product code. Full new-head
canonical acceptance is not claimed; the final combined pilot candidate still
requires its own complete canonical and runtime acceptance.


## T003 independent acceptance and T004 start

Independent Sol review cleared exact T003 candidate
`191c62baf227f64122770244d62d289900bb4d8d` (parent
`bc18034164597aa841bea561cf6a882e9c0715e2`) before T004 writes were authorized.
The reviewer independently reimplemented construction/evaluation and checked all
159 results against the pinned original AST closures: 145 parity cases, five depth
restrictions, two scalar-path restrictions and seven exact-dot restrictions. The
offline verifier reproduced the frozen bytes and all 15 controls passed. The 505
inherited required IDs/order/policy remained intact, with 15 added IDs; pristine
clone source/manifest/collection/repository/whitespace checks passed. These are
focused checks, not a full new-head canonical or Linux reproduction claim.
Feature 006 T003 is now checked; T004 and original Feature 001 markers stay open.

## T004 Go pure primitives — author candidate, review pending

Scope: only `apps/api_client/wirejson/`,
`apps/host_agent/internal/config/`, six meaningful Go required IDs in the existing
acceptance manifest, Feature 006 migration/task status and this execution record.
No callers, dependencies, provider operations, control-plane behavior or frozen
migration artifacts change. Public native types and rejection semantics are
specified in the migration inventory and package/function documentation.

RED before implementation: `go test ./wirejson` in API client and
`go test ./internal/config` in host agent both exited 1 because the test-only
packages had no production Go files. This was compile RED for absent implementations,
not a claim that production behavior had been executed. After implementation,
`go test -count=1 ./wirejson` and `go test -count=1 ./internal/config` first passed
in 0.261s and 0.269s. Test files were written before any product implementation.
Caches were in private `/private/tmp/workharbor-t004-*` directories.

Hash-pinned static Go tests execute all 113 JSON and 46 path product expectations;
Go never invokes Python. Independent Go negatives/goldens cover malformed Unicode
in keys/values, actual U+FFFD, valid surrogate pairs, duplicate decoded keys, EOF,
positive/negative 4300/4301-digit integers through all three APIs, depth 128/129,
cycles, exact native types and typed nils, literal controls/HTML/U+2028/U+2029,
unbounded canonical output and unchanged lexical spellings. Every rejection is
produced by a production entry point; adapters only materialize native inputs.
Source successes outside the admitted product profile remain separate evidence.


Focused author verification after the first GREEN:

- Go 1.27.2 / darwin-arm64: all three modules passed `gofmt -l .` (empty),
  `go vet ./...`, their library/executable build form and
  `go test -json -count=1 ./...`. The first author script incorrectly applied
  `go build -o <private-dir>/ ./...` to the library-only API module, which
  returned `no main packages to build`; the repository runner already uses
  `go build ./...` for that module. This was an author command error, corrected
  without a product or runner change. Final command outcomes are validated again
  from the pristine committed clone before handoff.
- Bounded fuzz (`-run='^$' -fuzz=<target> -fuzztime=5s -parallel=2`,
  `GOMAXPROCS=2`, private caches): JSON round-trip/digest target passed 197,471
  executions in 6.196s; lexical identity target passed 42,174 in 6.192s.
  No failure corpus or product files were written by fuzzing.
- Separate `python3.14 -I -B scripts/generate_go_vectors.py` reproduced all
  159 cases byte-for-byte; `python3.14 -I -B -m unittest discover -s tests
  -p test_go_vectors.py -v` passed all 15 controls in 1.081s. Runtime was
  CPython 3.14.8/macOS arm64, executable SHA-256
  `1b5cb11d163e5ba34e2a05fc533c0ed89574d4425f9749a9d89405f651980e0d`,
  integer limit 4300, recursion limit 1000. No Linux reproduction is claimed.
- `python3.14 -I -B scripts/check_repo.py` and `git diff --check` pass;
  the Feature 001 task and all accepted migration artifacts/generator/tests diff
  is empty. Only six required Go IDs are appended; Python required IDs/order,
  retained-failure policy and existing application IDs are preserved.

The full canonical suite is not duplicated for unchanged runner behavior in this
WI. Exact-head full canonical plus applicable real-host/browser/install acceptance
remains mandatory on the final combined pilot candidate. T004 stays unchecked
until its separately assigned Sol reviewer clears the committed implementation.


Precommit independent Sol review identified an adapter evidence gap: the initial
JSON fixture runner checked a shared error only after follow-on canonical/digest
calls, so a later rejection could mask incorrect acceptance by the selected
operation. Production depth enforcement was correct, but the test did not prove
each operation's own rejection. The adapter now asserts the selected operation's
status/code first and performs cross-API comparisons only for expected successes.
Independent direct Parse regressions cover array/object/alternating shapes at both
128 and 129 containers. No frozen fixture or production profile was changed.
The original absent-package RED chronology above remains unchanged; no artificial
stub or later mutation is represented as that original RED.


After the precommit test correction, API-client `gofmt -l .`, `go vet ./...`,
`go build ./...` and `go test -json -count=1 ./...` passed (vet 2.518s,
build 0.065s, tests 1.016s). The unchanged repository event validator accepted
actual outcomes for all three modules, including every required ID: whr 1,
host agent 64 and API client 162 passing top-level/subtest/seed outcomes.
Trusted `load_manifest`/`collect`/`validate_collection` also passed: 520 Python
required IDs among 846 discovered; all prior Python IDs/order and failure policy
are identical, and existing application IDs retain their order with six additions.
The author worktree's ignored `.specify/feature.json` is preserved (SHA-256
`05caf1787466a6410ce0cc3d1cdb371eae6187e38babba78a0c08d21edf1af83`);
source-context checks must run in a pristine validation clone instead.


## T004 independent acceptance and T005 foundation start

Independent Sol exact-implementation CLEAR accepted
`6abb7419c77b3712a6ac276bc798b102503d3144`. All 159 frozen Go cases and independent
Unicode/native probes passed. All three Go modules passed format/vet/build/tests.
Pristine source-context/identity, manifest/collection (520 required / 846 discovered),
repository and whitespace checks passed; exact-head resolved-root validation took
7.442 seconds. Separate Python reproduction covered 159 cases and 15 controls;
author fuzz observed 197471 JSON and 42174 path executions. These are macOS results;
no full new-head canonical, Linux or runtime acceptance is claimed.

Two author command errors remain evidence: library build with -o incorrectly
requested executable output, and logical /var versus physical /private validation
clone spelling caused GOWORK-root mismatch. Both were corrected without production
changes. Feature 006 T004 is checked; original Feature 001 markers remain untouched.

T005 implementation GO followed independent Sol design CLEAR of consolidated V3
with explicit native boundary, transport-neutral literal repository identity,
separate intent/runtime counters, start-existing-runtime restriction, immutable
requested/effective configs, guarded terminal results and atomic reservation audit.
The accepted contract is now persisted in Feature 006 owner API/data model/plan
before tests/behavior. T005 remains unchecked pending exact-candidate review.


## T005 — native scoped persistence (author candidate)

The accepted V3 contract was committed first at
`f51d6fe14764d37c3128f3a6e17439db21f76580`. No behavior existed when the initial
18 meaningful tests were written. Actual initial RED used MIX_ENV=test and private
MIX_BUILD_PATH/database override, then Mix.Task.run("test", foundation test files):
exit 2, 0/18 passed; all failed for absent foundation modules. This is absent-module
RED, not execution of a manufactured later stub.

First implementation runs exposed strict-boolean code errors, then a test helper
that accidentally queried the global Sandbox instead of its dynamic committed
Repo. These were corrected; 17/19 then passed, with test-only child-code quoting
and reused trigger-name failures. Correcting those fixtures yielded 19/19 in 1.2s.
A formatter path was initially redundantly listed despite the existing scoped
formatter; the duplicate was removed. These are author mistakes, not passing runs.

Independent Sol preparatory inspection found target config population, improper
native lists, loose integer/float binding equality, and malformed lookup keys.
Real regressions preceded each correction. The expanded pre-fix run was exit 2,
24/29 passing and five failing: those four classes plus a direct-SQL candidate
binding borrowing another operation's runtime. The guards now reject these cases;
29/29 passed in 4.2s. Precommit crash evidence was also corrected to reject nested
acceptance and pause the actual top-level path only after successful final audit
insertion; child cleanup runs on assertion failure. Post-claim restart is covered.

Additional audit scope insert negatives produced real RED (31/32, exit 2) before
adding workspace/host, operation/intent and runtime/workspace/generation FKs.
A test-only default-argument escape typo first caused compilation failure and was
corrected before this actual behavior RED. Complete envelope bytes/hash have an
independent stdlib-derived constant, alongside accepted frozen Unicode/control
anchors. All 32 meaningful Phoenix IDs are appended; existing Python/application
IDs and retained policy are preserved. T005 and original Feature 001 remain unchecked.

The foundation adds no credentials/auth, enrollment, trust/fetch, provider,
dispatch/retry, replacement-start, data-deletion acceptance or repeatable
observation API. No product runtime or hardware power-loss result is claimed.
Pristine committed application/source/manifest/repository results follow after
candidate validation; no full new-head canonical suite is claimed here.


After audit scope constraints were added, the complete Phoenix suite passed 37/37
in 4.3s (32 new foundation tests plus all 5 inherited tests), with actual ExUnit
outcomes accepted by the unchanged event validator. Format and warnings-as-errors
compile passed in the authorized native run. A later non-escalated lock check failed
EPERM at Mix's TCP lock, not production behavior; it is rerun natively below.
The repository link checker initially interpreted the host regex in inline Markdown
as a link. The exact same lexical rule is now written in prose; checker policy
was not changed. Frozen migration and original Feature 001 diffs remain empty.


Exact-candidate Sol review of b179f2959209afc26df0426d27ad35cea2f66c47 was
NOT CLEAR for one P2: false effective_config bypassed truthiness guards and could
commit a malformed terminal observation. Its independently passing 37/37, format
and warnings-as-errors results are superseded for acceptance by the correction.
Two regression tests preceded the fix: actual RED was exit 2, 25/27 foundation
tests passing and both new tests failing. The correction reuses strict config
normalization at the result boundary and explicit nil handling for optional fields;
malformed candidate and target results must leave runtime, operation and audit
unchanged. Both new IDs are appended without modifying prior acceptance IDs.

The correction's first warnings-as-errors compile caught misplaced private-function
clauses. The test invocation then passed 39/39 but emitted that warning; this was
not a clean compile result. After grouping the clauses, warnings-as-errors compile
and format passed. The expanded complete suite passed 39/39 in 4.4s; the unchanged
actual-event validator accepted all 39 required outcomes. The new tests include
malformed optional provider/deletion fields, normalized config defaults and exact
immutable replay as well as scalar/list/map config rejection. Repository and
whitespace checks passed before commit. Pristine exact-head evidence remains a
post-commit check; independent corrected-head review is still required.


## T006 — accepted authentication design

T005 independently Sol CLEAR at `1189c81ba81b79a47a0485a9d5148fb8286c8eb0`
with no open findings. Its pristine acceptance evidence includes 39/39 Phoenix
tests and malformed-config/nil probes; it establishes native persistence, not
authentication or strict raw JSON. Original Feature 001 completion markers and
frozen migration artifacts remain unchanged.

The single [consolidated T006 contract](../../../specs/006-whr-cli/contracts/authentication.md)
was independently design/consolidation CLEAR before behavior at SHA-256
`eeb455009081ec12a48eb3b64834b18ea9273fbd7a3bfd353f81cba8ebc29491`.
Three separate session stores, server-checked 600-second preauth expiry, explicit
owner/dashboard context and no automatic credential revocation replace prior
proposals. T006 implementation is authorized; its checkbox remains open until
exact-candidate tests and independent review. No activation is authorized.

### T006 behavior and finding ledger

Failure-first behavior checkpoints included absent modules/migration (0/18),
revocation actor/target audit (12/20), runtime configuration (10/11), ingress and
cookie/channel boundaries (12/19 then 12/22), encoded route aliases (12/23),
retention/audit (39/41), bootstrap audit (7/10), malformed effective authority
(25/26), parsed connection/browser controls (37/39), and raw issuance channel
(27/29). Initial wrong-directory and test-sigil compilation mistakes were corrected;
they are not behavior evidence. Early shell-tail wrappers did not retain separate
Mix exit codes; their counts are observed outcomes, not saved process exits.

Independent precommit findings were closed with focused regressions: distinct
actor/target revoke audit; real foreign credential preservation; literal debugger
POSTs; positive custom-origin socket handshake and kind/expiry/revocation/CSRF
negatives; fresh Jason compilation and integer boundaries; credential-lifetime
cookie rewrite cap; strict authority/default-port/IPv6/UTF8 admission; isolated
valid-baseline runtime negatives; empty logout schema; bearer-kind/query rejection;
closed per-route denial labels; protected navigation Fetch Metadata; canonical
route selection; root-context navigation; consumed adapter preservation; and
no-script scoped owner controls with CLI-only raw-token issuance.

The complete author Phoenix suite passed 86/86 with subprocess exit 0 in 5.2s.
Fresh exclusive reporter outcomes were validated against the appended 86-ID
manifest by the unchanged actual-event validator. All 39 inherited IDs and other
application/Python inventories are retained. The prior 82-event artifact was stale
because the reporter refuses overwrite; it is superseded by the fresh verified
artifact, not represented as 86 events. A Foundation contention test emitted the
expected Exqlite BEGIN IMMEDIATE database-lock error while passing; it is not a
compile warning. The inherited public-root test ID now asserts plaintext denial.

Author evidence is under `/private/tmp/workharbor-t006-author`; exact committed
pristine evidence and independent code disposition follow separately. No real
browser/network TLS, live listener, deployment, later-WI integration or full new-head
canonical acceptance is claimed here. T006 remains unchecked pending review.

Author test-profile format and warnings-as-errors compilation passed, as did
repository/bootstrap and whitespace checks. Fresh dev-profile compilation exited
0 with the pinned Plug dependency's Mix xref deprecation warning; application
compilation had no warning. This dependency warning is recorded rather than
represented as warning-free output. No listener started during either compile.

Initial pristine candidate `65af90c937a5f2e50a3651a0351611ca80b2bb8d` passed
source/context/identity, manifest/collection (846 collected, 520 required Python
IDs), repository, format, compile and 86/86 actual Phoenix outcomes. Its fresh
default test database exposed a migration module redefinition warning in the
auth test helper. The helper now follows the accepted foundation helper's loaded
module guard; the predecessor run remains truthful evidence, superseded for exact
acceptance by the corrective candidate. Product behavior is unchanged.

## T006 exact acceptance and T007 contract consolidation

T006 is independently Sol CLEAR at ef399f58f2d9343109e248a519231209a707a416,
including author implementation 65af90c and helper fixture correction ef399f5;
accepted contract is 5b28644. Author pristine evidence:
/private/tmp/workharbor-t006-validation.ayhnvdv5/evidence.json. Independent reviewer
evidence: /private/tmp/workharbor-t006-review-hw7l_5me/review-checks.json.
Author source/context/identity/manifest/application evidence passed; independent
compile and full 86/86 tests passed (47 new IDs, all 39 retained). These are
synthetic application results, not full canonical, live TLS/browser/host evidence.
Only Feature 006 T006 is checked here; Feature 001 markers remain unchanged.

Coordinator explicitly authorized T007 after accepted T005 1189c81 and T006 ef399f5.
The complete independently CLEAR R2 plus actual-interface alignment is consolidated
in contracts/host-protocol/README.md before failure-first tests or behavior.
Same Sol reviewer must confirm faithful consolidation. No T007 implementation
acceptance, transport, provider, dispatcher or durable replay ledger is claimed.

## T007 pure host protocol — author candidate, review pending

Contract consolidation was independently Sol CLEAR before tests/behavior and committed
at 5d81ae5. No owner-auth action registry, provider, transport, dispatcher or durable
command ledger is changed. First-create/mTLS/trust expectations remain supplied native
context; dispositions do not authorize execution.

Actual initial RED: Elixir `mix test test/workharbor/host_protocol_test.exs` exited 1
with undefined HostProtocol.Peer; Go private-cache `go test ./internal/protocol` exited 1
with no non-test Go files. A preceding Go default-cache invocation failed permission
setup and is not counted as behavior RED. Shared fixtures and both native suites preceded
implementation. Two fixture-construction mistakes (inner create-host mismatch; escaped
duplicate spelled kkind) were corrected without weakening expectations.

Early independent review found missing final normalized-message byte-budget check and
permissive observation effective-config normalization. New regressions failed first in
both languages: Go two failures/exit1; Elixir17/19/exit2. Fixed by budgeting the final
normalized message before canonical allocation and requiring already-normalized wire
effective config, including schema-required submodules. Legitimate runtime reuse fixtures
now have distinct operation/key/command IDs; changed same-key/action remains a negative.
Focused Go and Elixir19/19 then passed.

The artifact has 44 independently specified raw cases, four additional action
fixtures, separate request/command hash anchors, 20 required Go IDs and 20 new required
Phoenix IDs; every previous required ID is retained. Exact-candidate pristine
source/manifest/application results and independent disposition follow separately.
No full combined canonical, live TLS/browser/host acceptance is claimed.

Additional failure-first scope check classified a foreign workspace as identity_conflict
(Go exit1, selected Elixir exit2) rather than authorization denial. Explicit expected
workspace scope admission now precedes operation identity comparison. Wrong peer plus
nonnull malformed snapshot also proves peer rejection before expectation inspection.
One initial selected Elixir line ran the preceding green test; it is not counted as RED.
Fixture bytes are hash-pinned in both independently written native suites.

Author-cache full Phoenix runs retained two inherited 4300-digit failures (103/105)
from stale test-environment Jason artifacts. Default-environment force compilation
did not repair the separate test build. Native Mix lock permission failures and a
Go whr default-output source write denial are setup failures, not behavior results.
Explicit private Go output succeeds; exact-candidate verification uses a fresh private
Phoenix build with accepted compile-time Jason limit4301, without source changes.

Fresh private `MIX_ENV=test MIX_BUILD_PATH=/private/tmp/workharbor-t007-fresh-build`
format/compile warnings-as-errors/full tests passed: 106/106 including all86 inherited
and20 T007 cases. Locked Jason was freshly compiled with limit4301; no auth source
was changed. Exact committed-candidate source/application evidence remains the next
gate; this author-build result is not pristine-source or independent review evidence.

Final author application gates all exited0; event inventory validation passed: {'host_agent': 132, 'api_client': 162, 'whr': 1, 'control_plane': 106}.
Logs/argv/exits: /private/tmp/workharbor-t007-local-final-_o3775ll/checks.json.
All20 new protocol IDs per language and every inherited ID are present.
An author Go multi-package build used a nonexistent non-directory output and failed;
canonical precreated-directory output corrected it. One gofmt invocation used the
wrong cwd-relative path and was corrected. Neither failed command is counted passing.
No helper policy/runner behavior changed; root reserves full make check for combined
head unless new concern requires it. Exact-candidate pristine gates follow externally.

## T007 exact acceptance and S001 transport follow-up

T007 is independently Sol CLEAR at exact candidate
`e10c6997c24407e0f472a7ea22ba2dd1b42067e3` for the pure protocol scope.
Author pristine evidence: `/private/tmp/workharbor-t007-validation-nbe2oy8a/evidence.json`;
independent evidence: `/var/folders/g3/fzgcpsts26j_bzpn6ymprbwm0000gn/T/workharbor-t007-independent-yof68sbl/summary.json`.
Actual application counts: host_agent132, api_client162, whr1, Phoenix106; all
inherited IDs retained. Source/manifest collection retains 846 Python / 520 required.
Only Feature006 T007 is checked; Feature001 markers are unchanged. Full canonical,
real-host/browser acceptance and dependency security clearance are not claimed.

Locked Bandit1.12.5 emits EEF-CVE-2026-105659 / GHSA-69m6-wcv4-rwcf HIGH.
Primary upstream facts were checked on 2026-10-14: affected through1.12.5,
patched-version TODO, Hex latest1.12.5, fix in Unreleased. Root authorized S001
as a blocking follow-up before T008; independent Sol transport reviewer cleared
[the finite contract](../../../specs/006-whr-cli/contracts/transport-security.md)
before tests/behavior. Same worktree/branch, sole S001 writer; T008 remains held.
No dependency update, proxy, persistent listener, release/deploy or publication.

S001 failure-first suite is now actual RED before mitigation:
`MIX_ENV=test MIX_BUILD_PATH=/private/tmp/workharbor-t007-fresh-build mix test test/workharbor_web/transport_security_test.exs`
exited2, 2/7 passed and five behavior failures. Log:
`/private/tmp/workharbor-s001-linked-red.log`. HTTP1 browser/bearer authentication
and real WSS positives/negatives passed; guards accepted unsafe protocol/override
settings, runtime omitted restrictions, dual ALPN chose h2 and no-ALPN HTTP2 preface
received a SETTINGS frame. Initial disposable PEM encoding / request-process DB
ownership setup errors were corrected first and are not counted as behavior RED.
Actual tests use all three merged base/runtime environment configurations and
PhoenixAdapter child specs. Network request processes share the named repository's
sandbox transaction; generated certificates/keys and listeners are ephemeral.
Seven required transport IDs were appended without removing inherited IDs.

Independent early S001 review found a listener-activation bypass: Phoenix treats
truthy server values as active and missing values may inherit serve_endpoints,
while the guard originally validated only literal true. A new actual selected
regression failed first (exit2,0/1; `/private/tmp/workharbor-s001-activation-red.log`).
The guard now requires exactly one explicit boolean server setting; false remains
inert and true requires full validation. Duplicate activation is also rejected.
The eighth required transport ID is appended; inherited Phoenix relative order is
restored to minimize manifest churn. Endpoint fixture cleanup is registered before
later setup assertions so failed setup restores shared state.

First normal author `make check-apps` exited2: all Go gates passed, Phoenix111/113
with only the same two inherited 4300-digit stale Jason failures seen in T007.
Raw log `/private/tmp/workharbor-s001-apps.log` is retained. No tests are waived.
Root authorized recompilation of the disposable locked Jason test artifact under
the committed4301configuration before rerunning normal application checks; fresh
pristine exact-candidate validation remains separately required.

S001 normal author application gate is now GREEN after explicit locked Jason
`MIX_ENV=test mix deps.compile jason --force` (exit0, log
`/private/tmp/workharbor-s001-jason-recompile.log`). Full normal
`make check-apps PYTHON=/opt/homebrew/opt/python@3.14/bin/python3.14` exited0;
log `/private/tmp/workharbor-s001-apps-repaired.log`: host_agent132, api_client162,
whr1, Phoenix114 including all106 inherited and eight new transport IDs.
All Go format/vet/build/uncached test and Phoenix format/compile/assets/test gates
ran through the unchanged fixed helper. The advisory warning remains visible.
Focused eight transport cases passed with warnings-as-errors. Source bootstrap and
`git diff --check` passed; Python collection846 / required520 and unchanged
Feature001 tasks were checked. Exact committed/pristine verification and independent
Sol candidate review follow; no full canonical Python or live-host/browser claim.


## S001 exact acceptance and T008 contract-first start

S001 independently Sol CLEAR at c88f33959a4c0a767f638afaccc446a8040c55b4.
Author: /private/tmp/workharbor-s001-validation-h_fb25q3/evidence.json;
independent: /private/tmp/workharbor-s001-independent-leo2snzz/summary.json.
Actual full application gates: Phoenix114,whr1,host_agent132,api_client162 green;
846 Python collected/520 required retained, source pristine and prior IDs retained.
Mitigation only: locked Bandit remains affected; no dependency security clearance,
full canonical/live browser/pilot claim. Only Feature006 S001 is checked.

Root authorized T008 after T007/S001 exact acceptance. Its one complete
[accepted V2 contract](../../../specs/006-whr-cli/contracts/host-enrollment.md)
includes the final empty-subject enrollment CSR addendum and actual interface
alignment. Same retained Sol reviewer must confirm faithful consolidation before
failure-first tests/behavior. No superseded descriptor/manual-only issuer/owner
bearer renewal design applies. T008 remains unchecked; Feature001/migration and
ignored selector unchanged. Resource review precedes disposable OrbStack activation.


The retained independent Sol reviewer confirmed faithful consolidation of the
complete T008 contract before tests/behavior. No superseded alternatives remain.
T008 implementation/failure-first tests may now proceed under root GO; no runtime
acceptance is claimed. The S001 transport contract status is refreshed to exact
c88f339 acceptance without changing its mitigation/advisory boundary.


## T008 bounded identity progress and disposable resource plan

Contract e99c170 was faithful-consolidation CLEAR before tests/behavior. Actual
initial RED: Phoenix0/9 exit2 absent Issuer; Go identity/connection exit1 undefined
implementation symbols. A wrong write cwd/reserved shell variable and Accounts
login tuple setup error were corrected first, never counted behavior RED. Logs:
/private/tmp/workharbor-t008-absent-phoenix-red.log and
/private/tmp/workharbor-t008-initial-go-red.log. Native ASN.1 version/subject and
strict-boolean errors were corrected before the initial9/9 focused identity pass.

Independent early review identified six classes: cross-second certificate/response
expiry mismatch; movable revoked tombstone scope; accepted DER suffixes; ancestor
symlinks; permissive Go private-state case/default schema; incomplete issuer/leaf
profiles. Real regressions preceded corrections: native9/13 and13/14, then14/15;
Go case/missing/collision/ancestor and independently signed purpose/critical/subject/
issuer path/duration negatives failed. Native15/15 and both Go packages now pass.
Retained reviewer independently CLOSED all six on captured correction scope;
/private/tmp/workharbor-t008-profile-closure-56i5mkf_ retains its evidence. This is
not T008 code/runtime acceptance. Automatic native permission review timed out once;
its explicitly permitted identical retry succeeded, separately from behavior.
New liveness tests produced real0/4 absent-module RED before behavior; first4/4 now
pass metadata/history/fencing cases. Further transport/clock/crash evidence remains.
Meaningful new IDs append to the unchanged114Phoenix/132host/162API/1CLI inventory.

### Proposed resource plan (review required before activation)

Read-only preflight: host16GiB RAM/12logical CPUs; physical /private/tmp filesystem
reported180000000KiB available. vm_stat free+inactive+speculative approximately
5.1GiB, with existing compressor/swap activity, so do not allocate a6GiB guest.
OrbStack inventory was empty. Repeat these checks immediately before create; defer
if free+inactive+speculative<3GiB, disk available<20GiB or inventory/ownership unclear.
One disposable owned Ubuntu24.04 arm64 guest only, exactly2CPU/2GiB/16GiB thin disk,
--isolated and --isolate-network retained. This is a bounded T008 transport fixture,
not five-workspace/eight-attachment load sizing or capacity evidence. Record immutable
created machine ID and verify identity/isolation before every mutation; never reuse
or stop an unrelated machine. No host Docker/BuildKit/context/cache changes.

Transfer only explicit source/test inputs (exclude .git, host deps/build caches and
all existing credentials). Install exact .tool-versions Linux toolchain and locked
dependencies INSIDE guest. Bootstrap sequentially: build at most two jobs (OTP at
one job if source build is necessary), no listeners while toolchain builds. If2GiB
is insufficient, record failure and obtain reviewed revised sizing; no silent growth.
Docker29.1.3 from the independently exercised guest package route can establish
Linux Docker-host availability; T008 creates no workspace images or provider calls.
Systemd is the Linux HOST service manager, never placed in a workspace image.
Bootstrap may use up to45minutes; abort on capacity/identity/network failure. Tool
calls yield at<=60seconds with progress; no isolation bypass or host mount sharing.

Both genuine Elixir CP and Go client run INSIDE guest using guest loopback only,
127.0.0.1 and explicitly verified localhost test TLS identity, no host port forward.
Use exact source digest and record OTP/Elixir/Go/Linux/systemd/Docker versions.
Private fixture root0700, keys/state/env/code inputs0600, dedicated fixture service
UID. No secret in argv/stdout/journal: protected EnvironmentFile and private input
files carry runtime material. Explicit native issuer initializer runs only in that
fixture. Reviewed execution amendment: CP/test harness and ALL Go/build descendants share
one transient dedicated-UID unit with MemoryMax=512M, CPUQuota=100%,
RuntimeMaxSec=600 and KillMode=control-group. This stricter aggregate replaces the
original separate512MiB CP/128MiB Go proposal; no child escape/unrestricted unit.
Run host_transport then transport_security sequentially; no toolchain-build/listener
overlap. OOM/timeouts are failures requiring review before any limit change.
Stop processes between bounded journeys,
record measurements and failures. No persistent enabled unit or deployed runtime.

macOS first uses OS-assigned disposable loopback Elixir/Go TLS/WSS fixtures with
cleanup, separately from Linux real-host evidence. Preserve HTTP1 ALPN/H2 refusal
under narrow optional-client-cert extension. Linux journey must genuinely exercise
private-key TLS identity, enrollment, hello/heartbeat, socket loss/reconnect, staged
rotation/activation, old/pending/revoked/expired refusal. Lost activation ack plus
client process restart uses durably saved pending material after retired predecessor
expiry; label controlled clock/fixture setup explicitly and preserve real TLS.
No runtime/provider/browser/load acceptance is inferred from this finite journey.

Finally stop only recorded owned transient units/child processes, remove guest private
fixture material, stop/delete owned machine by immutable ID, and verify inventory
no longer contains it. Verify host Docker context/daemon/container inventory unchanged
against entry. Cleanup runs on assertion/setup/timeout failure and records errors;
never report success while owned resources remain. Generic prior fixture evidence
/private/tmp/workharbor-systemd-fixture-iyZ4ILpH supplies smoke/cleanup precedent only.
Review this plan before any guest or disposable listener activation.


## T008 bounded corrections and actual transport progress

All six early identity findings are independently closed. Three later liveness
findings (stale heartbeat revival, caller-snapshot projection race, discarded close
transaction failure) had actual8/11 RED, plus dedicated expiry timer made8/12 RED;
correction27/27 and independent31/31 passed. Evidence:
/private/tmp/workharbor-t008-liveness-closure-l0vh8_uy. Runtime/operation history
remains untouched by the conservative connection-fenced overlay.

Transport corrections had actual19/22 RED for consumed-Conn propagation and socket
worker lifetime, then37/37 GREEN. Issuer signed oversized DER had15/16 RED, then
44 non-network tests GREEN; independent signed12,320-byte issuer refusal proves
code unconsumed/no identity. Go exact activation response uses independently
signed saved pending positive and per-field negatives; expiry watchdog runs during
blocked IO. First ingress Host-header fixture/setup errors and empty line-filter
runs establish no behavior evidence. Actual TLS startup exposed unsupported peer
options at Bandit top level; the reviewed exact nested placement is now the sole
contract. Default OTP RSA test certificates failed strict Go parsing due missing
RSA SPKI NULL; independent public-only P256 fixture diagnosis and Go CA/chain/
positive+negative hostname preflight corrected test material without weakening
production verification.

First genuine Mac Elixir/Go TLS slice4/5 passed; actual reconnect exposed missing
continuous reads during heartbeat wait. A bounded single reader plus cancellation
fixed the real regression (1/1 rerun) and independently passed source/Go review.
Expanded actual matrix10/12 passed; corrected foreign/expired explicit certificate
presentation then2/2 passed. Generic TLS/dial failures never count as server401.
Normal expected application closure is code1008. Actual malformed lower-level
frames exposed recoverable raw bytes in OTP reports; owned total primary redaction
rebuilds only exact agent crash/terminate events. Independent unit3/3 and actual
post-primary wire1/1 prove four fixed events across both OTP report channels.
Full native `mix test --warnings-as-errors` passed170/170 including all12 genuine
Mac journeys (/private/tmp/workharbor-t008-native-full-first.log). Two additional
transaction regressions passed18/18 identity tests: concurrent owner revocation
wins issuance/redemption/renewal/activation, and enrollment audit failure rolls
back code consumption/identity/fingerprint. Evidence:
/private/tmp/workharbor-t008-trust-transaction-races.log. Required inventory now172
Phoenix tests. Logs remain under /private/tmp/workharbor-t008-*; pristine application
gates, exact candidate and separate Linux evidence remain pending.
T008 stays unchecked; no provider, browser, capacity or pilot acceptance is claimed.

### Reviewed Linux fixture amendment and ownership

The bounded outer orbstack Engine info/ps baselines timed out30s then20s; no guest
was created by the author. Root and independent Sol review allow the same isolated
2CPU/2GiB/16GiB fixture using fresh capacity+empty machine inventory and read-only
Docker context/config hashes/metadata while the outer Engine is unavailable. Outer
runtime/BuildKit state remains UNVERIFIED: no unchanged-state claim and no outer
restart/config/context/runtime mutation. Orb management works but shares resources;
management/capacity failure aborts. Repeat bounded outer probes after owned teardown.
Retained sol_systemd_fixture owns ALL guest mutations/bootstrap/ledger/teardown;
source author owns code/Mac tests and sends exact source/input digests separately.
Reviewed bootstrap script SHA256 d93f6a888217761a003cb2465a820da40a482a4fb89801d483ec2c4b6645601d,
/private/tmp/workharbor-t008-linux-bootstrap-1qylii8h/bootstrap.py.
No source transfer or Linux product test is inferred from infrastructure preparation.

Outer Engine subsequently recovered without repair/restart: native ping/version/info/ps
responded, original BuildKit container retained and running, machine inventory empty.
Root authorized the retained helper to bootstrap the exact reviewed fixture; Linux
product evidence remains separate and pending. Earlier unavailable baseline and
UNVERIFIED evidence are retained as historical outcomes, not rewritten as success.

### T008 exact candidate and durability correction

Candidate e42fe27ec353d262dcd2dd3496e9fbc98cc56937 passed fresh isolated fixed gates:
29commands, Phoenix172, Go host198/API162/CLI1 events, all prior required IDs retained,
Python846collection/520required. Author evidence:
/private/tmp/workharbor-t008-validation-psmexjz6/evidence.json; independent:
/private/tmp/workharbor-t008-independent-zxtay0dp/summary.json. All12 genuine Mac
TLS/WSS journeys passed in each full native run. Post-test tracked bytes and
pristine source passed after deleting only generated scratch nested Git dependencies;
initial cleanup-tool failure is retained. The wrapper's initially wrong foundation
filename proof was corrected against actual owner_foundation/owner_authentication
migrations plus001tasks; all unchanged. Fixed POSIX UID subprocess broken-pipe
output during intentional teardown is retained, not authentication evidence.

Independent exact review found the required file-failure/restart proof missing.
Go now tests real temporary files and actual AcceptEnrollment/PrepareRenewal/AcceptPending/Promote
and initial possession across partial-write, file sync/close, rename/link, parent
open/sync/close failures. Unexported native-default syscall wrappers have no external
configuration. Initial missing-seam compile failure is explicitly instrumentation
absence, not executed behavior RED. Existing Go error handling passed all32 initial fault
subcases on first wiring; the final matrix has40 subcases including AcceptEnrollment
plus a separate fresh reload process at each transition boundary. Native test-only process-local
IO instrumentation exposed original ignored file-close error:18/19 behavior RED
(/private/tmp/workharbor-t008-durability-native-close-red.log). Close failures now
propagate fixed persistence_failed, including parent close;19/19 identity tests
pass. Production native module uses only :file, with no test selector. Restart
retains one whole OLD or NEW valid state at its atomic commit boundary, never
claims successful durability after a failed sync, and never replaces uncertain
exclusive issuer/key possession. Required inventory now Phoenix173/Go host41topIDs.

Predecessor e42 genuine isolated Ubuntu Linux passed12 transport journeys (81.2s,
83.31s unit wall,216391680-byte memory peak) and9 transport-security tests (4.5s,
6.185s unit wall,141455360-byte peak); all349 archive file hashes/modes unchanged.
/private/tmp/workharbor-t008-linux-bootstrap-1qylii8h/amended-run/transport/assessment.json
records actual outcomes and limitations. Owned guest01M4Y2J3CZXQ4JR69E1QGEY0QV was
deleted and inventory empty; outer Engine/original BuildKit responding and config
metadata preserved in that observation window, historical hang continuity UNVERIFIED.
Effective UID/group and cgroup ancestor privilege/escape measurements were missing;
no complete resource isolation proof is claimed. Docker absent/N/A for this TLS-only
fixture. Corrected product runtime requires a fresh exact archive/Linux run and
same independent review; predecessor results do not accept the changed candidate.

### T008 accepted corrected runtime and documentation handoff

Independent exact application/source review and separately measured Linux evidence
are CLEAR for64541c1fa8b1f0d9f353272a0ea8ee6888ea5f5c, with no open material code
findings. Author29-command pristine proof is
/private/tmp/workharbor-t008-validation-dho78ci4/evidence.json; independent proof:
/private/tmp/workharbor-t008-independent-645-kb8o8fqu/summary.json. Both passed
173Phoenix tests including all12 actual MacTLS journeys,240host/162API/1CLI Go
events, all required IDs,846Python collection/520required and unchanged frozen
migrations/tasks/vectors. Native full-suite times were94.0s author/94.4s independent.
Full canonical Python remains reserved for final combined head.

Second Linux attempt for645 stopped BEFORE mix with guard125 because PolicyKit
was absent despite a system bus. Zero candidate tests ran; all352 source hashes/
modes matched and owned guest01M4Y478XAVKYYJNJ0J7B8RRKS was deleted. Failure evidence:
/private/tmp/workharbor-t008-linux-rerun-plan/amended-run/transport/assessment.json.
No policy package or weakened/cap-increased workaround was used. A narrowly reviewed
systemd255.4 absent/nonactivatable-PolicyKit proof required typed AccessDenied for
empty properties on the exact owned loaded unit, unchanged required properties,
nonwritable cgroup ancestors and measured dedicated UID/groups/capabilities.

Fresh corrected Linux actual12transport/9security tests then passed81.7s/4.7s
(83.573s/8.33s unit wall). Aggregate peaks269557760B/161996800B;321/23 live/postexit
samples and final OOM counters all zero. Both units measured UID/EUID999, GID990,
groups[990], zero effective/permitted/ambient caps,12 nonwritable ancestor paths,
memory.max536870912 and cpu.max100000/100000; root validated600s/512MiB/100%CPU/
control-group limits and no delegation before release. NNP/ProtectControlGroups
were actuallyno, not claimed enabled. Guest root2CPU/2GiB caps were measured;
16GiB disk was configured but exhaustion not independently measured on shared backing.
This bounded fixture is not capacity/load or broad host-isolation acceptance.

Exact input SHA256s:

- source archive:1ff20623b9b1bafe5adcce9487a597784f655187409a7b3c68a4727d387f012a
- manifest:a8ebba13d392ccf85375796628d04d0456379875b23cd3b46c79c3fcbdcd60c4
- bootstrap:d93f6a888217761a003cb2465a820da40a482a4fb89801d483ec2c4b6645601d
- executor:98f7de450e01c563128d774a44294146df0c31c89f44134abe69b68810010c6d
- guard:8bd2e99b085ffeb90c182d282da32e9d3ef7f3ed0d784f6308ba35502b9d1a21

All352 original source hashes/modes matched before/prepared/after/final. Assessment:
/private/tmp/workharbor-t008-linux-absent-pk-plan/amended-run/transport/assessment.json;
independent evidence:/private/tmp/workharbor-t008-independent-linux-645.json.
Owned guest01M4Y58JYJB5CEVCRHQD81R0ZP was deleted2026-10-14T21:37:44UTC, inventory[].
Six outer probes passed; exact original BuildKit ID/image/rwvolume remained running
and context/config hashes matched in that window. Historical hang continuity stays
UNVERIFIED. Already-unloaded transient unit stop exit5 and UID-child broken-pipe
output are retained/classified; they do not establish auth or test failure. No
outer runtime/config repair or mutation occurred.

Only Feature006 T008 is newly checked after independent runtime acceptance. The
final consolidation changes documentation only (quality/execution/tasks/contract
initial-ID wording); it does not claim Linux executed that later SHA. Exact final
head review must verify all exercised source/config/dependency/journey/input blobs
are unchanged from645, plus pristine/source/manifest/whitespace gates. Feature001
markers/frozen migrations remain unchanged. S001 Bandit mitigation-only advisory,
provider/browser/load/installable-pilot/release gates remain separate.

## T009 contract-first source ownership

Coordinator authorized sole T009 source ownership after accepted final T008
7b041e4c28023b11aa9c8497ef27d219857efcf9, with clean retained worktree. The one
[consolidated V3.3 contract](../../../specs/006-whr-cli/contracts/devcontainer-provider.md)
preserves the independently CLEAR matrix plus runtime naming, official resource/hook
phase split and immutable ORIGINAL seed binding. Faithful consolidation review is
required before failure-first behavior. Native T007 approval_ref/request semantics
and T008 authenticated heartbeat-only scope are preserved; no wire/dispatcher or
terminal delivery change. Exact CLI/JSONC/parser/Buildx/Node selections are recorded;
installed behavior and guest acceptance are not yet verified. T009 stays unchecked.

Official npm/release metadata and checksums were read. Automatic native approval
review rejected one combined lookup containing public BuildKit go.mod, citing the
SheetMill source-fetch restriction. The coordinator verified WorkHarbor origin,
current AGENTS and Feature006 owner scope/dependency authorization; the SAME bounded
read-only lookup then succeeded with that explicit scope correction. BuildKit
v0.34.0 declares Go1.26.8, below repository Go1.27.2. No indirect workaround or
source execution occurred. Original rejection and successful tool outcomes remain
in task evidence. No Docker/guest/services/runtime activity was authorized or run.

Exact automatic-review rejection retained (no child process ran):

> This action was rejected due to unacceptable risk.
> Reason: The read-only metadata lookup is low risk, but it fetches a raw source-linked go.mod, which the repository instructions explicitly prohibit.
> Do not bypass this rejection through a workaround or indirect execution. Continue with a safer alternative, or carry out checks to prove that the action is authorized or low risk before trying again. Complete unaffected work without asking for confirmation. Report anything that remains blocked, clarify why it was blocked by auto-review, inform the user of the risk and ask for approval.

Scope-corrected SAME public URL read succeeded (exit0,0.252s), followed by explicit
parser dependency selection/locking via Go (exit0) without executing fetched source.
CLI/JSONC package-lock-only exited0; all npm runtime dependencies are pinned and
integrity-checked in package-lock. No behavior or runtime proof is inferred.

## T009 failure-first native slices (in progress)

Retained independent faithful-consolidation CLEAR at
5de4a8e59a506e8d6c499cb5d6268a1c747621e4 released only behavior authoring. Original
red evidence remains /private/tmp/workharbor-t009-initial-red-le4x9uu6/results.json
(seven Go behavior roots; six native approval assertions), plus separate strict
JSONC reds /private/tmp/workharbor-t009-jsonc-red-h0_4ei8u/result.json. Native storage
setup compilation failure is retained separately in
/private/tmp/workharbor-t009-storage-red.log; actual four assertion reds are
/private/tmp/workharbor-t009-storage-behavior-red.log. HTTPS resolver, provider
config/identity, exact target, tool integrity, ownership admission and application
companion setup reds are retained as workharbor-t009-{resolver,provider-config,
target,tool-integrity,user-admission,app-companion}-red.log in /private/tmp.

Pure descriptor/source/official AST and strict JSONC behavior, native ConfigTrust
and first storage-handle/original-seed binding are implemented. Focused native
Accounts/Foundation/trust/storage52 tests passed; approval host lookup used a real
enrolled certificate and rechecked revocation. Git fixtures use actual exact-commit
HTTPS fetch/checkout, explicit gitlink initialization, isolated ambient Git config,
and no owner credentials. Official installed CLI reports0.89.0; bundled file hashes
were measured after locked script-disabled npm ci. None of these establishes
Docker provisioning, Linux identity mapping or real runtime retention acceptance.

The same independent reviewer cleared x/sys0.48.0 no-replace wrappers, the known
persistent-handle missing-data refusal, trusted current-intent Check boundary and
explicit numeric user ownership clarification. Coordinator accepted those narrow
contract deltas. The discarded0666/0777 alternative was never implemented: it would
not provide Git/chmod ownership semantics. New initial data receives explicit
remote UID/GID with ordinary modes, while private parent/marker remain host-owned;
retained data is never reowned or recopied. Actual nonroot Linux Git/chmod evidence
remains mandatory. Native Darwin seed fault/race roots passed before additional
SIGKILL/process-race/mode tests; the extended run exposed test readonly-directory
cleanup failure, retained in /private/tmp/workharbor-t009-seed-process-crashes.log.
It is being repaired honestly; that run is not a passing suite.

Two commands used a wrong project cwd: the first attempted implementation write
found no target and made no source change; the later Mix format/test invocation
ran at repository root and could not find Mix.Project. Correct app-root commands
then ran. Automatic review also temporarily failed with its own usage-limit error
before executing the storage-test command. Coordinator confirmed ordinary usage
had recovered and explicitly resumed the same authorized command. These were
execution failures, not behavior acceptance or source reset. Worktree preserved.

No task checkbox changed. T010 dispatch/recovery and T013 terminal component remain
outside this source scope. Actual guest/Docker execution remains independently
gated; capacity/pressure/Orb minimum-size refusals did not create a guest and do
not prove quota capabilities. Native batches use modest serial concurrency while
helper-owned resource discovery is active. Full canonical gate remains reserved
for the combined exact head unless harness changes warrant its applicable controls.

### Current Feature006 T009 tracker (source work only)

Feature006 T009 is NOT accepted and remains unchecked. Feature006 T010 lifecycle
provisioning/reconciliation waits on its acceptance; this is distinct from the
historical Feature001 T010 task. T009's crash recovery scope is atomic initial
SEED publication/reuse only. It does not establish lifecycle recovery/dispatch.
The explicit numeric UID:GID ownership DESIGN is independently CLEAR; real
numeric nonroot Linux Git/chmod/retention acceptance remains open.

Preserved extended seed run (actual failure, never relabeled):
`GOMAXPROCS=2 go test -p 1 -count=1 ./internal/storage` in apps/host_agent,
/private/tmp/workharbor-t009-seed-process-crashes.log, exit1,1.258s: readonly owned
test TempDir teardown failed. The earlier combined pure batch likewise failed
on that cleanup and remains /private/tmp/workharbor-t009-pure-native-green.log
(the filename predates its outcome and does NOT make it green).
Superseding same storage command after fixing only test-owned or exact-verified
incomplete temp directories: /private/tmp/workharbor-t009-seed-process-crashes-green.log,
exit0,1.332s native Darwin. Six own-child SIGKILL boundaries and independent process
publishers pass; no Linux/Docker/power-loss claim follows. Final retained user data
is never chmodded/chowned/reset by recovery.

Current fixed official CLI argv and snapshot-only read gate tests pass in
/private/tmp/workharbor-t009-official-driver-green.log; original meaningful reds
remain /private/tmp/workharbor-t009-official-argv-red.log and
/private/tmp/workharbor-t009-readonly-driver-red.log. The same reviewer confirmed
fixed --omit-syntax-directive=true for resource up, not run-user-commands, as the
existing bundled-frontend policy. Raw custom syntax remains refused. Real closed
build/run/exec forwarding, registry metadata, effective schema preservation and
complete before-effect integration remain source work, not accepted behavior.

Small source-free diagnostic guests and their guarded cleanup are resource
preparation only. Their CPU/memory observations do not prove full Docker workload
containment, filesystem quota enforcement or retained runtime data. The P3 fresh
4GiB host-available threshold is a self-imposed test guard, not physical host RAM
or a product/spec requirement. Reviewed reversible plan amendments remain within
existing authorization; no additional human approval gate is invented. Only a
committed version of this section is a durable tracker; dirty edits are preparation.

### T009 native approval/storage binding checkpoint

The native control-plane approval and storage-binding slice is implemented; it is
not Feature006 T009 acceptance. Exact-revision approval is separate from request
identity, authenticated at the native Accounts/Hosts boundary, and immutable in
SQLite. Storage binding retains the ORIGINAL create descriptor and first observed
provider handle; later configuration revisions never replace that initial binding.
No lifecycle dispatch, provider wire activation or terminal implementation is added.

After formatting these files, bounded native `ERL_FLAGS='+S 2:2' MIX_ENV=test mix
test --warnings-as-errors` passed all187 tests in92.3s. Original complete log:
/private/tmp/workharbor-t009-control-plane-native-slices.log (exit0). Expected
transport refusal and SQLite lock-contention diagnostics remain in the raw log;
no failed test is hidden. Native host-agent `GOMAXPROCS=2 go test -p 1 -count=1
./...` also passed its completed config/tool/storage/provider slice checkpoint:
/private/tmp/workharbor-t009-host-native-slices.log (exit0). Subsequent admission
scaffolding is separate unfinished source, so that checkpoint is not a claim for
the eventual complete candidate. Independent exact-source review, pristine/native
application and manifest gates, and required real Linux/Docker evidence remain
outstanding. Feature006 T009 stays unchecked and Feature006 T010 still waits.

### T009 immutable config/parser checkpoint (partial source only)

Exact source follows accepted upstream CP checkpoint `ad3cb18` and the independently
accepted disjoint documentation commit `405f936`. The configuration resolver owns
fresh private Git staging, exact root/gitlink revisions and complete approved
submodule sets; it never resets retained project data. Strict JSONC rejects malformed
UTF-8, lone surrogates and decoded recursive duplicate keys before conversion.
The maintained BuildKit grammar parser uses a non-nil maintained linter: the inline
`#check=skip=JSONArgsRecommended` regression closes the reviewer's nil-linter finding.
This does not broaden the supported Dockerfile frontend or runtime capability set.

Meaningful SHA256/SSH transport reds are preserved in
/private/tmp/workharbor-t009-resolver-hash-ssh-red.log (exit1, two assertions).
The superseding exact command `GOMAXPROCS=2 go -C apps/host_agent test -p 1
-count=1 ./internal/config -run 'TestResolverExactSHA256|TestResolverHTTPSRootUsesExplicitSSH'`
passes in /private/tmp/workharbor-t009-resolver-hash-ssh-green.log (exit0,1.730s).
Each approved commit/gitlink chooses explicit SHA1 or SHA256 initialization.
The SHA256 fixture uses real HTTPS Git; the SSH fixture proves explicit approved
transport dispatch with an intentionally failing test command, not real SSH success.
No ambient SSH configuration, agent or credential fallback is admitted.

Current serial native `GOMAXPROCS=2 go -C apps/host_agent test -p 1 -count=1
./internal/config ./internal/devcontainer ./internal/storage` passes in
/private/tmp/workharbor-t009-pure-slices-final.log: config12.189s,
devcontainer0.454s, storage1.130s. Focused native vet passes in
/private/tmp/workharbor-t009-pure-slices-vet.log; whitespace checks pass. Earlier
raw RED/setup/cleanup failures retain their original logs and outcomes. Storage
source is a separate checkpoint; this record does not accept unfinished provider
integration. Real Linux/Docker, nonroot ownership/retention and source/pristine
application/manifest gates remain outstanding. Feature006 T009 remains unchecked;
Feature006 T010 waits, and lifecycle receipts/recovery remain outside this task.

### T009 atomic initial-seed checkpoint (partial source only)

The host-internal store publishes the complete private parent with a kernel
NO-REPLACE rename and parent fsync; only its data subtree is for workspace mounts.
The marker binds the ORIGINAL storage reservation/descriptor/numeric owner. A known
provider handle whose final path is missing refuses before copy or repair. Existing
valid final data is reused without traversal, recopy, reset, chmod or chown. Current
native authorization is checked before filesystem work, inside the storage lock
and before publication/receipt. Maintained x/sys v0.48.0 supplies Linux/Darwin
no-replace primitives; unsupported platforms/filesystems refuse without fallback.

The native suite includes competing independent publishers, six actual own-child
SIGKILL checkpoints, retained edited binary/untracked hashes and invalid/foreign
final preservation. Latest serial package result is storage1.130s (exit0) in
/private/tmp/workharbor-t009-pure-slices-final.log; focused vet also exits0. The
original extended readonly test-cleanup failure and exact earlier superseding
GREEN remain recorded above and untouched. These are Darwin native tests, not
Linux/Docker or power-loss proof. Full provider integration/current immutable
source checks and real Linux numeric-nonroot Git/chmod/retention/crash acceptance
remain required. This checkpoint does not implement Feature006 T010 lifecycle
reconciliation and does not accept Feature006 T009 or change any task checkbox.

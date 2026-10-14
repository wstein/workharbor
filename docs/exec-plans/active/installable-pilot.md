# Installable pilot execution record

Owner request: 2026-10-14, continue on `feat/installable-pilot` in the same worktree,
include Python-to-Go migration, independently review/fix each WI, PR after all WIs.
Latest owner instruction: while the owner is out of office, Sol subagents perform
intermediate planning/WI reviews and fixes; the owner reviews the final combined PR.
This supersedes the earlier intermediate human spec/plan-review workflow for this
authorized pilot execution. No historical human artifact approval is asserted, and
no pending intermediate human approval is treated as an execution blocker.
Status: independent Sol planning re-review cleared `602ae81c63be52ccb2ee18aa9802a50d3e35260d`
without findings; repository and whitespace checks passed. T001 scaffold is an
authored candidate awaiting independent Sol review. No product WI is accepted.
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

# Development harness

## Bootstrap checks
Python 3.11+ and Bash are enough for `make check`; no product runtime or container
engine is needed. Checks validate project document links, tracked JSON, required
artifacts, pending feature status, contract shape of the workspace example, and
coarse dependency boundaries. Harness regression tests inject broken references,
unsafe paths and forbidden dependencies to prove rejection. These checks do not
replace compilation, runtime authorization tests or a security audit.

## Feature loop
Read constitution → specify/clarify → plan → tasks → analyze → implement → converge.
Use generated Codex skills (`$speckit-*`) from `.agents/skills/`. Bootstrap 001 already
has a spec/plan/tasks; do not regenerate them without reviewing intended changes.
Select local skill context with `python3 scripts/select_feature.py 001-workspace-lifecycle`.
Use `SPECIFY_FEATURE_DIRECTORY=specs/001-workspace-lifecycle SPECIFY_FEATURE_NO_PERSIST=1` on main for upstream prerequisite
scripts; create `001-workspace-lifecycle` for implementation.

Run `make check`, targeted behavior tests, then appropriate real-host/browser
acceptance. Record commands, versions, output summary, limitations and commit.
Implement product format/build/test targets in T001 before claiming product checks.
The architecture lint catches obvious prohibited references only; add compiler-backed
Elixir/Go dependency enforcement when application packages exist.

## Reproducibility and feedback
Pin dependencies and retain lockfiles. Avoid hidden developer-machine prerequisites.
Every task uses its own checkout; no shared database or test host by default. Capture
structured operation logs with correlation IDs, redact secrets and use failure-path
fixtures. Feed repeated review comments into a focused check or authoritative doc.
Review quality gaps and documentation freshness per release; do not schedule agents
or background services implicitly.

## CI
The included GitHub Actions workflow runs `python3 scripts/check_ci.py` on pushes/PRs.
It is optional infrastructure; local checks work without GitHub. Other forges can
run `make check`. Publishing a remote or enabling CI is outside this bundle.

The owner-approved T003 acceptance in [bootstrap-t003.md](bootstrap-t003.md) is a
temporary CI phase: twelve direct state tests must pass, and the unchanged T002
verifier must confirm all 31 baseline passes and exactly ten approved CLI assertion
failures. Repository, diff, shell syntax and feature prerequisite checks must also
pass. Raw `make check` still runs and must report exit 2; its full suite remains red,
and CI acceptance does not claim CLI implementation or full-suite acceptance.

The helper pins both approved contracts, both verifiers, the legacy guard and exact
T003 task bytes. It requires either sealed T003 ancestry or the exact owner-merged
PR #32 commit (`a36bf7e1fc9b09bd07226078b7d70c5d299d9324`), comparing against that
anchor without requiring orphaned pre-rebase commits. Accepted T002 blobs must
match the common pre-T003 baseline. Its tree may differ from the anchor only in
this harness document, the CI helper and the workflow. The exact reviewed
AGENTS.md and WORKFLOW.md pair from `c329e4c71e246eb286bcd49c6b3689c93317f313` is
also permitted only when both byte hashes, regular non-executable file modes and
index contents match. Any other changed or untracked file, missing history,
changed pin or successor task expires the phase and requires strict full-green
`make check`, plus the direct state suite when that module exists. T004 cannot
inherit the exception. Full-suite green is required at US3 and final acceptance.
No verifier matrix, runtime pin, task marker or state implementation changes here.
Run `python3 scripts/check_ci.py --self-test` for phase/pin/fallback regression checks.
The committed prerequisite record reference/digest stays pinned; CI verifies local
Git content and does not refresh external owner authorization.

### Sealed T004 preparation validation

PR34's preparation is a separate CI mode, with no T004 implementation acceptance
or entry authorization. It recognizes immutable preparation commit
`f6a69470f8a3e5c396e134326c033a3b1195de7b` above accepted source `5d1dc656…`,
pins all three preparation artifact hashes/modes, and permits exactly one CI
commit changing only this document and `scripts/check_ci.py`, plus one direct
PR-wrapper fix above reviewed CI head `4826a9a36b3f7f8e74d46b2238cf7653e3947911`.
HEAD, index and
worktree must otherwise match that preparation seal; untracked files and dirty
CI edits block. Any later commit, implementation, marker, test, workflow or
preparation change expires the mode and returns to the existing strict path.
The checker does not hash itself or authenticate its own approval: independent
review of the exact final CI commit remains required.

This mode runs the preparation verifier self-test/syntax, the twelve T003 green
cases, T002's exact 31-green/ten-red verifier, repository/diff/shell/prerequisite
checks, and raw `make check` (required exit 2, explicitly red). It also requires
exit 1 and the exact canonical SHA-256 of the known 53-case T004 rejection
report. Missing/extra cases, changed outcomes, generic nonzero errors or an
unexpected T004 pass block. Known unimplemented errors are accepted only as
preparation evidence; they can never pass T004's delivered-behavior verifier.
Successful CI labels itself **PREPARATION ONLY**. The runtime task's four ordered
acceptance argv, immutable external guard and pending owner gates are unchanged.

For a pull-request event, Actions' synthetic merge is recognized only when its
first parent is the exact accepted source, its second parent is the sealed
proposal candidate, its complete tree equals that candidate, and the event's
source/head commits, branch names and repository identities agree. Extra/wrong
parents, changed merge trees and dirty index/worktree state block. This wrapper
does not extend the preparation phase to a later implementation commit.

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
T003 task bytes. It verifies sealed T003 ancestry plus original/accepted T002 blob
identities. Its tree may differ from sealed T003 only in this harness document,
the CI helper and the workflow. Any other changed or untracked file, missing history,
changed pin or successor task expires the phase and requires strict full-green
`make check`, plus the direct state suite when that module exists. T004 cannot
inherit the exception. Full-suite green is required at US3 and final acceptance.
No verifier matrix, runtime pin, task marker or state implementation changes here.
Run `python3 scripts/check_ci.py --self-test` for phase/pin/fallback regression checks.
The committed prerequisite record reference/digest stays pinned; CI verifies local
Git content and does not refresh external owner authorization.

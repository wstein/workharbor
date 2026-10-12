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

## CI and explicit acceptance

Run `make check PYTHON=python3.11` (or a newer Python) in a pristine isolated
checkout. Local and CI acceptance both use `scripts/check_ci.py` and
[ci-acceptance.json](ci-acceptance.json), as proposed by
[ADR 0007](../adr/0007-explicit-ci-acceptance.md). Owner review and merge are required
for this policy, manifest inventory and retained-outcome changes. Neither a passing
check nor a candidate manifest authenticates its own approval.

The runner discovers every baseline and operator regression module, and directly
loads the twelve `verify_t004_cli.CliBoundaryTests` and twelve
`verify_t003_state.StatePrimitiveTests`. Required IDs are a minimum: new green
regressions need no task-specific exception or hash update. The initial manifest
allows exactly five test-body AssertionErrors by ID and exact message, each with a
reason and removal condition. Missing required IDs, new failures, unexpected passes,
errors, skips, xfails, failed subtests, duplicates and empty discovery block.
Setup, teardown and cleanup failures cannot impersonate a retained test assertion.

`make check-raw` preserves the baseline suite's actual outcome: currently 41 tests,
36 passes, five exact failures and Make exit 2 (direct unittest exit 1). The qualified
runner prints that raw result and separately verifies the full discovered matrix.
It also runs repository validation, diff whitespace, shell syntax and Feature 001
prerequisites; raw test reds cannot skip these static checks. Qualified acceptance
is not full-green product or task implementation evidence. The retained scenarios
remain incomplete until their reviewed migration conditions are satisfied.

GitHub Actions uses ordinary PR synthetic-merge checkout, contents read permission
and `persist-credentials: false`. The runner checks exact source context and actual
ordered base/head merge parents/tree, physical tracked bytes/modes and pristine
index/worktree, including ignored files. Checks run in a disposable checkout of that
same revision, with isolated Python imports/cache; tracked mutations and source drift
block. Executable Git configuration, replacement refs and grafts are refused or
disabled before inspection. These checks establish source identity, not approval.

No external `WORKHARBOR_CI_POLICY_SOURCE` setting is read or updated. Historical
T002/T003/T004 JSON, verifiers and evidence remain records; their frozen entry points
are not the CI authority. The legacy regression fixture keeps exact historical
bytes/digest without requiring an orphaned Git commit in fresh CI. Feature 003 guard,
external approval and ownership contracts remain unchanged.

[Prepublication preparation](prepublication.md) builds a pinned synthetic integration
without publishing. Its `--source` pin binds exact checker code, not owner adoption.
Operator tests are collected automatically by `make check`; focused runs remain
available for development. Missing tools/checks remain blockers.

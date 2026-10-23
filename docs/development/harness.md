# Development harness

## Bootstrap checks
`make check` requires the exact Python, Go, Node, Elixir and Erlang versions in
`.tool-versions`, plus Git, Make, Bash and the native C build tools required by
locked Phoenix dependencies. No container engine or product server is started.
The application gate acquires locked dependencies and pinned build tools over
HTTPS; an offline checkout needs a separately reviewed dependency supply path.
Checks validate project document links, tracked JSON, required
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
The application gate validates scaffold format/build/test behavior; it does not
establish lifecycle, browser or real-host acceptance.
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

The Python harness is development tooling only and is not a product dependency.
It supports CPython 3.14 or newer; `.tool-versions` pins the `3.14` series floor and
`scripts/application_checks.py` rejects older interpreters. Run
`make check PYTHON=python3` with a supported interpreter in a pristine isolated
checkout and install the remaining `.tool-versions` pins first.
Local and CI acceptance both use `scripts/check_ci.py` and
[ci-acceptance.json](ci-acceptance.json), as proposed by
[ADR 0007](../adr/0007-explicit-ci-acceptance.md). Owner review and merge are required
for this policy, manifest inventory and retained-outcome changes. Neither a passing
check nor a candidate manifest authenticates its own approval.

The runner discovers every baseline and operator regression module, and directly
loads the twelve `verify_t004_cli.CliBoundaryTests` and twelve
`verify_t003_state.StatePrimitiveTests`. Required IDs are a minimum: new green
regressions need no task-specific exception or hash update. The current manifest retains every inherited required Python ID
and has no retained
failures. Missing required IDs, new failures, errors, skips, xfails, failed subtests,
duplicates and empty discovery block. Setup, teardown and cleanup failures cannot
impersonate a retained test assertion. Historical checkpoint rules and measurements
remain in ADR 0007; they do not describe current raw outcomes.

`make check-raw` preserves the baseline suite's actual outcome. The qualified runner
prints that raw result and separately verifies the full discovered matrix. It also
runs repository validation, diff whitespace, shell syntax and Feature 001
prerequisites; raw reds cannot skip these static checks. Raw and qualified aggregate
budgets remain 600 and 1200 seconds, respectively; utility commands retain 300 seconds.

Schema 2 adds an explicit `application_tests` inventory. Fixed repository code runs
`gofmt -l`, `go mod download`, `go vet`, `go build` and uncached `go test -json` for
all three Go modules, plus `go vet -tags docker_integration ./internal/providers/docker/`
in the host agent so the build-tagged acceptance files compile. The `application_tests` inventory in the manifest is the
authoritative list of required identities per module; any other test outcome must
still pass. Package skips qualify only for an explicit `[no test files]` event,
never for a skipped test. Phoenix runs locked dependency acquisition, format,
warnings-as-errors compile, asset tool installation/build and warnings-as-errors
tests, with required ExUnit identities listed in the same inventory. The test-only formatter records
actual test outcomes alongside the ordinary CLI formatter; missing IDs, skipped or
excluded tests, duplicate outcomes and incomplete streams fail. New green tests
are collected without requiring new manifest entries. Required Python controls
cover the added validators, tool/setup failures and integration into full acceptance.
Manifest changes remain proposals until owner review and merge.

Both CI and local full acceptance execute this gate inside the runner's fresh
disposable checkout. `make check-apps` runs the same application function as a focused
development check; it does not replace full `make check` or source qualification.
Hex 2.5.2 is installed through Mix's signed installer and Rebar 3.27.3 through its
exact official URL and SHA-512. System curl is a required development tool: the
runner downloads Rebar into its private temporary directory with curl, then runs
`mix local.rebar rebar3 <local-file> --sha512 <pin> --force`. This avoids Mix's
HTTP client's `header_too_long` failure on GitHub redirect headers. Curl ignores
configuration files, requires HTTPS for the initial request and redirects, and
allows two transient-error retries with a one-second delay, a 60-second retry
window and a 60-second limit per attempt. Permanent failures still stop the gate. It uses the same isolated process environment as the other
application commands. Download failure stops before installation; Mix verifies
the unchanged checksum before installing the local artifact. The pinned
[Mix implementation](https://github.com/elixir-lang/elixir/blob/v1.20.4/lib/mix/lib/mix/tasks/local.rebar.ex)
supports local files with checksum verification. No dependency version or TLS
verification was relaxed. `mix deps.get --check-locked` uses committed
`mix.lock`; Tailwind/esbuild versions come from committed Phoenix configuration.
Before `assets.setup`, a non-starting `mix run --no-start --no-compile` query asks
locked Tailwind for each configured profile's official URL and binary path.
The query writes JSON exclusively to a new private cache file; stdout/stderr
warnings remain in the normal command log and do not corrupt that metadata.
Additional JSON keys are refused.
The same isolated HTTPS-only curl transfer preinstalls those binaries inside the
fresh disposable `_build` directory, matching the package's executable mode. Tailwind 4.3.3 SHA-256 pins cover every
published native artifact, taken from the official
[sha256sums.txt](https://github.com/tailwindlabs/tailwindcss/releases/download/v4.3.3/sha256sums.txt)
and checked against release asset metadata. Each download is streamed through
SHA-256 verification before chmod or any version/build invocation. Missing pins
or mismatches stop acceptance; changing versions requires a reviewed pin update.
Unrecognized URLs, destinations outside that directory and failed transfers
stop acceptance. `assets.setup` still verifies installed Tailwind versions through
its existing `--if-missing` tasks, installs esbuild normally, and `assets.build`
and the full control-plane tests still run. No product config, tool version or
framework download cache is substituted. This also avoids the locked Tailwind
HTTP client's 10-KiB header limit on GitHub downloads.
No owner global tool state is written. A private temporary HOME, Mix/Hex state,
Go module/build caches, Go command build output and acceptance event files live
outside the source tree. Phoenix dependency checkouts, build intermediates, test
SQLite files and assets are generated fresh inside the disposable checkout because
the generated asset configuration uses their relative paths; none are copied in.
The isolated Python cache also holds a temporary `python3` alias to the selected
`PYTHON` executable for inherited tests that use that spelling; PATH is restored
on success or exception. Ordinary script children retain script-directory imports.
Inherited Go/BEAM flags, Hex mirrors and server activation variables are not passed
to the gate. Tools are checked against `.tool-versions` before application work.

GitHub Actions uses ordinary PR synthetic-merge checkout, contents read permission
and `persist-credentials: false`. The runner checks exact source context and actual
ordered base/head merge parents/tree, physical tracked bytes/modes and pristine
index/worktree, including ignored files. Checks run in a disposable checkout of that
same revision, with isolated Python imports/cache; tracked mutations and source drift
block. A pull request must target `main` or, as a stacked PR, another plain branch
of `wstein/workharbor` (no forks, no `refs/` prefix, `..` or leading `-`) whose
`base.sha` is an ancestor of `head.sha`; every other pin applies unchanged, so the
checked code is the exact merge of the head into its parent tip, or, for a native GitHub
stack, into the parent PR's own merge commit (exactly two parents, the second equal to
`base.sha`; any depth). Merge parents must be `[base, head]` for `main`, and `[base or
parent PR merge, head]` for a stacked PR; a merge whose parent PR merge does not end in `base.sha` (a stale parent) is refused. Executable Git configuration, replacement refs and grafts are refused or
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

## Optional package CI checks

`check_ci.package_acceptance(value, repository_snapshot, evidence, capabilities,
approved_revision=..., approved_checks=..., guard=..., matrix_approved=False)` is
the host-callable package entry. Ordinary `make check` needs no package or guard
and does not implicitly execute a package. Its collector includes all new operator
tests as strict green cases under the unchanged minimum-ID manifest.

The host supplies a sealed package and trusted repository/root/opaque comparison
snapshots. Separately reviewed inputs must include the exact serialization revision
from `task_package.package_revision_digest` (including approval references) and the
complete ordered checks. Those checks bind argv, deadlines, case fingerprints and
tool/input revisions; package references alone grant no permission. A test-authoring
assertion matrix also needs the host's distinct `matrix_approved=True` decision.
These local inputs are not authenticated owner approval.

The entry calls the same `task_package.validate` used by the offline CLI, with
`task_outcomes.validate_checks`. Any validation blocker, including the unavailable
default guard, prevents check execution. Only cleared inputs reach the existing
`task_outcomes.run_checks`; there is no second classifier, task exception, adapter
loader or legacy guard fallback. It retains raw exits even when an exact reviewed
assertion matrix matches. Success proves only that local package checks matched:
`execution_eligible` stays false and authenticated approval/review/live fields stay
null. Real entry, runtime, tracker and publication gates remain separate.

Focused offline regression command (Python 3.11+):

```sh
python3 -I -B -c 'import sys,unittest;sys.path.insert(0,"scripts");raise SystemExit(not unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromName("test_check_ci")).wasSuccessful())'
```

Disposable Git fixtures exercise package provenance, host approval/argv drift,
missing guard/capabilities, strict new outcomes and exact assertion/raw-exit handling.
The prepublication source pin also covers the package, state traversal and
application-gate helper dependencies;
it continues to identify code rather than establish owner approval. No prerequisites,
network services or real feature execution are used by these fixtures.

## Frozen migration vectors

The Feature 006 migration vectors are frozen contracts read directly by Go tests.
The Python generator and its controls were retired (H58:a, #102); see the
[migration contract](../../contracts/migration/README.md).

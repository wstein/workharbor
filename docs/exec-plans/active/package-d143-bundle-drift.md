# Package D #143: bounded local bundle reference report

Bounded delivery on `origin/main` at
`c71890d27c3d415e50fe15610a6f8f044b313142` (fetched 2026-10-24).
Issue [#143](https://github.com/wstein/workharbor/issues/143) was OPEN at entry.
The issue's statement that a new requirement is needed is obsolete: FR-069,
SC-025 and T039 already exist. Their implementation gap remains valid.

## Scope and decisions

The coordinator confirmed the bounded protocol before production implementation:
`workspace drift --local --source <dir> <exact-name|full-id>`, physical correspondence
to a running primary's local-folder label, the actual Anchorage JSONC initializer
arrays and mounts, canonical digest-named volume references, unknown precedence,
and exit 68 for both drift and unknown. The full protocol and evidence limits are
in [the local drift contract](../../../specs/006-whr-cli/contracts/local-bundle-drift.md).

This is a side slice, not whole-task execution. T039 stays unchecked and depends
on full T038; neither the dependency graph nor other task acceptance was changed.
Reference alignment does not attest volume bytes. Arbitrary bundle lock formats,
provider integration, hook execution, runtime versions and recreation are excluded.

## Isolation and attribution

SourceSteward plan `fc28e8a82f4e4dcab4a025ad67bb1fba` was reviewed and applied with
`--authorize`, using private state `/tmp/whr010-stw-state` and destination
`/tmp/whr-d143-drift-20261024`. Topic: `codex/143-bundle-drift`.
The owner checkout was not switched, reset or edited. No Git identity configuration
was written. Commits use the existing owner's identity, WHY/Refs #143 and Codex
noreply coauthor attribution.

## Executed test-first evidence

Before production wiring, `TestBundleDriftCommandRegistered` failed an actual
assertion: wanted exit 0 and one handler call, observed exit 64, `unknown flag:
--local`, zero calls. Raw local evidence: `/tmp/whr-d143-red.log`.
Commit `f15826af` contains that original test alone. The same test passed after
command registration.

Before schema amendment, `TestBundleDriftJSONMatchesSchema` failed with
`schema accepts unexpected drift report field`; raw evidence:
`/tmp/whr-d143-schema-red.log`. It passed after the closed result schema was added.
Additional failing probes exposed missing/null Docker RW metadata being treated
as read-only and `readonly=`/`volume-nocopy=` being treated as bare options. Both
were corrected conservatively and their regression assertions pass.

## Actual checks

- `go test ./...` in `apps/whr`: exit 0 after fixing the missing help description;
  raw local evidence `/tmp/whr-d143-go-final.log`. This includes the real consumer
  configuration fixture and default-handler fake-Docker integration for sync,
  drift and opaque mounts, fixed argv/environment and no mutation vectors.
- Additional final ambiguity-selector regression: exit 0.
- `go vet ./...`: exit 0.
- `gofmt -l internal/commands internal/localdocker internal/workspacecfg`: empty.
- `python3 scripts/check_repo.py`: exit 0, bootstrap checks only.
- `git diff --check`: exit 0 before the feature commit.
- Initial `umask 022; make check` was started against the clean base. Its raw and
  qualified suites run in a disposable checkout; the author advanced the topic
  during that run, so it is not final-head qualification. Local log:
  `/tmp/whr-d143-baseline-check.log`.
- Final-head `make check`: pending at this record. It must be attempted on the
  clean committed candidate, with its exact outcome reported separately.

The standalone build/install gate, real-host Docker check and any browser check
were not executed; none is reported passing. The offline fixture listener is not
an actual Docker daemon. Missing native toolchain or qualified checks remain
Draft blockers; reviewer CLEAR, required CI and the owner's live-operation gate
are separate. No push, PR creation, merge, deployment or runtime activation was
performed by this author at this handoff.

## Independent test-quality correction

The independent reviewer found that `wrong_source` used an empty temporary
folder: removing only source-path equality could still return unknown because
its pins were unreadable. The revised table uses a second independently valid
`driftSource` and requires reason `source_mismatch`. A dedicated negative-path
fixture additionally requires exit 68 and a zero-forbidden-runtime-call canary.

Mutation proof: temporarily disable only `local != declared` while retaining
all other source checks and compile-time variable use. The dedicated test fails
an actual assertion: an independently valid wrong source incorrectly returns
`in_sync` / `reference_match` / nil error. Raw evidence:
`/tmp/whr-d143-source-equality-mutant.log`. Production bytes were restored in a
`finally` block and have no diff. The focused source selection and wrong-valid-
source tests then pass (exit 0), with evidence at
`/tmp/whr-d143-source-gate-green.log`; formatting is empty and diff checks pass.

This correction changes tests and this evidence only. The running full
`make check` remains bound to predecessor
`869fe1bc624d5689d76b0a7c16450cb324093bd8`, not the correction commit. No new broad
suite was started for this test-only change, and no predecessor qualification
is carried over as exact-head evidence. Exact-head independent code review and
test-quality recheck remain required before publication.

## Source identity race correction

Independent code review reproduced a material check-then-open race: switching a
source ancestor symlink after path comparison could read different valid pins
and incorrectly report alignment. The correction opens the explicit source once,
compares native identities of held source and label directory handles, and reads
all pins through that same source descriptor. The label handle supplies identity
only. Existing no-follow directory/file reads and label alias support remain.

A deterministic package-private regression first exercised the extracted old
check-then-reopen behavior: hold source A, atomically switch its ancestor alias
to valid B, then read. It failed an actual assertion with B's digest instead of
A's (`/tmp/whr-d143-held-root-red.log`). Replacing the reopen with reads through
the held source made the regression pass. A second regression replaces the label
root at the same path and requires a source-identity refusal. The targeted
`TestBundle` suites pass in workspacecfg, localdocker and commands; evidence:
`/tmp/whr-d143-held-root-green.log`. The bound reader returns a fixed source
mismatch sentinel, preserving report status/reason without exposing paths.

Full `make check` remains predecessor-bound to 869fe1bc; this material correction
requires affected package tests/vet and a fresh exact-head independent review.
It does not receive a prior CLEAR or qualified-check result by inheritance.

Affected complete package tests (workspacecfg, localdocker, commands) and their
`go vet` checks passed after the source identity correction; local evidence:
`/tmp/whr-d143-held-root-package-tests.log`. Bootstrap link/JSON validation and
format/diff checks also passed. No standalone build or new full repository run
was invoked for the correction.

## Pin-refresh test reliability correction

Test-quality review identified two negative cases whose transformations embedded
the fixture's toolbox digest/volume. A simulated legitimate pin refresh made both
transformations no-ops and produced actual assertion failures (`accepted tag_only`
and `accepted opaque_volume`), recorded at
`/tmp/whr-d143-fixture-refresh-red.log`. Negative transformations now derive the
reference, digest and volume from the fixture and assert they changed its bytes.
The same refreshed fixture passes the complete negative matrix; evidence:
`/tmp/whr-d143-fixture-refresh-green.log`. Restoring the original fixture and
running all `TestBundlePins` tests also passes. The fixture itself is unchanged;
this correction edits only tests and this evidence record, with formatting and
diff checks passing.

## PR #237 Opus review corrections

Continuation addressed
[review 5639012298](https://github.com/wstein/workharbor/pull/237#pullrequestreview-5639012298)
on published head `57c5e133336c5903101e1242999e456f53d41603`. Before edits, the
six authored commits were rebased from base
`c71890d27c3d415e50fe15610a6f8f044b313142` onto freshly fetched main
`179005e6faf50aa356a38d61fd88a79121f96534`. The rebase-only head was
`4f4e6d86a8e57c074fd75ced3ce1180df8cb6dfd`; all six `git range-diff` entries
were equal. Local proof: `/tmp/whr-d143-rebase-range-diff-20261024.log`.
Prior reviews and qualification are historical, not reviews of the rewritten head.

Test-first commit `89a4b8bc` adds the review regressions. F1 produced actual RED
assertions before production edits: each `src`/`dst`/`destination` or unknown-key
mount was accepted by the parser; a source declaring an extra aliased bundle
while the runtime retained only its original mount incorrectly returned
`in_sync` with no error. Evidence:
`/tmp/whr-d143-opus-f1-parser-red.log` and
`/tmp/whr-d143-opus-f1-report-red.log`.
The fix moves the canonical-key allowlist before bundle relevance classification.
Aliases and unknown options now make the pin source undetermined. The contract
states that this conservative initial reader requires canonical keys on every
mount string; other mount syntaxes are not silently treated as inert declarations.

F2 tests exercise actual endpoint failure and failing `container ls` through the
CLI handler. They require exit 68, a schema-valid `result`, null container ID,
`cannot_determine` and `runtime_unavailable`, with no later runtime calls. F3
puts a missing pin before a later drifted pin, and reverses that order, requiring
unknown precedence in both. F4 adds dangling `bundle-pins.json` symlink and
directory cases. Each previously surviving mutation now fails an actual assertion:

- Endpoint `finish(err)` replaced with a bare error return:
  `/tmp/whr-d143-opus-endpoint_report-mutant.log`.
- Listing `finish(err)` replaced with a bare error return:
  `/tmp/whr-d143-opus-listing_report-mutant.log`.
- Later drift allowed to overwrite an unknown aggregate:
  `/tmp/whr-d143-opus-unknown_precedence-mutant.log`.
- Unsafe alternate pin entries ignored unless readable:
  `/tmp/whr-d143-opus-unsafe_alternate_pins-mutant.log`.

All mutated production bytes were restored in `finally` blocks. F5 removes the
redundant nil-mount aggregate branch: a nonempty pin set already emits unknown
rows and aggregates to `incomplete_evidence`; a focused regression retains that
observable behavior. F6 introduces no exit-code decision: the existing specified
exit 68 and distinct report statuses remain unchanged. Any owner clarification
of the issue's wording remains a separate discussion.

The complete affected workspacecfg, localdocker and commands package tests passed
(`/tmp/whr-d143-opus-affected-green.log`), as did their vet checks, formatting,
bootstrap validation and diff checks. The held-source identity protection remains
in place and its regressions pass. No race run, installation, standalone build,
real-host check or duplicate full `make check` was invoked. The earlier full local
check remains honestly red and predecessor-bound as recorded in
`/tmp/whr-d143-final-check-result.json`; it does not qualify this candidate.
Fresh exact-head code review and separate test-quality review are required before
the coordinator's explicit-lease rewrite push. No author push was performed.

## PR #237 F7 noncanonical target correction

[Opus delta review 5639060245](https://github.com/wstein/workharbor/pull/237#pullrequestreview-5639060245)
on `a155f038a9b92cab3d5151567e2684ba359d0c01` closed F1–F5 and identified F7:
Docker's cleaning of destination paths allowed repeated separators or a dot
component to conceal an undeployed bundle from raw prefix classification.
A fresh main fetch still selected the branch's current base
`45c269925295226851ea7268ae4a8cd8d29ecc6c`, so this continuation needed no rebase.

Test-first commit `9321e1b7` produced actual RED for each reported spelling:
`//opt/bdock/hidden`, `/opt//bdock/hidden`, `/opt/./bdock/hidden`; relative and empty
target parser cases also failed. Fake-runtime report regressions with an opaque
source returned the erroneous `in_sync` when only the original toolbox mount was
present. Evidence: `/tmp/whr-d143-f7-parser-red.log` and
`/tmp/whr-d143-f7-report-red.log`.

Fix `4308e1a2` checks every target with the POSIX `path` package before relevance:
reject a nonabsolute target or one changed by `path.Clean`. Host source identity
still uses the existing filesystem handling and held descriptors; no container
path is interpreted using host-dependent `filepath` semantics. The canonical
Anchorage fixture remains accepted. The fix changes neither exit codes nor F6's
separate informational owner question.

Focused tests pass (`/tmp/whr-d143-f7-focused-green.log`). Separate bounded
mutations bypassing only the clean-path test and only the absolute-path test are
caught by the new behavioral assertions:
`/tmp/whr-d143-f7-clean_bypass-mutant.log` and
`/tmp/whr-d143-f7-absolute_bypass-mutant.log`. Mutated production bytes were
restored in `finally` blocks. Complete affected workspacecfg/localdocker/commands
package tests pass (`/tmp/whr-d143-f7-affected-green.log`), with affected vet,
formatting, bootstrap validation and diff checks passing. No full repository
rerun, race run, installation, standalone build or real-host operation was done.
Prior local full-check failures remain historical and predecessor-bound. Fresh
exact-head review/QA and publication remain with the coordinator; no author push.

## Superseded alias-normalization experiment

An intermediate change after `9f517bdb94c461a9308ec1889476a050abdd909a`
normalized aliases for the existing admitted keys. That experiment is superseded
by the canonical-key refusal engineering design recorded below. This section
retains historical execution evidence for the intermediate revision. The
[official Docker CLI parser](https://raw.githubusercontent.com/docker/cli/master/opts/mount.go)
was read on 2026-10-24 to identify the `source`/`src`, `target`/`dst`/`destination`
and `readonly`/`ro` cases. Main matched base
`45c269925295226851ea7268ae4a8cd8d29ecc6c` at that point, so no rebase occurred.

Test-first commit `817a7ad0` records actual RED on the old behavior: valid `src`,
`dst`, `destination`, `ro` and combined reordered options failed pin equality and
changed matching `in_sync` reports into undetermined reports. Evidence:
`/tmp/whr-d143-normalize-pins-red.log` and
`/tmp/whr-d143-normalize-report-red.log`.

Intermediate commit `1b1181a8` normalized lowercase key aliases before
assigned-value validation, duplicate detection, allowlist checking and relevance
classification. At that revision, assigned readonly/ro forms, unknown keys,
uppercase variants and equal-value canonical/alias or alias/alias collisions
were refused. Its tests required identical pins/reports for accepted aliases and
retained the undeclared aliased bundle and F7 POSIX target regressions. Those
alias-acceptance expectations are superseded by refusal tests in this correction.

Focused suites passed (`/tmp/whr-d143-normalize-focused-green.log`). Five bounded
mutations each produced a behavioral assertion failure:

- Disable `src` mapping: `/tmp/whr-d143-normalize-src_mapping-mutant.log`.
- Disable target alias mapping: `/tmp/whr-d143-normalize-target_mapping-mutant.log`.
- Disable `ro` mapping: `/tmp/whr-d143-normalize-ro_mapping-mutant.log`.
- Normalize after value checks, wrongly admitting `ro=`:
  `/tmp/whr-d143-normalize-late_value_normalization-mutant.log`.
- Normalize after duplicate detection, wrongly allowing alias overwrites:
  `/tmp/whr-d143-normalize-late_duplicate_normalization-mutant.log`.

All mutated bytes were restored in `finally` blocks. Complete affected
workspacecfg/localdocker/commands tests pass
(`/tmp/whr-d143-normalize-affected-green.log`), as do affected vet, formatting,
bootstrap validation and diff checks. The held-source descriptor protection,
clean absolute POSIX target guard, exit 68/report semantics and F6's separate
informational owner question remain unchanged. No duplicate full `make check`,
race run, installation, standalone build or real-host operation was done; prior
local qualification limitations are not relabeled passing. Fresh exact-head code
review and separate QA remain required before coordinator publication. No author
push was performed.

## Trailing target coverage gap (test-only continuation)

Explicit trailing-slash coverage was added on top of `ee75a559`.
Production already has the required clean absolute POSIX guard; this is a test
gap, not a new production fix. Fresh main still equals current base `45c26992`,
so no rebase was needed. Both `/opt/bdock/hidden/` and `/ordinary/` now appear in
the parser and report matrices and the default-handler CLI schema fixture. They
require exit 68, `cannot_determine`, `pins_unavailable` and a schema-valid report.

A temporary clean-guard bypass, with alias normalization retained, produced
actual historical/mutation RED in the new ordinary trailing-path assertions:
`/tmp/whr-d143-trailing-guard-bypass-red.log`. The bundle trailing path still
refuses through unmapped-bundle detection, so it alone would not prove validation
before relevance. This probe is not a claim that current guarded code failed;
production bytes were restored exactly in `finally` and have no diff.
Focused target/alias/parser/report/schema tests pass across the three affected
packages (`/tmp/whr-d143-trailing-focused-green.log`), as do affected vet,
formatting, bootstrap and diff checks. Exit behavior and all prior production
fixes remain unchanged. No full suite, build, installation, race run, real-host
operation or author push was performed; exact-head review/QA remain separate.

## Restored canonical-key refusal by engineering design

This correction starts from the verified clean
`0cc24b871b1d9b4f8d40dba9c3417d1047133754` and adds new commits without rebasing
or rewriting any existing commit. The engineering design accepts only canonical
mount keys. Aliases `src`, `dst`, `destination`, `ro` and every unknown key refuse
before relevance and produce `cannot_determine` / `pins_unavailable` / exit 68.
The intermediate normalization experiment above is superseded. Current documents
state the technical rule without attributing alias policy to an approval request.

Test-first commit `2a39f78a` produced actual RED against the normalization behavior:
alias equivalents of otherwise valid canonical mounts were accepted and reported
`in_sync`. Evidence: `/tmp/whr-d143-restore-refusal-pins-red.log` and
`/tmp/whr-d143-restore-refusal-report-red.log`. Tests now require refusal for each
alias and retain canonical key-order positive pins/report cases. The still-valid
unsupported-key, collision/value, source-identity, F2–F5, F7 and trailing-target
coverage remains. Revert-style commit `6fe6931a` removes only alias mapping;
absolute clean POSIX target validation and exit behavior remain unchanged.

Bounded key-allowlist and clean-target bypasses were caught; additionally, moving
key admission after relevance was caught by otherwise-inert ordinary mounts with
unsupported aliases. Evidence:
`/tmp/whr-d143-restore-refusal-unknown_key_bypass-mutant.log`,
`/tmp/whr-d143-restore-refusal-clean_target_bypass-mutant.log`, and
`/tmp/whr-d143-restore-refusal-ordinary-aliases-mutant.log`. All mutated bytes were
restored in `finally` blocks.

The complete affected packages ran once with `go test -json -count=1`:
workspacecfg passed 135 top-level tests / 639 subtests, localdocker 65 / 244, and
commands 111 / 43, with zero failures or skips. Raw JSON events and summary:
`/tmp/whr-d143-restore-refusal-affected.jsonl` and
`/tmp/whr-d143-restore-refusal-affected-summary.json`. Affected vet, formatting,
bootstrap validation and diff checks passed. No ignored feature-selection state
was observed. The coordinator owns the required full repository check on the
committed pristine candidate; no standalone build, installation, race run or
author push was performed. F6 remains unchanged, and fresh exact-head review/QA
are separate gates.

# Package D #143: bounded local bundle reference report

Owner-authorized bounded delivery on `origin/main` at
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

The owner requested continuation of
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

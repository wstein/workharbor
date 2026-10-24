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

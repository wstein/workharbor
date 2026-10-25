# T037 Claude settings acceptance reconciliation

Audited main baseline: `6d7d2b20dfde1e8678ec70cddc4644e646479260`,
2026-10-25 UTC. Scope: Feature 006 T037, FR-065 and SC-023. This record concerns
source acceptance of the existing implementation, not Claude Code execution.

## Existing implementation and audit

The baseline already implements the authored default through
`scaffold/claude_settings.go`, bounded no-follow discovery in
`claude_discover.go`, the shared proposal/apply transaction and wizard consent.
The [first-increment matrix](first-increment.md) records the prior delivery
through #200. The inspected behavior matches the approved
[FR-065 and SC-023 contract](../../../specs/006-whr-cli/spec.md): five exact
LF-terminated lines, absent-file creation, identical-file skip, existing-file
preservation without overwrite, whole-file replacement with path consent,
non-regular target preservation/refusal, and stale-preview checks.

No product implementation, schema, flag, dependency or workflow change was
justified. The implementation still writes only workspace configuration; this
work installs, authenticates and launches no native agent and does not access
owner home settings. Tasks/progress ticks remain reserved for the final wave.

## Acceptance changes

Existing public test IDs now cover 14 leaf cases:

- Absent settings with absent or existing `.claude`, plus identical settings:
  exact authored bytes, mode 0644, exactly one settings row and one create/reuse
  directory row, dry-run/apply diff parity, false `applied` for skips and all
  dry-run actions, and no-op rerun.
- Differing JSON, invalid JSON, empty and over-64-KiB settings, each with and
  without overwrite: bytes preserved or replaced entirely; private mode 0600
  retained; exactly one next step naming `attribution.sessionUrl: false`, the
  overwrite flag and whole-file replacement for each `left_unchanged`; the
  human output line; no extra agent warning.
- Settings appearing after the preview, with or without the overwrite flag:
  conflict 68 with the specific stale-preview reason in the typed JSON envelope
  and stderr, unapplied actions, concurrent-file preservation and no manifest.
- Unattended overwrite without `--yes`: typed needs-human 70 with the exact
  missing-consent reason and no workspace effects.

Dry runs and reruns compare file identity, `ModTime.Equal` and mode, in addition
to bytes. Default/current cases also check parent/root metadata; a fixed historic
mtime makes same-byte rewrite probes deterministic.

The existing Unix special-target test now covers five target kinds (settings
symlink, FIFO, directory, parent file and symlinked parent), each with/without
consent and in dry-run/apply mode: 20 leaf cases. It retains the discovery seam
that checks special targets are not opened. An unrequested edit is left unchanged
with one remediation/output row; a real parent is reused, and a non-directory
parent has no `.claude` row. Requested edits require the typed conflict path and
specific special-target reason. Protected targets and outside regular files keep
their bytes, identities, modes and mtimes; link destinations stay unchanged. A
FIFO is inspected only with `Lstat`, never read. Refusals and dry runs create no
manifest or directory mutation.

Existing scaffold stale, exclusive-create, tail-change and restore-bound cases
now assert typed paths and owning reasons instead of only exit 68. The interrupted
rollback case asserts its interruption reason and retains its restoration checks.
The wizard tests assert preconfirmed exact bytes, no settings prompt, one relevant
remediation when preserved, unchanged input data, and typed missing-consent and
special-target refusal reasons before prompts.

No top-level test ID was added: the existing explicit CI inventory remains
unchanged. Changes stay within the four scoped Claude settings test files.

## Executed checks

Environment: Go 1.27.2, Linux/arm64, umask 022. The full affected baseline packages
passed once before edits:

```sh
go test ./internal/scaffold ./internal/commands ./internal/wizard -count=1
```

Final restored-source focused check:

```sh
go test -json ./internal/scaffold ./internal/commands ./internal/wizard -run 'ClaudeSettings' -count=1 -timeout=90s
```

Exit 0: 14 top-level tests, 60 leaf cases (commands 14, scaffold 42, wizard 4),
zero skips and zero failures. `go vet` for those three packages, `gofmt -l` on
changed Go files, `git diff --check` and `python3 scripts/check_repo.py` passed.
The broad package suite was not repeated for individual mutation probes.

## Compiled mutation evidence

Seven Go overlays held outside the repository each compiled and failed for the
intended behavioral assertion (exit 1). No mutation touched a tracked product file.

| Mutation | Observed failure |
| --- | --- |
| Swap special-target reason for stale-preview reason | Unix special-target typed-reason assertion |
| Swap stale-preview reason for special-target reason | Public post-preview JSON/stderr reason assertion, both flag states |
| Replace exclusive-create failure wording with an unrelated refusal | Scaffold exclusive-create owning-reason assertion |
| Create settings with 0600 | Public absent-parent and reused-parent mode assertions |
| Force replacement and verification mode to 0644 | Public private-settings mode assertion |
| Rewrite identical bytes during dry-run | Public untouched-file identity/time/mode assertion |
| Rewrite identical bytes during a no-op rerun | Public rerun-file identity/time/mode assertion |

The restored production sources then passed the focused check above. Swapping
refusal reasons cannot satisfy the acceptance solely by returning the same 68.

## Remaining gates

The coordinator owns repository-wide `make check`, PR publication, hosted required
checks and independent review/readiness. This author claims neither independent
CLEAR nor task/feature completion. No standalone build was authorized or run.
No Git/network/container/native-agent call was added to init acceptance, and no
real-host/browser/macOS/native-agent acceptance was performed by this author.

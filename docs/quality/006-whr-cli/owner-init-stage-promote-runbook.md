# Owner host run: init, stage, promote and disposal

Status: planned acceptance procedure, with no host results recorded. Run after
#272's combined CLI/cleanup implementation is accepted and merged. This procedure
uses an already installed reviewed `whr` binary. Building, installing, tagging
and publishing are separate owner actions. Repeat on Linux Docker and macOS
with OrbStack; retain the exact binary/source revision for each result.

## Record the environment

Record OS/architecture, terminal and shell versions, Git, Docker client/server,
Dev Container CLI and Node versions, OrbStack version when present, selected
Docker endpoint, `whr --version`, and the implementation/review/CI links.
Use disposable repositories and a disposable primary chosen by the owner.
Record the primary ID and original home name/data before starting. Pause source
writers during every execution phase as the previews require.

Choose `WHR_SOURCE` as the host source directory matching that primary,
`WHR_PRIMARY` as its exact container name or full ID, and `WHR_NAME` and
`WHR_PROJECT_URL` as valid init inputs. These are owner-supplied shell variables,
not inferred values. Use the documented default private state location with
physical, nonsymlink path components. No host run is performed by this document.

## Init: flags and wizard

The following are commands to execute and record, not observed transcripts:

```sh
whr workspace init "$WHR_SOURCE" --name "$WHR_NAME" --project-url "$WHR_PROJECT_URL" --non-interactive --dry-run --json
whr workspace init "$WHR_SOURCE" --name "$WHR_NAME" --project-url "$WHR_PROJECT_URL" --non-interactive --yes --json
whr workspace validate "$WHR_SOURCE" --json
whr workspace init "$WHR_SOURCE" --non-interactive --yes --json
whr host doctor --json
```

Start with an admitted existing devcontainer. For multiple choices, explicitly
select its repository-relative path with `--devcontainer`, following the preview.
Record exit status, stdout JSON and stderr separately. Dry-run must preserve the
pre-run tree. After apply, validation must have no error; rerun must preserve the
result. Existing files require explicit per-path overwrite consent; this runbook
does not add overwrite flags automatically.

In a second equivalent disposable checkout, run `whr workspace init` on a real
terminal, enter the equivalent settings, review and confirm. Compare output files
byte for byte with the flag-driven result; record the supplied answers and the
comparison. Also cancel a wizard run and decline an overwrite, recording no
change. Check selected devcontainer bytes and modes before/after reuse.

## Stage and choose a verification policy

```sh
whr ls --local --json
whr workspace stage --local "$WHR_PRIMARY" --source "$WHR_SOURCE" --dry-run --json
```

Inspect the full preview, including hooks, mounts, image, fresh candidate home,
pause-writers condition and fingerprint limitations. Copy its exact fingerprint
to `WHR_FINGERPRINT` only after explicit trust review. Source trust and normal
mutation confirmation are separate. If the candidate has no declared Docker
healthcheck, choose the explicit workspace smoke path with `--smoke` in preview
and apply; the existing Anchorage smoke script is passed over stdin. Do not run
the host smoke orchestrator or modify Anchorage files for this check.

```sh
whr workspace stage --local "$WHR_PRIMARY" --source "$WHR_SOURCE" --trust "$WHR_FINGERPRINT" --yes --json
whr ls --local --json
```

For the explicit-smoke policy, add `--smoke` to both stage commands above. Record
the resulting full candidate ID/name as `WHR_CANDIDATE`, its
`whr-stage-home-<id>` home and all disclosed private paths. Successful stage keeps
its usable candidate. Confirm writable normal Git history and the project hooks
required by the accepted execution profile. Record verification status; an
unverified candidate is not a verified promotion result.

## Remove, then independently stage and promote

For a candidate removal run:

```sh
whr rm --local "$WHR_CANDIDATE" --dry-run --json
whr rm --local "$WHR_CANDIDATE" --yes --json
```

Record that container, fresh home, private clone/results and candidate records
are absent. Normal confirmation covers loss of unfetched candidate work. If the
owner wants that home preserved, back it up beforehand using Docker tools.
There is no keep flag or built-in backup. Primary/home data and the protected
`anchorage-home-*` guard must retain their documented behavior.

Stage another candidate for the promotion run. Review its commits/results, then:

```sh
whr workspace promote --local "$WHR_PRIMARY" --dry-run --json
whr workspace promote --local "$WHR_PRIMARY" --yes --json
```

Promote selects the original primary, not the candidate. It uses normal
confirmation; there are no promote `--volumes` or `--force` flags. Record expected
result refs/OIDs in the original repository before candidate-clone disposal.
Confirm the replacement uses the original home and preserves its data. Confirm
all candidate artifacts are absent. Replacement ends existing shell/exec panes;
trusted project hooks may write the original workspace, as previewed.

## Failure and incomplete-disposal records

Record the original cause, exit status, effects, residual inventory and recovery
instructions for failed stage and failed promote. Exercise failure injection
only through an explicitly approved disposable host scenario; hermetic fault
fixtures remain source evidence. A verified rollback must restore the original
and dispose of the failed replacement/candidate artifacts. Uncertain recovery
must report its actual retained inventory rather than imply successful rollback.

If cleanup is incomplete, records must remain as reported residuals until the
container, home and private trees are absent. Record an ordinary confirmed
remove retry against the receipt-bound candidate name/full ID and its final
inventory. Receipts/journals are deleted last. Do not manually delete the private
records to make an acceptance run appear clean.

## Attach results without closing gates implicitly

For each scenario attach commands, statuses, sanitized JSON/stderr, before/after
file/resource inventories and differences from expected behavior. Include
stage/remove/promote, dry-run and declined-consent outcomes. Never attach tokens
or credential contents. A result can be pass, fail or blocked with the exact
reason; missing tools and unexecuted host cases are not passes.

The [init audit](init-handoff-audit-20261026.md) keeps the remaining SC-017 through
SC-020 evidence and T034 separate. Completion, real shell-completion transcripts,
offline whole-increment evidence and Feature 010 pane gates require their own
records and approval; this runbook ticks none of them.

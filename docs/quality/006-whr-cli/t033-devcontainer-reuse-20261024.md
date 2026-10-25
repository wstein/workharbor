# T033 devcontainer reuse evidence

Measured on 2026-10-24 against base
`86008fab4b0131187c3d3b244fef94a48063b48b`, with the original test version. The current test commit is listed in the PR
history. Product sources are unchanged.
This record adds applied preservation evidence for T033, FR-049 and FR-046a;
it does not close the feature, T030 or its remaining SC-018 safety evidence.

## Existing implementation and acceptance bindings

Paths below are relative to `apps/whr/internal/`.

| Requirement | Existing source and test |
| --- | --- |
| Select an existing validated devcontainer and update the manifest path | `scaffold/selection.go`, `scaffold/proposal.go`, `commands/init_options.go`; `TestInitDevcontainerSelectionValidated` |
| Reuse JSONC and PATH bytes without modification or startup execution | `TestInitDevcontainerNeverModified` in `commands/devcontainer_test.go` |
| Refuse unsafe relative paths and leaf/ancestor symlinks | `TestInitDevcontainerPathTraversalRefused`, `TestInitDevcontainerSymlinkRefused` |
| Display the contract catalog as unsupported; refuse templates | `scaffold/starters.go`, `scaffold/options.go`; `TestStartersMatchContract`, `TestTemplateRefusedWhileUnsupported`; `TestInitTemplateRefused` asserts exit 64 |
| Preserve existing JSONC/PATH and unrelated startup settings | `TestInitExistingDevcontainerPathPreserved`, `TestInitExistingShellSettingsPreserved` in `scaffold/discover_test.go` |

All six literal T033 test IDs already occur in
`docs/development/ci-acceptance.json`; this change does not edit that inventory.
The existing startup-file test only discovered and built a dry-run proposal.
The new `TestInitDevcontainerReusePreservesAppliedStartupFiles` exercises the
public command's actual application, independently of the wizard.

## Added coverage

Both new-manifest and replacement-manifest cases execute dry-run, write and
rerun, each with LF JSONC and CRLF JSONC without a final newline.
The replacement begins with an existing v1 manifest and supplies
`--overwrite=workharbor.json`; the selected custom path contains a space.
After every mode the test checks bytes, full mode, filesystem identity
(`os.SameFile`) and modification time for both the selected custom JSONC file
and the original default devcontainer, plus `.bashrc`, `.profile`, `.zshrc`
and `.config/fish/config.fish`. Fixtures include custom PATH and GOBIN settings.
The `.bashrc` fixture is explicitly set to mode 0750 before observation, so
executable-bit preservation is checked alongside the other 0600 fixtures.
A fixed old modification timestamp makes modification-time changes observable.

The test also requires exactly one selected-file JSON action with `action: reuse`
and `applied: false`, absence of applied actions
on dry-run/rerun, no dry-run manifest change, and the written manifest's selected
path. An absolute startup-hook marker must remain absent after every mode.
These are configuration-preservation checks; no shell or container is launched.

## Measured checks

Commands ran from the repository root with Go `go1.27.2 linux/arm64` on
Linux `7.2.9-orbstack-00391-g2b298d05fbd4`.

| Command | Result |
| --- | --- |
| `go test ./apps/whr/internal/commands -run '^TestInitDevcontainerReusePreservesAppliedStartupFiles$' -count=1` | Exit 0; both manifest variants passed. |
| `go test ./apps/whr/internal/commands -count=1` | Exit 0; complete affected commands package, run once (5.714 seconds). |
| `go test ./apps/whr/internal/scaffold -run '^(TestInitDevcontainerSelectionValidated\|TestStartersMatchContract\|TestTemplateRefusedWhileUnsupported\|TestInitExistingDevcontainerPathPreserved\|TestInitExistingShellSettingsPreserved)$' -count=1` | Exit 0. |
| `go vet ./apps/whr/internal/commands` | Exit 0. |
| `python3 -I -B scripts/check_repo.py` | Exit 0; repository bootstrap checks passed, product runtime not evaluated. |
| `gofmt -l apps/whr/internal/commands/devcontainer_reuse_acceptance_test.go` | Exit 0; no output. |
| `typos apps/whr/internal/commands/devcontainer_reuse_acceptance_test.go docs/quality/006-whr-cli/t033-devcontainer-reuse-20261024.md` | Exit 0; no findings. |
| `git diff --check` | Exit 0. |

## Fresh compiled negative controls

These are new mutation results, not historical failing-first implementation
proof. Before the unchanged-product focused GREEN run, temporary Go source
overlays replaced only the `Proposal.Apply` wrapper in `scaffold/apply.go`.
The wrapper first called `p.apply(ctx, nil)`, then, only for a successful
non-dry-run result, called:

```go
os.WriteFile(path.Join(result.Directory, target), []byte("mutated\n"), 0600)
```

It propagated a mutation write error, otherwise returned the original result.
Two separately compiled overlays set `target` to `.bashrc` and
`custom config/devcontainer.json`, respectively. Product checkout files were
never modified. Each ran:

```text
go test -overlay=/tmp/whr-t033-<control>-overlay.json ./apps/whr/internal/commands -run '^TestInitDevcontainerReusePreservesAppliedStartupFiles$' -count=1
```

For `control=shell` and `control=selected`, exit 1 occurred in both manifest
variants at the new assertion: `write changed preserved file .bashrc` and
`write changed preserved file custom config/devcontainer.json`, respectively
(`bytes, mode, identity or mtime`). Both controls compiled and reached test
assertions; these were not setup or import failures. The unchanged sources then
passed the focused test and complete commands package listed above.

## Boundaries

This Linux-only measurement does not supply macOS, browser, container-provisioning,
network-isolation, physical ENOSPC or full signal-matrix acceptance. Access time
is excluded because observing file bytes may update it; modification time is
checked. Shell startup execution, future PATH generation/activation under
SC-014a, T035 resolver changes and wizard acceptance are outside this scope.
T030 remains open under its separate evidence record. No task checkbox changes,
standalone builds, installs, runtime activation, pushes or merges accompany these
author measurements. The coordinator owns `make check`, independent review and
PR readiness gates; their results are not claimed here.

## Integration refresh

The two owned commits were rebased onto
`79d2f4eb336514c27b93d220edf7f5c6efe49b0d` after the owner merged PRs #248
and #252. Before this citation refresh, `git range-diff` showed both commits
unchanged (`=`). The original measured base above remains historical evidence;
the reachable test-commit citation reflects the rebase. Source checks, spelling
and whitespace checks passed on the refreshed tree. Tests were not repeated:
the newly merged commits change tests and documentation, not product sources.
Independent review must cover the final exact head before publication.

## Review corrections and remaining evidence gaps

The first review covered historical candidate
`933872920d60d7a176b5f301137c3c5143aae04c` ([review](https://github.com/wstein/workharbor/pull/255#pullrequestreview-5640394770)).
Before these corrections, all three owned commits were purely rebased from
`79d2f4eb336514c27b93d220edf7f5c6efe49b0d` onto
`d18ccc87926084ca3f51d08774f7a627606b255e`; `git range-diff` showed `=`
for all three commits. Final exact-head review and independent QA remain pending
at this author checkpoint; the historical review is not approval of this tree.

The review's M6 probe identified an open unsafe-path refusal-reason gap:
the older `TestInitDevcontainerPathTraversalRefused` and
`TestInitDevcontainerSymlinkRefused` assert exit codes only. In particular, an
unrelated downstream manifest validation can preserve exit 64 while the
relative-path rule is relaxed, and M6 survived with a message naming unrelated
flags. Those tests therefore did not prove the intended refusal reason or
complete path-safety acceptance. The preservation-only change left that gap
open during parallel authoring and deferred reason-specific assertions to a
follow-up. It did not claim all T033 acceptance was complete. This corrects
the historical M6 description per
[review F7](https://github.com/wstein/workharbor/pull/255#pullrequestreview-5640605892).

On 2026-10-25, the follow-up strengthened both older tests: traversal must
report `--devcontainer needs a safe relative file path` with exit 64;
leaf/ancestor symlinks must report the unsafe-target discovery reason with
exit 68. A compiled overlay admitting only `./custom.json` through
`relativePath` survives the old traversal test, then fails the new assertion:
the command still exits 64, but reports downstream manifest validation naming
the wrong flags. Unchanged production source passes both tests. This closes
the specific refusal-reason gap without claiming full T033 acceptance.

The portable metadata observations also have a deliberate limit: an in-place
write of identical bytes that preserves mode and inode and restores mtime is
invisible to this test. It does not inspect platform-specific ctime or audit all
write system calls, so byte/metadata equality is not proof that no write occurred.

For the selected-action correction, a freshly compiled Go overlay removed only
the selected custom path from `result.Actions` immediately before returning the
JSON result in `commands/init.go`. Human preview and filesystem behavior were
left intact. Command:

```text
go test -overlay=/tmp/whr-t033-json-row-overlay.json ./apps/whr/internal/commands -run '^TestInitDevcontainerReusePreservesAppliedStartupFiles$' -count=1
```

Exit 1 reached the new assertion in all four variants:
`dry: expected exactly one selected-file JSON action, got 0`. This is a compiled
assertion failure, not a setup failure. The same focused command without the
overlay then passed all four variants on unchanged product sources (exit 0).
`go vet ./apps/whr/internal/commands`, source checks, spelling, formatting and
whitespace checks passed after the corrections. The full commands suite was not
repeated for this bounded delta; its earlier run remains historical evidence.

The new preservation test's explicit CI inventory binding is a follow-up after
PR #251 merges and releases `docs/development/ci-acceptance.json`. The complete
commands package currently discovers the test, but this record does not claim
that the new literal ID has an explicit acceptance-inventory binding. No inventory
file is changed here.

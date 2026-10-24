# T033 devcontainer reuse evidence

Measured on 2026-10-24 against base
`86008fab4b0131187c3d3b244fef94a48063b48b`, with test commit
`b3b272460851a23b6b4ce57d2be0391573958fab`. Product sources are unchanged.
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

Both `new_manifest` and `replace_manifest` execute dry-run, write and rerun.
The replacement begins with an existing v1 manifest and supplies
`--overwrite=workharbor.json`; the selected custom path contains a space.
After every mode the test checks bytes, full mode, filesystem identity
(`os.SameFile`) and modification time for both the selected custom JSONC file
and the original default devcontainer, plus `.bashrc`, `.profile`, `.zshrc`
and `.config/fish/config.fish`. Fixtures include custom PATH and GOBIN settings.
A fixed old modification timestamp makes metadata rewrites observable.

The test also checks the selected file's reuse action, absence of applied actions
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

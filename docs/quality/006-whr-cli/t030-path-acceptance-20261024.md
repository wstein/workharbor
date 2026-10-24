# T030 path retention and reuse acceptance

This separate acceptance unit adds five literal T030 IDs as dedicated behavioral
assertions in `apps/whr/internal/scaffold/init_acceptance_paths_test.go`.
It changes no existing source or tests. Base:
`f2d96805383410f401826877b634d2c03780e977`; test commit:
`a3a1b150df17ae44518a50e5414d7ddd78558378`. Measured on 2026-10-24 with Go
`go1.27.2 linux/arm64`, using temporary filesystem fixtures.

## Dedicated assertions

| Test | Cases and observed contract |
| --- | --- |
| `TestInitSymlinkTarget` | Two targets: workharbor.json and the existing devcontainer. Even with yes and overwrite consent naming that target, option-aware discovery refuses 68. The original symlink identity and destination survive; the foreign file retains bytes, identity, mode and modification time. |
| `TestInitExistingDirectoriesReused` | Successful apply and injected ENOSPC rollback after Claude settings creation both preserve existing .devcontainer, projects and .claude directory identities, modes and owner sentinel files. Each directory is proposed as unapplied reuse. Complete rollback removes newly created manifest, placeholder, agent workflow, settings and journal, with no remaining recovery paths. |
| `TestInitIdenticalFileSkipExisting` | After actual initialization, explicitly requesting the already persisted fish setting produces identical canonical manifest bytes. Both without and with overwrite permission, preview/result use skip_existing without a diff; no action is applied, and manifest bytes, identity, mode and modification time survive. |
| `TestInitBundlesLeftUnchanged` | An on-disk v1 manifest carrying a nonempty unsupported bundle list refuses 68 with and without a requested manifest edit and overwrite consent. The full manifest bytes, identity, mode and modification time remain; no projects, agent workflow, Claude directory or journal is created. |
| `TestInitOverwriteTempFilePrefixDetected` | Root orphan present before discovery; root orphan created after a consented overwrite preview; nested devcontainer orphan present before discovery. All three refuse 68 naming the recovery prefix, preserving both manifest and orphan bytes, identities, modes and modification times. |

These are five tests with 11 table cases, not aliases for existing combined
tests. Existing default console, wizard, devcontainer, agent workflow and Claude
settings behavior is retained.

## Regression sensitivity

Five independent temporary Go source overlays produced assertion failures
(exit 1), one for each dedicated test. The checkout's product files stayed
unchanged. Reproduction definitions:

| Product source under apps/whr/internal | Single mutation | Failing test, prefixed TestInit |
| --- | --- | --- |
| scaffold/discover.go | In observe, change the unsafe target branch for `err != nil || info.Mode()&fs.ModeSymlink != 0` from `return discoveryConflict()` to `return nil`. | SymlinkTarget |
| scaffold/proposal.go | Change existing-directory `action = "reuse"` to `action = "create"`. | ExistingDirectoriesReused |
| scaffold/proposal.go | Change existing-file `action = "skip_existing"` to `action = "overwrite"`. | IdenticalFileSkipExisting |
| workspacecfg/manifest.go | Remove the `manifest.bundles_deferred` finding for a nonempty bundle array. | BundlesLeftUnchanged |
| scaffold/discover.go | Change scanTemps' `strings.HasPrefix(e.Name(), ".whr-init-tmp-")` predicate to match `.unrecognized-orphan-` instead. | OverwriteTempFilePrefixDetected |

Copy the specified source to a temporary file, make the single replacement,
map the absolute original path to the temporary file with Go's JSON
`{"Replace": {"original": "replacement"}}` overlay, and run from apps/whr:
`go test -count=1 -overlay=<overlay.json> ./internal/scaffold -run
'^TestInit<name>$'`. These mutants are regression probes of existing behavior,
not newly discovered implementation failures. Local overlay inputs/logs live
under `/tmp/whr-t030-path-mutants-h8gxi8re`; the definitions above are durable.
An initial orphan mutant was compile-invalid due to an unused loop variable;
it was corrected to the changed-prefix predicate before the recorded assertion
failure and is not counted as RED evidence.

## Measured boundary observation

A separate temporary test overlay explored an orphan created inside
.devcontainer after preview. Current Apply succeeded and retained that orphan;
the next Discover refused 68 with the recovery-prefix diagnostic. This was
measured by `TestPathNestedOrphanObservation`, a local probe excluded from the
committed acceptance test file; its source, overlay and log are in the same
local directory above. Apply scans the root before mutation and does not
rescan every nested directory for newly introduced orphan names. The spec's
FR-057(c)/(e) recovery wording requires the next run to refuse leftovers and records
the filesystem race limitation; this observation does not introduce a new
post-preview nested-scan requirement or claim broader protection. The permanent
orphan assertions cover exactly the three boundaries listed above.

## Checks and remaining limits

From apps/whr with `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`:

```sh
go test -count=1 ./internal/scaffold -run '^TestInit(SymlinkTarget|ExistingDirectoriesReused|IdenticalFileSkipExisting|BundlesLeftUnchanged|OverwriteTempFilePrefixDetected|ApplyHostileTargetsRefused|ApplyNoOpAndDryRunPreserveFiles|FileChangedSincePreview)$'
go vet ./internal/scaffold
```

Both passed, including three existing adjacent safety regressions. Repository
`python3 scripts/check_repo.py` passed (bootstrap source checks, product runtime
not evaluated). `gofmt -l`, targeted `typos` and `git diff --check` passed.

No product changes, task/progress ticks, full suite, race, standalone build,
installation, runtime network, macOS or full make execution is included.
Independent review, pristine full gates and publication remain separate.
Consent, custom-project gitkeep, cancellation/signals, no-Git/no-network and
other remaining T030 acceptance units are not completed by these five tests.

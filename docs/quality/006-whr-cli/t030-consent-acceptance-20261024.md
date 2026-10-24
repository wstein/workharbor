# T030 manifest preservation and consent acceptance

This bounded acceptance unit adds five literal T030 test IDs with dedicated
behavioral assertions in
`apps/whr/internal/scaffold/init_acceptance_consent_test.go`. It does not wrap
existing tests or change product behavior. Base:
`f2d96805383410f401826877b634d2c03780e977`; test commit:
`e268952388766f5ddae631909414f8783a2e9112`. Measured on 2026-10-24 with Go
`go1.27.2 linux/arm64`.

## Acceptance assertions

| Dedicated test | Assertions |
| --- | --- |
| `TestInitNeverOverwritesWithoutOverwriteFlag` | `--yes`, no overwrite path, or consent naming only AGENTS.md or the devcontainer cannot authorize replacing workharbor.json. Refusal returns 70 naming the missing manifest path, yields no proposal, and preserves discovered target bytes, identity, mode and modification time. |
| `TestInitOverwriteRequiresYesAndPath` | All four unattended yes/path combinations are exercised. Both are necessary; the authorized branch previews an unapplied diff, applies exactly the retained manifest bytes, records an applied overwrite, validates, and retains explicit zsh/herdr/shortcuts-off settings. |
| `TestInitExistingManifestPreservedWithoutRequestedEdit` | Both v1 and v2 manifests retain deliberately noncanonical bytes, identity, mode and modification time through real Apply, with and without overwrite permission alone. The manifest action remains skip_existing, unapplied, without a diff. |
| `TestInitManifestEditsNeedOverwrite` | Each of name, human shell, console mode, shortcuts, start directory and project URL needs manifest-path consent even in dry-run. Consented previews carry the requested value and overwrite diff; preview construction preserves the source tree. |
| `TestInitNonInteractiveWithoutYes` | Both a fresh target and existing v2 target refuse unattended application without yes, exit 70 naming yes, without a proposal. Their dry-run counterparts work without yes and Apply reports no applied actions or target changes. |

The five tests contain 21 table cases. They exercise the public Build/Apply
behavior at the scaffold boundary using existing filesystem fixture helpers.
The current plain default, explicit herdr retention, agent workflow proposal,
Claude settings proposal and existing devcontainer reuse remain enabled. These
are source acceptance tests, not executions of a separately built whr binary.

## Explicit regression RED evidence

Before the final green run, four independent temporary Go source overlays were
executed. The checkout's product files were never edited. Each overlay changed
one existing predicate; each invocation exited 1 with assertion failures rather
than a compiler failure. Overlay inputs and logs are local, non-durable evidence
under `/tmp/whr-t030-consent-final-mutants-z24e0tnc`; the mutation definitions and
observed failures are retained below for reproduction.

| Overlay mutation | Observed failing dedicated tests |
| --- | --- |
| proposal.go: replace `if !o.permits(name) {` with `if false {` | NeverOverwritesWithoutOverwriteFlag, OverwriteRequiresYesAndPath, ManifestEditsNeedOverwrite |
| options.go: replace `if o.NonInteractive && !o.Yes && !o.DryRun {` with `if false {` | OverwriteRequiresYesAndPath, NonInteractiveWithoutYes |
| proposal.go: add `&& !o.permits("workharbor.json")` to the existing `exists && !o.requestsEdit() && !selectionChanged` retention condition | ExistingManifestPreservedWithoutRequestedEdit (both v1 and v2 with permission only) |
| options.go: replace the return expression of `Options.requestsEdit` with `return false` | ManifestEditsNeedOverwrite |

Test names in this table carry the `TestInit` prefix. To reproduce, copy only the
specified product source to a temporary file, make the single replacement, map
its absolute original path to that temporary file with Go's JSON
`{"Replace": {"original": "replacement"}}` overlay, then run the relevant
anchored test selection with `go test -count=1 -overlay=<overlay.json>
./internal/scaffold -run <selection>` from apps/whr. Do not retain the mutation
in the checkout. These RED results demonstrate regression sensitivity of tests
for existing accepted behavior; they do not claim a newly discovered product
bug or an absent-implementation RED phase.

## Green checks and limits

From `apps/whr`, with `GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off`:

```sh
go test -count=1 ./internal/scaffold -run '^TestInit(NeverOverwritesWithoutOverwriteFlag|OverwriteRequiresYesAndPath|ExistingManifestPreservedWithoutRequestedEdit|ManifestEditsNeedOverwrite|NonInteractiveWithoutYes|PreservesExistingSettings|ExistingShellSettingsPreserved|ApplyNoOpAndDryRunPreserveFiles)$'
go vet ./internal/scaffold
```

Both passed. The test selection includes the five new tests and three existing
adjacent preservation regressions. `gofmt -l` reported no new-file differences;
`git diff --check` passed. There was no full-suite, race, standalone build,
installation, runtime network, namespace-denial, host/browser, macOS or full
make run in this acceptance unit. Repository/application gates and independent
exact-head review remain separate coordinator/CI responsibilities.

Other T030 literal IDs, signal/cancellation coverage, no-Git/no-network evidence,
custom-project gitkeep acceptance, output/exit-code mappings and inherited
wave-boundary wording are outside this unit. In particular, the historic
`TestInitDevcontainerFlagNotInRelease` requirement is superseded by the already
accepted devcontainer-selection implementation; no false refusal is restored.
T030/T034 completion and task/progress ticks are not claimed or changed here.

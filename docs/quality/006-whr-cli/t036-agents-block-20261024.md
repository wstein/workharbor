# T036 agent workflow block acceptance reconciliation

Audited implementation baseline: `3f899fd7391a5038b1185295e52ece87afeb2da7`
(main). Audit and source measurements: 2026-10-24 UTC; evidence assembled
2026-10-25 UTC (the filename retains the request date). Scope: Feature 006 T036, FR-064 and SC-022; this is
acceptance reconciliation of existing source, not a new block implementation.

## Audit and retained behavior

The baseline already ships `scaffold/agents_workflow.txt`, marker classification
and action planning in `agents_block.go`, bounded no-follow discovery, proposal
and transactional apply integration, wizard path consent, command flags, JSON
actions and the always-present warning array. The generic template covers the
FR-064 workflow subjects without Anchorage paths or vendor-specific model names.
No implementation, template, command-table or output-schema change was justified.
The existing first-increment evidence identified this reconciliation for
[#131](https://github.com/wstein/workharbor/issues/131).

The existing scaffold SC-022 matrix exercises classification and consent. Existing
`TestAgentsBlockTemplateIntegrity` checks that the shipped body has no marker-like
line. `TestInitAgentsSkipNoReadAndRefusedKinds` and
`TestInitAgentsFIFOAndReplacedFIFORefused` cover symlink, FIFO and substituted-FIFO
refusal and opt-out. Existing stale-preview, retained-output, rollback and read-only
preservation tests remain intact. `TestWizardPTYAgentsConsentAndRefusal` exercises
Linux native PTY decline, unoffered refusal and flag-preconfirmed refusal. Wizard
unit tests also cover lower/same/higher intact templates and path consent.

## Additional public acceptance evidence

`TestInitPublicAgentsActionsAndWarningsValidate` now runs 23 regular-file cases,
each with and without `--overwrite AGENTS.md` (46 cases): absent and empty files;
missing blocks, including BOM/CRLF outside bytes; current; lower/same/higher intact
templates; modified bodies at current/lower/higher versions; oversized input;
oversized malformed input; an oversized append result; duplicate, unclosed,
orphan, nested and out-of-order markers; CRLF-converted, indented, BOM-prefixed
and fenced marker-like lines.

Each successful case checks public dry-run, apply and rerun: schema-valid JSON,
exactly one AGENTS.md action, correct `applied`, the always-present warning array
with exact code/path and stderr diagnostic, dry-run/apply diff equality, exact
expected written bytes and a no-op rerun. Replacement expectations preserve the
BOM/CRLF prefix and suffix independently of the retained proposal. Dry runs and
refused edits preserve the original file and create no manifest. Refused edits
return 68. The opt-out test now starts from an existing malformed AGENTS.md,
checks byte preservation and no action/warning, and retains the conflicting-flag
usage error 64. The marker-like unit table also checks edit refusal 68 for every
row and rejects empty, signed, fractional and nonnumeric template versions.

No new top-level Go tests were added, so the explicit CI inventory is unchanged.
Task/progress checkboxes are reserved for the last wave PR and remain untouched.

## Executed checks and mutation proof

Environment: Go 1.27.2, Linux/arm64, `umask 022`. The complete affected baseline
packages were run once before edits:

```sh
go test ./internal/scaffold ./internal/commands ./internal/wizard ./cmd/whr -count=1
```

All four passed, including Linux native PTY cases. After the test changes:

```sh
go test ./internal/commands -run '^TestInitPublic(AgentsActionsAndWarningsValidate|NoAgentsBlockConflictAndNoEntry)$' -count=1
go test ./internal/scaffold -run '^TestAgentsBlockRejectsMarkerLikeLines$' -count=1
```

Both passed (exit 0). `gofmt` and `git diff --check` passed. Two compiled Go overlay
mutations, kept outside the repository, each produced the intended test failure:

| Mutation | Focused public case | Observed result |
| --- | --- | --- |
| Limit modified-body detection to the shipped version, losing hash precedence for foreign versions | `lower_modified/edit=false`, `higher_modified/edit=false` | Exit 1: wrong `.different_template` warning instead of `.modified` |
| Replace the preserved suffix after a managed block with `lost suffix` | `lower_intact/edit=true` | Exit 1: written bytes differ from the specified outcome |

The unmodified production sources then passed the focused checks above. These are
behavioral failures after successful compilation, not setup failures. No mutation
was applied to a tracked product file.

## Remaining gates

Coordinator-owned independent exact-head review, separate QA, `make check`, hosted
required checks and PR readiness remain separate gates. No task completion or
issue closure is claimed here. A standalone build was not authorized or run; Go
test compilation is source-test evidence only. No container, network, install,
real-host/browser/IDE or macOS PTY acceptance was performed by this author. T036's
bounded source scope adds no product runtime operation.

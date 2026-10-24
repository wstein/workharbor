# Feature 006 first-increment evidence (WP0)

Inspected source baseline: `628bc4ded0f96ed0ffebc501eae833fe4e2d3e5f`
(main after owner merges #223/#224), 2026-10-24. This is the bounded
reconciliation requested in [#145](https://github.com/wstein/workharbor/issues/145),
following #145's request to reconcile before lifecycle work. It does not replace
Feature 006 requirements or approve new behavior.

## Task matrix

All T025–T039 checkboxes were unchecked at this baseline. “Present” below means
source and named regression evidence, not complete task/feature acceptance. Paths
are relative to `apps/whr/internal/` unless stated otherwise. Named tests exist
at the baseline; the complete whr Go suite was rerun there successfully.

| Task | Existing source | Named evidence | Remaining boundary / next action |
| --- | --- | --- | --- |
| T025 | Present: commands, output, exitcode, terminal, version | TestCommandTableMatchesContract; TestJSONCommandFieldIsCanonical | Reuse the existing foundation; later commands extend current contracts coherently. Delivered by #121, corrected by #175; no replacement CLI. |
| T026 | Present: workspacecfg, commands/validate | TestValidateGitSubcommandsAreTheSevenVectors; TestManifestProjectPathMinimumCountsUnicodeCharacters; TestValidateRootSymlinkAndLinkTargetsStayHidden | Reuse validator. #149/#172 contain corrections relevant to still-open #147; issue closure needs its own acceptance reconciliation. |
| T027 | Present: doctor and commands/doctor | TestDoctorNeverPassesUnmeasured; TestDoctorConfigValidEqualsValidate | Local checks exist (#124/#187); remote/control-plane checks and real-host findings remain separate. |
| T028 | Present: workspacecfg and contracts/workspace schema/vectors | TestManifestVectors; TestManifestSchemaVersionSpellings | Reuse schema v2 (#122/#148). P3 changed only the approved effective console default, retaining explicit choices. |
| T029 | Present: resources and shared resource vectors | TestCPUVectors; TestMemoryVectors; TestResolutionVectors | Library exists (#120); do not imply workspace.create is implemented. |
| T030 | Present: scaffold and commands/init | TestInitApplyCreatesAndValidates; TestInitFailureInjectionRollback; TestInitApplyNoOpAndDryRunPreserveFiles | Preview/transactional source exists (#177–#179); source evidence is distinct from all T034 acceptance. |
| T031 | Present: ptytest | TestPTYDrivesPromptAndCapturesTranscript; TestPTYChildEnvironmentIsExactAndIsolated | Reuse native PTY harness (#123); Linux tests do not establish notebook/SSH behavior. |
| T032 | Present: wizard; cmd/whr native PTY tests | TestWizardScriptedAnswersMatchFlags; TestWizardBackAndReview; TestWizardPTYHappyPath | Already integrated in accepted init source; do not recreate the wizard because an earlier stacked PR closed without merge. |
| T033 | Present: scaffold/selection; commands/devcontainer_test.go | TestInitDevcontainerNeverModified; TestInitDevcontainerPathTraversalRefused; TestWizardPTYDevcontainerSelection | Selected devcontainer reuse exists; real VS Code/IDEA attach acceptance remains separate. |
| T034 | Partial: commands/acceptance tests and source acceptance composition #193 | TestFirstIncrementInitOfflineSource; TestFirstIncrementCommandProcess; TestCompletionExecutesInShells | Preserve actual skip/tool/native-platform results; no complete real-host or installable-pilot acceptance inferred. |
| T035 | Not complete: host-agent resolver still admits only exact v1 fields | resolver.go schema_version check; not covered by the whr-only rerun | Separate owner-gated resolver work; do not silently broaden accepted provider inputs for lifecycle/drift. |
| T036 | Present: scaffold/agents_* and generic shipped agents_workflow.txt | TestInitAgentsSC022Table; TestAgentsBlockTemplateIntegrity; TestInitPublicAgentsActionsAndWarningsValidate | #131 is stale as an implementation request; reconcile its acceptance before closing it. No host-specific template rewrite. |
| T037 | Present: scaffold/claude_settings and commands/init_claude_test | TestInitClaudeSettingsExactDefault; TestInitClaudeSettingsCases | Implemented through #200; preserve existing user settings and per-file consent. |
| T038 | Partial: localdocker discovery/filter/endpoint, commands/local_ls | TestLocalLsListsLabelledContainersAndVolumes; TestLocalLsEscapesHostileMetadata; TestLocalLsNeverMutatesOrContactsControlPlane | #220 delivered FR-066. Local rm still refuses 64; safe removal and FR-068 staging/promotion remain unbuilt. No SC-024 completion. |
| T039 | Absent: no drift command/result schema or comparison engine | No TestBundleDrift* tests at baseline | Implement after accepted T038; preserve cannot_determine as distinct from known drift. #143 tracks it. |

The verifying revision for every whr rerun in this table is the baseline above.
Merge links are provenance, not substitutions for current checks. Existing
checkboxes are retained: the source rerun does not establish every acceptance
criterion or approval required by those task texts.

## Checks and external evidence

- `go -C apps/whr test ./... -count=1`: exit 0, including Linux native PTY tests.
- `python3 scripts/check_repo.py`: passes (product runtime not evaluated).
- Initial pristine `make check` attempt: interrupted at the explicit five-second
  bound (124), not passed. Local Python 3.13 is below the 3.14 development floor;
  Mix is unavailable. Required GitHub CI on the published head remains mandatory.
- No standalone builds, real Docker, host mutations, image activation, installs,
  provider credentials or live IDE/browser/SSH acceptance were performed.
- The GitHub connector returned 404 for Anchorage #14, but direct
  `gh issue view 14 -R wstein/anchorage-workspace` succeeded on 2026-10-24:
  [issue #14](https://github.com/wstein/anchorage-workspace/issues/14) is OPEN,
  with zero task-list items and zero comments at the read.
  It concerns a separately authorized real-Docker check of Anchorage's existing
  devcontainer-rm/clean and FORCE=1 primary-volume protection (follow-up to #12),
  not an unimplemented whr prerequisite or Feature 010 SC-009. No real test or
  issue change was performed. T038/T039 offline work is not blocked by it;
  Feature 010 SC-009 independently remains owner-operated and UNVERIFIED.

## Next delivery sequence

1. Reuse discovery and the five-second endpoint/client supervision from #220;
   extend inspection only for the metadata required by FR-067. Keep fixed argv,
   minimal environment and terminal escaping. Test the safety planner before
   mutation support.
2. Deliver local rm with all FR-067 invariants: explicit selectors, ownership,
   default home retention, consent, primary/shared/tool/read-only volume refusal,
   dry-run parity and residual-volume partial failure. Add current command/result
   contracts with the implementation; remote invocations retain refusal.
3. Deliver FR-068 stage/promote in a separate reviewed slice, reusing the removal
   safety rules. Its absent command syntax, candidate build inputs and health/
   smoke contract need a concrete reviewed contract before implementation; this
   record does not invent those choices or drop staging from T038.
4. After accepted T038, deliver T039 read-only bundle comparison and JSON evidence:
   in-sync 0, known drift distinct from 0, unreadable/unknown 68, no mutations.
   Pin-source and mounted-identity interpretation must be explicit and tested.

Each coherent slice has tests first, independent exact-head review and required
CI before Ready; owner merges. Source checks and fake Docker evidence do not
authorize actual lifecycle operations, builds, resource deletion or activation.
Feature 008/009 implementation is outside this sequence; their dependency and
owner-approval gates remain unchanged.

# Feature 009 validation guide

Status: future acceptance guide for the offline increment; not a runnable product claim. No real `brew`, `sudo`, network, Docker or host
mutation runs in these checks (fakes only).

```sh
python3 scripts/select_feature.py 009-host-setup-wizard
umask 022   # sandboxes may start at 0000
python3 scripts/check_repo.py
(cd apps/whr && go vet ./internal/hostsetup/... && go test ./internal/hostsetup/...)
git diff --check
git diff --stat $(git merge-base origin/main HEAD) -- contracts/cli docs/adr docs/container-runtime.md specs/006-whr-cli specs/008-workspace-sandbox   # empty for each 009 PR (SC-014)
```

Production catalogue acceptance remains `TestInitialCatalogueEntries` (13 ids, defaults, config markers and owner-measured minima); it is not supplied by TEST ONLY inline fixtures. Protocol shape controls already use `TestProtocolAcceptsRequiredAuditPayload` and `TestProtocolRejectsMissingAuditPayloadAndInvalidValues`; sequence/redaction/durability still need T006 engine tests.

Check by scenario (fake package manager, fake sudo, fake network, temporary HOME): User Story 1 `TestWizardYesCustomizeNo`; 2 `TestOptOutPersistsAndHonored`,
`TestRerunIsNoOp` (managed installations/config/profile/backups unchanged; confirmed protocol/lock bookkeeping retained); 3 `TestMoveLegacyToXDG`, `TestMoveRefusals`; 4 `TestPlainDoctorReadOnly`, `TestFixWithoutYesExit70`; 5
`TestPrivilegedStepShownConfirmedNeverUnattended`, `TestPrecedence73`; 6 `TestLinuxSetupRoutesToEnrollment`.

Endpoint, Colima and Docker client scenarios (T016 to T032; fakes only, all red first): selection order and authority `TestSelectionOrderOptionConfigDockerHostStandard`, `TestSelectionConfigStaysAuthoritative`; investigation and plain `doctor` listing `TestInvestigationDialsOnlyFixedCandidatesInOrder`, `TestPlainDoctorInvestigationListsInEvidenceAndNeverAsks`; wizard question and unattended `skip` `TestWizardEndpointAnswerIsOnePreviewedProfileWrite`, `TestWizardEndpointUnattendedDefaultSkipOneStderrLineSetupContinues`; drift `TestDriftIsWarningOnlyNeverStatusOrSwitch`; unselected entries `TestUnselectedEntrySkippedWithReasonUnselected`; catalog `TestInitialCatalogueEntries` (13 ids today; 15 after T017, 18 with Feature 011) and `TestNoColimaLifecycleOrContextCall`. Context file layout, Colima files and the new entries' minima are measured by the owner (T016) and are not claimed here.

Intended flow once registered (not available before T011, which is owner-gated):

```sh
whr setup --dry-run        # preview, writes nothing
whr setup                  # wizard: [yes / customize / no]
whr doctor                 # diagnose, read-only for the host
whr doctor --fix --dry-run # preview the repair
whr doctor --fix --yes     # create-class repairs; add --confirm <id> for modify-class or privileged items
whr setup --container-endpoint unix://$HOME/.colima/default/docker.sock   # answers the endpoint question (macOS; T031)
whr doctor --fix --confirm container-endpoint   # re-record a drifted endpoint (modify class; T031)
```

Real-host checks (Homebrew, the OrbStack cask, the owner's real HOME) need the owner's explicit authorization and are recorded separately; a
missing tool is a blocker, not a pass.


H37 permits manual per-work-item source scope/evidence with exact CLEAR local
prerequisite parents; the strict single-task table grammar is unchanged and
still rejects cross-feature/range prerequisites. Never invent a guard journal or
complete a checkbox from these source checks. H38 confirms the plan DP matrix;
AP-6 placement and applying external amendments remain separately gated.

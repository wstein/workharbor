# Feature 009 validation guide

Status: future acceptance guide for the offline increment; not a runnable product claim. No real `brew`, `sudo`, network, Docker or host
mutation runs in these checks (fakes only).

```sh
python3 scripts/select_feature.py 009-host-setup-wizard
umask 022   # sandboxes may start at 0000
python3 scripts/check_repo.py
cd apps/whr && go vet ./... && go test ./internal/hostsetup/...
git diff --check
git diff --stat $(git merge-base origin/main HEAD) -- contracts/cli docs/adr docs/container-runtime.md specs/006-whr-cli specs/008-workspace-sandbox   # empty for each 009 PR (SC-014)
```

Initial catalogue: `TestInitialCatalogueEntries` (13 ids, defaults, config markers).

Check by scenario (fake package manager, fake sudo, fake network, temporary HOME): User Story 1 `TestWizardYesCustomizeNo`; 2 `TestOptOutPersistsAndHonored`,
`TestRerunIsNoOp`; 3 `TestMoveLegacyToXDG`, `TestMoveRefusals`; 4 `TestPlainDoctorReadOnly`, `TestFixWithoutYesExit70`; 5
`TestPrivilegedStepShownConfirmedNeverUnattended`, `TestPrecedence73`; 6 `TestLinuxSetupRoutesToEnrollment`.

Intended flow once registered (not available before T011, which is owner-gated):

```sh
whr setup --dry-run        # preview, writes nothing
whr setup                  # wizard: [yes / customize / no]
whr doctor                 # diagnose, read-only for the host
whr doctor --fix --dry-run # preview the repair
whr doctor --fix --yes     # create-class repairs; add --confirm <id> for modify-class or privileged items
```

Real-host checks (Homebrew, the OrbStack cask, the owner's real HOME) need the owner's explicit authorization and are recorded separately; a
missing tool is a blocker, not a pass.

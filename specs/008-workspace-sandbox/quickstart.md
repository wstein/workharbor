# Feature 008 validation guide

Status: future acceptance guide for the offline increment; not a runnable product claim. No Docker, build or host mutation.

```sh
python3 scripts/select_feature.py 008-workspace-sandbox
umask 022   # sandboxes may start at 0000
python3 scripts/check_repo.py
cd apps/whr && go vet ./... && go test ./internal/runtime/... ./internal/sandbox/... ./internal/gitprofile/...
git diff --check
git diff --stat $(git merge-base origin/main HEAD) -- contracts/cli docs/adr   # must be empty for each 008 PR (SC-008)
```

Check by scenario (all with the fake provider or fake Docker):

1. User Story 3: run `TestAdmitTable`; every refusal row has its code and zero provider calls.
2. User Story 1: `TestDeveloperPlanMountsDirectory`, `TestPreflightEBUSYBlocksReady`, `TestHostOnlyKeysReportedWithoutValues`.
3. User Story 2: `TestCanaryAssetsAbsentUntrusted`, `TestExportPatchAndManifest`.
4. User Story 5: `TestCancelPreservesResults`, `TestCleanupOnlyTouchesLabelledObjects`.

Real-host checks (Docker bind-mount rename behavior, cleanup, IDE attach) require the owner's explicit authorization of that
operation and are recorded separately; a missing tool or credential is a blocker, not a pass.

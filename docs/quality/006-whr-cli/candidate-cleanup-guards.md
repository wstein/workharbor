# Candidate cleanup: existing removal guard conformance

Date: 2026-10-24. Base: `c71890d27c3d415e50fe15610a6f8f044b313142`.
Test commit: `d942cc7ced9122509e93072daad0e167e5dbf21c`.

This bounded Package B protection slice covers the two existing-guard units in
[the proposed cleanup matrix](../../../specs/006-whr-cli/contracts/candidate-home-cleanup-task-proposal.md).
It adds tests and evidence only. The existing FR-067 planner, PrepareRemoval,
Apply and LocalRmHandler remain unchanged. This does not implement candidate
allocation, provisioning, promotion, new flags, orphan recovery, S0 or all T040;
no spec/task status or approval gate changes. The fixed new-prefix name in tests
is a fixture, not evidence of random allocation or resource ownership.

## New assertions

- `TestCandidateCleanupPreservesWholeAnchorageGuard`: 48 cases covering staging
  and temp; exact full ID, role and `anchorage-<role>=1` label selectors; both
  supported home destinations; container-only, force-only, volumes-only and
  volumes+force. Every protected-prefix case exposes no planner targets, no
  backend transaction and no mutations. Volumes without force returns 70 before
  Docker inspection; the other combinations return 68.
- `TestCandidateCleanupPrimarySharedAndProtectedRefused`: 128 cases use
  `whr-stage-home-0123456789abcdef0123456789abcdef` (or its BundleDock-prefixed
  protected counterpart). They cover primary/shared running and exited mounts,
  read-only mounts, global read-only/tool references, `/opt/bdock` and descendants,
  BundleDock volumes and foreign ownership. Both destinations, roles and selectors
  participate; foreign metadata uses exact ID or an unrelated matching label,
  since a foreign container cannot match a recognized role. Checks cover pure
  planning, backend preparation, missing-force 70 precedence and safe preview
  followed by unsafe Apply metadata (68 with no attempted/applied actions).
  Primary references also refuse container-only removal.
- `TestCandidateCleanupMissingMountMetadataRefused`: incomplete mounts refuse 68
  at the actual snapshot boundary with no transaction or mutations.
- `TestCandidateCleanupRemovesOnlyExplicitCandidate`: eight success cases cover
  roles, home destinations and container-only versus explicit volume deletion.
  Preview names the selected volume/mount, exact mutation logs prove candidate
  container then candidate home only, and a separately mounted primary and its
  protected home remain outside all actions.
- `TestCandidateCleanupDefaultHandlerConsentAndGuards`: nine cases run the actual
  DefaultHandlers path against a private Unix listener and Docker shim, with
  schema validation. They prove protected-prefix refusal, missing force or yes
  returning 70, dry-run zero effects, and explicit candidate deletion preserving
  the primary. No mock transaction supplies the safety result.

These extend existing generic removal regressions with the new-prefix spelling,
selector/destination combinations, prepare/apply drift and command/backend
composition. Shared and global protection can overlap; those cases assert refusal
without claiming each overlapping guard is independently necessary.

## Executed verification

- Existing localdocker baseline: `go test ./internal/localdocker` passed (5.847 s).
- Affected full suites: `go test ./internal/localdocker ./internal/commands
  -count=1 -json` passed: 47 top-level/266 total test events in localdocker;
  105 top-level/146 total in commands. Counts include parent and nested test events.
- Final focused run: `go test ./internal/localdocker ./internal/commands
  -run TestCandidateCleanup -count=1 -json` passed: five top-level tests,
  193 named subtests plus the standalone missing-metadata case (198 pass events).
- `go vet ./internal/localdocker ./internal/commands`, Go formatting and
  `git diff --check` passed.
- Initial uncommitted `make check` refused unavailable/invalid source evidence.
  On the committed pristine test revision, `timeout 5s make check` reached raw
  repository tests, then exited 124. This is incomplete, not passing acceptance;
  required full checks remain a CI/review handoff gate.

## Isolated red probes

Each probe copied the Go module to a separate temporary directory, changed only
that copy's production source and ran `go test ./internal/localdocker -run
TestCandidateCleanup -count=1 -json`. Every exit was 1 with assertion failures
in new tests, not compilation errors. Final source remained unchanged and the
focused run was green. The table gives the isolated change and failing leaf count.

| Probe | Isolated change | Failing leaves |
|---|---|---:|
| Whole prefix | Remove `anchorage-home-` prefix disjunct from staging/temp guard | 36 |
| Primary reference | Remove `primary[m.Name]` disjunct | 24 |
| Shared reference | Disable `other.ID != c.ID` refusal | 24 |
| Read-only | Remove `!m.RW` protection | 12 |
| Tool path | Remove `/opt/bdock` root and descendant protection | 24 |
| BundleDock | Disable `bundledock-` protection in planner | 12 |
| Ownership | Remove `!recognized` selection refusal | 8 |
| Apply snapshot | Replace Apply's `planRemoval(fresh, ...)` inputs with original selected containers | 128 |

Logs were captured outside the repository under `/tmp/whr-b1-mutations-20261024/`,
with final focused/full JSON output under `/tmp/whr-b1-*-green.json`.
Fixture evidence proves neither actual Docker behavior nor exclusive control over
other Docker users. No Docker daemon operations, standalone builds, installs,
race suite, publication, activation or deployment were performed for this slice.

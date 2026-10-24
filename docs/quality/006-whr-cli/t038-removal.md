# T038: local removal and home-volume protection

Scope: FR-067 and the removal portion of SC-024, building on accepted FR-066
discovery from #220. FR-068 stage/promote remains unimplemented; this delivery
does not complete T038 or unblock T039's full-T038 prerequisite.

The owner requested T038 followed by T039 after WP0. The source baseline is
`628bc4ded0f96ed0ffebc501eae833fe4e2d3e5f`; the exact reviewed artifact head of
[approved amendment #144](https://github.com/wstein/workharbor/pull/144) is
`eeb4d90f16f8fde589a2992a4466006115b16bf9`. FR-067/068/069, SC-024/025 and
T038/T039 compare byte-identically between that head and the source baseline.
T025's implemented CLI/exit/output foundation is reused, not re-created.

## Behavior and evidence

Local removal accepts exactly one explicit full container ID/name,
`--role staging|temp`, or `--label key[=value]`. The admitted Unix endpoint is
pinned for the transaction. Each snapshot shares the existing five-second
inspection budget; each mutation subprocess also has a five-second bound.
All subprocesses reuse the explicit minimal environment, empty private Docker
config, bounded output and process-group cleanup. Inspect projects labels and
mounts, never container environment. Duplicate JSON keys and ambiguous field
spellings are rejected before metadata can conceal safety facts.

The immutable preview separates container removal, selected home-volume removal,
retained home volumes and per-action attempted/applied flags. JSON carries the
current localRemovalResult; every human metadata value is terminal-escaped.
`--dry-run` returns a preview and never calls Apply. Unattended/JSON mutation
requires --yes; interactive confirmation previews before a bounded yes/no answer.
A changed safety-relevant plan after confirmation is refused; the transaction is
one-use, so a failed/uncertain mutation is not retried automatically.

- Default removal, including --force alone, preserves all named home volumes.
- Home deletion requires --volumes and --force plus confirmation.
- Before the first mutation the entire selected set is checked. Before each
  subsequent action, metadata is read again. Foreign containers are refused.
- Staging/temp cleanup cannot touch a primary home (including anchorage-home-*
  and any primary container's home mount), even with force/volume flags.
- Tool/read-only volumes and any home mounted by another container are refused.
  Stopped containers also retain volume references: Docker refuses removing an
  [in-use volume](https://docs.docker.com/reference/cli/docker/volume/rm/).
- Container mutation is exactly docker rm -f -- FULL_ID; volume mutation is
  exactly docker volume rm -- NAME. No --volumes is passed to docker rm, no
  volume --force, prune, shell evaluation, pull or recreate operation is added.
  [Docker's removal contract](https://docs.docker.com/reference/cli/docker/container/rm/)
  documents the separate named-volume handling.
- A failed or unconfirmed mutation returns 73 with the complete action inventory,
  unresolved target names and recovery guidance. Applied marks acknowledgement;
  attempted without applied is uncertain, not a no-side-effect claim. Context
  cancellation before mutation retains 72/71; after an attempt, 73 takes priority.

Named red-first tests initially failed on the absent planner/API:
TestLocalRmContainerOnlyPreservesHomeVolume,
TestLocalRmWithVolumesAndForceRemovesHomeVolume,
TestLocalRmVolumesWithoutForceRefused,
TestLocalRmNeverRemovesBundleDockOrReadOnlyVolumes,
TestLocalRmRefusesForeignUnlabelledContainer,
TestLocalRmRefusesDeletingPrimaryHomeVolumeFromStagingOrTemp,
TestLocalRmRefusesVolumeMountedByOtherContainer,
TestLocalRmDryRunExecutesNoMutations and TestLocalRmPartialVolumeFailureExits73.
The duplicate-safety-metadata regression failed against the initial decoder before
duplicate rejection was added. Confirmation cancellation/deadline regressions
first returned 70, then 72/71 after correction. CLI selectors, aliases, explicit
consent, partial-result schema and human escaping have independent mock tests.

TestLocalRmDefaultHandlerUsesGuardedBackend exercises the actual default handler
with a private Unix listener and fake Docker executable, covering dry-run parity,
fixed full-ID mutation, JSON/schema and credential canaries. This is fixture
evidence, not a real Docker operation or an installed binary acceptance test.

## Remaining gates

Author verification: `go -C apps/whr test ./... -count=1` passed, including
Linux native PTYs and the default-handler Unix fixture. Affected-package vet,
gofmt, repository and whitespace checks passed. Required exact-head CI and
independent reviews belong to the published PR. Local Mix is unavailable and
Python 3.13.5 is below the development floor; pristine make-check evidence is
recorded in the PR, never inferred from the Go suite. No standalone builds, real Docker or
resource deletion, deployment, installation or owner-host action is authorized
by these tests. Existing SC-009 and Anchorage #14 live checks remain separate.

Repeated snapshots cannot make Docker operations atomic against another Docker
user. Labels remain usability guards. A concurrent volume attachment can cause
a partial failure after container removal; that case is tested and reported,
not hidden by force or rollback that would recreate data. Real-host lifecycle
acceptance and FR-068 require their own accepted scope and evidence.

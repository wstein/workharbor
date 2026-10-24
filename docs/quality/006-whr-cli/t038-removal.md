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

## Current Anchorage naming limitation and owner question

The [Opus review of #226](https://github.com/wstein/workharbor/pull/226#pullrequestreview-5637841455)
identified a real usability limit of the settled FR-067(e) rule, not a code
exception. Anchorage's current `.devcontainer/devcontainer.json` uses
`anchorage-home-${devcontainerId}` for every role. Staging/temp containers therefore
also have distinct homes named `anchorage-home-*`. The prefix guard treats all
such homes as protected and refuses staging/temp cleanup with 68 and zero mutations,
**even for container-only removal without --volumes**. It does not merely compare
against the currently mounted primary home. The Makefile's actual-primary
comparison is different and is not substituted for the approved rule.

Owner question before FR-068 cleanup or Anchorage #14 acceptance: retain this
whole-prefix protection and choose compatible staging/temp naming, or approve an
explicit spec amendment defining different protection semantics? Also confirm
that container-only refusal remains intended under “resolve to, unlink, or delete.”
Until resolved, whr cannot clean up existing Anchorage staging/temp containers
using that home template. No flag, force mode or undocumented fallback bypasses it.
This PR retains the settled code guard; changes to Anchorage are outside its scope.

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

## Independent QA corrections

The production review at `3dd2bfcac07b93c019944b483ac97956a968b37e` found
no source defect; executed test-quality review identified three missing opposing
cases. The correction changes only tests and this evidence:

- Empty/wrong exit-0 acknowledgements for both container and volume steps now
  require 73, exact attempted/applied inventory and no unauthorized next action.
- The backend default-preservation test includes --force without --volumes.
- Staging/temp primary-home refusal is tested both with and without --volumes,
  using the protected prefix and an actual primary-home reference.

Each corresponding guard-removal mutant now fails a direct assertion; restoring
production makes all TestLocalRm cases green. Production files are byte-identical
to the full-suite-verified candidate. Pristine make check at that candidate was
explicitly bounded to five seconds and interrupted with124, not passed; required
CI remains authoritative. The final exact-head delta review and CI belong to the
PR, with no real-Docker or host evidence inferred.

## Opus correction evidence

[Review 5637841455](https://github.com/wstein/workharbor/pull/226#pullrequestreview-5637841455)
requested the naming limitation above and two direct guard regressions:

- `TestLocalRmAmbiguousIDAndNameRefusesBeforeMutation` gives two recognized
  containers: one's full ID equals the other's exact name. Preparation must
  return 68, a nil transaction and zero mutations.
- `TestLocalRmApplyIsOneUseWithoutFurtherDockerCalls` successfully applies once,
  then requires 68 and no additional Docker call of any kind on the second apply.
  The fake provides a valid repeated snapshot, so an absent one-use guard cannot
  pass merely because the target disappeared or fixture metadata ran out.

Both tests pass with unchanged production. The exact previously surviving
mutations (`len(selected) != 1` to `== 0`, and bypassing `if r.used`) each fail the
corresponding named regression. Restoring production makes all `TestLocalRm`
cases pass. These are test-gap corrections, not new product behavior or a change
to the protected-prefix decision. Fresh exact-head review/CI remain PR gates.

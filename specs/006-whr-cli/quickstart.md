# Installable pilot validation

Status: future acceptance guide, not a runnable product claim.

## Existing checks

From the committed pilot checkout with Python 3.11+:

```sh
python3 scripts/select_feature.py 006-whr-cli
make check PYTHON=python3.14
git diff --check
```

Native permission is required for the runner's isolated temporary Git worktree.
A permission error is blocked, not passing. After scaffolding, run format/vet/build/
tests in each Go module and Phoenix format/compile-with-warnings-as-errors/tests.
Run independent Python/Go golden-vector parity before switching migrated callers.

## Real acceptance after implementation and separate live authorization

1. Record exact head, versions, Linux Docker host, client, TLS identity and approved
   disposable scope. Install from a clean local archive and perform hidden-input
   owner login, enrollment and health. Dry-run must write nothing.
2. Provision trusted image/Dockerfile workspaces; deny changed revisions before hooks.
   Compare operation/audit/state through browser and CLI, including lost responses.
3. Type/resize, detach a running console, measure <=5s relay cleanup, reattach with
   a new grant and verify retained/dropped output. Exercise grants/quotas/buffers.
4. Stop/start/rebuild and inject failures/restarts; compare uncommitted file hashes
   and prove inspection/reconciliation creates no duplicate runtime.
5. Exercise conforming real agent interface and independent stub, without requiring
   an AI subscription. Lost send reply must not resend. Mock evidence is labelled.
6. Reject dashboard mutation, foreign owner and hostile browser origin requests;
   prove immutable Git source display and explicit unavailable states.
7. Exercise isolated SSH, generated config, key mismatch/expiry, statuses 0/1/255
   and transport loss on an advertised SSH-capable fixture.
8. Quiesce backup, clean restore, retained hashes/outcomes, unknown observations,
   revoked identity rejection and resumable offboarding with data retained by default.
9. Exercise five workspaces/eight attachments; capture exact commands, timings and
   idle/load footprint. Missing real host/browser results block pilot acceptance.

Package local unsigned archives, checksums, SBOM, recorded build inputs/provenance
and clean install/uninstall instructions; verify archive contents and draft-only
release machinery/policy tests without publishing a release. Internal-pilot
acceptance requires SC-001–005 and SC-007–012, including every real-host/browser/
load result above. Open the combined PR for final human review only after all
internal-pilot WIs clear independent Sol review and exact-head required checks pass.

The owner-authorized allowlisted human-signed release exercise on `main`, including
end-to-end signature/checksum/SBOM/provenance verification for SC-006, is a separate
follow-up outside internal-pilot acceptance. Unsigned local artifacts do not satisfy
SC-006. Full Feature 006 completion remains open until SC-006 passes; no release
publication, tag or package-channel update is authorized by this pilot handoff.

## First increment validation (H77; future acceptance, nothing run yet)

Needs no control plane, login or Docker daemon beyond what `doctor` measures. After T034, on a built
binary (ordinary `go build` into a temporary directory; no archive or release):

```sh
whr --help                      # short-form hints; unavailable commands in a separate block
whr version --json
mkdir -p /tmp/ws/.devcontainer && cp <a valid devcontainer.json> /tmp/ws/.devcontainer/   # wave 1a reuses an existing file
whr init --dry-run --non-interactive --name demo --human-shell fish /tmp/ws             # preview only, writes nothing
whr init --non-interactive --yes --name demo --human-shell fish /tmp/ws                 # writes workharbor.json (version 2) and projects/.gitkeep
whr init --non-interactive --yes --name demo --human-shell fish /tmp/ws                 # rerun is a no-op
whr validate /tmp/ws --json     # canonical command workspace.validate; also: whr ws validate, whr workspace validate
(cd /tmp/ws && whr doctor --json)   # control-plane checks skipped; verdict incomplete
whr ls; echo $?                 # unavailable in this release: usage exit 64, no network
```

Record exact binary revision, OS, architecture, Git and runtime versions, and the commands and exit codes
for each: a Linux Docker host, macOS with OrbStack, a machine without a Docker socket (expect
`container.socket` fail with the container-runtime link), the hostile corpus of SC-017, the `init`
failure-injection matrix of SC-018 and an offline run (SC-020). Mock-only or injected-port results are labelled as
such and do not replace the real runs (SC-007). Owner-API commands are not exercised: they are blocked on the
control plane (router: only `/api/v1/hosts` and `/api/v1/auth` today).

Wave 1b adds the interactive wizard (`whr init` on a terminal, with PTY transcripts as evidence) and `--devcontainer`
selection. The wizard's fish and herdr choices are recorded as configuration only; launching them is not exercised.

## Local removal (T038 / FR-067)

Inspect a local workspace before removal with
`whr rm --local <full-container-id-or-name> --dry-run --json`. Select a scoped
group instead with `--role temp`, `--role staging`, or `--label key[=value]`;
selectors cannot be combined. Without --local, the remote command still refuses.

Outside the new-candidate exception defined by the
[throwaway amendment](contracts/throwaway-staging-amendment-20261025.md), removal
preserves named home volumes by default, even with --force. An interactive run
previews then asks for confirmation; scripts and --json require
--yes. Other home deletion additionally requires --volumes and --force; its preview
names the volumes and mount targets. Protected/shared/primary homes cannot be
removed through staging/temp cleanup. Inspect a 73 partial-failure inventory
before retrying: attempted but unacknowledged actions may already have happened.

Current Anchorage limitation: its home template is
`anchorage-home-${devcontainerId}` for every role. FR-067(e) protects the entire
`anchorage-home-*` prefix, so whr refuses existing Anchorage staging/temp cleanup
with 68, including container-only removal without --volumes. A distinct staging
ID does not waive the prefix guard. The [owner decided on 2026-10-24](https://github.com/wstein/workharbor/pull/226#issuecomment-6382864474)
to keep this refusal: removing those Anchorage staging/temp containers is not
covered by this task. A later FR-067(e) change is a separate decision and task;
no bypass is provided. See the [recorded decision](../../docs/quality/006-whr-cli/t038-removal.md#current-anchorage-naming-limitation-and-owner-decision).

Stage/promote and bundle drift are separate remaining T038/T039 work, not
available commands in this increment. See the [removal evidence and limits](../../docs/quality/006-whr-cli/t038-removal.md).

## Stage/promote direction (proposed, not available)

The name/full-ID plus explicit source, fingerprint trust and healthcheck/workspace-
smoke direction from [LS-1](spec.md#ls-1-owner-direction-2026-10-24) is recorded in the
[local stage/promote amendment](contracts/local-stage-promote.md). It is not an
implemented command. The owner approval/merge record of [PR #229](https://github.com/wstein/workharbor/pull/229)
approves the interaction only; the execution-profile gate still precedes source
work. Fresh candidate-home naming is recorded in the
[candidate-home decision](contracts/candidate-home-cleanup-decision.md). The
[2026-10-25 throwaway amendment](contracts/throwaway-staging-amendment-20261025.md)
removes separate deletion flags for new whr-managed candidates; normal
confirmation does not waive protected-resource or ownership checks in FR-067.
Current Anchorage protected staging/temp cleanup remains excluded, with no recovery
cleanup path promised. T038 stays open and T039 follows its complete acceptance.

## New candidate disposal amendment (owner, 2026-10-25)

New whr-managed staging is throwaway. The
[separate amendment](contracts/throwaway-staging-amendment-20261025.md) changes
removal/promote to dispose of candidate container, home, clone/result tree and
candidate metadata under the normal confirmation. No keep mode or later clone
cleanup remains; back up a staging home with Docker yourself before disposal if
needed. Existing `anchorage-home-*` and primary-home guards remain unchanged.

The required library/CLI delta and host-run runbook are still planned; the old
stage/promote library acceptance and pending CLI #271/#272 do not establish this
new disposal behavior. Follow the
[current delivery plan](plan.md#throwaway-staging-delivery-amendment-owner-2026-10-25)
for the code, tests and real Linux/macOS+OrbStack evidence before claiming it.

## Local init, stage and promote handoff

The [owner host-run procedure](../../docs/quality/006-whr-cli/owner-init-stage-promote-runbook.md)
covers the merged throwaway decision after the combined #272 promote CLI and
throwaway cleanup (including #274) are accepted and merged. It records separate Linux Docker
and macOS+OrbStack results;
it claims no host pass. The [init source audit](../../docs/quality/006-whr-cli/init-handoff-audit-20261026.md)
maps current test definitions and remaining acceptance work. T034 and owner host
gates stay open.

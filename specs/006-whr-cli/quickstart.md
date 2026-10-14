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

Package archives/checksums/SBOM/provenance and clean install/uninstall instructions.
Unsigned local pilot artifacts are not signed releases; SC-006 remains open until
an owner-authorized signed-tag verification exercise. Publish the combined PR only
after all pilot WIs clear independent review and exact-head required checks pass.

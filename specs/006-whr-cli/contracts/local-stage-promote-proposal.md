# T038 / FR-068: local stage/promote decision proposal

Status: **PROPOSED; not an implemented command or approved execution contract.**
The owner requested T038 followed by T039. FR-068's outcome is settled; the choices
below fill its missing local interaction and verification contract. No existing
FR, SC, task, dependency, ADR status or approval marker is changed by this file.
An independent exact-head CLEAR review and owner decision must precede applying
selected text to the authoritative artifacts or implementing these choices.

## Existing authority and actual gap

[FR-068](../spec.md) requires side-by-side candidate construction, optional
`--smoke`, verification of a healthy candidate, recreation of the primary using
the verified image while preserving its home, and staging cleanup under FR-067.
[T038](../tasks.md) includes this work; [PR #226](https://github.com/wstein/workharbor/pull/226)
implements removal only and leaves T038 open. T039 requires accepted T038.

The command table currently has no stage/promote entry. FR-068 does not define
workspace selection, local source trust, what `--smoke` executes or what counts
as healthy. The command table and result schema therefore cannot simply be
copied from an existing implementation. [ADR 0014](../../../docs/adr/0014-whr-command-structure.md)
retains its alias and owner-reviewed command amendment rules. No new bare
`whr stage`/`whr promote` alias is proposed.

The [existing provider contract](devcontainer-provider.md) supplies relevant
preflight and official-CLI boundaries. Its authenticated operation/approval
contexts are not a local-mode API. T038 depends on T025, not T009 or a live
control plane; implementation must not add a hidden dependency or manufacture
an authenticated provider context. Any proposed shared-source extraction or
expanded task scope must be reviewed explicitly, not smuggled into local CLI work.

Constitution II requires official Dev Container CLI semantics; IV requires
explicit trust before repository hooks; VI says: “Resolve product ambiguity
before an implementation plan becomes ready.” Those rules motivate this proposal.

## Choices for the owner

### D1 — explicit workspace and exact local trust

Recommendation: use an explicit existing workspace directory, rather than an
implicit current directory or a guessed container. Resolve its one primary
container from the accepted workspace labels on the already admitted local
Unix daemon. A missing or ambiguous primary is refused before any effects.

Proposed surface:

```text
whr workspace stage --local <dir> [--smoke] [--dry-run] [--yes] [--trust <digest>]
whr workspace promote --local <dir> [--dry-run] [--yes]
whr ws stage ...
whr ws promote ...
```

The preview identifies the primary, candidate name, home-volume plan and exact
source fingerprint. Building or running hooks requires explicit trust in that
fingerprint, separate from general confirmation. Interactive use can confirm the
shown source; unattended use supplies the exact `--trust <digest>` plus `--yes`.
A prior preview can supply the digest without executing hooks or builds. Changed
inputs invalidate it. The fingerprint must cover the reviewed configuration,
relevant submodule/source identities and actual build/hook inputs; an implementation
plan must define this set before promising dirty-working-tree support.

Alternative: require immutable, committed config/submodule revisions and reuse a
separately approved local carrier of the provider trust descriptor. That simplifies
input binding but restricts testing uncommitted devcontainer edits; it must not be
implemented by requiring the absent remote owner API.

**Owner choice D1: pending.** Neither the proposed flags nor a source-trust store
are authorized by this proposal alone.

### D2 — candidate health and the meaning of --smoke

Recommendation: promotion requires a running candidate, matching verified image
and mount/user configuration, and a passing Docker healthcheck when one is declared.
Without a declared Docker healthcheck, require an explicit successful smoke result;
running alone is not evidence of application health. A failed, absent required or
stale verification blocks promotion before the primary is stopped.

For `--smoke`, propose one reviewed workspace-owned entrypoint:
`.devcontainer/smoke-container.sh`. It runs only inside the candidate, as the
expected container user, with the workspace path as its sole data argument. The
executed bytes must match the trusted source fingerprint. Invoke it as a fixed
argv file entrypoint using its admitted interpreter; do not introduce an arbitrary
host command, command-string flag, native-agent launcher or Docker socket mount.
Missing entrypoint/interpreter or nonzero result is explicit failure. No raw
script output or environment is retained in the operation receipt.

Alternative: limit verification to runtime liveness/configuration and native
Docker healthchecks, and define `--smoke` as a fixed built-in runtime sanity check.
That avoids a workspace-script convention but must explicitly describe the weaker
health claim; it must not silently assert application acceptance.

**Owner choice D2: pending.** The source `.devcontainer/smoke.sh` in Anchorage is
a HOST orchestrator; it must not be guessed as the candidate entrypoint. The
separate smoke-container.sh convention is proposed, not inferred as existing whr behavior.

### Settled obligation — promotion reuses the exact verified image

Recommendation: bind a private, bounded candidate receipt to the daemon, workspace,
full primary/candidate IDs, exact source fingerprint, immutable Docker image ID,
primary/staging home identities and verification outcome. The receipt contains
only the required metadata, no credentials, environment dump or hook output.
Reinspect all bindings before promotion; names, labels, exit zero or a mutable
image tag alone do not establish a verified candidate. Labels stay usability guards.

The launch configuration is a private full admitted configuration, not a repository
rewrite. Candidate launch replaces the home source with a distinct staging volume;
primary recreation uses the immutable verified image and the original primary
home. Preserve other admitted configuration semantics. Do not rerun a mutable
Dockerfile build as a substitute for using the verified image. Official CLI
`--override-config` replaces the document, so a partial override is insufficient.

The fixed candidate name remains FR-068's `whr-anchorage-staging`, with
`anchorage-staging=1`. A collision or unrelated candidate is refused, never adopted
or removed by name. No automatic cleanup/rollback may remove a primary, protected
or shared home. Candidate cleanup uses the reviewed FR-067 engine only after
successful primary verification. Failure after primary replacement reports the
completed and remaining effects; recreating data is not a rollback promise.

The exact verified-image outcome is already settled by FR-068 and is not an
owner question here. The receipt schema, restart/recovery rules, source admission
capability matrix and phase budgets need the subsequent concrete plan; no new
path, durability or compatibility promise is implemented here.

## Proposed delivery after decisions

1. Record the selected D1/D2 text in the authoritative spec/plan/contract and
   command/result definitions through their owner-reviewed amendment process.
   Define admitted source inputs, exact argv, phase/recovery bounds and refusal
   cases; explicitly reconcile any task-scope or dependency change.
2. Tests first: stage leaves primary untouched; isolated candidate home; collision
   refusal; trust/source changes; declared smoke/health failure; stale receipt;
   immutable-image promotion; original home retained; safe staging cleanup;
   cancellation and partial failure; dry-run zero effects; JSON/escaping.
3. Implement one coherent slice per reviewed PR with fake Docker/CLI only.
   No source hook, build, actual Docker or resource mutation is run on the owner's
   machine by preparing or merging the source changes.
4. Close T038 only after discovery, removal AND stage/promote have their required
   code/test/review/CI/owner-merge evidence. Then implement T039. Keep any separately
   required real-host evidence explicit; no fake result closes SC-009 or Anchorage #14.

## Verification and decision record

This proposal itself needs repository/whitespace checks and independent exact-head
review, not a synthetic behavioral test. Owner choices and approval reference:
**PENDING**. Local make-check limitations and required CI are recorded in its PR.

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
(open Draft at this proposal's review) proposes removal only and leaves T038 open.
Its code is not accepted on main. T039 requires accepted T038.

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

### D1 — workspace selection and exact local trust

Selection recommendation A: use an explicit existing workspace directory. This
would be a new selector for stage/promote, deliberately different from FR-067(a)'s
container `<id|name>` and Feature 010 OQ-2's container name/ID selection. The reason
is to bind the build source and primary lookup to one explicit checkout; it is
not an interpretation that changes the settled rm or shell selectors.

Proposed surface A (`--local` remains a boolean; `<dir>` is positional):

```text
whr workspace stage --local [--smoke] [--dry-run] [--yes] [--trust <digest>] <dir>
whr workspace promote --local [--dry-run] [--yes] <dir>
whr ws stage ...
whr ws promote ...
```

Resolve exactly one recognized primary on the admitted local Unix daemon whose
workspace-folder label matches the canonical source directory. Exclude staging
and temp roles even when their local-folder labels match. Resolve symlinks in
both host paths before comparison; macOS `/private` aliases count as equal only
when host filesystem resolution establishes equality, never by stripping a
prefix. Unresolvable source paths refuse with 68, no recognized primary match with 65,
and ambiguous primary selection with 64, all before side effects. The concrete plan must test this canonicalization and label-to-path
admission; labels remain usability guards, not authorization.

Selection alternative B preserves the existing container name/ID model: stage
selects a primary `<id|name>` and separately names its source directory; promote
selects that primary and its bound candidate receipt. Proposed surface B:

```text
whr workspace stage --local --source <dir> [--smoke] [--dry-run] [--yes] [--trust <digest>] <id|name>
whr workspace promote --local [--dry-run] [--yes] <id|name>
```

Here `<id|name>` means exact container name or full container ID, consistent with
FR-067(a)'s removal surface. Feature 010 shell's additional unique ID-prefix rule
stays unchanged. Source-to-primary correspondence must still be checked; B avoids
introducing a directory as the container selector. `--source` is proposed, not
an existing or approved flag. No role/label batch staging is proposed.

For either selection, the fixed FR-068 name `whr-anchorage-staging` permits only
one candidate per admitted Docker daemon (one per host in the single-daemon
case), not one per workspace. Even a different `<dir>` uses this Anchorage-specific
name; a second workspace cannot stage concurrently. This is a limitation of the
settled name, not permission to generalize it.

Trust recommendation, independent of selection A/B: the preview identifies the
primary, candidate name, home-volume plan and exact source fingerprint. Building
or running hooks requires explicit trust in that fingerprint, separate from
general confirmation. Interactive use can confirm the shown source; unattended
use supplies the exact `--trust <digest>` plus `--yes`. A prior preview can supply
the digest without executing hooks or builds. Changed inputs invalidate it. The
fingerprint must cover the reviewed configuration, relevant submodule/source
identities and actual build/hook inputs; an implementation plan must define this
set before promising dirty-working-tree support.

Trust alternative: require immutable, committed config/submodule revisions and
reuse a separately approved local carrier of the provider trust descriptor. That
simplifies input binding but restricts testing uncommitted devcontainer edits;
it must not require the absent remote owner API.

**Owner choice D1: pending: selection A or B, and source-trust model.** Neither
the proposed flags nor a source-trust store are authorized by this proposal alone.

### D2 — candidate health and reuse of existing smoke checks

Recommendation: promotion requires a running candidate, matching verified image
and mount/user configuration, and a passing Docker healthcheck when one is declared.
Without a declared Docker healthcheck, require an explicit successful smoke result;
running alone is not evidence of application health. A failed, absent required or
stale verification blocks promotion before the primary is stopped.

Anchorage already has the workspace-owned
[`.devcontainer/smoke-container.sh`](https://github.com/wstein/anchorage-workspace/blob/28a9662f61ab6b8b2853bdc7453b78685ed63f64/.devcontainer/smoke-container.sh):
it runs inside the candidate and takes the container workspace path as its sole
data argument. Its current checks assume user `vscode` and write to the candidate
home as well as checking tools and configuration; it is not a generic application
health test. The existing host
[`.devcontainer/smoke.sh`](https://github.com/wstein/anchorage-workspace/blob/28a9662f61ab6b8b2853bdc7453b78685ed63f64/.devcontainer/smoke.sh)
creates a throwaway container and feeds the host script bytes over stdin:

```text
devcontainer exec --workspace-folder <host-dir> --id-label <candidate-label> bash -s -- <container-workspace-path>
```

The script is stdin, not a file argument. This is existing Anchorage behavior,
not existing whr behavior. Propose reusing its contract, with an explicit execution
sub-choice: recommend feeding the bounded, fingerprint-verified host bytes over
stdin to fixed argv `bash -s -- <container-workspace-path>`, as Anchorage does.
The admitted Bash interpreter and expected container user must match the candidate
configuration. Alternative: run a fixed in-container file entrypoint only with a
verified binding between the executed bytes and the trusted source fingerprint;
a pathname or a prior host hash alone does not establish that binding. The concrete
plan must prove it before that alternative is implementable.

Run smoke only in the verified candidate, never the host orchestrator. Do not
introduce an arbitrary host command, command-string flag, native-agent launcher
or Docker socket mount. Missing entrypoint/interpreter or nonzero result is
explicit failure. Any displayed script stdout/stderr, including failing command
excerpts, is untrusted output escaped per FR-022; JSON escapes those values too.
No raw script output or environment is retained in the operation receipt.

Health alternative: limit verification to runtime liveness/configuration and
native Docker healthchecks, and define `--smoke` as a fixed built-in runtime sanity
check. That avoids requiring the existing workspace-script contract but must
explicitly describe the weaker health claim; it must not silently assert
application acceptance.

**Owner choice D2: pending: health model and, if reusing workspace smoke, stdin
(recommended) or a proven file-entrypoint binding.** This reuses an existing
Anchorage contract; it does not authorize running its host smoke orchestrator or
changing Anchorage files.

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

Promotion recreates the primary: all active `docker exec` sessions, including
Feature 010 `whr shell --local` panes, end with the old container. Herdr cannot
preserve the process across replacement; a new shell needs to be opened afterwards.
The proposed preview names the primary and full ID and warns of this interruption
before confirmation. It does not claim a complete inventory of active exec sessions;
if that inventory cannot be established it reports it as unknown. Interactive
promotion requires confirmation; unattended/JSON promotion without `--yes` refuses
70 before replacement. Existing Feature 010 shell exit/liveness semantics stay
unchanged; this proposal does not promise one universal shell exit status.

### Candidate collision, retry and recovery

The fixed candidate name remains FR-068's `whr-anchorage-staging`, with
`anchorage-staging=1`. A collision or unrelated candidate is refused, never adopted
or removed by name. A failed/abandoned stage may leave its container, staging home
or receipt; a rerun reports those resources rather than overwriting them. The
proposed recovery is to inspect/preview removal, explicitly remove the verified
staging resources through FR-067, and then stage again. Once #226 is accepted,
`whr rm --local --role staging --dry-run` previews the selection; cleanup requires
the normal confirmation, and deleting its home also requires `--volumes --force`.
The preview must be reviewed because a role selector can match multiple containers.
No name collision alone authorizes adoption, deletion or automatic retry.

There is an existing upstream safety conflict, not a cleanup exception:
Anchorage's current home template is `anchorage-home-${devcontainerId}` for every
role, whereas settled FR-067(e) protects the entire `anchorage-home-*` prefix.
Thus even a distinct staging/temp home with that prefix is refused by #226,
including container-only removal. The owner must reconcile naming/protection
before relying on that cleanup path for FR-068 or real-host acceptance; do not
weaken the code guard or prescribe a bypass in this proposal.

No automatic cleanup/rollback may remove a primary, protected or shared home.
Candidate cleanup uses the accepted FR-067 engine only after successful primary
verification. Failure after primary replacement reports completed and remaining
effects; recreating data is not a rollback promise. An interrupted promotion must
report surviving primary/candidate IDs, homes and receipt validity, and require a
fresh inspected preview before recovery. Detailed restart/recovery rules belong
to the subsequent concrete plan, not an implied safe rerun.

The exact verified-image outcome is already settled by FR-068 and is not an
owner question here. The receipt schema, source admission capability matrix and
phase budgets need the subsequent concrete plan; no new path, durability or
compatibility promise is implemented here.

## Proposed exit and output mapping

Use the existing [ADR 0013](../../../docs/adr/0013-whr-exit-codes.md) categories;
this table proposes no new exit code. Partial effects take precedence as below.

| Exit | Proposed stage/promote case |
|---|---|
| 0 | Successful operation or valid dry-run with zero mutations. |
| 64 | Invalid syntax, missing/conflicting positional selectors, ambiguous primary selection or unsupported flag combinations. |
| 65 | No recognized primary match, including a selector matching only an unlabelled foreign container. |
| 68 | Pre-effect conflict/validation failure: wrong primary state/role, collision, source/receipt mismatch, failed health verification, or protected/shared home resolution. |
| 70 | Required human choice/trust/confirmation absent; unattended mutation without `--yes` or required source trust. |
| 71 | Deadline exceeded before any partial effects; identify the local phase and target as described below. |
| 72 | Cancellation/interrupt before any partial effects. |
| 73 | Completed or uncertain mutation effects with remaining work: failed/interrupted staging, primary replacement or staging cleanup; list attempted/completed/remaining resources and recovery. Takes precedence over 71/72 after partial effects. |

The selector cases follow the settled [Feature 010 local contract](../../010-host-and-workspace-panes/contracts/local-cli.md):
no labelled match is 65, ambiguity is 64, and non-running state is 68. This does
not change FR-067 removal's separately specified refusal mapping.

For local timeout 71, propose printing the phase that timed out and the selected
primary name/full ID if already resolved; otherwise say the target is unresolved.
If a candidate receipt already exists, identify that receipt and candidate too.
Escape all displayed metadata per FR-022 and include the same structured context
in JSON. Explicitly state that no remote operation ID exists for this local
operation; do not invent one or imply a control-plane operation was submitted.
ADR 0013's “operation id printed” wording needs this local reconciliation in the
authoritative amendment before implementation. Timeout after partial effects
remains 73 with the effect inventory, rather than 71 or a claim of no changes.

`--json` uses the existing envelope in `contracts/cli/output.schema.json`, with a
stage/promote result schema name still to be specified in the authoritative
amendment. Include the preview/effects and categorized error; never raw child
output or credentials. Invalid supplied trust data is validation/usage input,
not permission to run untrusted hooks. The plan must pin each precise refusal to
its test and code rather than treating this proposal as an implemented schema.

## Proposed delivery after decisions

1. Record the selected D1/D2 text in the authoritative spec/plan/contract and
   command/result definitions through their owner-reviewed amendment process.
   Resolve the home-prefix conflict before claiming usable staging cleanup.
   Define admitted source inputs, exact argv, phase/recovery bounds and refusal
   cases; explicitly reconcile any task-scope or dependency change.
2. Tests first: primary-only canonical selection and ID/name alternative; stage
   leaves primary untouched; isolated candidate home; collision and rerun refusal;
   trust/source changes and smoke-byte binding; declared smoke/health failure;
   FR-022 smoke output; stale receipt; immutable-image promotion; session-loss
   preview/confirmation; original home retained; safe staging cleanup; cancellation
   and partial failure; dry-run zero effects; exit mapping and JSON/escaping.
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

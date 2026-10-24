# Proposed decision: new candidate homes and promotion cleanup

Status: **proposed for owner review**, 2026-10-24. This document approves nothing.
It is separate from [PR #229](https://github.com/wstein/workharbor/pull/229)'s
[stage/promote interaction](local-stage-promote.md) and from T038's S0 execution
profile. Approval of a documentation PR may record this proposed decision only
if the owner explicitly accepts it at that exact revision; it does not approve
implementation, S0, live Docker operations, builds or deployment.

## Authority and scope

The [owner decision on #226](https://github.com/wstein/workharbor/pull/226#issuecomment-6382864474)
keeps the entire `anchorage-home-*` staging/temp guard, including container-only
refusal, and requires a separate decision and task for a later naming change.
This proposal concerns **new WorkHarbor-managed candidates only**. Existing
Anchorage home names and containers are never renamed, adopted or exempted.
FR-067(e), all existing removal tests and the owner limitation remain intact.
There is no protected-candidate cleanup or recovery path in this proposal.

Recommendation: accept both the new-home scheme and the explicit promotion
cleanup consent below, then separately approve an implementation task and the
completed S0 profile. Until then the interaction amendment has no executable
cleanup surface and primary replacement remains gated.

## New allocation, without adoption

Allocate a fresh named volume `whr-stage-home-<opaque-id>` for each new candidate
attempt, mounted only at existing `/home/captain` or `/home/vscode` home destinations. Recommend a cryptographically random 128-bit ID encoded as 32 lowercase
hex characters, unrelated to source paths, user names or secrets. The fixed
candidate name remains `whr-anchorage-staging`, label `anchorage-staging=1`:
**one candidate per admitted Docker daemon**, not one per source or primary.
A different volume ID does not allow a second candidate.

Before effects, inspect the admitted daemon for the fixed name/label, any retained
attempt, the selected primary home, and the proposed volume name. Any candidate,
unrelated or abandoned attempt, or pre-existing proposed volume is a collision
(68); never adopt it, silently choose another candidate, delete it or retry the
whole stage. The candidate home must differ from every recognized primary home,
including stopped primaries, and from all protected/shared volumes. A name prefix
is not ownership evidence. Bind the allocated name, daemon identity and actual
container/mount identities to the private receipt/journal.

Allocation is **not implemented** by merged removal code. S0 must specify bounded
volume inspection, safe creation/reservation, durable intent before allocation,
metadata/receipt correlation, official-CLI mount configuration and interruption
handling. Docker's create-or-return-existing behavior is not exclusive allocation.
Use fresh absence checks, rechecks and an attempt journal; a local lock cannot
exclude another client of the same Docker user. That race limitation is explicit:
no atomic daemon-wide reservation is promised. S0 must prove its admitted
allocation model or refuse unsupported cases; no helper is invented here.

## Promotion requires explicit deletion consent

Proposed complete apply form, extending only the interaction's promotion surface:

```text
whr workspace promote --local --volumes --force [--yes] <id|name>
whr workspace promote --local --dry-run [--volumes --force] <id|name>
```

`--volumes` and `--force`
must each be explicitly supplied for apply. Interactive apply also confirms the
exact preview; unattended/JSON apply also requires `--yes`. Neither `--yes`, source
trust, automation nor internal defaults may set either deletion option. Force
volume deletion in automated execution still requires the explicit human
confirmation required by FR-067(d); machine policy is not a substitute.

| Invocation/condition | Proposed result before any effects |
|---|---|
| Apply missing either deletion flag, even with `--yes` | 70, no primary replacement and no candidate/container/home deletion. |
| Both flags, but interactive consent declined or unattended/JSON `--yes` absent | 70, no mutation. Explicit cancellation remains 72. |
| Valid dry-run without deletion flags | 0 preview, no consent needed and zero effects; explicitly show that apply requires both flags. |
| Dry-run with `--volumes` but no `--force` | 70, consistent with the existing removal flag rule; zero effects. |
| Protected, primary-shared, otherwise shared, foreign or drifted cleanup target | 68, zero effects, regardless of flags/confirmation. |

Default data preservation remains FR-067(c). Promotion without explicit deletion
consent **refuses**; it does not replace the primary and leave a candidate while
claiming success. Successful promotion under FR-068 includes candidate container
and home cleanup. There is no new preserve-candidate mode. The original primary
home is always preserved, even when the new flags are supplied.

The fresh preview identifies the primary full ID/name and lost exec sessions,
exact candidate full ID and allocated home, **all mounts** of that home, preserved
primary home, daemon identity and all replacement/cleanup actions. Dry-run
never creates a volume, lock/receipt recording an execution, container or image,
and never runs a hook, smoke, pull or build; read-only collision checks are allowed.

## Compatibility with merged removal and failure ordering

Checked against merged #226 foundation `ae9cbb7410df42e62687a476ccdd6f6d1abb25da`,
present in proposal base `870899a6ec2238d78460aaace313ef2f721442d1`:

- [The removal planner](../../../apps/whr/internal/localdocker/removal_plan.go)
  rejects staging/temp home mounts named `anchorage-home-*` or mounted by a
  recognized primary before its container-only preservation branch. Keep this.
- Home deletion rejects any other container mount, **including stopped
  containers**, and globally protected read-only, `/opt/bdock` or `/opt/bdock/*`,
  and `bundledock-*` volumes. Keep this stricter existing behavior. A new prefix
  never overrides any of these checks; unsupported/missing home metadata refuses.
- [Removal application](../../../apps/whr/internal/localdocker/removal.go)
  reinspects targets, consumes an apply attempt once, removes the exact container
  before its home, rechecks mounts before volume deletion and reports uncertain
  or partial mutation as 73. Its command handler requires explicit deletion
  options independently of confirmation. Reuse these invariants, without an
  internal bypass or assuming a prepared transaction remains valid indefinitely.

Before stopping/replacing the primary, preflight the **entire** candidate cleanup
plan with the real deletion options and receipt-bound IDs against fresh daemon
metadata. Refuse 68 if candidate cleanup is inadmissible; missing consent is 70.
Reinspect before effects and again before cleanup. Recreate the primary from the
verified immutable image with its original home; verify the resulting primary
before removing the candidate container, then its home. Do not delete any other
volume. S0 must define promotion composition and closed schemas before code.

A concurrent mount or drift appearing after replacement prevents unsafe cleanup;
report 73 with attempted/applied/remaining resources and honest recovery guidance.
No rollback, automatic retry, hook rerun or successful-cleanup claim follows.
If home deletion fails after candidate removal, retain and identify the residual
volume. Missing receipts and orphaned volumes are not proof of ownership; existing
`rm` selects containers, so it cannot clean up a detached orphan home. A separate
recovery capability needs its own approved scope; no volume prune, manual deletion
recipe or protected-home bypass is authorized here. Retained/uncertain attempts
block another stage until an approved recovery has resolved them.

## Approval boundaries

Owner acceptance must name the exact reviewed revision and explicitly accept the
new-only prefix/ID scheme, no-adoption rules, consent matrix and preserve-by-refusal
behavior. It leaves the approved task graph/checkmarks and FR-067 guard unchanged.
The follow-on task must be approved separately; S0 then incorporates this decision
and resolves allocation, receipts, concurrency and recovery limitations before
implementation. Exact-head independent review, repository/application checks,
real-host evidence and owner merge gates remain separate. No existing task or
feature is completed by this documentation.

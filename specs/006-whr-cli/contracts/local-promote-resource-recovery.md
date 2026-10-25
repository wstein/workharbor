# Local promote resource recovery amendment

Status: implementation contract for the owner's 2026-10-25 direction to continue
with promote and rollback; exact-head security review, required CI and native
owner-host evidence remain pending. This does not claim T038/T039 completion.

## Scope and preserved decisions

This amendment reconciles the later rollback direction with the
[accepted execution profile](local-stage-execution-profile.md), whose cleanup
paragraph forbids automatically stopping/restarting the old primary. Only bounded
resource recovery below replaces that prohibition. It does not authorize Docker
operations during development, deployments, remote publication or a PR merge.
D1/D2, FR-067/FR-068, separate candidate home, no Anchorage adoption and explicit
`--volumes --force` plus confirmation remain unchanged. The complete cleanup
preflight and consent checks precede Git export/import and primary stop.

## Replacement ordering

The fake official CLI must model lookup by container labels/configuration and
mounts, including reuse of a matching running candidate. The promotion request
must select a distinct primary; candidate reuse is a refusal, not a replacement.
Retain the original full container ID and writable layer. Stop it and move its
name aside only after preflight and durable intent. Never remove it until a
distinct replacement has completed configured hooks and health/smoke verification.
Only then may guarded old-primary, candidate and candidate-home cleanup proceed.
Every action records durable intent and an observed or uncertain outcome.

## Bounded resource recovery

A known, normally reaped nonzero hook/smoke result, or an explicitly observed
unhealthy replacement, may initiate recovery while the old primary is retained
and the replacement is not verified. Native-client failure, signal, timeout,
missing observation, input/identity drift or an unobserved effect never qualifies.
Cleanup failures after primary verification do not initiate recovery.

Use a fresh recovery context, bounded to 120 seconds overall and 30 seconds per
native action. Recheck the bound Unix endpoint, daemon, private receipt, current
configuration and inputs, exact original/replacement IDs and bindings, name
occupancy and shared primary mounts between actions. Refuse an occupied retention
name or a foreign mount of the primary home/original writable workspace.

The only recovery actions are:

1. Stop the exact failed replacement and observe it stopped.
2. Rename it to `whr-failed-primary-<operation-id>` and retain its writable layer.
3. Restore the original name to the exact retained original container.
4. Start that exact original and observe its bound identity and running state.

Each step requires durable intent before execution and a fresh verified
postcondition. Any failed or uncertain step stops recovery without another
attempt, deletion or compensating action. Report the cause and full resource
inventory for manual inspection. Successful resource recovery still returns
partial status and exit 73; it is not successful promotion or permission to retry.
Candidate, candidate home, result refs and both container writable layers remain.

## Limits and evidence

Resource recovery cannot undo lifecycle hooks, entrypoint effects, writes to the
original workspace or shared home, imported Git objects or create-only result
refs. Starting the original runs its normal container entrypoint. A restored
container therefore does not prove restoration of application data or behavior.
No automatic shared-data rollback, Git reset, checkout overwrite or deletion is
promised. Retained resources require explicit owner reconciliation.

Tests must prove original retention until replacement verification, distinct
lookup, known-failure recovery, uncertainty/no-retry outcomes, occupied names,
shared mounts and failed recovery steps. Every refusal test asserts both exit and
reason. Fake Docker tests are library evidence; real official-CLI/Docker and owner
host gates remain open. Command wiring and the full T038 acceptance matrix remain
separate work until delivered.

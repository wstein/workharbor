# Decision: new candidate homes and throwaway cleanup

Status: **DECIDED by the owner, 2026-10-25**. This updates the 2026-10-24
candidate-home decision for new whr-managed candidates. The fresh-home allocation
model remains; retained clones and separate candidate-home deletion flags are
superseded. Implementation and host acceptance of this delta remain pending.

The owner's words, scope, failure matrix and planned code/tests are recorded in
the separate [throwaway staging amendment](throwaway-staging-amendment-20261025.md).
The accepted [execution-profile body](local-stage-execution-profile.md) is retained
unchanged; the separate amendment overrides its old disposal rules.

## Allocation and identity stay decided

Allocate a fresh `whr-stage-home-<opaque-id>` named volume per new candidate,
using a random 128-bit ID encoded as 32 lowercase hex characters. Its admitted
home destination remains `/home/captain` or `/home/vscode`. The fixed candidate
name `whr-anchorage-staging`, label `anchorage-staging=1` and one-candidate-per-
Docker-daemon limit remain unchanged.

Keep the existing allocation, daemon/operation/nonce/full-ID bindings and
collision/no-adoption rules. A Docker name, prefix, label or idempotent volume
creation response alone does not prove ownership. Candidate home must differ
from primary homes, including stopped primaries, and all protected/shared homes.
The allocation libraries are merged in #265/#266/#267; this documentation does
not claim their new terminal-disposal behavior is implemented.

## Normal confirmation includes complete disposal

Removing or successfully promoting a new whr-managed candidate removes its
container, fresh home, private clone/result tree and candidate receipts,
journals/configuration/metadata. No archive, keep mode, automatic backup or
separate cleanup remains. Ordinary removal deliberately loses unfetched commits
and uncommitted clone work; preview that loss under the normal prompt/`--yes`.
Users who want to retain a staging volume must back it up with Docker themselves
before disposal.

Promotion uses `whr workspace promote --local [--dry-run] [--yes] <id|name>`.
The previous promote-specific `--volumes --force` surface is removed. Do not
manufacture those flags internally. Generic `rm` keeps its existing flags for
other homes; those flags cannot turn new staging into a keep mode.

| Condition | Required behavior |
| --- | --- |
| Dry-run | Preview all candidate disposal with zero apply effects; no deletion flags required. |
| Normal interactive confirmation declined, or unattended/JSON `--yes` missing | 70 before apply; explicit interruption remains 72 before effects. |
| Confirmed ordinary candidate removal | Delete all candidate artifacts, including unfetched work. |
| Confirmed successful promote | Verify the required result refs in the original and replacement primary before terminal candidate disposal. |
| Failed stage or post-transfer promote rollback | Clean the attempt's candidate artifacts through the existing bounded lifecycle; keep/restore the original primary and preserve its home. |
| Cleanup cannot finish or its result is uncertain | Report the existing failure category and actual remaining resources/cause; never claim successful complete cleanup. |

## Existing protected resources stay protected

The entire `anchorage-home-*` staging/temp guard from
[the owner decision on #226](https://github.com/wstein/workharbor/pull/226#issuecomment-6382864474)
remains, including container-only refusal. No existing Anchorage container/home
is renamed, adopted or exempted. Do not broaden the new-candidate exception to
an arbitrary prefix match or another container in a mixed removal selection.

Keep primary-home, shared-volume (running or stopped), read-only, `/opt/bdock`
and `bundledock-*` protection and the existing ownership/drift checks. Candidate
cleanup never targets the primary or original home. Promotion still performs its
accepted primary replacement with that same original home; imported result refs
belong to the original repository and remain there.

Use the existing source trust, verification, receipt and resource rollback model.
Do not introduce volume backup, another consent layer, global cleanup or a new
recovery service. The amendment changes disposal, not D1/D2 or source execution.
No task checkbox, feature completion, native-host result, build, release or live
Docker action is implied by this documentation.

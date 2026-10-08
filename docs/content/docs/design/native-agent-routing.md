---
title: Native agent implementation routing
description: "Focused remediation, package ownership and deferred workflow improvements after D53."
weight: 7
toc: true
---

[D53](native-codex.md) supplies the native contract. This separate routing record
preserves its implementation boundaries and Werner's subsequent lifecycle updates.

## Routing after the native contract

#283's open medium findings require two focused fixes under its existing scope:
nonblocking rooted no-follow opens followed by descriptor validation before any
read, with a deterministic FIFO-swap regression that cannot hang; and an immutable
host revision key binding both manifest and inventory digests, with coexistence,
idempotent reinstall, tamper and original-pin/resume tests. Keep the guest target
`/skills/<inventory-sha256>` and full selected-source provenance. A common guest
target is not permission to reuse a different host revision. No new D-row,
inventory encoding change or published/shared history rewrite is needed.

Crewbook #10 follows Codex author slots. Existing #84 evidence is scoped to
Antigravity 1.2.14 on Apple Container 1.5.0
([committed spike](../spikes/agy.md)); it does not prove full approvals, current
native loading, provider bindings or end-to-end support. Retain measured event,
cancel and resume evidence with its exact setup, investigate soft-denied work
even when exit status is zero, and report missing host approvals honestly.
AGY model/effort choices require its own native validation; do not assign GPT
bindings or loosen permissions to satisfy the full-support request.

After #9, route crewbook #11, #12, #13 and #15 to `cb/docs`, P2, serially in that
order: canonical simplicity ladder, root-cause checks, precise handovers, then
material limitations. They reuse the guarded starting prompt already approved
in their issue bodies and preserve policy, meaningful tests and independent
review. #14 is `cb/verify`, P2, after those changes and #9's bindings: first an
offline fixture/scoring design and self-check under #8/#5; executable evaluation
tools stay outside the runtime text package. Live inference, paid evaluation
and savings claims require separate authorization and measured evidence. This
batch routes Priority/Session; dispatch owns claims and status moves except
Werner's explicit #35 release from Blocked to Todo. #35's offline bounded
foundation may be claimed now; source edits wait for this native specification
to land. #34 measurements and #164 verification gate production support, not
offline adapter construction against the existing #20 contract.

Crewbook #16 is `cb/docs`, P2, after #9 and the lifecycle/reporting inputs in
#4/#13: consume handbacks, preserve a run registry, reclaim completed capacity,
honor exact human-approved reviewer substitutions and SHA gates, and wait with
a named resume path. Scenario checks coordinate #8/#5 without live credentials.
It authorizes no daemon, timer, autonomous backlog, new source enforcement or
push. Paused crewbook #8 is pre-existing work; do not edit its drafts or start
new P2 authors ahead of the runnable Codex foundation.


### Package foundations and board ownership

The native contract landed first at `c6b28807eaf73a85676a81e9a8332b77d08549ee`.
Dispatch reports #35 claimed by Nash, sol6.1 low, with the adapter-only boundary;
this design batch spawns no worker. Its fresh independent medium design reviewer
is separate from the author. The pre-existing crewbook #8 Go worker has finished
`403b924` and `f6aadff` on clean local main, moved In review and closed its author;
a fresh independent medium review is active. #35 remains the active native
author, so the existing #6 inventory/provenance author may resume next, followed
by #5 CI. This is the second source slot, not a third author. Completed handbacks
and active readers do not occupy source-author capacity. New P2 content and
#7's queued lock fix wait rather than preempt this native dependency sequence.

The following Priority/Session assignments use the scoped crewbook project 10
adapter (owner `wstein`, project `PVT_kwHNjWrOAZaiCg`, prefix `cb`). Priority
ranks pending work, not completion. Dispatch owns status changes and claims;
review alone clears a change for its PR, with the exact independent reviewed SHA.

| Crewbook item | Priority / Session | Current state and next gate |
| --- | --- | --- |
| #1 entrypoints/root | P1 / `cb/review` | In review: already on local main, not Todo; final independent review |
| #2 policy/precedence | P1 / `cb/review` | In review: already on local main; preserve trusted project/platform boundary |
| #3 portable manual/profile | P1 / `cb/review` | In review: already on local main; final independent review and links |
| #4 coordinator/leaf lifecycle | P1 / `cb/review` | In review: already on local main; role ownership/handback review |
| #5 package validation/CI | P1 / `cb/platform` | Next after #6, using #8's landed plain-Go tooling and reviewed interfaces; no hosted CI success claim |
| #6 provenance/inventory | P1 / `cb/platform` | In progress: dispatch resumed the existing author after #8; agreed encoding is not completed producer/package acceptance |
| #7 board integration | P2 / `cb/platform` | Blocked: scoped tools/fields landed, reviewed and live; optional view lock fix/review/live verification, UI-only automation mapping and profile/docs remain |
| #8 maintenance Go | Existing / `cb/review` | In review: `403b924` / `f6aadff` on clean local main; source author closed, fresh medium review active |
| #9 native Codex | P1 / `cb/design` for this batch | Design done, full tracker open; dispatch owns transition to the next package/integration phase |
| #10 native AGY | P1 / `cb/runtime` | After #9's Codex author slots; coordinate declarative package work with `cb/docs` and existing host evidence/pins |
| #11 / #12 / #13 / #15 | P2 / `cb/docs` | After #9, serial canonical prompt/root-cause/reporting/limitations edits |
| #14 evaluation | P2 / `cb/verify` | After #9 and guarded rules, offline-first scoring only; live/paid inference separately authorized |
| #16 dispatch recovery | P2 / `cb/docs` | After #9 and #4/#13 inputs, coordinate scenario self-checks with #8/#5 |

The independent medium D53 review cleared exact `c6b28807` without findings,
recorded in crewbook #9 comment `5980399162` and workharbor #35 comment
`5980399287`. That is specification clearance only; implementation and
full-support measurements remain pending. Werner's approved reviewer substitution
is honored without a false Claude-only dependency.

#7's previous hardcoded-tools blocker is resolved by `01e8cef`, `00277f4` and
`2e1c1c5`; the live Session migration preserved IDs and Werner. Its remaining
optional views, UI-only automation and docs are distinct unmet scope. Do not
claim complete live setup or infer a new automation runner. The optional-board
review reports a medium active-lock takeover after 120 seconds; its bounded fix
is queued for the next available source slot, not a third author alongside #35
and #6. Preserve a live owner's lock rather than take it solely because of age,
and retain deterministic lock-owner/concurrency checks and independent review.
Nothing here moves
an already-landed card back to Todo or claims that queued work is implemented.

### Native host residuals

#34 and #241 stay with `wh/verify`; #164 and #242 with `wh/platform`, P1 for the
Codex portions. #35 is `wh/runtime`, P1, claimed by dispatch after the explicit
Todo release. #283 has open stage-1 findings and stage-2/cutover dependencies,
with no active author reported; dispatch can mark it Blocked rather than count
it as source capacity. #227 remains the advisory record. REST issue scopes
were reconciled to D53 without inventing duplicate host implementation issues.
The historical Copilot ordering is retained. All new related tracker items
belong to crewbook.

#283 remediation uses two new focused fix commits, not a rewrite of its landed
ancestors: FIFO/open validation and independent immutable revision coexistence.
Only after those fixes and native interfaces can stage 2 own service/serve/runtime
composition. #35's bounded author does not edit those shared files. #242's doctor
and native binding work remains serialized with #283 cutover. The guarded prompt
in #11–#15 is already approved; package guidance cannot weaken policy, tests or
review. #16 must honor human-approved model substitutions and reviewed SHAs,
consume completed handbacks and reclaim their slots without inferring that a
silent or paused author is gone.

{{< status unverified >}} Native runtime loading, exact model/effort access,
production isolation and E2E remain unmeasured. #281's code and its protected
status reconciliation are independently scoped; the clean code review removes
no #282 requirement and proves no target-image behaviour. This routing is not a
review clearance, paid evaluation authorization, publishing permission or push.

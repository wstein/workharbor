---
title: Board tooling
description: Scoped repository queues and reviewed setup of existing GitHub projects.
weight: 9
---

`scripts/board-snapshot.sh` is the host-side access path for project reads and
writes. Keep it in workharbor; consuming repositories supply a mapping rather
than copying an executable board adapter. All commands below are provisional.

## Target mapping

With no mapping, the adapter uses `wstein/workharbor`, user `wstein`, project 6
and `wh/` sessions. A different repository requires an explicit project number
or node ID. It cannot target workharbor project 6. Explicit owner, number and ID
combinations are checked against GitHub before item mutations.

| Environment variable | Meaning |
| --- | --- |
| `WHR_BOARD_REPOSITORY` | Exact `owner/repository` issue queue |
| `WHR_BOARD_OWNER` | Project owner; defaults to the repository owner |
| `WHR_BOARD_PROJECT_NUMBER` | Existing user-owned project number |
| `WHR_BOARD_PROJECT_ID` | Existing project node ID; when both selectors are supplied they must agree |
| `WHR_BOARD_LANE_PREFIX` | Session prefix, default `wh` |
| `WHR_BOARD_ROLES` | Comma-separated role whitelist, default `desk,dispatch,design,review,platform,runtime,docs,verify,spikes` |
| `WHR_BOARD_SNAPSHOT` | Absolute base cache path; nondefault mappings get separate scoped directories even with this override |

The snapshot, field cache, lock and budget log are scoped to the mapping.
Snapshots include target identity; stale fallback never returns another target's
data. Queues contain only Issues from the configured repository, so a PR or an
issue in another repository with the same number cannot become that card.
Configure uses a project-level lock under the base cache directory, shared by
number and ID mappings. Operators should share that base directory when setting
up the same project.

## Existing crewbook project

Werner created crewbook project 10 at `https://github.com/users/wstein/projects/10/views/1`
(sign-in required).
Dispatch reported identity readback `PVT_kwHNjWrOAZaiCg`, owner `wstein`, repository
`wstein/crewbook`, and eight Issue cards after reviewed population on 4 October
2026. The adapter never creates or copies a project.

```bash
export WHR_BOARD_REPOSITORY=wstein/crewbook WHR_BOARD_OWNER=wstein
export WHR_BOARD_PROJECT_NUMBER=10 WHR_BOARD_PROJECT_ID=PVT_kwHNjWrOAZaiCg
export WHR_BOARD_LANE_PREFIX=cb
export WHR_BOARD_ROLES=desk,dispatch,design,review,platform,runtime,docs,verify,spikes
bash scripts/board-snapshot.sh metadata
bash scripts/board-snapshot.sh metadata schema
```

## Reviewed configuration

{{< status unverified >}} Schema and view setup has fixture coverage; live
configuration must wait for independent review of the exact code used.

`configure` requires an explicit repository, owner and project plus confirmation
of the resolved node ID. It refuses project 6. It reads all field, view and
workflow pages before writing; conflicting field types, duplicate names or
unapproved existing options require manual resolution. Existing option IDs are
preserved so card values survive. A failed operation reports partial setup;
read metadata and repeat the same command after resolving the failure. Existing
fields and views are reused by name. Other views and cards remain untouched.

After review clearance and the human's authorization:

```bash
bash scripts/board-snapshot.sh configure-fields PVT_kwHNjWrOAZaiCg
bash scripts/board-snapshot.sh priority 8 P2
bash scripts/board-snapshot.sh configure PVT_kwHNjWrOAZaiCg
bash scripts/board-snapshot.sh metadata schema
```

The schema is Status (`Todo`, `In progress`, `Blocked`, `In review`, `Ready to
push`, `Done`), Priority (`P1`, `P2`, `P3`) and Session (configured project lanes).
Views show Title, Status, Priority, built-in Assignees, Session and Milestone.
Assignees identify the accountable human; configuration does not change issue
assignees. Assign a human only with the appropriate authorization.

`configure-fields` sets only the three single-select fields and verifies their
readback, without depending on view or workflow reads. Its output uses `null`
for unread views and workflows. This lets authorized card updates proceed
before optional view setup. It never updates a card's Status, Priority or
Session itself. Choose card states from the consuming project's profile;
population alone does not establish that an issue has reached review.

| View | Status filter |
| --- | --- |
| Dispatch queue | Todo |
| Active work | In progress |
| Review queue | In review |
| Blocked work | Blocked |
| Release and milestones | Ready to push |

Each filter restricts results to open Issues in the configured repository.
The [documented Projects API](https://docs.github.com/en/graphql/reference/projects)
supports view layout, visible fields and filters. In the UI, open **Dispatch
queue**, choose **Sort**, set **Priority** ascending (`P1`, `P2`, `P3`) and save
the view. Open **Release and milestones**, choose **Group**, select **Milestone**
and save. Verify those saved settings after reloading. Sort and grouping are not
configured by this adapter.

## Automation and limits

{{< status unverified >}} `metadata schema` reports workflow names, IDs and
enabled flags; those fields do not establish trigger conditions or destinations.
Workflow creation and configuration are not implemented because the documented
API does not provide those mutations.

In project 10, open the project menu, choose **Workflows**, inspect **Item
closed**, and verify that only issue closure sets Status to **Done**. Disable
any workflow that sets **Ready to push**, sets **Done** on merge or another
trigger, or closes an issue when Status changes. Save changes, reload and record
the settings in crewbook #7. Inspect each enabled workflow's trigger and action;
an enabled flag alone is insufficient evidence. Verify a real closure only when
the human has authorized closing that issue.

`move` still refuses `Ready to push` and `Done`; only the explicit `ready`
operation represents the independent review gate. Configuration never promotes
cards. Dispatch enforces worker limits from the project profile. Board column
limits are visual aids and do not enforce concurrency; verify profile limits
and saved board settings separately.

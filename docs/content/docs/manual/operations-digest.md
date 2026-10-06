---
title: Operations digest
description: "An index of long-lived operating knowledge: decisions, recurring procedures, how-tos and known gaps, as pointers to their sources."
weight: 16
toc: true
---

This page is an index only. It links to the committed source for each topic and does not copy it. The linked page is authoritative: when this page and a source disagree, the source wins, and the drift is reported (an issue or a comment on the card that touches the row) rather than fixed here by hand. A row that is no longer right is deleted, not amended.

Maintenance: the desk session drafts entries at hand-over and writes no files. The docs session commits and lands them as a normal docs card. Every edit re-checks the touched rows against the source at the commit named in the As of column. A change to `.agents/desk.md` or `AGENTS.md` that tells desk to maintain this page is a separate, Opus-reviewed change.

## Decisions index

Decision rows are IDs in the [decision table](../design/decisions.md). A status other than decided is marked.

| Topic | One-line pointer | Source | Status | As of |
| --- | --- | --- | --- | --- |
| How decisions are recorded | Where a decision is proposed, settled and recorded | [D10](../design/decisions.md) | decided | 4e5d4a8 |
| Documentation site | The site toolchain and its deployment | [D9](../design/decisions.md) | decided | 4e5d4a8 |
| Language and storage | Implementation language, binary shape, database | [D3](../design/decisions.md) | decided | 4e5d4a8 |
| Agents and pushing | Who pushes, and what approves a push | [D18](../design/decisions.md) | decided | 4e5d4a8 |
| Autonomy defaults | Default autonomy per action and its floor | [D36](../design/decisions.md) | decided | 4e5d4a8 |
| Workflow presets | Per-repository workflow presets | [D47](../design/decisions.md) | decided | 4e5d4a8 |
| Releases | Tag, build and distribution of releases | [D24](../design/decisions.md) | decided | 4e5d4a8 |
| Host software | What is installed on the host, and how | [D28](../design/decisions.md) | decided | 4e5d4a8 |
| Remote access | Web UI, API and browser-session exposure | [D29](../design/decisions.md) | decided | 4e5d4a8 |
| Forge access | How GitHub is reached from the product | [D31](../design/decisions.md) | decided | 4e5d4a8 |
| Subscription logins | Vendor terms and the credential boundary | [D40](../design/decisions.md) | decided | 4e5d4a8 |
| Workspaces | Workspaces with named agents | [D42](../design/decisions.md) | decided | 4e5d4a8 |
| External skill sets | Skill-set selection, mounting and the default | [D52](../design/decisions.md), [design](../design/skill-sets.md) | decided | 4e5d4a8 |

## Recurring procedures

| Topic | One-line pointer | Source | Status | As of |
| --- | --- | --- | --- | --- |
| Which sessions run | Sessions to keep open and their roles | [Sessions to keep open](sessions-and-agents.md#sessions-to-keep-open) | decided | 4e5d4a8 |
| Models per role | Subagents and the model each uses | [Subagents and their models](sessions-and-agents.md#subagents-and-their-models) | decided | 4e5d4a8 |
| Review before landing | The review gate | [The review gate](sessions-and-agents.md#the-review-gate) | decided | 4e5d4a8 |
| Landing and worktrees | How work lands, and where it is checked | [Landing and worktrees](sessions-and-agents.md#landing-and-worktrees) | decided | 4e5d4a8 |
| Commit conventions | Message form and trailers | [Commits](sessions-and-agents.md#commits) | decided | 4e5d4a8 |
| Make targets | The project's checks and builds | [Make targets](sessions-and-agents.md#make-targets) | decided | 4e5d4a8 |
| Issue work through REST | How issues are read and written | [Issues through REST](sessions-and-agents.md#issues-through-rest) | decided | 4e5d4a8 |
| Publishing an artifact | The pre-publication secret scan for a file | [Desk instructions](https://github.com/wstein/workharbor/blob/main/.agents/desk.md) | decided | 4e5d4a8 |
| Documentation sessions | What the docs session does and returns | [Docs instructions](https://github.com/wstein/workharbor/blob/main/.agents/docs.md) | decided | 4e5d4a8 |
| Repository rules | Hard rules and ways of working | [AGENTS.md](https://github.com/wstein/workharbor/blob/main/AGENTS.md) | decided | 4e5d4a8 |

## How-tos

| Topic | One-line pointer | Source | Status | As of |
| --- | --- | --- | --- | --- |
| Writing issues and comments | How to write coordination text | [Issues and comments](coordination-writing.md#issues-and-comments) | decided | 4e5d4a8 |
| Shortening an issue body | Keep contracts when trimming | [Preserve contracts](coordination-writing.md#preserve-contracts-before-shortening) | decided | 4e5d4a8 |
| Publishing evidence | Sanitizing and linking evidence | [Publish evidence safely](coordination-writing.md#publish-evidence-safely) | decided | 4e5d4a8 |
| Workflow protocol record | The approved protocol requirements | [Workflow protocols](workflow-protocols.md) | decided | 4e5d4a8 |
| Preparing the host | Step-by-step host setup | [Prepare the Mac mini](host-setup.md) | {{< status unverified >}} | 4e5d4a8 |
| Reaching the host remotely | VPN options for the phone | [Reach it from your phone](host-setup.md#7-reach-it-from-your-phone) | {{< status unverified >}} | 4e5d4a8 |
| Host secrets | Where host secrets live | [Secrets](host-setup.md#12-secrets) | {{< status unverified >}} | 4e5d4a8 |

## Known gaps

| Topic | One-line pointer | Source | Status | As of |
| --- | --- | --- | --- | --- |
| Crewbook split | Remaining integration of the separate crewbook repository | [#283](https://github.com/wstein/workharbor/issues/283) | {{< status open >}} | 2026-10-06 |
| Procedures as skills | Moving land, board, hand-over and delegate procedures out of the prompts | [#240](https://github.com/wstein/workharbor/issues/240) | {{< status open >}} | 2026-10-06 |
| Go tests and HOME | Two tests depend on the caller's HOME and fail in some environments | [#305](https://github.com/wstein/workharbor/issues/305) | {{< status open >}} | 2026-10-06 |
| GraphQL deny rules | The deny rules for direct GraphQL are best-effort, not a boundary | [#314](https://github.com/wstein/workharbor/issues/314) | {{< status open >}} | 2026-10-06 |
| Link fragments | The link checker does not yet check `#anchor` fragments in CI | [#318](https://github.com/wstein/workharbor/issues/318) | {{< status open >}} | 2026-10-06 |

---
title: Sessions and agents
description: Which sessions to keep open while you build workharbor with agents, which model each runs on, and why.
weight: 4
toc: true
---

How to set up the sessions and subagents that build workharbor itself. This is about the development workflow in this repository, not about running agents with `whr`. The rules are in [`AGENTS.md`](https://github.com/wstein/workharbor/blob/main/AGENTS.md) (Project board, Models, Context and cost, GitHub rate limit) and in the lane prompts in [`.agents/`](https://github.com/wstein/workharbor/tree/main/.agents); this page does not copy them. `wh/dispatch` has not run yet, so what it does here is {{< status unverified >}}.

## Sessions to keep open

| Session | Model | When |
| --- | --- | --- |
| `wh/desk` | Sonnet | Always on. Your point of contact: status, discussion, filing and routing ([`desk.md`](https://github.com/wstein/workharbor/blob/main/.agents/desk.md)). |
| `wh/dispatch` | Sonnet | Always on. Pulls cards, starts the lane agents and the reviews, lands, moves cards and hands over ([`dispatch.md`](https://github.com/wstein/workharbor/blob/main/.agents/dispatch.md)) {{< status unverified >}}. |
| `wh/design` | Opus | Only while a decision is waiting. It decides in the issues, writes a resume note and ends ([`design.md`](https://github.com/wstein/workharbor/blob/main/.agents/design.md)). |

Everything else is a subagent that `wh/dispatch` starts, not a session of its own. Open `/wh-desk` and `/wh-dispatch` in two terminals and check the model in each. Ask `wh/desk` for status, not `wh/dispatch`.

## Subagents and their models

Every subagent's model is pinned in `.claude/agents/` and never inherited from the session that starts it.

| Subagent | Model | Used for |
| --- | --- | --- |
| `wh-platform`, `wh-runtime`, `wh-docs`, `wh-verify` | Sonnet | One issue for that lane, in the lane's worktree. |
| `wh-worker` | Sonnet | One research batch, read-only on the repository. |
| `wh-reviewer` | Opus | Review of code and the rule sections. |
| `wh-docs-reviewer` | Sonnet | Review of documentation outside the rule sections only. |
| `wh-helper` | Haiku | A quick, bounded lookup or mechanical edit; changes no git state. |

## The review gate

A `wh-reviewer` or `wh-docs-reviewer` subagent is `wh/review`: its comment `Reviewed by wh/review at <sha>` with no open findings is the review note, and no separate `wh/review` session is needed. The session that started the reviewer, normally `wh/dispatch`, then sets `Ready to push` on its behalf, only for the reviewed sha and only when the comment has no open findings. The author never starts the review of its own change in its own context, and a dispatcher never reviews. A security-relevant change needs the Opus reviewer. You push only `Ready to push` work.

## Setup steps

1. Run `make hooks` in every clone and worktree.
2. Create the lane worktrees once, `git worktree add ../workharbor-<role> --detach main`, for `platform`, `runtime`, `docs` and `verify`.
3. Open `/wh-desk` and `/wh-dispatch` and check their models.
4. Allow subagent starts without a prompt in the dispatch session only. Leave the board writes (`scripts/board-snapshot.sh move`) asking each time.

## Rules of thumb

- At most 2 code workers (issue subagents that edit) run at the same time, each in its own lane's worktree, and only one editing subagent per worktree.
- Keep the Opus session short and end it after each decision.
- Do not hand running agents to a new session.

## Why

A long session pays for its whole history on every turn. Measured on 3 October 2026: Opus cost $25.38 of $35.69, and about 80 % of its tokens were the `wh/design` session re-reading its own history (54M of 67.4M cache reads over 209 requests) {{< status verified >}}. So the long-lived sessions run on Sonnet, Opus is used where it pays (decisions and reviews) and ends quickly, and each issue runs in a fresh subagent whose context is discarded when it returns. The cap of 2 code workers also spares the one GitHub token every session shares ([rate limit](https://github.com/wstein/workharbor/blob/main/AGENTS.md)).

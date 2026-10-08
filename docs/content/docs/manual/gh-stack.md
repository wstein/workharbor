---
title: Stack pull requests with gh stack
description: How-to for related or dependent issues that land as a stack of pull requests.
weight: 18
---

Related or dependent issues are stacked automatically: the desk decides this at dispatch time, so the issue needs no hint, and records the stack order in the dispatch note. Independent topics stay single pull requests. The size budget (10 commits, about 500 lines) applies per pull request, not per stack. The counterpart in the crewbook is issue 79. Commands are provisional; the review gate is in [Pull request flow](sessions-and-agents.md#pull-request-flow-412).

## Who may run what

On their own branches, and when the human allows it, agents may run `gh stack init`, `add`, `push`, `submit`, `view`, `sync` and `rebase`. Agents never run `gh stack merge`: the human merges, with rebase.

## Adopt existing branches

Measured by the desk on a real repository with crewbook pull requests 85 and 89 (89 on top of 85):

1. Make the local topic branch pointer equal the reviewed head first. A review (for example an Opus CLEAR) names a head SHA, and `gh stack` leaves heads unchanged, so the CLEAR stays valid.
2. `gh stack init <bottom> <top>` adopts the existing branches and an already open pull request.
3. `gh stack submit --auto` creates only the missing pull request, as a draft with the lower branch as its base, and links the stack.
4. Fix the `Closes #N` line of each body with `gh pr edit`, because the generated body does not carry it.
5. The human merged the stack with `gh stack merge --rebase`.

The desk posts the `review/<tier>` status on each head SHA as usual; `scripts/stack-status.sh` does this for a stack.

## Status in workharbor

{{< status unverified >}} The same run has not been repeated on a workharbor pull request. The lab findings are in issue 421.

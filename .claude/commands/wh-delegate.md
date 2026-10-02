---
description: Hand a quick task to a helper from the pool, then review and land its result
argument-hint: "<helper session> <task>"
---

Delegate this to a helper ($ARGUMENTS). Only tasks of the kinds in
`.agents/helper.md` (find and report, board hygiene, mechanical edits, small
tests, checks); never a rule section, security-relevant code, a design choice
or anything outward.

1. Send the helper one message with: the task in one sentence, the files or
    area, what "done" means, `Refs: #N` if any, and whether it may commit (on its
    own branch `helper/<N>/<topic>`, never `make land`).
2. When it replies, review its branch like a reviewer: `git log -p
    main..helper/<N>/<topic>`, run the tests it touched. Its work reaches `main`
    only through you; that review approves the landing on local
    `main`, and the push gate (`wh/review` on your card) still applies.
3. Land it from your own worktree: `git rebase main helper/<N>/<topic>` on a
    branch of yours, then `/wh-land`. Tell the helper it can delete its branch,
    or send it back with what to change.
4. Credit stays in the helper's `Assisted-by` trailer; the card stays yours.

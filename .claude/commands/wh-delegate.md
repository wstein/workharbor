---
description: Hand a quick task to a helper from the pool, working in your worktree
argument-hint: "<helper session> <task>"
---

Delegate this to a helper ($ARGUMENTS). Only tasks of the kinds in
`.agents/helper.md` (find and report, board hygiene, mechanical edits, small
tests, checks); never a rule section, security-relevant code, a design choice
or anything outward.

1. Send the helper one message: your worktree's absolute path, the exact files
    it may change (or "read-only"), what "done" means, and the check to run.
    Do not edit those files yourself until it reports back.
2. When it reports, review its change in your worktree (`git diff`), run the
    check yourself, and fix or send back what is wrong.
3. Commit it on your branch with `Assisted-by: Claude Code:<helper model-id>`
    added to your own trailer, then land as usual (`/wh-land`). The card stays
    yours, and the push gate (`wh/review`) still applies to it.

---
description: Rebase your branch onto main and land it with make land, retrying safely
argument-hint: "[branch, default: the current one]"
---

Land a finished branch on local `main` as AGENTS.md step 3 describes. Branch:
$ARGUMENTS (empty: the current branch). Never push.

1. Check the worktree: you are in your own worktree, not the shared checkout;
    `git status --short` is empty (commit or ask first; never stash someone
    else's work). The branch has commits that `main` lacks.
2. Squash your own fixups: `GIT_SEQUENCE_EDITOR=: git rebase -i --autosquash
    main`. If the rebase conflicts in a file your own issue changed (a second
    worktree of your lane landed first: AGENTS.md, A second worktree), resolve
    it and run the tests; any other conflict: stop and report, and do not
    resolve someone else's code by guessing.
3. Note `git rev-parse main`, run `make land` and read its last lines:
    - `land: main is now <sha>`: done; go to 4.
    - `main moved during the checks` or `is not on top of main`:
      `git rebase main`, then start again at 3 (noting `main` anew).
    - `Not possible to fast-forward` or an `index.lock` error from the merge:
      if `main` differs from the sha you noted, another lander won the race:
      `git rebase main` and start again at 3 (noting `main` anew). If `main` did not move,
      stop and report to the human: a stale lock in the shared checkout is
      theirs to clear. Never remove `index.lock` (or anything else) in the
      shared checkout yourself, whatever git's message suggests.
    - `Rejected status code: 50x` from github.com in the link check: not your
      content; wait 60 seconds and run it again, at most 4 times, then report.
    - Anything else (tests, lint, commitlint, secrets): stop, fix it and fold
      the fix into the commit it belongs to (own unpushed commits only;
      AGENTS.md, Commits), then start again at 2. Never use `--no-verify`.
      - A failure in the content (tests, lint, secrets, build): stage the fix
        and run `git commit --fixup <sha>`.
      - A failure of the commit message only (a subject over 72 characters, a
        missing trailer): `--fixup=reword:<sha>` ignores staged changes and
        opens an editor, which an agent session does not have. Write the
        corrected message to a file and give git an editor that keeps the
        first line (the `amend!` marker autosquash matches on) and replaces
        the rest:
        `GIT_EDITOR="sh -c 'head -n1 \"\$1\" > \"\$1.n\"; echo >> \"\$1.n\"; cat <msgfile> >> \"\$1.n\"; mv \"\$1.n\" \"\$1\"' --" git commit --fixup=reword:<sha>`
      - Then `GIT_SEQUENCE_EDITOR=: git rebase -i --autosquash main`. Never
        touch the keychain or the global git configuration.
4. Only after a successful land: `git switch --detach main`, then
    `git branch -d <branch>` (`-D` only after `git merge-base --is-ancestor
    <branch> main` confirms it is in).
5. Set the card of each issue to `In review`, and report the landed commits
    (`git log --oneline <old main>..main`) to `wh/design`.

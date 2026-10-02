---
description: Report what is ready to push, what it closes and what waits on Werner
---

Write the hand-over for Werner. Read only; change nothing.

1. `git fetch -q origin`, then in the shared checkout:
    `git log --oneline origin/main..main`. Say how many commits are unpushed and
    whether `main` is a fast-forward of `origin/main`
    (`git merge-base --is-ancestor origin/main main`).
2. The issues the push would close: the `Closes:`, `Fixes:` and `Resolves:`
    trailers in that range.
3. From project 6 (`gh project item-list 6 --owner wstein --format json`):
    - `In review`: not yet reviewed, so not ready to push;
    - `Ready to push`: each with its `Reviewed by wh/review at <sha>` note,
      checked that the sha is in `main`;
    - waiting on Werner: cards with `Session` `Werner`, and `Blocked` cards
      whose issue says they wait on him.
4. Anything unverified that the unpushed commits introduce (new
    `status unverified` markers in the diff).

Answer in a short list: push or not yet, why, and the exact command
(`git push origin main`) when it is ready. Never push yourself.

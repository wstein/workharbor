---
description: Check the project board against the issues and main, and list the drift
argument-hint: "[--fix to correct your own lane's cards]"
---

Check project 6 against the issues and the repository. Read the board through
the snapshot (`scripts/board-snapshot.sh`, at most one query per 5 minutes for
all lanes; add `--refresh` only after you moved a card), never with
`gh project item-list` yourself. Only `wh/design` runs this board-wide check
(AGENTS.md, GitHub rate limit); other lanes check their own cards
(`scripts/board-snapshot.sh card <number>`). $ARGUMENTS

Report each drift with the issue number:

- A closed issue whose card is not `Done`.
- An open issue whose card is `Done`.
- `Ready to push` without a `Reviewed by wh/review at <sha>` comment, or with a
  sha that is not in `main`.
- `In review` or `Ready to push` whose commits are already on `origin/main` and
  whose issue is still open (tick and close, or say what is left).
- `In progress` with no commit on any branch for a day, or no `Session`.
- An acceptance criterion ticked in the issue body without a commit or comment
  that shows it, or a closed issue with unticked criteria and no comment
  saying why.
- A card that is not on the board at all (`gh issue list --state open` minus the
  board).

Without `--fix`, change nothing. With `--fix`, correct only cards of your own
lane (AGENTS.md: move your own cards only) and list the rest for their lane or
`wh/design`.

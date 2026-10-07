---
title: Sessions and agents
description: Which sessions to keep open while you build workharbor with agents, which model each runs on, and why.
weight: 4
toc: true
---

How to set up the sessions and subagents that build workharbor itself. This is about the development workflow in this repository, not about running agents with `whr`. The rules are in [`AGENTS.md`](https://github.com/wstein/workharbor/blob/main/AGENTS.md) (Project board, Models, Context and cost, GitHub rate limit) and in the lane prompts in [`.agents/`](https://github.com/wstein/workharbor/tree/main/.agents); this page does not copy them, except the procedure detail that left `AGENTS.md` to keep it under the 24,000-byte cap of Antigravity's always-on rules {{< status unverified >}} (the last section; #233). A rule or prohibition always stays stated in `AGENTS.md` itself. `wh/dispatch` has not run yet, so what it does here is {{< status unverified >}}.

## Sessions to keep open

| Session | Model | When |
| --- | --- | --- |
| `wh/desk` | Sonnet | Always on. Your point of contact: status, discussion, filing and routing ([`desk.md`](https://github.com/wstein/workharbor/blob/main/.agents/desk.md)). |
| `wh/dispatch` | Sonnet | Always on. Pulls cards, starts the lane agents and the reviews, lands, moves cards and hands over ([`dispatch.md`](https://github.com/wstein/workharbor/blob/main/.agents/dispatch.md)) {{< status unverified >}}. |
| `wh/design` | Opus | Optional. You can open an Opus `wh/design` session yourself while a decision is waiting; it decides in the issues, writes a resume note and ends ([`design.md`](https://github.com/wstein/workharbor/blob/main/.agents/design.md)). Normally `wh/dispatch` starts a design subagent instead (below). |

The lane agents, the reviewers and the helpers are subagents that `wh/dispatch` starts, not sessions of their own. Open the `wh/desk` and `wh/dispatch` prompts in `.agents/` in two sessions and check the model in each. Ask `wh/desk` for status, not `wh/dispatch`.

## Development branch names

New issue-work branches for developing workharbor use
`<category>/<issue>-<slug>`. The category is an existing Conventional Commit
type (`feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`,
`style` or `revert`), or `spike` for measurement work. The issue is the actual
positive issue number, without `#` or leading zeros. The short slug uses
lowercase letters and digits in words separated by single hyphens; it starts
and ends with a letter or digit. For example, `feat/331-landing-queue` and
`spike/89-bundle-export`.

This convention applies to development issue branches, including new spike
branches. Product branches created by `whr` (such as `agent/<role>`), `main`,
remote refs, bot branches and test fixtures keep their existing contracts.
Rename only a branch you own; coordinate any rename with its author. A rename
keeps the commit SHA, so a review note bound to that SHA remains valid. Local
landing confirmations also record the branch name; keep their recorded
provenance intact and obtain any required confirmation for the current branch
when landing.

## Shared and personal Claude Code settings

The tracked `.claude/settings.json` shares project permissions without granting access to a maintainer’s external checkouts. Put personal paths and standing approvals (never board writes: [`AGENTS.md`](https://github.com/wstein/workharbor/blob/main/AGENTS.md), GitHub rate limit) in `.claude/settings.local.json`, which Git ignores, including when you create it by hand. Keep credentials out of both files.

The [Claude Code permission grammar](https://code.claude.com/docs/en/permissions#read-and-edit) defines `~/` file rules from the current user’s home and `//` rules from the filesystem root. The shared sensitive-directory denies also match those directories outside the current home. Bash rules match command text; their wildcards do not resolve a home directory or provide process isolation. Native enforcement across macOS and Linux remains {{< status unverified >}}; static configuration checks do not complete the session-usage verification in #274.

## Subagents and their models

The repository no longer ships pinned Claude Code subagent types or slash commands (`wh-*`). A session or subagent that a lane starts has its model set explicitly by whoever starts it, never inherited. Tool grants are not enforced any more either: the read-only helper and the read-only reviewers were limited by `tools:` frontmatter in the deleted prompts and are now prose only, so the starter (the dispatcher's Agent call) must restrict the tools.

A decision that loosens a Hard rule or a security control, changes release scope or order, costs money, publishes or sets product direction is not made by the subagent: it goes to you first, through `wh/desk` ([`AGENTS.md`](https://github.com/wstein/workharbor/blob/main/AGENTS.md), Models).

## The review gate

A review subagent is `wh/review`: its comment `Reviewed by wh/review at <sha>` with no open findings is the review note, and no separate `wh/review` session is needed. The designated dispatcher then sets `Ready to push` on its behalf, only for the reviewed sha and only when the comment has no open findings. The author never starts the review of its own change in its own context, and a dispatcher never reviews. A security-relevant change needs the Opus reviewer. You push only `Ready to push` work.

## Setup steps

1. Run `make hooks` in every clone and worktree.
2. Create the lane worktrees once, `git worktree add ../workharbor-<role> --detach main`, for `platform`, `runtime`, `docs` and `verify`, and `../workharbor-platform-2` for a second platform issue at the same time.
3. Open the `wh/desk` and `wh/dispatch` prompts and check their models.
4. Allow subagent starts without a prompt in the dispatch session only. The dispatcher's card moves are covered by [`AGENTS.md`](https://github.com/wstein/workharbor/blob/main/AGENTS.md) (GitHub rate limit); do not add a settings allow for them. Leave other board writes (`ready`, `session`, `priority`, `add`) asking each time.

### Client capacity

For development sessions using [crewbook](../glossary.md#names), start with capacity for eight subagents. `wh/dispatch` is an always-on session in its own terminal (see the table above), not a subagent of `wh/desk`, so neither session counts. From the dispatch session the subagents are two authors, two independent reviewers, one design batch and two bounded helpers: seven of the eight slots, leaving one spare for a third helper or reviewer. A count from the desk session needs no more. This is a capacity recommendation, not a measured optimum or a request to fill every slot {{< status unverified >}}. The limits in [`AGENTS.md`](https://github.com/wstein/workharbor/blob/main/AGENTS.md) still apply: at most two code workers, one editor per worktree, disjoint editing scopes and explicitly pinned role models. When actual capacity is lower, sequence work while preserving coordination and independent review.

**Codex.** For a fresh crewbook session from the repository:

```sh
codex -m gpt-6.1-sol -c model_reasoning_effort="low" -c agents.max_concurrent_threads_per_session=8 '$crewbook'
```

`$crewbook` invokes the crewbook skill by name in Codex (the skill's public name is `crewbook`, per the [glossary](../glossary.md#names)) {{< status unverified >}}; the single quotes keep the shell from expanding `$crewbook` as a variable and pass the text literally. The command sets no lane: the session becomes `wh/dispatch` or `wh/desk` when you paste [`.agents/dispatch.md`](https://github.com/wstein/workharbor/blob/main/.agents/dispatch.md) or [`.agents/desk.md`](https://github.com/wstein/workharbor/blob/main/.agents/desk.md) into it, as those prompts say. To persist just the capacity setting, add it to the existing `[agents]` table in user configuration; create that table only if it is absent:

```toml
[agents]
max_concurrent_threads_per_session = 8
```

The [official OpenAI configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference) defines this as the limit on concurrently open spawned-agent threads, excluding the primary thread {{< status unverified >}}. Codex chooses the default when unset {{< status unverified >}}. `agents.max_threads` is the legacy alias {{< status unverified >}}. A completed agent's turn does not by itself establish that its thread has closed; use the host's reported capacity.

**Claude Code.** For version 2.1.217 or later {{< status unverified >}}, start a session with:

```sh
CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS=8 CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH=3 claude
```

The [official nesting documentation](https://code.claude.com/docs/en/sub-agents#let-subagents-spawn-their-own-subagents) defines spawn depth as subagent layers below the main conversation {{< status unverified >}}. From the dispatch session, dispatch → author → bounded helper is two layers, so a depth of 2 suffices; 3 stays valid and leaves one spare layer, and the desk session is not an ancestor of either. Versions 2.1.217–2.1.218 default to one layer; 2.1.219 and later default to three {{< status unverified >}}. The explicit depth setting makes the example work with either default. It does not replace pinned tool restrictions, permissions or repository controls.

The [official concurrency documentation](https://code.claude.com/docs/en/sub-agents#concurrent-subagent-limit) accepts a positive integer {{< status unverified >}} and gives a default of 20 {{< status unverified >}}. It counts running subagents and blocks new Agent-tool starts at the limit {{< status unverified >}}. `/subtask` forks occupy slots but bypass the cap; resuming finished subagents can exceed it {{< status unverified >}}. Ultracode sessions are exempt, and workflows and agent teams have separate limits {{< status unverified >}}. Keep the existing role model pins; this setting changes capacity only.

**Antigravity (`agy`).** The [official subagent documentation](https://www.antigravity.google/docs/subagents/) describes parallel subagents and the `/agents` panel for inspecting their states {{< status unverified >}}. Inspect available capacity in the actual host and sequence work as needed. That source establishes no equivalent numerical concurrency setting {{< status unverified >}}; its nesting depth of ten is not a concurrency limit {{< status unverified >}}. Native role bindings and production support for this workflow remain {{< status unverified >}}.

## The project board

The sessions share one GitHub token, and a board query is the expensive call, so they read and write the board only through `scripts/board-snapshot.sh` (needs `bash`, `jq` and a logged-in `gh`; it holds no token). Never run `gh project item-list` or `gh project item-edit` yourself. The rules are in [`AGENTS.md`](https://github.com/wstein/workharbor/blob/main/AGENTS.md) (GitHub rate limit, Who sets which status); the script's header comment lists every mode.

```text
scripts/board-snapshot.sh                     # the snapshot JSON, cached for 5 minutes
scripts/board-snapshot.sh queue wh/platform   # the lane's Todo cards, P1 first, then by issue number
scripts/board-snapshot.sh card 132            # one card
scripts/board-snapshot.sh --refresh           # force a query
scripts/board-snapshot.sh budget              # refreshes, their cost and the lowest GraphQL budget left, last 24 hours; no gh call (#186)
```

Who may write cards, and what still asks, is set once in [`AGENTS.md`](https://github.com/wstein/workharbor/blob/main/AGENTS.md) (GitHub rate limit): authors, reviewers, design and `wh/desk` report outcomes to the dispatcher. `move` sets `Todo`, `In progress`, `Blocked` and `In review`; it refuses `Ready to push` and `Done` before any call. `Ready to push` is approved only by `wh/review` after its review comment and written by the designated dispatcher on its behalf for the reviewed sha, with `scripts/board-snapshot.sh ready <number>`; `Done` follows when the issue closes. A card moved by hand in the browser is not seen until the snapshot is 5 minutes old or a read passes `--refresh`.

## Rules of thumb

- At most 2 code workers (issue subagents that edit) run at the same time, each in its own worktree (two may both be `wh/platform`, in `../workharbor-platform` and `../workharbor-platform-2`, when their issues touch no file in common), and only one editing subagent per worktree.
- Keep the Opus session short and end it after each decision.
- Do not hand running agents to a new session.

## Why

A long session pays for its whole history on every turn. Measured on 3 October 2026 (Werner's usage report, recorded in [#167](https://github.com/wstein/workharbor/issues/167)): Opus cost $25.38 of $35.69, and about 80 % of its tokens were the `wh/design` session re-reading its own history (54M of 67.4M cache reads over 209 requests) {{< status verified >}}. So the long-lived sessions run on Sonnet, Opus is used where it pays (decisions and reviews) and ends quickly, and each issue runs in a fresh subagent whose context is discarded when it returns. The cap of 2 code workers also spares the one GitHub token every session shares ([rate limit](https://github.com/wstein/workharbor/blob/main/AGENTS.md)).

## Procedure detail moved out of AGENTS.md

`AGENTS.md` stays below 20,000 bytes (a test in `internal/docscheck` fails above 24,000: #233). What follows is procedure and reference that a session needs only when it does the step; every rule and prohibition is still stated in `AGENTS.md`.

### Make targets

```bash
make build         # go build -o bin/whr ./cmd/whr
make test          # go test ./...  (make test-short skips the slowest, for the inner loop)
make race          # go test -race on the packages with goroutines of their own
make fmt           # gofumpt + goimports (make fmt-check fails on unformatted sources)
make lint          # golangci-lint (pinned; runs via go run)
make check-local   # fmt-check, lint, editorconfig, enabled hooks: before every commit
make check         # full fmt/vet/lint/editorconfig/test/race suite: CI or explicit human request
make check-ci      # what CI runs beyond make check: docs build, typos, lychee, gitleaks, actionlint
make commitlint    # check this branch's commits against the commit rules
make land BRANCH=<name> # local checks, commit/range scans, affected templates, then fast-forward main
make hooks         # enable hooks and the commit template (once per clone and worktree)
make generate      # compile the web UI's templ templates (the generated files are committed)
make docs          # build the Hugo site into _site (make docs-serve for live reload)
make temp-ls       # list temporary containers, volumes, networks and images (LANE=<lane> to narrow)
make temp-clean    # remove one lane's: LANE=<lane> is required
```

For each work item, run focused behaviour and regression checks for the changed code and affected dependencies, including race checks where concurrency changes. For documentation, check the affected build, links and wording; for generated files, verify source and output together. Record commands, outcomes, scope and exact candidate SHA in the handoff. Independent review covers that exact SHA after a rewrite. Reuse successful unaffected evidence; repeat checks only for relevant changes, failures or unresolved concerns.

`make land BRANCH=<name>` runs local mechanical checks and scans the pinned candidate range for secrets; it checks generated templates when that range changes a template or its generated Go file. Focused test selection and evidence remain the author and reviewer's responsibility, not a receipt enforced by make. Full local `make check` and `make check-ci` suites require an explicit human request before push; hosted CI retains its full suites. Hooks always run and are never bypassed.

The `Makefile` also has `install`, `install-release`, `changelog` and `editorconfig`. `make check-ci` needs `typos` and `lychee` (`brew install typos-cli lychee`).

### Lanes

The models and tasks of the lanes are in the [sessions table](#sessions-to-keep-open) near the top of this page and in [`AGENTS.md`](https://github.com/wstein/workharbor/blob/main/AGENTS.md) (Models). Two lanes are not in the table: `wh/review` (Opus) reviews independently before every push and is at least as strong as the author, and `wh/spikes` runs spikes on new tools on Antigravity (Gemini); `wh/review` reviews its results. A helper (`.agents/helper.md`) does one quick task for a lane: find and report, web research and issue drafts go to a read-only helper; mechanical edits, small tests and checks go to an editing helper. Board writes stay with the designated dispatcher through `scripts/board-snapshot.sh`; the lane reports outcomes to it. A helper is not a lane, and neither type edits a security-relevant path.

### Commits

```text
feat(domain): add run interrupted state

Why the change was made.

Refs: #12
Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>
```

Use the actual exposed model ID (`unknown` if unavailable), never a guessed model. The examples apply only to sessions exposing that model. Attribution identities and the legacy `Assisted-by` compatibility rule are in [AGENTS.md](../../../../AGENTS.md).

`Refs` and `Closes` also accept `owner/repo#12` and comma lists. The changelog lists `feat`, `fix`, `perf`, `revert` and breaking changes. The repository allows only rebase merges, so every commit lands on `main` as written. Only Werner force-pushes, by lifting the `main` ruleset for it.

### Spike pages

A spike page under `docs/content/docs/spikes/` links the spike branch once it is pushed and says "a local branch until it is pushed" before that. Spike files that reached `main` anyway are removed from its tree, and the page links them at the commit that added them, which stays in `main`'s history (#194).

### Layout

The design is in `docs/content/docs/design/` (start at `_index.md`; §3 `decisions.md`, §4 `domain.md`, §5 and §8 `architecture.md`, §6 and §7 `security.md`, §9 and §10 `interfaces.md`, §11 to §13 `roadmap.md`), the threat model in `threat-model.md`; also `glossary.md`, `spikes/` and `manual/`. `cmd/` holds `whr` (the single binary), `whr-proxy` (the egress proxy in the sidecar), `whr-shim` (the in-guest launcher, D25) and `commitlint`. Among the packages of `internal/`: `domain`, `policy`, `store`, `hostgit`, `service`, `api`, `cli`, `serve`, `web`, `config`, `toolstore`, `egress`, `devcontainer`; the adapter contracts in `runtime/`, `agent/`, `forge/` and `ci/` have fakes and conformance suites in `runtimetest/` and `agenttest/`.

### Issues through REST

`gh api repos/wstein/workharbor/issues/<n>` reads an issue, `.../issues/<n>/comments -f body=...` comments, `-X PATCH .../issues/<n> -f body=...` ticks criteria, and `-X POST .../issues` opens one, then `scripts/board-snapshot.sh add` puts it on the board. REST does not prompt for labels, so a new issue carries one type label (`bug`, `enhancement`, `documentation`, `ops`, `decision`, `spike`, `security`, ...) and an `area:` label where one fits, set on the create with `-f "labels[]=<name>"` once per label. Before stopping for a rate limit, `gh api rate_limit` costs no points; a secondary limit with points left is waited out once for a minute and retried once (#214). `--url` on a project command trips a secondary limit even with points left, which is why the board goes through the script.

### Landing and worktrees

A session uses the explicit `make land BRANCH=<name>` form from its own worktree, subject to the landing policy (AGENTS.md, step 3). Before landing, your worktree is your own and `git status --short` is empty (commit or ask first; never stash someone else's work). Note `git rev-parse main`, run `make land BRANCH=<name>` and read its last lines:

- `land: main is now <sha>`: landed.
- `main moved during the checks` or `is not on top of main`: `git rebase main`, then run it again (noting `main` anew).
- `Not possible to fast-forward` or an `index.lock` error from the merge: if `main` differs from the sha you noted, another lander won the race, so `git rebase main` and run it again. If `main` did not move, stop and report a stale lock to the human: it is theirs to clear. Never remove `index.lock`, or anything else, in the shared checkout, whatever git's message suggests.
- `the shared checkout's index is stale` or `the shared checkout's index differs from HEAD after the merge` (both end in `repair it with: git -C <shared> reset ...`): stop and report that line to the human, and never run the printed `reset` or any other git command there. The first message comes before the merge, so nothing landed. The second comes after `land: main is now <sha>`, so the land succeeded: go on with the cleanup and report the repair as open.
- `candidate moved during the checks`: the branch changed while the checks ran; run `make land BRANCH=<name>` again only on the intended, unchanged branch.
- `the shared checkout <path> is not on main` or `the shared checkout left main during the checks`: stop and tell the human; never switch it yourself.
- A temporary-worktree failure (`cannot create the temporary worktree`, `could not remove the temporary worktree`), `introduces merge commits` (rebase to a linear history), or a SHA or BRANCH error (unknown or ambiguous sha, no review note, not a branch tip, no or several worktrees): stop and report the message; fix only what it names.
- Anything else (tests, lint, commitlint, secrets): fix it and fold the fix into the commit it belongs to (your own unpushed commits only), then land again; never `--no-verify`. For a failure in the content, stage the fix and run `git commit --fixup <sha>`. For a failure of the commit message only, `--fixup=reword:<sha>` opens an editor, which an agent session does not have: write the corrected message to a file and give git an editor that keeps the first line (the `amend!` marker that autosquash matches on) and replaces the rest: `GIT_EDITOR="sh -c 'head -n1 \"\$1\" > \"\$1.n\"; echo >> \"\$1.n\"; cat <msgfile> >> \"\$1.n\"; mv \"\$1.n\" \"\$1\"' --" git commit --fixup=reword:<sha>`. Then `GIT_SEQUENCE_EDITOR=: git rebase -i --autosquash main`.

After a successful land, `git switch --detach main`, then `git branch -d <branch>`; use `-D` only after `git merge-base --is-ancestor <branch> main` confirms the branch is in. Report the landed commits (`git log --oneline <old main>..main`) to the designated dispatcher, which sets each card to `In review`, and to the design lane. These messages and the guards behind them are in the `Makefile`'s `land` target, which is what runs.

`make land BRANCH=<name>` does the same from any checkout of the repository, for a ready branch whose worktree you do not remember. It finds the worktree that has `<name>` checked out (`git worktree list --porcelain`), changes into it for the run and applies the landing guards: the shared checkout on `main`, the branch on top of `main`, no merge commits, `make check-local`, `make commitlint`, the secrets range scan, and the `main`-moved and candidate-moved checks. It never switches a checkout. It refuses, with a message, when no worktree has the branch (a detached worktree does not count), when more than one does, when the worktree is prunable (its directory is gone), in a bare repository, and for `main`, an empty name, a name that starts with a dash or is not a valid branch name; give the short name, not `refs/heads/<name>`. Names containing `$` must be written `$$` on the make command line, because make expands its arguments first. A `BRANCH` in your shell environment is ignored; only the command-line form counts.

For a human, `make land` with no arguments starts a line-oriented terminal wizard (#331). It lists stamped local branch tips in lexical branch order with their titles, review notes, stamp matches, path classes and diffstats, and explains the checks that will run. Choose a numbered candidate, or `f` for optional `fzf` selection when installed; `q` cancels. From a worktree, it asks before using the shared checkout's current-main recipe for the run, without switching any checkout. A shared checkout off `main`, a selected branch that moved, or a branch behind `main` is refused with the next step. A rebase or changed tip needs a fresh independent review. Selection passes the exact SHA to the existing resolver for its summary and human confirmation: `y` for ordinary changes, the seven-character SHA for a carve-out. Success records the confirmation and prints a push hint; it never pushes. Without a terminal it refuses and points to `land-list` and `land-preview`. The explicit SHA and BRANCH forms remain available, and `make -n`, `-t` and `-q` never run the wizard or land.

`make land SHA=<sha>` (7 to 40 lowercase hex characters; #315) lands one exact commit from any worktree or the shared checkout, with no `BRANCH`: `scripts/land.sh`, read from `main`'s own blob, resolves the commit to the one local branch whose tip it is (it refuses an inner commit, a tip shared by several branches, and `main` itself), requires a review note (`git notes --ref=review`) that names that same full sha, derives the path class from the diff against `main` (carve-out for `AGENTS.md`, `.agents/`, `.claude/`, `.github/`, scripts, code and the design or threat-model pages; ordinary for documentation and the exempt packages), prints it all and asks the human on a terminal (`y`, or the first 7 characters of the sha for a carve-out). Run it from the shared checkout, whose `Makefile` runs. With `BRANCH=<name>` the `SHA` must be the full 40-character id and only compares it with the branch tip: it refuses when the tip differs, for example because the branch moved after the review, and it does not check that a review covered it. Paste-ready: `make land 'BRANCH=<name>' SHA=<sha>`.

After a successful human-confirmed `make land SHA=<sha>` (including `land-next` and `land-all`), the pinned resolver writes a canonical v1 JSON record to `refs/notes/confirm` on that commit. It retains the answer time, actual `yn` or `typed_sha` answer, displayed review-note object and ref, base, path class and passed checks, with `channel: cli`, `by: human` and `assurance: local`. This is an honest log, not proof against an agent running as the same user. The Git note stores the canonical JSON payload followed by one LF; that LF is storage framing and is excluded from the record digest. Notes remain local in v1. `python3` is required for this writer. A refusal, preview or failed fast-forward creates no record; the branch-only forms have no human answer and create none. If writing fails after the fast-forward, the error explicitly reports that `main` moved but the note was not recorded; it never undoes the landing or silently replaces an existing record.

`make land-list` shows local branches not merged into `main`, in lexical branch-name order, with each tip, review note, stamp match, path class and diffstat. `make land-next` takes the first branch with a review note; `make land-all` visits those branches in the same order. Unstamped branches remain visible in the list but are skipped during execution. A stale stamp or any phase 1 refusal stops the queue immediately; each candidate keeps its own review note and confirmation. Queue tips are captured before execution and checked again before use. Independent branches may need rebasing and a fresh review after an earlier landing advances `main`.

`make land-preview SHA=<sha>` reuses the phase 1 resolver and prints the candidate information without prompting, running checks or landing. It requires the same unique branch tip and matching review note, works without a terminal, and writes no state or confirmation record. The queue and preview load `scripts/land.sh` from `main`, so they become available after this implementation reaches local `main`. As with phase 1, run them from the shared checkout so its `Makefile` runs.

The landing targets refuse caller-selected recipe shells, including a command-line `SHELL=/bin/sh` or `SHELL='/bin/sh -n'`. Leave the recipe shell to make; an inherited `SHELL` that make ignores remains supported. Landing check sub-makes use `/usr/bin/env` to clear make flags so an exported `env` function cannot swallow them. This narrows accidental or malicious check bypasses; it does not sandbox arbitrary shell code already supplied by the caller.

Each lane's worktree is created once with `git worktree add <worktree> --detach main` and `make hooks` (the Setup steps above). In `wh/platform`'s second worktree, `workharbor.lane=wh/platform-2` keeps its temporary resources out of `make temp-clean LANE=wh/platform`.

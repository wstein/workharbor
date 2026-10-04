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
| `wh/design` | Opus | Optional. You can open an Opus `wh/design` session yourself while a decision is waiting; it decides in the issues, writes a resume note and ends ([`design.md`](https://github.com/wstein/workharbor/blob/main/.agents/design.md)). Normally `wh/dispatch` starts the `wh-design` subagent instead (below). |

The lane agents, the reviewers and the helpers are subagents that `wh/dispatch` starts, not sessions of their own. Open `/wh-desk` and `/wh-dispatch` in two terminals and check the model in each. Ask `wh/desk` for status, not `wh/dispatch`.

## Subagents and their models

Every subagent's model is pinned in `.claude/agents/` and never inherited from the session that starts it.

| Subagent | Model | Used for |
| --- | --- | --- |
| `wh-platform`, `wh-runtime`, `wh-docs`, `wh-verify` | Sonnet | One issue for that lane, in the lane's worktree. |
| `wh-worker` | Sonnet | One research batch, read-only on the repository. |
| `wh-design` | Opus | The decisions waiting in the issues, and nothing else. Only `wh/dispatch` starts it, in one batch when decisions wait and at most once an hour unless a P1 is blocked. |
| `wh-reviewer` | Opus | Review of code and the rule sections. |
| `wh-docs-reviewer` | Sonnet | Review of documentation outside the rule sections only. |
| `wh-helper` | Haiku | A quick, bounded lookup or web research; read-only (no Edit, Write or Bash). |
| `wh-helper-edit` | Haiku | A mechanical edit or a check that runs a command, on files the requester names one by one; never a security-relevant path; changes no git state. |

A decision that loosens a Hard rule or a security control, changes release scope or order, costs money, publishes or sets product direction is not made by the subagent: it goes to you first, through `wh/desk` ([`AGENTS.md`](https://github.com/wstein/workharbor/blob/main/AGENTS.md), Models).

## The review gate

A `wh-reviewer` or `wh-docs-reviewer` subagent is `wh/review`: its comment `Reviewed by wh/review at <sha>` with no open findings is the review note, and no separate `wh/review` session is needed. The session that started the reviewer, normally `wh/dispatch`, then sets `Ready to push` on its behalf, only for the reviewed sha and only when the comment has no open findings. The author never starts the review of its own change in its own context, and a dispatcher never reviews. A security-relevant change needs the Opus reviewer. You push only `Ready to push` work.

## Setup steps

1. Run `make hooks` in every clone and worktree.
2. Create the lane worktrees once, `git worktree add ../workharbor-<role> --detach main`, for `platform`, `runtime`, `docs` and `verify`, and `../workharbor-platform-2` for a second platform issue at the same time.
3. Open `/wh-desk` and `/wh-dispatch` and check their models.
4. Allow subagent starts without a prompt in the dispatch session only. Leave the board writes (`scripts/board-snapshot.sh move`, `ready`, `session`, `priority`, `add`) asking each time.

## The project board

The sessions share one GitHub token, and a board query is the expensive call, so they read and write the board only through `scripts/board-snapshot.sh` (needs `bash`, `jq` and a logged-in `gh`; it holds no token). Never run `gh project item-list` or `gh project item-edit` yourself. The rules are in [`AGENTS.md`](https://github.com/wstein/workharbor/blob/main/AGENTS.md) (GitHub rate limit, Who sets which status); the script's header comment lists every mode.

```text
scripts/board-snapshot.sh                     # the snapshot JSON, cached for 5 minutes
scripts/board-snapshot.sh queue wh/platform   # the lane's Todo cards, P1 first, then by issue number
scripts/board-snapshot.sh card 132            # one card
scripts/board-snapshot.sh --refresh           # force a query
scripts/board-snapshot.sh budget              # refreshes, their cost and the lowest GraphQL budget left, last 24 hours; no gh call (#186)
```

Every write (`move`, `session`, `priority`, `add`, `ready`) asks for permission each time. `move` sets `Todo`, `In progress`, `Blocked` and `In review`; it refuses `Ready to push` and `Done` before any call. `Ready to push` is set only by `wh/review` after its review comment, or by `wh/dispatch` on its behalf for the reviewed sha, with `scripts/board-snapshot.sh ready <number>`; `Done` follows when the issue closes. A card moved by hand in the browser is not seen until the snapshot is 5 minutes old or a read passes `--refresh`.

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
make check         # fmt-check, vet, lint, editorconfig, test, race: run before every commit
make check-ci      # what CI runs beyond make check: docs build, typos, lychee, gitleaks, actionlint
make commitlint    # check this branch's commits against the commit rules
make land          # from your worktree: check, then fast-forward main
make hooks         # enable hooks and the commit template (once per clone and worktree)
make generate      # compile the web UI's templ templates (the generated files are committed)
make docs          # build the Hugo site into _site (make docs-serve for live reload)
make temp-ls       # list temporary containers, volumes, networks and images (LANE=<lane> to narrow)
make temp-clean    # remove one lane's: LANE=<lane> is required
```

The `Makefile` also has `install`, `install-release`, `changelog` and `editorconfig`. `make check-ci` needs `typos` and `lychee` (`brew install typos-cli lychee`).

### Lanes

The models and tasks of the lanes are in the two tables near the top of this page ([sessions](#sessions-to-keep-open), [subagents](#subagents-and-their-models)). Two lanes are not in them: `wh/review` (Opus) reviews independently before every push and is at least as strong as the author, and `wh/spikes` runs spikes on new tools on Antigravity (Gemini); `wh/review` reviews its results. A helper (`.agents/helper.md`, `/wh-delegate <task>`) does one quick task for a lane: find and report, web research and issue drafts go to the read-only `wh-helper`; mechanical edits, small tests and checks go to `wh-helper-edit`. Board hygiene (`/wh-board`) stays with the lane, because it runs `scripts/board-snapshot.sh`. A helper is not a lane, and neither type edits a security-relevant path.

### Commits

```text
feat(domain): add run interrupted state

Why the change was made.

Refs: #12
Assisted-by: Claude Code:claude-sonnet-5-5
```

`Refs` and `Closes` also accept `owner/repo#12` and comma lists. The changelog lists `feat`, `fix`, `perf`, `revert` and breaking changes. The repository allows only rebase merges, so every commit lands on `main` as written. Only Werner force-pushes, by lifting the `main` ruleset for it.

### Spike pages

A spike page under `docs/content/docs/spikes/` links the spike branch once it is pushed and says "a local branch until it is pushed" before that. Spike files that reached `main` anyway are removed from its tree, and the page links them at the commit that added them, which stays in `main`'s history (#194).

### Layout

The design is in `docs/content/docs/design/` (start at `_index.md`; §3 `decisions.md`, §4 `domain.md`, §5 and §8 `architecture.md`, §6 and §7 `security.md`, §9 and §10 `interfaces.md`, §11 to §13 `roadmap.md`), the threat model in `threat-model.md`; also `glossary.md`, `spikes/` and `manual/`. `cmd/` holds `whr` (the single binary), `whr-proxy` (the egress proxy in the sidecar), `whr-shim` (the in-guest launcher, D25) and `commitlint`. Among the packages of `internal/`: `domain`, `policy`, `store`, `hostgit`, `service`, `api`, `cli`, `serve`, `web`, `config`, `toolstore`, `egress`, `devcontainer`; the adapter contracts in `runtime/`, `agent/`, `forge/` and `ci/` have fakes and conformance suites in `runtimetest/` and `agenttest/`.

### Issues through REST

`gh api repos/wstein/workharbor/issues/<n>` reads an issue, `.../issues/<n>/comments -f body=...` comments, `-X PATCH .../issues/<n> -f body=...` ticks criteria, and `-X POST .../issues` opens one, then `scripts/board-snapshot.sh add` puts it on the board. REST does not prompt for labels, so a new issue carries one type label (`bug`, `enhancement`, `documentation`, `ops`, `decision`, `spike`, `security`, ...) and an `area:` label where one fits, set on the create with `-f "labels[]=<name>"` once per label. Before stopping for a rate limit, `gh api rate_limit` costs no points; a secondary limit with points left is waited out once for a minute and retried once (#214). `--url` on a project command trips a secondary limit even with points left, which is why the board goes through the script.

### Landing and worktrees

A session lands with `make land` from its own worktree (AGENTS.md, step 3). What to do with each of its messages, the retries, a stale index or lock in the shared checkout (the human's to repair) and the branch cleanup afterwards are in [`/wh-land`](https://github.com/wstein/workharbor/blob/main/.claude/commands/wh-land.md); the `Makefile`'s `land` target is what it runs.

Each lane's worktree is created once with `git worktree add <worktree> --detach main` and `make hooks` (the Setup steps above). In `wh/platform`'s second worktree, `workharbor.lane=wh/platform-2` keeps its temporary resources out of `make temp-clean LANE=wh/platform`.

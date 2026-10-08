---
title: Pull request landing
description: "Design note: replace make land with the GitHub pull request flow, a gate workflow and commit statuses."
weight: 10
toc: true
---

## Landing through pull requests (#407)

{{< status open >}} Design only; no workflow, ruleset change or script exists yet. Human decision (8 October 2026): landing is fragile and hurts DX, so it moves to the standard GitHub pull request flow. Until the first PR-flow merge succeeded, `make land` stays the only way to land.

Each item of the issue checklist is marked here as verified (how) or unverified. Verified on 8 October 2026 with read-only `gh api` GET calls, the repository files, or GitHub documentation as recalled (named as such).

### Flow

1. **One PR per branch, rebase and merge.** The repository allows rebase merge only (squash and merge commits are off), so the atomic conventional commits survive. The human pressing the merge button replaces the typed short SHA and the terminal of `land.sh`. {{< status verified >}} Merge settings read with `gh api repos/wstein/workharbor`; auto-merge allowed, `delete_branch_on_merge` on.
2. **Ruleset on `main`.** Add a pull request rule, required status checks and no bypass. {{< status verified >}} for the current state, see the table below.
3. **No required approvals.** A solo maintainer cannot approve his own PR. The desk sets commit statuses `review/sonnet` and `review/opus` on the PR head SHA (`POST /repos/{r}/statuses/{sha}`); the evidence goes into a PR comment. This replaces `refs/notes/review` and `TO_LAND.md`.
4. **A `gate` workflow.** On `pull_request`, read-only, pinned actions, job-level `permissions: {contents: read, statuses: read}` (`statuses` is a documented permission scope, {{< status unverified >}} until a workflow runs). It lists the changed files, derives the class, reads `GET /repos/{r}/commits/{head}/statuses`, and fails unless the required tier is present and its `creator.login` is the human or the desk identity.
5. **Stacking.** Small independent PRs merged in order, each rebased on the new `main`. A chain of PR bases is optional; the fast lane (#405) maps onto one PR per lane.
6. **Roles.** The dispatcher opens a draft PR per branch until CLEAR; the desk sets the statuses and posts the evidence comment; CI-watch reads the checks of the PR; the human marks it ready and merges (or enables auto-merge once the checks are green).

### Class logic and its single copy

`scripts/land.sh` derives the class from `git diff --name-only <main> <tip>`, never from the note. Each path is lowercased and matched in order:

- **carve-out** (needs an Opus CLEAR): `agents.md`, `claude.md`, `.claude/*`, `.agents/*` (any depth), `.github/*`, `docs/content/*design*`, `docs/content/*threat*`, and every path not allowed below.
- **ordinary** (any CLEAR): `internal/exitcode/*`, `internal/version/*`, `internal/docscheck/*`, `docs/*.md`, `readme.md`, `changelog.md`, `contributing.md`, `license`.
- One carve-out path makes the whole branch a carve-out. A carve-out tip with a Sonnet CLEAR passes only when every commit of `main..tip` is covered by its own Opus CLEAR or an equal patch-id (#365).

Proposal: move the path matcher into one file, `scripts/path-class.sh` (POSIX sh, reads paths on stdin, prints `ordinary` or `carve-out`). The gate runs it from the base branch checkout (never from the PR head, so a PR cannot change the rule applied to itself), and `land.sh` calls it until retirement. Patch-id inheritance is not part of the gate (see Stacking cost).

### Verified and unverified

| # | Item | State | Basis |
| --- | --- | --- | --- |
| 1 | Rebase merge only, auto-merge, branch deletion | verified | `gh api repos/wstein/workharbor` |
| 2 | Ruleset `main` (id 24335774), active, no bypass actors | verified | `gh api repos/wstein/workharbor/rulesets/24335774`: rules are `deletion`, `required_linear_history`, `non_fast_forward`; `bypass_actors` is empty; `current_user_can_bypass` is `never` |
| 2 | Missing in the ruleset | verified | no `pull_request` rule, no `required_status_checks` rule. Direct pushes to `main` are therefore still possible (fast-forwards), which `make land` relies on. Adding the PR rule ends that; "admins cannot bypass" already holds (empty bypass list) |
| 3 | Statuses by the desk's token | unverified | the Statuses API exists and the token has `push: true` (`gh api repos/wstein/workharbor --jq .permissions`); nothing was posted, because the task forbids writes |
| 4 | Workflows and check names | verified | `.github/workflows` and `gh api repos/wstein/workharbor/commits/main/check-runs`: ci has `check (1.26.x)`, `check (1.27.x)`, `agent-cli`, `commits` (pull_request only); docs-quality has `links`, `spelling`, `site`; security has `actionlint`, `zizmor`, `govulncheck`, `dependency-review` (pull_request only); scan has `secrets`, `docs-vulnerabilities`; `analyze` is CodeQL. The required names are these job names (matrix names include the Go version); `gate` does not exist yet. Pick the set before the ruleset change: a check that runs only on pull_request must not be required for pushes |
| 4 | Required check from a `pull_request` workflow | unverified | ordinary GitHub behavior (checks of any workflow can be required once they ran in the repository), not tested here. A required check that does not run (path filter, skipped job) blocks merging: `gate` must run on every PR |
| 4 | Status `creator` is recorded | verified | the Statuses API returns a `creator` object per status (seen in the API schema; the history of `main` has no statuses to show an example) |
| 4 | Workflow can read statuses | unverified | `statuses: read` is a documented `permissions` scope; not run here |
| 5 | Merge queue on a personal repository | unverified | recalled from the documentation: merge queue is for organization-owned repositories; the owner here is type User. Plan without it |
| 7 | Migration | decided | below |
| 9 | crewbook files | unverified | not readable from this worktree (`/Users/werner/workspaces` holds only workharbor) |

### Security consequences

- **Who can set a status.** Anyone with write access, with any context name and any token of theirs, including every agent whose token can push. A status is not a signature. The gate may only accept a status whose `creator.login` is the human or one named desk identity; that check is as strong as the secrecy of that token, and the desk token is a write token on this repository.
- **What the gate can trust.** The changed paths (computed by the gate from the PR, not from a note), the class rule (run from the base branch) and the creator of a status. It cannot trust the PR text, a comment, a status description, or the head of the PR for the gate's own code. It cannot tell whether a reviewer really reviewed.
- **Forks.** The workflow uses `pull_request` (never `pull_request_target`, a hard rule). A fork PR gets a read-only token and no secrets, so its gate cannot read statuses reliably ({{< status unverified >}}); fork PRs are outside the flow, only branches of this repository are landed.
- **The human merging his own PR.** Nothing separates author from merger; the merge click is the consent, and the checks and statuses are the only guard. A compromised human token can set any status and merge: the same trust as `land.sh`, whose prompt was a UX safeguard against a same-user agent, not a boundary.
- **Rebase merge changes the SHAs.** Statuses sit on the PR head SHA and do not carry to the new commits on `main`; they gate the merge only. After the merge, `main` carries no review record except the PR comment, so the evidence comment is the audit trail, and it must name the head SHA and the tier.
- **Weaker than today in one respect.** `land.sh` runs `check-local`, `commitlint` and the secrets range on the exact tip locally; PR checks run on a merge ref or head in CI instead. The required list in the ruleset must include `commits` and `secrets`.

### Stacking cost

CLEAR is bound to a head SHA. A rebase changes every SHA after the first changed base, so each later PR needs new statuses. The patch-id inheritance of `land.sh` (#365) is lost: for an unchanged patch the desk re-posts the CLEAR on the new head together with a `git range-diff` showing the patches equal. Chained PR bases keep the diff small, but retargeting after the base merges still rebases. Expect one status round per PR per rebase; the fast lane (#405) needs one PR per lane.

### What must change at retirement

In this repository: `scripts/land.sh` and its tests (`scripts/land_*_test.go`, `scripts/linear_land_test.go`, `scripts/land_confirmation_test.go`); the Makefile targets `land`, `land-list`, `land-next`, `land-all`, `land-preview` (and `.PHONY`); the `land` pointer branch and its deprecated name `landing` (`.agents/design.md`, `.agents/helper.md`, `.agents/review.md`); `refs/notes/review` and `refs/notes/confirm` and `docs/confirmation-record.md` with `internal/confirm` (decide: keep as history or retire); `TO_LAND.md` (kept by the dispatcher outside git here; not present in the tree); `scripts/index-state.sh` and `.claude/settings.json` allowances that mention landing; `AGENTS.md` (commands line, "Merge into `main` only through `make land`", board statuses); `.agents/code.md`, `.agents/docs.md`, `.agents/dispatch.md`, `.agents/review.md`; `docs/content/docs/manual/sessions-and-agents.md` (landing, landing order, `land-preview`); and `CONTRIBUTING.md`.

In crewbook (merged mode): `docs/git-history.md#landing-pointer` and `#rebase-re-review`, and the `tools/review_lines.py` gate, plus the same landing text in its agent docs. {{< status unverified >}} I could not read those files; the desk files that issue (checklist 9).

### Migration plan

1. **Ruleset check.** Decide the required check set from the table; add the PR and required-checks rules in evaluate mode if available, else only after step 3. Exit: the ruleset lists the checks and a test PR shows them.
2. **Shared path script and `gate`.** Add `scripts/path-class.sh`, make `land.sh` call it (tests unchanged), add the `gate` workflow. Exit: `make check-local` green, and on a test PR the gate fails without a status, fails with a status from another account, passes with the right one for each class.
3. **Statuses and comments.** The desk posts `review/*` statuses and the evidence comment next to the notes (both, in parallel). Exit: three PRs show the same verdict as the note.
4. **First PR-flow merge** of a real branch, with `make land` untouched. Exit: it merged by rebase, branch deleted, `main` linear.
5. **Enforce.** Turn on the PR rule and required checks; `make land` stops working for direct pushes, so keep it only until step 4 succeeded. Exit: a direct push to `main` is refused.
6. **Retire** `land.sh`, the targets, the `land` pointer, `TO_LAND.md` and the notes, and update the files above. Exit: `docscheck` and `make check-local` green, no remaining reference found by `grep -rn "make land\|land.sh\|refs/notes/review"`.

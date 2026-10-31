# Codex hand-off: Feature 009 host-setup-wizard

Status: H37 authorizes staged source work with per-work-item manual scopes/evidence and exact-head independent review; it permits reviewed CLEAR local source parents before owner merge. No guard journal or task completion is inferred. This file states order, guardrails and
reporting; it restates no requirement. Where it and `tasks.md` differ, `tasks.md` wins and the difference is a question to the owner. DP-1 to DP-5 and confirmed no-op protocol/lock bookkeeping are confirmed by H38 (2026-10-20). OD-1 to OD-5 remain confirmed (OD-2 amended: small curated host catalogue, minimum versions only, no toolbox parity). AP-6 reason/result placement remains unselected; hold only affected JSON integration/registration, not independent source work.

## Common brief

**Read first, do not skip:** the workspace `AGENTS.md` (owner workflow, delegation, git identity), `AGENTS.md` and `WORKFLOW.md` of this
repository, `spec.md`, `plan.md`, `tasks.md`, the files in `contracts/`, Feature 006 FR-009, FR-026, FR-043 to FR-046, FR-060, FR-063 and tasks
T015, T025, T027, T031, T032, Feature 008 FR-019 to FR-026 and T005, ADR 0013, ADR 0014, `docs/container-runtime.md`. Use CrewBook split mode
where the workspace instructions require it; each author works in its own git worktree on its own topic branch.

**Rules:**

1. Test-driven: write the named failing tests first, show them failing, then the smallest implementation, then refactor. Atomic Conventional
   Commits, one logical change each; trailer `Co-Authored-By: <your agent identity>`.
2. Never write git configuration (`git config user.*` leaks into every worktree); the owner's global identity applies. Before every push,
   `git log --format='%an <%ae> | %cn <%ce>' origin/main..HEAD` shows only the owner.
3. Run checks with `umask 022`; never run tests inside `git rebase --exec`. Required: `python3 scripts/check_repo.py`, `git diff --check`, `go vet`
   and `go test` for the touched packages in `apps/whr`; record exact commands and results in the PR.
4. PRs target `main`, are at most about 1,500 changed lines, name task IDs, prerequisites, tests, commands and review state. Draft only with a
   named blocker; Ready for review is set once the exact head has an independent CLEAR review and required checks are green, and is the owner's signal to accept and merge. Never merge,
   tag, release, push `main` or change settings. Stack dependent PRs natively (`GH_REPO=wstein/workharbor gh stack link <PRs>`).
5. A rewritten head needs a new review of the new SHA before `--force-with-lease=<ref>:<observed SHA>`.
6. No real `brew`, `sudo`, cask, OrbStack, Docker, host mutation, image or documentation build, release or publication without the owner's
   explicit approval of that operation. Every test uses the fake package manager, fake sudo, a fake network port, a temporary HOME, fake clocks
   and sockets.
7. Requirements change only through a spec amendment PR with its own review, never silently in code.
8. A checkbox is not proof: tick a task only with cited code paths and test names.

## Order

Stage 1: T001 and T009 first; T002 and T004 after T001; T003 after T001 and T002; T006 after T002 to T004; T005, T007, T008 after T006 (T005 also
needs 008/T005, T007 needs 006/T027). Stage 2: T010 after T005, T006 and 006/T031, T032; T012, T013, T014 (docs-only, owner-gated) after T009;
T011 only after T012, T013 and T014 are approved/merged and AP-6 reason/result placement is chosen; T015 last. At most the number of concurrent authors the owner states for the session.

## Guardrails

- Stage 1 and T010 register no command and edit none of `contracts/cli/`, ADR 0013/0014, `docs/container-runtime.md`, Feature 006 or Feature 008
  text. Each PR records an empty `git diff --stat $(git merge-base origin/main HEAD) -- contracts/cli docs/adr docs/container-runtime.md specs/006-whr-cli specs/008-workspace-sandbox`
  except T011 to T014.
- `whr` never changes the Docker VM, never starts or configures the runtime application and never mounts the Docker socket; the step-kind set is
  closed (FR-002).
- `whr` refuses to start as root or with `SUDO_USER`; `sudo` only as `/usr/bin/sudo -- <catalogue argv>` then `sudo -k`, for catalogue-marked steps,
  exact command shown and confirmed, with a terminal; never `brew` under sudo; never a password; never in `--dry-run`, plain `doctor` or
  unattended runs (FR-011).
- Never overwrite silently: diff first, backup first, atomic write, no symlink following, no merge into a user's file (FR-013).
- Never print git configuration values and never set a git key (FR-014). Git preview/confirmation shows only structural key names, redacted paths and actions, never a content/value diff.
- `--dry-run` and plain `doctor` write nothing; only real runs of `setup` and `doctor --fix` write the append-only protocol (FR-018, FR-019).
- Detection never runs `brew` or the Command Line Tools `git` shim (FR-006). The catalogue never lists an agent tool (FR-010).
- Opted-out items are never touched by `setup` or `doctor --fix` (FR-005). Linux `setup` stays the Feature 006 enrollment (FR-001).
- The host catalogue is small and curated (the 13 initial entries of FR-010; nothing else from the toolbox, no change of git configuration or the login shell outside FR-014); never add toolbox tools, version equality or sync with BundleDock; a new entry is OG-7.

## Stop and ask

Stop for missing relevant prerequisite evidence or a real scope conflict. H37 permits source-only use of exact reviewed CLEAR local 006 prerequisite parents with source/check evidence before merge; it grants no008 implementation override or real acceptance. Stop and ask the owner when: a task text contradicts the code, the spec or another task;
an unselected decision blocks its affected scope (currently AP-6 reason/result integration); a real `brew`, `sudo`, cask or host action seems necessary; a requirement, frozen contract or ADR must change;
the real git behavior differs from a spec statement; scope grows beyond the task or a PR beyond about 1,500 lines. Do not work around a blocker.

## Report format

At each PR and weekly: done, in review (PR, head SHA, review state), blocked (named blocker), next; at most 200 words, with file and line
references for any finding. Ask for review by naming the PR and the exact head SHA; send only the delta after a fix.


Confirmed no-op real runs still write protocols and use lock bookkeeping; only
managed installations/config/profile/backups must be unchanged. All tests stay
offline; actual host actions and measurements remain OG-1/OG-2/OG-9. The strict
single_task grammar incompatibility is retained under H37's manual exception,
never hidden by edited dependencies, fake contexts or checkbox completion.

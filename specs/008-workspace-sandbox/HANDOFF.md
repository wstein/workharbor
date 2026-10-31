# Codex hand-off: Feature 008 workspace-sandbox

Status: for use only after the spec, plan and tasks are approved at one exact head (WORKFLOW.md). This file states order,
guardrails and reporting; it restates no requirement. Where it and `tasks.md` differ, `tasks.md` wins and the difference is a
question to the owner.

## Common brief

**Read first, do not skip:** the workspace `AGENTS.md` (owner workflow, delegation, git identity), `AGENTS.md` and `WORKFLOW.md`
of this repository, `spec.md`, `plan.md`, `tasks.md`, the files in `contracts/`, Feature 006 FR-030 and tasks T025, T027, T038,
T039, ADR 0009, ADR 0013, ADR 0014. Use CrewBook split mode where the workspace instructions require it; each author works in its
own git worktree on its own topic branch.

**Rules:**

1. Test-driven: write the named failing tests first, show them failing, then the smallest implementation, then refactor. Atomic
   Conventional Commits, one logical change each; trailer `Co-Authored-By: <your agent identity>`.
2. Never write git configuration (`git config user.*` leaks into every worktree); the owner's global identity applies. Before
   every push, `git log --format='%an <%ae> | %cn <%ce>' origin/main..HEAD` shows only the owner.
3. Run checks with `umask 022`; never run tests inside `git rebase --exec`. Required: `python3 scripts/check_repo.py`, `git diff
   --check`, `go vet` and `go test` for the touched packages in `apps/whr`; record exact commands and results in the PR.
4. PRs target `main` (owner decision 2026-10-20), are at most about 1,500 changed lines, name task IDs, prerequisites, tests,
   commands and review state. Draft only with a named blocker; Ready for review is set once the exact head has an
   independent CLEAR review and required checks are green, and is the owner's signal to accept and merge. Never merge, tag, release, push `main` or change settings. Stack
   dependent PRs natively (`GH_REPO=wstein/workharbor gh stack link <PRs>`).
5. A rewritten head needs a new review of the new SHA before `--force-with-lease=<ref>:<observed SHA>`.
6. No real Docker, host mutation, image or documentation build, release, publication or volume deletion without the owner's
   explicit approval of that operation. Container work is verified with fake-Docker tests only.
7. Requirements change only through a spec amendment PR with its own review, never silently in code.
8. A checkbox is not proof: tick a task only with cited code paths and test names.

## Order

Stage 1: T001, then T002, T003, T005 (parallel); T004 after T003; T006 after T002 and T005; T007 after T002; T008 after T002,
T003, T006, T007; T009 after T008. T010 (docs only) at any time. T017 (an ADR 0014 amendment PR, docs only) can run after T010 and needs owner approval. Stage 2 starts only when T008 is merged and Feature 006 T038 is
on `main`: T011, T012, T013, T014, then T015; T016 only after the owner approves the ADR 0014 amendment (T017) and the schema change, in three PRs. At most the number of concurrent authors the owner states for the session.

## Guardrails

- Stage 1 and T011 to T015 register no command; only T016, after the owner approves the ADR 0014 amendment PR (T017), registers the `sandbox` group (never `exec`). Edit no file under `contracts/cli/`, `docs/adr/0013*`, `docs/adr/0014*`, the doctor catalogue or Feature
  006 text. Each PR records an empty `git diff --stat $(git merge-base origin/main HEAD) -- contracts/cli docs/adr`. Proposals go through T010 only.
- Export never runs git against an instance-writable repository with its config trusted (FR-007); use only the hardened invocation.
- Agent-ness is never detected: refusals depend on the requested profile and declared capabilities (FR-024, FR-029).
- Feature 006 FR-030 (and 008 FR-029, FR-030): no `run` of an agent, no agent-specific flag, no agent detection, no provider credential in any plan, environment or
  log. Identity variables carry operator values only and are never logged.
- No provider-kind branch in production code (FR-034). A microVM provider is out of this increment.
- Never print git configuration values; never write the host configuration except through the preflight probe of T006 against a
  fixture. Never create or move a host git configuration (OG-2).
- Terminology: Mac host, Docker VM, sandbox backend. `whr` never changes the Docker VM.
- Tests use `t.TempDir()` fixtures only; no network, no Docker, no real credentials. Treat repository content as untrusted input.

## Stop and ask

Stop and ask the owner when: a task text contradicts the code, the spec or another task; 006/T038 or T025 is not on `main`; a real
Docker, host or network action seems necessary; a requirement, frozen contract or ADR must change; an unapproved amendment blocks the work; a
test needs real credentials; the real git behavior differs from a spec statement (for example `user.useConfigOnly` with the
identity variables); scope grows beyond the task or a PR beyond about 1,500 lines. Do not work around a blocker.

## Report format

At each PR and weekly: done, in review (PR, head SHA, review state), blocked (named blocker), next; at most 200 words, with file
and line references for any finding. Ask for review by naming the PR and the exact head SHA; send only the delta after a fix.

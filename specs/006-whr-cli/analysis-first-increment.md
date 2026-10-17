# Independent review record: H77 first-increment amendment

Rounds 1 to 3 below reviewed the pre-rebase branch (backup/h77-pre-rebase); their FR, SC and work-item numbers are the old ones (FR-039..049, SC-013..017, T025..T029; US7..US9). After the owner's upstream revisions (US0, US1a, FR-039..FR-050, SC-013..SC-015, 2026-10-16) and the scope decisions of 2026-10-17 the amendment was reconciled on origin/main with new numbers: FR-051..FR-063, SC-016..SC-021, US7..US8, T025..T034. Round 4 and later review the reconciled text.

Records review rounds of the amendment text (spec FR-039–FR-049, US7–US9, plan, tasks T025–T029, ADR 0014 and the
CLI/workspace contracts). A READY verdict is not owner approval; owner approval of an exact head needs a tracker or PR
link (WORKFLOW.md). Reviewer: read-only Plan agent (opus), prompt naming the worktree and head.

## Round 1 - reviewed revision 5c32c3c338ef8518a372bcfca4b83089a2f333bc - NOT READY (5 high, 13 medium, 5 low)

| # | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | high | Approval wording claimed a conditional, automatic approval | Reworded in spec, ADR, plan, checklist, tasks: needs CLEAR review then explicit owner approval with link; chat go-ahead is decision H77 only |
| 2 | high | Order/branch contradicted sequential chain; open question stated as fact | Marked as proposed H77 (5) owner decision; T001–T024 chain stays authoritative |
| 3 | high | `scripts/single_task.py` accepts only `T###` and refuses spec/plan/tasks paths | WIs renumbered T025–T029; allowed paths exclude guarded files and root README; status/reconciliation moved to workpad or owner-reviewed docs commit |
| 4 | high | Doctor "no process but git --version" vs `config.valid` | Guarantee, US9-7, contract rule and tests extended to validate's fixed Git calls |
| 5 | high | `init --project` rollback could delete pre-existing files | Rollback limited to this run's paths verified by device/inode; `.gitmodules` and `projects/<name>` in preflight; SC-014 and test added |
| 6 | med | Git hardening gaps (GIT_CONFIG_COUNT, env, safe.directory, shim) | `-c` flags, fresh environment, min version, `git.unsafe_path`, macOS shim detection in FR-043/plan/doctor |
| 7 | med | validate vs resolver disagreements | Not-pinned entry now error; unsupported key warning; https userinfo, scp-like, case-folded `.git`, integer 1 specified |
| 8 | med | wirejson cannot give distinct codes | whr classifies and scans before wirejson; api_client untouched |
| 9 | med | Catalogue holes; cap could hide errors | `gitmodules.*` and `limit_exceeded` (error) codes added |
| 10 | med | Output schema gaps | created/remaining, result required on 68/73, applied const for dry run, `unknown` command |
| 11 | med | 74 for pre-existing-content failure | Generated-file errors 74; pre-existing content 68; validate before and after write |
| 12 | med | TOCTOU/symlink underspecified | Per-component no-follow openat, dev/inode checks, ancestors followed, identity-checked rollback |
| 13 | med | Short-form rule inconsistent | Allowlist plus constraint wording; completion shells aligned |
| 14 | med | Cobra help/completion/flag errors | Custom help template, ValidArgsFunction, SetFlagErrorFunc, native `ws` alias |
| 15 | med | macOS git shim dialog | Detected without executing; fail/unavailable |
| 16 | med | Missing tests/mechanisms/bounds | Tests named; injected dialer and `unshare -n`; 5 s / 256 MiB; atime excluded; devices real-host only |
| 17 | med | `--project` details | userinfo refused, path derivation, ssh BatchMode, SIGKILL semantics, existing repo is 68 |
| 18 | med | Doctor vs real hosts | DOCKER_HOST sole candidate, container storage unknown cases, platform.host semantics, user name redaction |
| 19–23 | low | Edge case wording, partial ADR tests, template pin, EACCES codes, nits | Fixed, except: SC-016/SC-011 ordering and `exit-codes.json` status "proposed" left as pre-existing cosmetics |

## Round 2 - reviewed revision c336c22125e5285d93ea38f3b7d87ef8321a6c20 - NOT READY (1 high, 8 medium, 10 low)

| # | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | high | Output schema pattern rejected `control_plane.reachable` | Patterns now `^[a-z][a-z_]*(\.[a-z][a-z_]*)+$`; id/code conformance tests named |
| 2 | med | Placement implicit; effect on approval unstated | tasks.md header states the edits are conditional and need renewed approval of tasks.md from T009 on; open question 1 |
| 3 | med | Three exit codes for init self-validation | FR-045, US8-5, ADR 0014 aligned: generated files 74, pre-existing content 68 |
| 4 | med | Temp HOME broke "creates nothing" | HOME is a fixed path never created |
| 5 | med | init --project environment contradictory | Separate exact environment, insteadOf detection, test |
| 6 | med | Finding catalogue unqualified | `contracts/cli/validate-findings.json` plus freeze tests |
| 7 | med | Resolver disagreements | All resolver rejections are errors with codes; sources stated; committed-blob parity vectors |
| 8 | med | Git failures hid errors | `git.listing_failed` is an error; 74 limited to init |
| 9 | med | single_task.py fit | Exact file lists per issue, fixtures generated in `t.TempDir`, go.work.sum and notices named |
| 10-19 | low | Regex test home, existing dirs, reason enum, SIGTERM, vector count, test transport, open questions, H13:a wording, test placement, Git minimum | Fixed: vectors replace regex runner; skip_existing; enum values; SIGTERM 72; five vectors; httptest HTTPS server; plan open questions; H13:a bound to pre-amendment text; TestUnknownCommandHint in T025; Git 2.39.5 |

## Round 3 - reviewed revision 150034f26f2182990ebe0b0f0c98280cf4bb5d98 - NOT READY (1 high, 1 medium, 7 low)

Three rounds are the limit set for this task; the fixes below are unreviewed. A fresh independent review of the final head is still required.

| # | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | high | `init --project` could not pass self-validation (unborn HEAD, staged submodule) | Unborn-HEAD rule in FR-042, validate-findings sources, plan (six vectors), tests T026/T028 |
| 2 | med | Manifest schema description still named a regex runner | Reworded: vectors prove equivalence |
| 3 | low | init exit 68 result placement | Schema requires `result` for init exit 68 |
| 4 | low | FR-049 duplicates and 72 vs 73 precedence | 73 takes precedence (FR-049, ADR) |
| 5-6 | low | Stale contract lists and research bullet | Updated |
| 7 | low | GIT_NO_LAZY_FETCH needs Git 2.44 | Stated; partial-clone corpus case |
| 8 | low | doctor `needs` | Changed to none |
| 9 | low | `auth status` rationale wording | Left: rationale kept as decided in H77 (1); owner may reword |

## Reconciliation onto origin/main (2026-10-17)

The branch was rebuilt linearly on origin/main 2b3f2ba1 (a rebase conflicted in the spec header, FR-001 and numbering).
Merging semantics: FR/SC/US renumbered after upstream's highest; my `init` design was replaced by a reference to upstream
FR-043..FR-050 and US0 (kept: self-validation with `validate`'s code, no-follow creation, identity-checked rollback,
exit-code mapping, unborn-HEAD rule for `validate`); my `--project` Git clone was dropped because FR-049 forbids
fetching and `init` runs no Git command (so the init Git-version preflight is moot); `--image` was dropped with the
starters (unsupported); settings moved from a separate preferences file to a proposed manifest `schema_version` 2 by the
owner's decision; `workspace.create` is registered as `later` with only a pure resource library and shared vectors in
the first increment.

## Round 4 - reviewed revision 869f18f872071c732e2d8f7e0643bd0af84a95f7 (reconciled set) - NOT READY (2 high, 11 medium, 11 low)

| # | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| 1 | high | Per-protocol `protocol.<name>.allow` in a hostile `.git/config` overrides `protocol.allow` (partial clone fetch) | `protocol.<name>.allow=never`, `GIT_ALLOW_PROTOCOL`, `GIT_NO_LAZY_FETCH`, commondir/alternates unsafe, corpus and tests (FR-055, plan, T026) |
| 2 | high | "init runs no Git" vs validating the written result | Git-free validation profile for init, `git_checks: not_run`, `project.declared_not_pinned` always when declared (FR-057, schema, T030) |
| 3 | med | Unborn HEAD not implementable within the vectors | Seven fixed vectors incl. `symbolic-ref -q HEAD`, `--quiet`, presence conditions |
| 4 | med | Old numbering in plan | Renumbered |
| 5 | med | False parallelism claims | vectors.json owned by T028; T027 uses a port; wizard step moved from T033 to T032 |
| 6 | med | Existing version 1 manifests and creation-readiness | v1 preserved unless `--overwrite`, warning not creation-ready (FR-057 (g)) |
| 7 | med | Version 2 contradictions in validate | "integer 1 or 2" |
| 8 | med | `git.unavailable` was a warning | Error when `.git` is a real directory |
| 9 | med | Exit code for existing target | 70 regular file without `--overwrite`; 68 non-regular, symlink, replaced |
| 10 | med | init validation null on dry run; error.code mapping | In-memory result; `error.code` bound to `exit_code` by `allOf` |
| 11 | med | project.url grammar and path derivation | Stricter URL pattern, derivation from `projects_path`, vectors |
| 12 | med | No classification precedence in resource vectors | `classification_order` and added cases |
| 13 | med | Approval overclaim in checklist | Thresholds only are owner-confirmed |
| 14-24 | low | Labels, vector fidelity, leftovers, partial acceptance claims, 5 s meaning, overwrite races, scope overstatements, bundles, stale text, checklist wording, `.git` internals | Fixed in the same commit series |

## Round 5 - reviewed revision 05864dabd8ec80de2e8ecfa5354e592326cd7399 - NOT READY (1 high, 6 medium, 14 low)

| # | Sev | Finding | Resolution |
| --- | --- | --- | --- |
| H1 | high | Explicit flags vs preserved version 1 manifest undefined | Defaults-only run preserves (exit 0, `settings_not_persisted`); explicit flags exit 70 naming `--overwrite workharbor.json` (FR-057 (g), T030) |
| M1 | med | classification_order inconsistent with vectors | Relaxed lexical form, leading zeros accepted, all vectors recomputed with a scratch classifier |
| M2 | med | 5 s per call vs per run; hostile `.git` files | One shared 5 s budget per run, lstat pre-checks of `.git/HEAD`, `index`, `config` |
| M3 | med | No resolver work item | T035 (after T009 and T028) |
| M4 | med | US0-4 vs wave 1a | Owner-decided narrowing recorded; exit 70 names `.devcontainer/devcontainer.json` |
| M5 | med | Rebasing vs single_task.py; vectors ownership | Rules in the plan; T028 owns vectors.json and adds only cases the T026 checker passes |
| M6 | med | Approval preconditions | Stated in the status line, H77 (5), tasks header: renewed approval at one exact head before T009 or T025-T035 start |
| L1-L14 | low | Wording, notes, type-exact vectors (`true`, `1e0`), README scope, Assumptions, leading zeros accepted, equivalence and partial-profile vectors, exit 70 clauses, structured init result fields, T030/T027 dependency, data-root evidence, `.gitkeep` path | Applied in the round 5 commits |

Owner answers of 2026-10-17 ("1j 2j 3j 4j 5approved") are recorded as H77 (7) and under H77 (5); they approve no head's text.

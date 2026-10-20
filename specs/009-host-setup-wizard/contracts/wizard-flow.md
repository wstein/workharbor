# Wizard prompts, flags, exit codes and result (PROPOSED)

Status: proposed design baseline for Feature 009 (FR-004 to FR-021); not implemented; nothing is registered. Applies to `whr setup` on macOS and
`whr doctor --fix`. Prompts and previews go to standard error. Any answer outside the closed sets re-asks (interactive) or is a usage error (flags).

## Flow of `whr setup` (macOS)

1. **Detect** (read-only, FR-006). No question before it ends.
2. **Show** a table: item, state, planned action, source (type, tap, name), minimum version, declared network destinations and
   purpose, any privileged command with its exact `sudo` line, blocked steps with reason; then the git location report (key names only).
3. **Ask**: `Set up the recommended developer experience on this host? [yes / customize / no]`

   | Answer | Effect |
   | --- | --- |
   | `yes` | apply the displayed create-class plan; modify-class steps are asked one by one (default `no`) |
   | `customize` | step 4 |
   | `no` | exit 0, nothing written |
   | end of input, interrupt | exit 72 |

4. **Customize**, per item in catalogue order, with `back`, `review` and `cancel` always available:

   | State | Question | Answers (default first) |
   | --- | --- | --- |
   | `absent` | `Install <title> (<type> <name>)?` | `install`, `skip` |
   | `present_outdated` | `<title> <found> is below <min>. Upgrade?` | `skip`, `upgrade` |
   | `present_other_source`, `present_unverifiable` | information only | none |
   | dotfile absent | `Create <target>? (preview shown)` | `create`, `skip` |
   | dotfile differs | `<target> differs. Replace it? (diff shown, backup first)` | `skip`, `replace` |
   | git `legacy_home_file`, `split` | `Move the global git configuration to ~/.config/git/config? A backup is made first.` | `skip`, `show diff`, `yes` |

   `skip` records an opt-out of that item (`git-config` for git).
5. **Confirm** each modify-class step (also under `yes`): diff or command shown, `[yes / no]`, default `no`. A privileged step shows its exact
   command and states that `sudo` asks for the password on this terminal.
6. **Apply** (real run: run lock and protocol start here): the plan hash is rechecked; one status line per step; a failure blocks only dependents.
7. **Report**: per step status and reason, backup paths with the note that restore is a manual copy, and next steps, for example
   `Open OrbStack once to finish its first start; whr does not start or configure it. Then run whr doctor.`

## Flags

| Flag | Meaning |
| --- | --- |
| `--dry-run` | detect, read-only lookups, show the plan and previews (may show the `sudo` command); writes nothing, runs no sudo |
| `--non-interactive` | no prompts (also implied by no terminal on stdin or stderr, and by `--json`) |
| `--yes` | confirm the create-class plan; never approves modify-class or privileged steps |
| `--only <id>` / `--skip <id>` | repeatable; restrict the plan / opt an item out and record it |
| `--confirm <id>` | repeatable; confirm the modify-class or privileged step of this item (`git-config` for the git move) |
| `--json` | one envelope on stdout; privileged steps are never run |

Linux enrollment flags on macOS and unknown ids are usage errors (64).

## `whr doctor`

| Invocation | Behavior |
| --- | --- |
| `whr doctor` | read-only for the host; network allowed; adds `host.profile`, `host.tool.<id>`, `host.dotfile.<id>`, `git.global_config` (AP-6) |
| `whr doctor --fix --dry-run` | repair plan and preview, exit 0, writes nothing, no sudo |
| `whr doctor --fix` | no `--yes`: preview, exit 70 |
| `whr doctor --fix --yes` | real run: create-class repairs; modify-class and privileged steps only with `--confirm <id>` and, for sudo, a terminal |
| `--dry-run`, `--yes` or `--confirm` without `--fix` | 64 |

`--fix` never prompts, never touches an opted-out item, needs a host profile (`profile_absent`) and never touches the Docker VM.

## Exit codes

ADR 0013 table only (FR-020): 0 success, nothing to do, or `no`; 64 usage; 68 unsupported platform, root start, run lock, refused target, git
refusal, blocked or failed with nothing applied, failed doctor check; 70 unattended without `--yes` or `--confirm`, or privileged step without
terminal (73 when other steps were applied); 71 deadline with nothing applied; 72 interrupt or cancellation; 73 partial failure (precedence over
72, 71, 68); 74 invalid catalogue or unclassified failure. Plain `doctor` keeps Feature 006 FR-060 (failed check 68, `incomplete` 0).

## Brew invocation

Only these forms, separate argv elements, no shell, stdin closed, `brew` at `/opt/homebrew/bin/brew` or `/usr/local/bin/brew`:
`brew install <name>`, `brew install --cask <name>`, `brew upgrade <name>`, `brew upgrade --cask <name>`; `<name>` matches the schema pattern.
Not used: `sudo` with brew, `--HEAD`, `--force`, `--ignore-dependencies`, `--no-quarantine`, `tap`, `update`, `shellenv`, URL or path arguments.
Environment: `HOME`, `PATH` (prefix `bin` and system directories, absolute), `LANG`, `LC_*`, `TERM`, `TMPDIR`, `HTTP(S)_PROXY`, `NO_PROXY`, plus
`HOMEBREW_NO_AUTO_UPDATE=1`, `HOMEBREW_NO_ANALYTICS=1`, `HOMEBREW_NO_ENV_HINTS=1`. Step deadline 900 s (DP-2): SIGINT, 30 s grace, then kill.

## Privileged step

Only in real runs: `/usr/bin/sudo -- <catalogue argv>`, then `/usr/bin/sudo -k`; no shell, no `-E`, `-S`, `-A`; terminal inherited so `sudo`
prompts itself; `whr` never handles a password.

## Result

Envelope per Feature 006 FR-015 with `command` `host.setup` or `host.doctor`; `result`: `platform`, `mode` (`setup`, `fix`), `dry_run`, `plan_hash`,
`catalogue_version`, `git` (location class, finding keys; no values), `steps` (`step_id`, `item`, `kind`, `class`, `status`, `reason`,
`exit_status`), `applied`, `remaining`, `backups` (`~`-relative), `protocol` (`~`-relative; absent for `--dry-run`, plain `doctor`, `no`). When
`ok` is false, `result.reason` carries the code of [refusal-codes.json](refusal-codes.json) (the schema's `error` object has no field for it;
AP-6).

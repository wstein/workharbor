# 0016-mise-tool-layer: mise as the tool layer of whr workspaces (spike #319)

Date: 2026-11-03
Status: Proposed (draft from spike [#319](https://github.com/wstein/workharbor/issues/319)). Nothing is implemented. The owner
decides about BundleDock, Specs 007, 008 and 009 and OD-8 afterwards.

## Context

Tools come from BundleDock OCI volumes (`/opt/bdock/<bundle>`), versions are fixed per bundle, and the Mac host declares its
tools again in the Spec 009 catalogue. Owner principle (#319): one source of truth for tools, `mise.toml` layers; GUI apps
and system components stay with the host setup wizard. This ADR records what the spike measured on one Mac (Apple silicon,
OrbStack, mise 2026.10.25, Debian 13 arm64 container). Evidence:
[docs/quality/319-mise-spike](../quality/319-mise-spike/) holds the scripts, a lab setup script and stored output for layers
(`layers.out`, section 1), `mise.lock` (`lock.out`, section 4) and start time (`bench.out`, section 6). Results of the trust,
read-only, shared-store and volume experiments (sections 2, 3, 5, OD-8) come from runs of the listed scripts whose output is not
stored; they are reproducible but not recorded as files. Inventory and registry lookups (section 7) are not scripted. The lab root `$T` is a scratch directory; no repository
under `Anchorage/` was changed.

## Findings

### 1. Layers (design point 1): confirmed with mise's own hierarchy
Five files, lowest to highest precedence: user global (`~/.config/mise/config.toml`), whr global (`mise.toml` in an ancestor
directory of the workspace root), workspace root `mise.toml`, project `mise.toml`, `mise.local.toml`. The same tool (`jq`) was
declared in all five; the nearest file won (`1.8.2` from `mise.local.toml`). Tools and env keys keep their origin file:
`mise ls --current --json` (`source.path`) and `mise set` (third column). `whr mise plan` can be a reader over these two
commands; no whr merge logic is needed. Limits: (a) the whr global layer must sit in an ancestor directory of the workspace
(system config `MISE_SYSTEM_CONFIG_DIR` ranks below the user global); (b) `mise.<env>.toml` files load only when `MISE_ENV` is
set, so whr must pin `MISE_ENV`; (c) `.nvmrc`/`.python-version` were not loaded by default (`mise cfg`), idiomatic files are
per tool and UNKNOWN beyond that.
Config files mise loads without `MISE_ENV` (`mise cfg` in a fixture): `.tool-versions`, `mise.toml`, `.mise.toml`,
`mise.local.toml`, `.mise.local.toml`, `mise/config.toml`, `.mise/config.toml`, `.config/mise.toml`, `.config/mise/config.toml`,
`.config/mise/conf.d/*.toml`; with `MISE_ENV=dev` also `mise.dev.toml`, `.mise.dev.toml`, `mise.dev.local.toml`. This matches the
FR-037 list; `_.file`/`_.source` targets and `include` were not exercised (UNKNOWN).

### 2. Trust (design point 2): mise's defaults execute untrusted configuration
Hostile fixture (env template, `enter` and `postinstall` hooks, task) in a repository with no trust record, host and container:

| Setting | Result |
| --- | --- |
| default, non-CI | `mise env`, `ls`, `tasks`: refused (untrusted). `mise run`, `exec`, `install`, `x` and task shorthand **trust the config themselves and run** the env template, task and `postinstall` hook |
| `CI=true` or `MISE_YES=1` | everything runs, including `mise ls`/`env`; with `CI=true` also the `enter` hook (via `hook-env`) |
| `MISE_PARANOID=1` | every command refuses the repo config; `MISE_YES=1` does not override it |
| `MISE_IGNORED_CONFIG_PATHS=<repo dir>` | repo config is not loaded at all, even with `MISE_YES=1`; whr layers still apply; `mise run pwn` refuses |
| `MISE_TRUSTED_CONFIG_PATHS=<path>` | works from the environment, path prefix; a repo `[settings]` block cannot widen it (global-only) |

Plain configs (only `[tools]` with plain versions, tasks) load **without** trust: `mise exec -- rg` used the repository's
`ripgrep = 14.1.0` while `mise trust --show` said untrusted. "Tools only from whr layers" therefore needs `ignored_config_paths`,
not trust alone. Consequences:
- whr must start every mise process with `MISE_PARANOID=1` and `MISE_IGNORED_CONFIG_PATHS` for untrusted repositories, scrub
  `CI` and `MISE_YES` from the environment, and trust its own layer files by exact file path in `MISE_TRUSTED_CONFIG_PATHS`
  (under paranoid mode even the plain workspace layer needs trust; trusting the directory `/ws` also trusts the repository below it,
  measured). Host-side calls (`whr mise plan`, `mise lock`, `mise install`) run outside any sandbox and are the real exposure:
  a plain `mise run|install|exec` in a hostile repository directory on the Mac executes it.
- In the sandbox this is a guard against accidental execution, not a boundary against the agent: a process that controls its own
  environment can set `MISE_PARANOID=0`, empty the ignore list or write `~/.config/mise/config.toml` (all three ran the template
  in the container). The boundary is the sandbox itself and the read-only tool root; the acceptance wording "does not execute
  in the sandbox" holds for whr-started mise processes only. Spec 008 FR-037 should say so.
- Path trust has no content binding: with `MISE_TRUSTED_CONFIG_PATHS=<own repo>`, a `mise.local.toml` written later ran its
  template even in paranoid mode.

### 3. Tool storage (design point 3): read-only data directory works
Linux mise installs with `install --locked` into a bind-mounted data directory, then a container mounts it `ro` with cache and state
on tmpfs: `mise exec`, shims and direct PATH use run (8 tools); `touch`/`mkdir` in the data directory fail (EROFS); installing a
missing tool fails (`create_dir_all ... Read-only file system`, exit 1); `mise install --locked` reports "already installed" but exits 1
on shim regeneration, so installs must run outside the job. State and cache read-only also worked for `exec`. `MISE_SHARED_INSTALL_DIRS=/store/installs`
makes a read-only store plus a writable overlay data directory work: tools resolve from the store (`(shared)`), a new tool
installs into the overlay only. That is the rejected alternative for `developer` in section 5.
Compared with an in-sandbox install: the install needs network and write access, and a compromised install step would persist
in the sandbox; outside install plus `ro` mount keeps the job profiles unwritable (FR-016, OD-7).

### 4. mise.lock (design point 4): enforced by `--locked`
`mise lock` wrote checksums and URLs for 7 platforms. `mise install --locked`: in sync exit 0; version drift in `mise.toml` exit 1
("not in the lockfile"); a tool added without lock entry exit 1; a tampered checksum exit 1 (mismatch reported). Drift report for
whr: `mise install --locked --dry-run-code` exits 1 on drift. Costs: the lock must contain the target platform (`fd 10.4.0` had no
`linux-arm64` entry; `--locked` refused it); `mise install <tool>` rewrites `mise.lock` in the project. Release-age hiding
(`minimum_release_age`) was seen on `github:wstein/sourcesteward`.

### 5. Volume model (owner addition: explicit, named, persistent)
Current patterns: BundleDock `bundledock-<name>-<64 hex from the image ID>`, mounted `readonly,volume-nocopy` at `/opt/bdock/<name>`,
immutable, new image means new volume; whr refuses a writable `bundledock-*` mount (`runtime/admit.go`) and never removes one
(`localdocker/removal.go`). Home: `anchorage-home-${devcontainerId}` (`.devcontainer/devcontainer.json`), writable, one per
devcontainer. 3 `anchorage-tools-<64 hex>` volumes exist on this Docker host; their origin is not in this repository (UNKNOWN).
Proposal (needs a spec; nothing implemented):
- **Store volume** `whr-mise-<mise-version>-<os>-<arch>` (example `whr-mise-2026.10.25-linux-arm64`), labels `dev.workharbor.kind=mise-store`,
  `dev.workharbor.mise.version`, `.platform`. One per mise version and platform, shared by all projects: installs are keyed by tool and
  version inside, so layers add entries instead of new volumes. A per-lock-hash volume (BundleDock style) duplicates every
  shared tool and was rejected; the lock hash is recorded as a label or manifest of what was installed, not in the name.
  The name never contains a project, workspace or job id, so no container, workspace or job lifetime owns it.
- **Population** only by a whr step outside the sandbox (`install --locked` into the store mounted `rw` by that step alone).
- **Profiles** (owner decision 2026-11-03): `cooperative_job` and `untrusted_job` mount the store `ro` (verified). `developer`
  mounts the same store `rw`. Consequence: any `developer` session can replace binaries that later jobs run from the `ro` mount (FR-024
  accepts this at the owner level); a job's tools are only as trustworthy as the last `developer` session on that store. The population step
  and a `developer` session are then the only writers; concurrent writers on one store are UNKNOWN.
  Rejected alternative: store `ro` plus a per-workspace overlay volume `whr-mise-dev-<workspace-id>` through `MISE_SHARED_INSTALL_DIRS`
  (verified to work, section 3): it keeps job stores unwritable by `developer` but duplicates new tools per workspace and needs a promotion step.
- **Prune** (proposed `whr tools prune`, dry-run by default, `--apply` deletes): candidates are volumes with the label above that are
  not the newest per platform (the retention rule is an owner decision). Deletion uses `docker volume rm <name>` per volume. Measured:
  the daemon refuses an in-use volume ("volume is in use") and a volume referenced by a **stopped** container is kept too
  (`docker volume prune -a --filter label=...` skipped it); plain `docker volume prune` without `-a` removes only anonymous volumes
  and would never touch named ones. Unlabeled volumes are never selected. whr's fixed removal list currently refuses `bundledock-`
  and permits other names: a prefix rule for `whr-mise-` is needed there. Check-then-delete races are settled by the daemon refusal.
  Not verified: behavior under concurrent `docker create` during prune.
- The tool-root detection `isToolRoot` knows only `/opt/bdock`; a new root (for example `/opt/whr/mise`) needs the same `ro` rule (T018).

### 6. Measurements (design point: start time)
Workload: container start plus `--version` of 8 tools (jq, rg, fd, bat, fzf, gh, starship, zoxide), three runs, seconds, wall clock
of `docker run`, same Debian image. Baseline `docker run true`: 2.44, 1.83, 1.89 (container start is most of every number).

| Case | Runs | Mean |
| --- | --- | --- |
| mise cold: empty data, cache, state; `install --locked` over network, then run | 4.27, 3.76, 3.83 | 3.95 |
| mise cold, download cache kept | 2.61, 2.71, 2.71 | 2.68 |
| mise warm, data `ro`, `mise exec` | 1.58, 1.81, 1.82 | 1.74 |
| mise warm, shims on PATH | 1.75, 2.02, 1.62 | 1.80 |
| mise warm, PATH precomputed (no mise process) | 1.79, 1.87, 1.90 | 1.85 |
| BundleDock cold, init only: new volume via `init-volume` from the local carrier image (image pull excluded) | 38.65, 25.72, 39.07 | 34.48 |
| BundleDock cold, init plus the tool run that follows (comparable to the mise cold rows) | 40.69, 27.44, 40.85 | 36.33 |
| BundleDock warm, existing volume `ro` | 2.75, 1.61, 1.63 | 2.00 |

Warm start is not distinguishable between the two (differences are inside the baseline noise). In `bench.out` the BundleDock cold section interleaves init and tool-run times (lines alternate: init, run, init, run, init, run); the init-only
mean uses the init lines, the combined row adds each run's following tool run. Cold: mise 8 tools 4 s and 81 MB
against 1.9 GB toolbox volume (57 tools) and 27 to 41 s combined (the volume holds 57 tools; pull time is not included and not measured). Cold mise
needs network and depends on GitHub availability. The Anchorage workspace itself was not started: lab layers replicate its structure;
real workspace start is UNKNOWN. Docker and mise versions vary; one Mac only.

### 7. Inventory
Spec 009 catalogue (13 entries, `catalogue.json`):

| Entry | Class | Note |
| --- | --- | --- |
| orbstack | host-only | Homebrew cask, GUI/VM runtime |
| git | host-only | not in the mise registry; base image supplies git in containers |
| fish | mise-managed (`github:fish-shell/fish-shell`, owner decision 2026-11-03) | not in the registry; `mise install github:fish-shell/fish-shell@4.9.3` and `fish --version` worked on macOS arm64 (host) and on linux-arm64 (container, `fish-4.9.3-linux-aarch64.tar.xz`); Intel Mac and linux-amd64 UNVERIFIED; dotfile template stays with the wizard |
| gh, jq, starship, ripgrep, fd, fzf, zoxide, bat, jj, herdr | mise-managed | registry `aqua:` backends (`herdr`: aqua and github); gh, jq, rg, fd, bat, fzf, starship, zoxide installed in the lab on linux-arm64 (jq and rg also on macos-arm64); jj, herdr: registry only |

Count: 11 mise-managed (with fish), 2 host-only (orbstack, git). The 4 dotfile templates (fish, fzf, starship, zoxide) are host configuration and stay with Spec 009. Open for the owner: the wizard keeps
Homebrew for the host, so "no second catalogue" holds only if the host `mise.toml` replaces the 11 mise-managed entries.

BundleDock toolbox catalog (57 entries, `packages.catalog.json`; 56 are `github` release assets, 1 is a source build):
- mise-managed with registry backend (46): actionlint, age, bat, bottom, btop, cosign, direnv, dust, fd, fzf, gh, git-cliff, git-lfs, gitleaks,
  hadolint, herdr, hugo, hyperfine, jj, jq, just, lazygit, lsd, lychee, navi, nu, opentofu, pinact, regctl, rg, ruff, shellcheck, shfmt, sops, starship,
  syft, tmux, trivy, trufflehog, typos, typst, uv, watchexec, xh, yq, zoxide (registry lookup for all; lab install only for the 8 tools of section 6
  plus jq and ripgrep on macOS; the rest UNVERIFIED beyond registry presence).
- mise-managed through the `github:` backend (listing verified; install verified only for fish, see section 7; others UNVERIFIED): fish, gh-stack, jsonschema-cli (listing shows only
  language-prefixed tags, likely needs an asset filter), misspell, and the owner's `stw`, `whr`, `smill`, `cscribe`, `scc`.
- base image content: `mise` itself (bundle carries it; mise is not in its own registry), `sqlite3` (source build; base image or dropped).
- dropped: none proposed. Optional trims are the owner's call (btop, bottom, navi and similar interactive tools).
Counts: 46 + 9 + 2 = 57. Catalog name `rg` is `ripgrep` in the mise registry. A per-row table with install proof for all 57 is a follow-up.

## Content binding of trust (OD-8)
Spec 008 records OD-8 as decided in stages (2026-11-03): the residual risk stays accepted now; this section is the input for the owner's
re-evaluation. Practicable, with one mechanism and two limits. Measured in the container (`bind.sh`): `mise trust <file>` run outside the sandbox
in paranoid mode stores a sha256 of the file content in the state directory (`trusted-configs/*.hash`, equals `shasum -a 256` of the file);
the sandbox mounts that state directory read-only and cannot forge a record. After the sandbox edits `mise.toml`, mise treats it as untrusted
again; restoring the bytes restores trust; a new `mise.local.toml` is untrusted (hashes are per file). Proposed binding:
whr resolves the reviewed commit outside the sandbox, compares the hash of each FR-037 input file in the worktree with the
file at that commit, runs `mise trust` only on matches, and mounts the state directory read-only. A committed `mise.lock` alone is
insufficient: `--locked` binds tool artifacts (URL, checksum) but not tasks, hooks or env templates. Limits: (a) the record format is
mise-internal; whr must call `mise trust` and not write records; (b) trust is per path, so the path inside the sandbox must equal
the path used when trusting; (c) it does not cover the host-side path of a `cooperative_job` worktree: files the job writes run through
the owner's own mise. Countermeasure for (c), measured on the host: `mise settings paranoid=true` (or `MISE_PARANOID=1`) in the owner's
global configuration makes `mise run|install|exec` refuse untrusted files instead of trusting them. Recommended as an owner setting
in the Spec 009 wizard; OD-8 stays with the owner.

## Recommendation
1. Adopt mise as the tool layer for whr workspaces with the explicit named store volume above (`rw` for `developer`, `ro` for jobs); keep BundleDock as the base image carrier (toolbox with `mise`) until the owner decides otherwise; freeze it as stated in #319.
2. Amend Spec 008 before product code: FR-037 states that whr-started mise processes run with paranoid mode, ignore list, scrubbed `CI`/`MISE_YES`, exact-file trust of whr layers; the guard is not a boundary against the agent; add the volume, prune and `whr-mise-` removal rules; generalize `isToolRoot`.
3. Content binding: implement the reviewed-commit hash check plus `mise trust` with a read-only state directory (T019); owner decides OD-8.
4. Open: `whr mise plan` is not prototyped (no product code before the spec; the two reader commands above are its source); `whr tools prune` needs a spec and an owner retention rule.

## Not verified (UNKNOWN)
Windows/Intel Mac; the Anchorage workspace start; image pull time of the carrier; `_.file`, `_.source` and `include` handling; mise `exec`
while the data volume is shared by two concurrent writers; prune racing a concurrent `docker create`; macOS installs of the `github:` tools;
`.nvmrc` and similar idiomatic files; origin of the `anchorage-tools-*` volumes; mise behavior after upgrades (trust record format).

# Feature 009 research

Status: researched choices for review; nothing claimed implemented. Facts about Homebrew, OrbStack and macOS below are from general knowledge and
are measured on the owner's Mac in the task named (OG-1).

- **R-40 (archived)** said `whr setup` never installs a container runtime. The owner reversed that on 2026-10-20 (D-5). The archive stays
  unedited; the supersession is recorded here, in FR-003 and in AP-4 and AP-7. The kept part: the socket is never mounted into an environment.
- **Homebrew only (OD-1)**: one adapter. Homebrew has no per-version lock for core formulae, so "pinned" means exact name, tap and a minimum version in the catalogue, verified
  after install; a version below the minimum is reported, not forced. Homebrew is a precondition because
  installing it needs a downloaded script (to measure in T003).
- **Root and sudo**: Homebrew refuses to run as root, so `whr` refuses to start as root and never runs `brew` through sudo. `sudo` is only for
  catalogue-marked privileged steps, with the system `sudo` prompting on the terminal.
- **Detection without `brew`**: formula management is inferred from the resolved path under `<prefix>/Cellar/<name>/` and casks from
  `<prefix>/Caskroom/<name>` (to measure in T002). The Command Line Tools `git` shim (`/usr/bin/git` without a Command Line Tools or Xcode `git`)
  is detected by file checks because running it can open an installer (Feature 006 FR-060).
- **Git global configuration**: git reads `$XDG_CONFIG_HOME/git/config` (default `~/.config/git/config`) first and `~/.gitconfig` second; later
  entries win and multi-valued keys append, so XDG content followed by legacy content keeps the effective sequence. Relative `include.path` values
  resolve against the including file's directory, so a move changes their meaning. Verification by a staged file avoids reading the legacy and the
  new file as two live files (to measure with the installed git in T005).
- **Dotfiles**: starship reads `~/.config/starship.toml`; fish sources `~/.config/fish/conf.d/*.fish`. A dedicated drop-in leaves the user's
  `config.fish` alone; TOML cannot be merged safely without a parser and policy, so only skip or replace is offered.
- **Doctor and repair**: `--fix` moves mutation behind an explicit flag plus `--yes`; the host profile is what stops repair from reinstalling a
  tool the owner declined (DP-1).
- **Small curated catalogue (OD-2, amended)**: a mirror of about fifty toolbox tools on the host would cause version and install problems. The host list is
  small, owned by WorkHarbor and has a minimum version per tool; the same user experience means the same few DX tools and configs.
- **Protocol**: one append-only JSON Lines file per real run is the record; the text file renders it. `whr` never prunes it.
- **Not decided**: further catalogue entries (added deliberately); login shell and terminal profile; a restore command; signature verification of any `whr`
  download (none exist).

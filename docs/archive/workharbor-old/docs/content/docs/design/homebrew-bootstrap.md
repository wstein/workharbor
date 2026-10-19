---
title: Homebrew bootstrap
description: "Design note: whether setup installs Homebrew on a fresh Mac, the options and a recommendation (#491)."
weight: 11
toc: true
---

## Installing Homebrew from setup (#491)

**Status:** {{< status decided >}} option C (D59; Werner's decision of 2026-10-09, issue #505), built in a4d798c8 as the step `whr setup --only homebrew` runs (the administrator's run, D61). The page keeps the options as they were weighed. Claims about Homebrew, the Command Line Tools (CLT) and macOS were not measured on this host and are marked {{< status unverified >}}; sources are Homebrew's [installation page](https://docs.brew.sh/Installation) and [FAQ](https://docs.brew.sh/FAQ).

### Context

Product goal (2026-10-09): a freshly unpacked Mac mini, OS updated and logged in, reaches a working WorkHarbor without the user installing anything by hand first; the install step may use only what stock macOS ships. Today the doctor step `homebrew` (`internal/doctor/host.go`, manual step 5) only reports a missing `/opt/homebrew/bin/brew` and guides: `xcode-select --install` plus the pointer to brew.sh, "Its installer is a script from the internet, so whr does not run it for you." The next steps depend on it: `brew-packages` runs `brew bundle` for `container`, `git` and `gh`, and `brew-pin` pins `container`. Those two run `brew` through a `Fix` with `Cmds`, shown first and confirmed; privileged steps use `sudo` per command (one `sudo -v`, never kept alive), and `brew` itself never runs as root. Required order: CLT, Homebrew, Brewfile packages, `brew pin container`, then whr (step 13).

### What a truly fresh Mac needs (all options except A)

- **Command Line Tools.** Homebrew's installation page says that on Apple Silicon casks and bottles install without developer tools, and that the CLT (or Xcode) are needed for source builds and on Intel {{< status unverified >}}. Two separate reasons remain: `install.sh` installs the CLT itself, headless via `softwareupdate -i` with `xcode-select --install` as the fallback, when they are missing (read from the script, not run) {{< status unverified >}}; and WorkHarbor wants the CLT for `make` (manual step 5), whatever Homebrew needs. `xcode-select --install` alone opens a graphical dialog and cannot be answered from SSH {{< status unverified >}}.
- **Installer and sudo.** The official command is `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`. It must run as an administrator, not as root, and it asks for the password through `sudo` to create `/opt/homebrew` {{< status unverified >}}. Setting `NONINTERACTIVE=1` skips its confirmation prompts, but it still needs sudo to work without a prompt (a cached ticket from `sudo -v`) {{< status unverified >}}.
- **Prefix ownership.** `/opt/homebrew` ends up owned by the installing administrator (Homebrew FAQ; `install.sh` chowns it to `$USER:admin`) {{< status unverified >}}; the standard `workharbor` account can run `brew`-installed binaries but cannot install, upgrade or pin (manual step 5 already says so). That is the intended split and does not change with any option; whether `workharbor` can read and execute everything under the prefix by default is {{< status unverified >}}.
- **Where it runs.** The administrator's `whr setup` (D61; `whr setup host` is a hidden alias) runs as the administrator, so it could run the installer as that user. Not tested on a host with no CLT, no Homebrew and no `gh`.

### Options

| Option | What setup does | Value / effort (issue) | Risks |
| --- | --- | --- | --- |
| A. Keep guiding | Status quo: report and print the CLT command and brew.sh. | 3 / 1 | The fresh-Mac goal stays manual; the first step still stops the bootstrap. |
| B. Run the installer with a hash pin | Show URL and the script's SHA-256, ask for confirmation, run it as the administrator with `NONINTERACTIVE=1`. | 7 / 4 | A pinned hash breaks whenever Homebrew changes `install.sh` (a maintenance chore, and a stale pin blocks setup); a remote script runs with `sudo` rights; unattended behaviour {{< status unverified >}}. |
| C. Download, show, then run | `whr setup --only homebrew` downloads `install.sh` to a private file, prints its path, URL and SHA-256 (informational, not a pin), and runs it as the administrator only after the administrator confirms. | 6 / 3 | The human must actually read a long script; a TLS-only trust root (raw.githubusercontent.com); still a remote script run with `sudo`. |
| D. Pin a Homebrew `.pkg` | Use the macOS installer package from Homebrew's releases instead of the script, and check it with a SHA-256 pin or, alternatively, only the expected signing team ID from `pkgutil --check-signature`. | unrated | Missing from the issue. Apple Silicon only (docs.brew.sh). Whether the package is signed at all, and carries Homebrew's Developer ID signature (certified by Apple, not an Apple review), is {{< status unverified >}}; so are its prefix, ownership and CLT handling. A hash pin needs maintenance per release; a team-ID check needs the ID kept correct. |
| E. Offline or mirrored | Take the script from a path or an internal mirror given on the command line. | unrated | Missing from the issue. Homebrew itself downloads bottles and formulae from the network, so a mirror helps only with the script itself; a true offline bootstrap is not possible {{< status unverified >}}. |

### Common risks

- **Remote code as administrator.** Every scripted option runs downloaded code with `sudo`. Mitigation in B and C: display first, explicit confirmation, never `root`, never `workharbor`, and no way around the confirmation: no `--yes`, no `--save-answers` or answers file, no `--unattended` may answer it, and the step's command is not marked as a `sudo` command. This is a departure from the per-command `sudo` rule: other steps show and run each `sudo` argv one at a time, whereas here the shown argv is the script, and its internal `sudo` calls (`chown`, `mkdir`, `softwareupdate`) use the cached `sudo -v` ticket without being shown one by one.
- **Pinning.** A hash pin (B, and D if chosen) detects a changed file but cannot tell a good change from a bad one; it only moves the review to whoever updates the pin. C pins nothing in code and relies on the human reading the script.
- **Offline or a flaky network.** The step fails closed with a clear message; nothing is half-configured, but Homebrew's own failure state after an interrupted install is {{< status unverified >}}.
- **Order of trust.** Per the product goal, the first install of whr trusts TLS and its checksums; the Homebrew script under C is trusted by TLS only (the printed SHA-256 is informational). `gh` attestation checks come later, once Homebrew has installed `gh`.

### Recommendation

Option C, as the issue says, with two additions: the doctor `homebrew` step gains a `Fix` whose `Cmds` run the downloaded file only after the administrator confirms (no non-interactive skip for this step), and the CLT step stays guided: the `install.sh` route installs them headless only when it runs, so the doctor still guides `xcode-select --install` for `make` until that route is measured. Revisit D once its signing and prefix behaviour are measured, because it could replace the script with a pinned, signed binary. A is the fallback if the maintainer weighs the privilege risk above the fresh-Mac goal.

### Not measured; needed before building

A run on a host with no CLT, no Homebrew and no `gh`: the CLT prompt flow, the installer with `NONINTERACTIVE=1` and a cached `sudo -v` ticket, the resulting ownership of `/opt/homebrew`, and `workharbor`'s access to it. Until then every claim above marked unverified stays so.

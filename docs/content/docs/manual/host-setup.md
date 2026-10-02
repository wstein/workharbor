---
title: Prepare the Mac mini
description: A minimal, hardened host for workharbor on an Apple-silicon Mac mini.
weight: 1
toc: true
---

A checklist for the host, in order. Each step says why. Steps marked {{< status unverified >}} have not been tried on a real setup yet. The design decisions behind this page are D28 (software) and D29 (reachability) in the [design](../design/_index.md).

## The quick way: `whr setup` (provisional)

Two commands do most of this page, and the steps below stay the reference for what they do. Both check a step first, show the exact commands of its fix, run them only after you answer `y`, and check again; a step whose check passes does nothing. They need a terminal, and `--dry-run` runs the read-only checks for real and prints every fix without running any. `--only <step>` and `--from <step>` pick steps (the shell completes their names), and the steps marked optional run only when named.

1. **As your administrator account, not `root` and not `whr`:** `whr setup host`. It covers steps 2 to 5 and 8: the standard user `whr`, power settings, the firewall, SSH with keys only, FileVault (guided, because enabling it prints a recovery key whr must not see), no automatic log-out (guided: `autologout` reads `com.apple.autologout.AutoLogOutDelay`, absent or 0 is fine), the workspace volumes (`workspace-volume`: a root under `/Volumes` must be encrypted, which is guided because it needs your passphrase, and must honour ownership, which whr turns on with `sudo diskutil enableOwnership`; both read `diskutil info` text, and the key and the output format are {{< status unverified >}} on macOS 26), Homebrew and the Brewfile with `brew pin container`, and the `/opt/whr` prefix. Every privileged command runs as its own `sudo` command, shown first; it asks for your password once (`sudo -v`) and never keeps it alive. A file that must be root's is written to a private temporary file and installed with `sudo install`. Screen Sharing and Tailscale are guided and optional.
2. **Install whr** from a draft release (step 13, `make install-release`): the wizard refuses a `whr` that is not in an admin-owned prefix (`/opt/whr`, or Homebrew's) or that sits in a git working tree.
3. **As `whr`, in its desktop session (Terminal on the Mac, or over Screen Sharing), not over SSH:** `whr setup`. It covers steps 6, 12 and 13: the container kernel and system, the standard-user check, the private `~/.config/whr`, the API token (generated, never shown), an optional API key (typed without echo), the base configuration, `whr github app create`, the configuration's GitHub App keys (shown as a diff and written after a `y`, keeping every key it does not know), the tool store and `whr service install`.

Secrets are only ever generated or typed without echo, written `0600` with an exclusive create, never overwritten and never printed. What the wizard cannot do (enabling FileVault, the App's confirm click and installation, the ruleset check, signing in to Tailscale) it says, opens the right System Settings pane or link where it can, and checks afterwards. All of the wizard's commands and the output formats it reads are {{< status unverified >}} until it has set up the reference Mac mini (issue #73).

## 1. Hardware and macOS

- An Apple-silicon Mac mini. **Recommended: M6 with 32 GB memory and 512 GB storage**, for about ten concurrent agent environments. **On a budget: 16 GB**, for about four, with 512 GB or with 256 GB plus an external SSD. 24 GB / 512 GB sits in between, for about six (D32, [design §8](../design/architecture.md#8-resources)).

  | M6 configuration | US price, 1 October 2026 | Environments (estimate) |
  | --- | --- | --- |
  | 16 GB / 256 GB, budget with an external SSD | $899 | about 4 |
  | 16 GB / 512 GB, budget | $1,099 | about 4 |
  | 24 GB / 512 GB | $1,299 | about 6 |
  | **32 GB / 512 GB** | **$1,499** | **about 10** |

  Each memory step costs $200 and adds about four environments; prices change, so check the Apple Store for your country (Germany: from €1,049).
- Spend on memory before storage: memory cannot be upgraded later, storage can be added externally. An external SSD holds repositories, workspaces and backups well; whether Apple Container's own storage (images, volumes with the agents' build caches) can live there is {{< status unverified >}} (issue #54).
- macOS 26. The Apple Container measurements were made on macOS 26.6.2.
- Keep automatic security updates on, but **install macOS updates that restart the Mac yourself**: a restart stops every running agent until the supervisor resumes them.

## 2. Users

- Use your own administrator account for setup only: Homebrew, `sudo`, system settings. Each step below says which account runs it.
- Create a **standard user `whr`** for workharbor and Apple Container. Agents never run next to your own home directory, keychain or SSH keys.
- **`whr` needs a desktop login session**, not only SSH. Apple Container registers its services in the logged-in user's GUI launchd domain (`gui/<uid>`), and its state lives in that user's `~/Library/Application Support/com.apple.container`: a shell from SSH, `sudo -u whr` or `su whr` cannot start or reach them. So log in as `whr` on the Mac (or over Screen Sharing), start what step 6 and step 13 start from a Terminal in that session, and leave the session logged in; use fast user switching to reach your own account. Commands that do not touch containers (files, the configuration, `whr tools build`) also work from `sudo -iu whr`.
- **Keep that session logged in.** Turn off automatic log-out (*System Settings → Privacy & Security → Advanced → Log out automatically after inactivity*): a log-out ends Apple Container's services and every agent with them. A locked screen is fine; the session and its services keep running. Automatic login is not an option, because it needs FileVault off (step 3). `whr doctor` reads this setting from the system preferences; a delay that a managed (MDM) profile sets is not read, and a read that fails is reported as not verified, never as passing {{< status unverified >}}.
- Whether Apple Container runs for a **standard (non-administrator) user** is {{< status unverified >}}: the services are per user, but nobody has tried it on a fresh standard account yet. Check it once in step 6; if it fails, tell us in issue #38 before you make `whr` an administrator.

```bash
# as the administrator; you are asked for a password
sudo sysadminctl -addUser whr -fullName "workharbor" -password -
```

## 3. FileVault and restarts

Keep **FileVault on**. That rules out automatic login, which is the right trade-off for a machine that holds agent logins.

- **An external SSD is not covered by FileVault.** Encrypt it on its own, as the administrator: erase it as *APFS (Encrypted)* in Disk Utility, or encrypt an existing APFS volume with `diskutil apfs encryptVolume /Volumes/<ssd> -user disk` (it asks for a passphrase; keep it in your password manager). When `whr` first unlocks it, tick *Remember this password in my keychain*, so it mounts when `whr` logs in; before that login it stays locked, which matches FileVault's behaviour after a restart. macOS ignores file ownership on external volumes by default, which would let every account write to the workspaces: turn it on with `sudo diskutil enableOwnership /Volumes/<ssd>` and check *Get Info → Ignore ownership on this volume* is off. The keychain mount at login and the ownership default are {{< status unverified >}} on macOS 26.
- **Planned restarts:** `sudo fdesetup authrestart` restarts once without the unlock prompt.
- **After a power cut:** the Mac stops at the FileVault unlock screen. Nobody is logged in yet, so the Tailscale app (step 7) is not running and Screen Sharing is not available: unlock it with a keyboard and display, then log in as `whr` so its services start. Whether macOS 26 accepts a remote unlock over SSH at that screen is {{< status unverified >}}. A small UPS makes this rare.
- **workharbor runs as a LaunchAgent of `whr`, never a LaunchDaemon** (issue #38): Apple Container's services are in `whr`'s GUI launchd domain, so a daemon cannot reach them. `whr service install` (step 13) loads it, and it starts again at every login of `whr`. So after a power cut, one login as `whr` brings the supervisor, Apple Container and the agents back. Loading and restarting it was measured on the developer's own account ({{< status verified >}}, 2 October 2026, macOS with `container` 1.5.0); that it also works for the standard user `whr` and after a real reboot is {{< status unverified >}} (issue #73).

## 4. Power

A sleeping Mac pauses every agent.

```bash
sudo pmset -a sleep 0 disksleep 0 autorestart 1 womp 1
```

`autorestart 1` starts the Mac after a power cut; `womp 1` lets it wake on a Wake-on-LAN magic packet.

## 5. Software (Homebrew)

**As the administrator.** Homebrew's prefix `/opt/homebrew` belongs to the account that installed it, so `whr` cannot install, upgrade or pin anything; it only runs what is installed. Install the Xcode Command Line Tools (`xcode-select --install`, for `make`), [Homebrew](https://brew.sh), then the host packages from a `Brewfile`. Keep Homebrew from upgrading anything you did not ask for: `HOMEBREW_NO_AUTO_UPDATE` stops the automatic index refresh, `HOMEBREW_NO_INSTALL_UPGRADE` stops `brew install` from upgrading what is installed, and `brew pin` (step 6) holds a version through `brew upgrade`.

```bash
echo 'export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_UPGRADE=1' >> ~/.zprofile
```

```ruby
# Brewfile: the whole host software for workharbor (D28)
brew "container"      # Apple Container; workharbor was measured with 1.5.0
brew "git"
brew "gh"             # downloads and verifies whr's draft releases (step 13) until v0.1.0
cask "tailscale"      # only for the Tailscale option in step 7
# tap "wstein/tap"; brew "whr"   # from v0.1.0 (issue #62); until then step 13
```

```bash
brew bundle --file=Brewfile
brew pin container    # hold the version you tested through brew upgrade
```

`whr` finds these through its `PATH`: as `whr`, add `eval "$(/opt/homebrew/bin/brew shellenv)"` to `~/.zprofile`.

Do **not** install Claude Code, Codex CLI or other agent CLIs on the host for workharbor. workharbor fetches them into its own verified tool store and mounts them into each environment read-only (D19).

## 6. Apple Container

**As `whr`, in a Terminal of its desktop session** (step 2), install the Linux kernel the containers boot once, then start the container system and check it:

```bash
container system kernel set --recommended
container system start --disable-kernel-install
container system status
launchctl print gui/$(id -u) | grep com.apple.container   # the services run in this session
container list --all                                     # answers without an error
```

`--disable-kernel-install` skips the interactive kernel prompt, which is why the kernel is installed first; without a kernel no container starts. The last two lines are the standard-user check of step 2: if `container system start` or `container list` fails with a permission or bootstrap error, note the message in issue #38. After a restart the system does not start by itself; the workharbor LaunchAgent (step 13) runs `container system start --disable-kernel-install` in `whr`'s session before it starts the supervisor, which then resumes the agents.

## 7. Reach it from your phone

The web UI of workharbor listens on **loopback only** (`listen`), and the JSON API is not on the network at all: it is served on a unix socket, `api.sock` in the state directory (`~/.local/state/whr`, a `0700` directory), which only `whr`'s own account can open, so the `whr` commands work in `whr`'s session and a leaked API token is no use from the phone (D29). Sign-in to the web UI is by passkey once one is enrolled. A guest container reaches anything on the Mac's LAN address or on all interfaces, even from an isolated network, but not loopback (issue #69). Your phone reaches workharbor through a forwarder. Pick one option.

### Option A: Tailscale (default)

The quickest option. The Mac gets its own VPN interface and a stable name, and Tailscale can issue HTTPS certificates for that name, which the phone app (PWA) needs.

1. Install the Tailscale app on the Mac (step 5, as the administrator) and on the phone. Sign in on the Mac **in `whr`'s session**, the one that stays logged in; whether the app keeps the Mac on the VPN while no one or another user is logged in is {{< status unverified >}}.
2. As `whr`, forward the Mac's Tailscale name to workharbor on loopback with HTTPS: `tailscale serve --bg <port>` (check the exact syntax with `tailscale serve --help`). workharbor itself stays on loopback.
3. Optional: limit the phone to that port with a Tailscale access rule.
4. Whether a guest container can reach the Mac's Tailscale address is {{< status unverified >}} (issue #69); the API token guards it either way.

Tailscale's coordination server is a third party. [Headscale](https://github.com/juanfont/headscale) replaces it with a self-hosted one.

### Option B: WireGuard on your router (FRITZ!Box)

No VPN software on the Mac and no third party. A FRITZ!Box offers WireGuard from FRITZ!OS 7.50 ({{< status unverified >}}: check your version).

1. On the FRITZ!Box: *Internet → Permit Access → VPN (WireGuard)*, add a connection for your phone, and import it into the WireGuard app with the QR code.
2. The phone then reaches the Mac at its LAN address. There is no VPN interface on the Mac, so the address alone does not tell your phone from any other device on the LAN.
3. So workharbor stays on loopback, and a small HTTPS proxy on the Mac's LAN address forwards to it. Guest containers can reach that proxy too, so a `pf` packet-filter rule admits only the addresses the FRITZ!Box gives VPN clients to its port, and the API token guards every request. The macOS firewall in System Settings cannot do this: it filters by app, not by address. The `pf` rule and how the FRITZ!Box numbers VPN clients are {{< status unverified >}} (issue #69): check the address your phone gets.
4. The phone app needs HTTPS: use your own certificate authority (installed on the phone) or a certificate for a domain you own.

A line without a public IPv4 address (DS-Lite, carrier-grade NAT) may not accept inbound WireGuard ({{< status unverified >}}); Tailscale works there.

### Option C: WireGuard on the Mac

A VPN interface like Tailscale's, without a third party, but you forward a UDP port on the router to the Mac and manage keys yourself. Choose it only if you need both properties.

## 8. Firewall and SSH

- macOS firewall on, in stealth mode: *System Settings → Network → Firewall*.
- **Every guest container can reach the Mac's services that listen on all interfaces**, even from an isolated network (issue #69). Turn off what you do not need in *System Settings → General → Sharing* (File Sharing, Screen Sharing, AirPlay Receiver, Media Sharing).
- Remote Login (SSH) only for your administrator account (*Allow access for*), with **keys only**: set `PasswordAuthentication no` and `KbdInteractiveAuthentication no` in a file under `/etc/ssh/sshd_config.d/`. Otherwise an agent could guess passwords. Keep it until workharbor's short-lived SSH certificates exist (issue #32).
- A `pf` rule that blocks the container subnets (`192.168.64.0/24` for the default network, and the `--internal` networks') from the Mac's own addresses closes this for every service; it is {{< status unverified >}} and comes with issue #69.
- Screen Sharing over the VPN works once a user is logged in; it does not reach the FileVault unlock screen (step 3). It is how you reach `whr`'s desktop session from afar (step 2); if you keep it on, allow it only for your administrator and `whr`, and it stays reachable from guests until the `pf` rule above exists.
- **Headless is fine.** After setup the Mac runs without a display, keyboard or mouse, and you reach it over SSH and Screen Sharing. Without a display attached, Screen Sharing may offer only a low resolution; an HDMI dummy plug fixes that ({{< status unverified >}} on macOS 26). Keep a display and keyboard at hand for the FileVault unlock after a power cut (step 3).
- From SSH as the administrator, `sudo -iu whr` gives a `whr` shell for files and builds, but not for `container` or `whr serve` (step 2).
- **Only the administrator logs in to the host, and only for administration** (macOS and `whr` upgrades, recovery). Your daily shell work, in the workspaces and across them, goes through the console instead: `whr console` opens a shell in a VM with git, zsh, fish, `ripgrep`, `tmux`, `make` and an editor, every workspace mounted read-only, behind the same egress allowlist as the agents (the package registries and `github.com` for reads, never the model API), with none of your credentials or the agents'. `whr console <workspace>` starts in that workspace; `whr console <workspace> --write` mounts that one workspace read-write, which you want when you edit, and needs the console closed first if it was opened with other writable workspaces (`whr console --close`; its home, with your dotfiles and history, is kept). `git` in the console ignores the hooks, filters, aliases and other commands that a workspace's configuration sets, because agents write that configuration. The shell comes through `whr` and the token on the private socket of the supervisor; it is not reachable from the phone or through the web UI. The first console builds its image, which takes a while.
- **SSH into the console (issue #32).** `whr ssh` opens an SSH session in the console, never on the host, and `whr ssh --config` prints a `Host whr-console` block for `~/.ssh/config` so that VS Code Remote-SSH and JetBrains can use it. Turn it on once: run `whr setup` (its optional `ssh-ca` step makes the authority key, a `0600` file in the configuration directory) and add `"console": {"ssh_ca_key_file": "<that path>"}` to the configuration. Your key stays on the machine you run `whr` on; each connection gets a certificate for it that lasts minutes (a certificate that still has four minutes left is reused, so a connection does not sign twice), for the user `whr` only, and the console's host key is pinned from `whr`, so nothing is trusted on first sight and nothing needs renewing by hand. The console listens on no port: SSH is carried over the same API as `whr console`, so it is reachable exactly as far as that is, and the `ssh` client must be installed where you run `whr ssh`. Port forwarding, which the editors' remote modes need, is off unless you pass `--forward`, and then reaches only the console's own loopback. Losing the authority key means anyone holding it can sign a certificate, so it is a secret like the API token: back it up the same way, and if it leaks, delete the file, run the step again and restart `whr serve`.

## 9. Backups

- Back up the `whr` user's workharbor state: the database, configuration and audit log (Time Machine or another backup).
- Exclude container volumes and images: they are rebuilt, and volumes grow sparsely (issue #54).

## 10. Phone notifications (optional)

Install the [ntfy](https://ntfy.sh) app and subscribe to the topic workharbor generates during onboarding ([design §9.4](../design/interfaces.md#94-notifications)). A notification carries only a task ID, an event kind and a link; the link needs the VPN from step 7.

## 11. The GitHub App

workharbor talks to GitHub as an App of your own, never with your personal token (D15, D31): its tokens last about an hour, cover one repository and only the permissions below, and the App cannot merge, tag or release. On github.com, as the repository's owner:

**The quick way: `whr github app create`** (provisional name). It creates the App from a manifest, so there is no form to fill in and no `.pem` to download. You need the `listen` and `api_token_file` lines of the configuration (step 13) and the HTTPS name your forwarder gives workharbor (step 7). `whr serve` must not be running, because the supervisor needs the App this creates.

```bash
whr github app create --public-url https://<your-forwarded-name>
```

1. It prints a link and waits on the configuration's `listen` address. Open the link on any device that reaches that name (the Mac or the phone, over the VPN of step 7) and press **Continue to GitHub**. GitHub shows the App's name and asks you to confirm: **Create GitHub App**. For an organization's App, add `--org <name>`; you must own the organization.
2. GitHub sends the browser back to workharbor with a one-time code. Only a link this command just made is accepted, once, and for ten minutes (`--ttl`); anything else is refused. `whr` exchanges the code for the App and writes the private key to `~/.config/whr/github-app-<id>.pem` (mode `0600`, never overwriting a file). The key, the client secret and the webhook secret are never printed or logged; the two secrets are dropped.
3. The command prints the two lines to add to the configuration, `"github": {"app_id": <id>, "key_file": "<path>"}`: it does not edit the file. It also prints the link to install the App. Install it as in step 3 below, and check the ruleset as in step 4. The App is private, has no active webhook and only the permissions below.
4. `whr doctor` then checks, from GitHub, that the App is installed on every configured repository with exactly those permissions.

Whether GitHub accepts the manifest as `whr` sends it (its inactive webhook, a private App) is {{< status unverified >}} until the first run against github.com. **If it does not work, or you prefer to click, create the App by hand** with the steps below:

1. **Create the App.** Your account's **Settings → Developer settings → GitHub Apps → New GitHub App**.
    - **Name:** for example `workharbor-<your-name>` (it must be unique on GitHub). **Homepage URL:** the repository's URL.
    - **Webhook:** untick **Active**. `whr` asks GitHub when it needs something; webhooks would need a public address, which workharbor does not have (D29).
    - **Repository permissions:** Contents **Read and write**, Issues **Read and write**, Pull requests **Read and write**; Metadata stays **Read-only**. Nothing else: without the Workflows permission the App cannot change `.github/workflows`, and without Administration it cannot change rulesets.
    - **Where can this GitHub App be installed:** **Only on this account**.
    - **Create GitHub App**, then note the **App ID** on its General page.
2. **Generate a private key** on the same page (**Private keys → Generate a private key**). The browser downloads a `.pem` file: that file is the secret, see step 12.
3. **Install it** (**Install App**) on your account with **Only select repositories** and pick the repositories workharbor works on. `whr` finds the installation by itself. **One App serves all your repositories**: to add one later, add it to the installation's repository list and to `repositories` in the configuration; each token `whr` mints still covers one repository only. Repositories of an organization need the App installed there too, which takes **Any account** in step 1, or a second App owned by the organization.
4. **Check the ruleset of `main`** (the repository's **Settings → Rules → Rulesets**): changes need a pull request with a human review, force pushes are blocked, and the App is **not** in the bypass list (D15). The supervisor pushes only `agent/*` branches and opens pull requests; merging stays with you.

    **Which ruleset each workflow needs** (D47; set per repository in the configuration as `"workflow"`, default `integration`; `whr doctor` checks it as `forge-workflow`):

    | Workflow | Where approved commits go | The ruleset to set up |
    | --- | --- | --- |
    | `prototype` | the supervisor fast-forwards the integration branch (`integration_branch`, required, and never the default branch) to the approved commit; no pull request | force pushes blocked on that branch, and only the App and you may write to it ({{< status unverified >}}: whether a ruleset can name the App as the only other writer) |
    | `integration` (default) | a pull request into `integration_branch` (default `develop`) that you merge and promote to `main` | that branch protected, a pull request required |
    | `published` | a pull request into the default branch; the agent asks for every tool (`manual`) | the default branch requires a pull request with a review and status checks and signed commits, and has **no bypass actor** |

    Whether the App's token can read these rulesets (and a ruleset's bypass list) is {{< status unverified >}}: where it cannot, `whr doctor` says "not verified", never "ok". Changing a repository's workflow in the configuration is a policy change: `whr serve` refuses to start until you confirm it with `--accept-workflow-change`, the change is recorded, and tasks already started keep the workflow they started under.

## 12. Secrets

Every secret is a file with mode `0600`, owned by the `whr` user, outside the workspace roots and the tool store, and the configuration names only its path. Never paste a secret into a chat, an agent session, a command line, a script or an issue: an agent never needs the value, only the path. `whr serve` refuses a secret file that is not `0600`, belongs to another user, is a link or has a second hard link, or lies inside a workspace root or the tool store.

As the `whr` user:

```bash
mkdir -p ~/.config/whr && chmod 700 ~/.config/whr

# The API token the local whr CLI uses: generated, never typed.
umask 077 && openssl rand -base64 32 > ~/.config/whr/api.token

# The GitHub App's private key from step 11. `whr github app create` has already
# written it to ~/.config/whr/github-app-<id>.pem; only a key you downloaded needs moving.
mv ~/Downloads/<app-name>.*.private-key.pem ~/.config/whr/github-app.pem
chmod 600 ~/.config/whr/github-app.pem
```

**The agent's login:**

- **A subscription (Claude Pro or Max) is the default** and has no file: you sign in inside the environment with Claude Code's own login, and `whr` never sees it (design D40, [agent vendor terms](vendor-terms.md)). How that works from the console is being measured (issue #82); the first sign-in needs a Terminal in `whr`'s desktop session (step 2).
- **An API key is optional.** Type it so that it is neither echoed nor kept in the shell history nor visible in the process list (`read -s` does not echo, and `printf` is a shell builtin):

  ```bash
  umask 077; read -rs KEY; printf 'ANTHROPIC_API_KEY=%s\n' "$KEY" > ~/.config/whr/agent.env; unset KEY
  ```

**Keys for your own experiments** (a spike, a test) live in a password manager such as `pass` or 1Password, and a script reads them from a `0600` env file named by an environment variable, never inline: the repository's hooks refuse a literal key. If a secret leaks anyway, follow the runbook in [SECURITY.md](https://github.com/wstein/workharbor/blob/main/SECURITY.md).

## 13. Install and configure whr (dogfood)

Until `v0.1.0` the host runs a **dogfood draft release**: a signed prerelease tag `v0.1.0-alpha.N` on `main` that CI built, attested and left as a draft ([design D24, D34](../design/decisions.md)). Nothing is built on the host. From `v0.1.0` on, `brew install wstein/tap/whr` replaces step 1.

1. **Install, as the administrator.** A draft can be downloaded only by a writer of the repository, so this runs with your own GitHub login (`gh auth login`), never in `whr`'s account. The prefix belongs to the administrator, so nothing running as `whr`, an agent that escaped included, can replace the supervisor:

    ```bash
    sudo install -d -o "$(id -un)" -g admin -m 755 /opt/whr
    git clone https://github.com/wstein/workharbor.git && cd workharbor
    make install-release VERSION=v0.1.0-alpha.1    # PREFIX=/opt/whr is the default
    ```

    It downloads the macOS archive, the guest archive and `checksums.txt`, checks both archives against the checksums and against the build-provenance attestation of the repository's release workflow, and installs `whr` in `/opt/whr/bin` and the guest binaries `whr-shim` and `whr-proxy` in `/opt/whr/libexec/whr`; it installs nothing if a check fails. Whether `gh release download` finds a draft by its tag is {{< status unverified >}} until the first draft (issue #103). Upgrade the same way with the next tag. Then, as `whr`, add `export PATH=/opt/whr/bin:$PATH` to `~/.zprofile`: `whr version` shows the tag, and `whr completion zsh` (or `bash`, `fish`) prints the shell completion.

The remaining steps run as the `whr` user: steps 2 and 3 from any `whr` shell, step 4 from a Terminal of its desktop session (step 2).

2. **Fill the tool store.** `whr tools build -store <tool store> -shim /opt/whr/libexec/whr/whr-shim-linux-arm64` downloads Claude Code at the version pinned in the repository, checks it against the pin and the vendor's manifest, stores it read-only and adds the launcher. A checksum mismatch stops it with nothing stored.
3. **Write the configuration file**, `~/.config/whr/config.json`. Secrets are paths (step 12), never values:

    ```json
    {
      "listen": "127.0.0.1:8787",
      "repositories": [{ "name": "<owner>/<repository>" }],
      "roots": {
        "workspaces": ["/Volumes/<ssd>/workspaces"],
        "tool_store": "/Users/whr/tools"
      },
      "github": { "app_id": 123456, "key_file": "/Users/whr/.config/whr/github-app.pem" },
      "api_token_file": "/Users/whr/.config/whr/api.token",
      "agent_allowed_tools": ["Read", "Edit", "Write", "Bash(git status:*)", "Bash(make check:*)"]
    }
    ```

    To mirror task state on a GitHub project board (provisional, issue #70, {{< status unverified >}} until it runs with the real App), add `"board": {"owner": "<organization>", "organization": true, "number": <project number>, "public_url": "https://<your-forwarded-name>"}`: the project needs a single-select `Status` field with the options `Needs you`, `In progress`, `Ready to push` and `Done`, and may have a `Session` field and a text field `Task` that holds the link. The App then needs the organization's Projects permission (`whr github app create --board` asks for it; `whr doctor` checks it).
    To sign in to the web UI and answer reviews from the phone with a passkey (provisional, issue #101, {{< status unverified >}} until it runs on a real phone), set `"public_url": "https://<your-forwarded-name>"`, restart, and run `whr passkey add phone` on the host: open the link it prints on the phone within 5 minutes. From then on the web UI signs in with the passkey only; `whr passkey ls` and `whr passkey rm <id>` manage them, and only on the host.
    Add `"agent_api_key_env_file": "/Users/whr/.config/whr/agent.env"` only for an API key. `agent_allowed_tools` is required in the default `dontAsk` mode: only the tools listed there run, and the list above is an example to adapt. To approve each tool use yourself instead, set `"agent_permission_mode": "manual"` and remove the list: every prompt then arrives in `whr inbox` as an approval (`whr approve <id>` or `whr reject <id>`) and the agent waits for you for ten minutes before it is told no.
4. **Start it and create a workspace.** In `whr`'s desktop session, because the supervisor talks to Apple Container's per-session services (step 2): `whr serve` checks the whole configuration at start and lists every problem. The web UI is on `listen` (`http://127.0.0.1:8787/`, or your forwarded HTTPS name) and the `whr` commands use the API socket in the state directory (keep `state_dir` short: a unix socket path is at most 100 bytes): sign in with the API token, then use the harbor, the inbox and the task pages from a browser or the phone (design §9.3). In another terminal: `whr ws add <name> --path <empty folder below a workspace root> --repo <owner>/<repository> --role <role>` creates the workspace, seeds its agent clone and starts its environment; `whr agent add <workspace> <role>` adds an agent. Then `whr run <issue-url> --agent <workspace>/<role>`. To keep it running, let launchd do it (provisional command): `whr service install` writes `~/Library/LaunchAgents/io.github.wstein.workharbor.plist` (mode 0644, yours), loads it into `gui/<your uid>`, and from then on the job runs `container system start --disable-kernel-install`, then `whr serve`, and starts it again if it exits, at most every 30 seconds. It refuses to run outside the graphical session (a shell from SSH or `sudo` is in another domain), with a `whr` that sits in a git working tree (the job runs the installed binary, `/opt/whr/bin/whr` or `$(brew --prefix)/opt/whr/bin/whr`, never a build of a topic; run `whr service install` from it, or pass `--whr`), and with a configuration that `whr serve` would reject. Output goes to `~/Library/Logs/whr/whr.out.log` and `whr.err.log`, which launchd does not rotate. `whr service status` shows whether it is loaded and its pid, and `whr service uninstall` unloads it and removes the plist.

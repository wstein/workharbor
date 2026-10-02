---
title: Prepare the Mac mini
description: A minimal, hardened host for workharbor on an Apple-silicon Mac mini.
weight: 1
toc: true
---

A checklist for the host, in order. Each step says why. Steps marked {{< status unverified >}} have not been tried on a real setup yet. The design decisions behind this page are D28 (software) and D29 (reachability) in the [design](../design/_index.md).

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
- **`whr` needs a desktop login session**, not only SSH. Apple Container registers its services in the logged-in user's GUI launchd domain (`gui/<uid>`), and its state lives in that user's `~/Library/Application Support/com.apple.container`: a shell from SSH, `sudo -u whr` or `su whr` cannot start or reach them. So log in as `whr` on the Mac (or over Screen Sharing), start what step 6 and step 13 start from a Terminal in that session, and leave the session logged in; use fast user switching to reach your own account. Commands that do not touch containers (files, `make install`, the configuration) also work from `sudo -iu whr`.
- Whether Apple Container runs for a **standard (non-administrator) user** is {{< status unverified >}}: the services are per user, but nobody has tried it on a fresh standard account yet. Check it once in step 6; if it fails, tell us in issue #38 before you make `whr` an administrator.

```bash
# as the administrator; you are asked for a password
sudo sysadminctl -addUser whr -fullName "workharbor" -password -
```

## 3. FileVault and restarts

Keep **FileVault on**. That rules out automatic login, which is the right trade-off for a machine that holds agent logins.

- **Planned restarts:** `sudo fdesetup authrestart` restarts once without the unlock prompt.
- **After a power cut:** the Mac stops at the FileVault unlock screen. Nobody is logged in yet, so the Tailscale app (step 7) is not running and Screen Sharing is not available: unlock it with a keyboard and display, then log in as `whr` so its services start. Whether macOS 26 accepts a remote unlock over SSH at that screen is {{< status unverified >}}. A small UPS makes this rare.
- Whether workharbor runs as a LaunchAgent of `whr` or as a LaunchDaemon is still open (issue #38).

## 4. Power

A sleeping Mac pauses every agent.

```bash
sudo pmset -a sleep 0 disksleep 0 autorestart 1 womp 1
```

`autorestart 1` starts the Mac after a power cut; `womp 1` lets it wake on a Wake-on-LAN magic packet.

## 5. Software (Homebrew)

**As the administrator.** Homebrew's prefix `/opt/homebrew` belongs to the account that installed it, so `whr` cannot install, upgrade or pin anything; it only runs what is installed. Install the Xcode Command Line Tools (`xcode-select --install`, for `make` and the compilers), [Homebrew](https://brew.sh), then the host packages from a `Brewfile`. Keep Homebrew from upgrading anything you did not ask for: `HOMEBREW_NO_AUTO_UPDATE` stops the automatic index refresh, `HOMEBREW_NO_INSTALL_UPGRADE` stops `brew install` from upgrading what is installed, and `brew pin` (step 6) holds a version through `brew upgrade`.

```bash
echo 'export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_UPGRADE=1' >> ~/.zprofile
```

```ruby
# Brewfile: the whole host software for workharbor (D28)
brew "container"      # Apple Container; workharbor was measured with 1.5.0
brew "git"
brew "go"             # builds whr with make install (step 13) until there are releases
cask "tailscale"      # only for the Tailscale option in step 7
# tap "wstein/tap"; brew "whr"   # from the first release (issue #62)
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

`--disable-kernel-install` skips the interactive kernel prompt, which is why the kernel is installed first; without a kernel no container starts. The last two lines are the standard-user check of step 2: if `container system start` or `container list` fails with a permission or bootstrap error, note the message in issue #38. After a restart the system does not start by itself; the workharbor launchd job will start it in `whr`'s session and then resume agents (issue #38).

## 7. Reach it from your phone

workharbor listens on **loopback only**, and every request needs its API token (D29). A guest container reaches anything on the Mac's LAN address or on all interfaces, even from an isolated network, but not loopback (issue #69). Your phone reaches workharbor through a forwarder. Pick one option.

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
- From SSH as the administrator, `sudo -iu whr` gives a `whr` shell for files and builds, but not for `container` or `whr serve` (step 2).

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

## 13. Build and configure whr (dogfood)

As the `whr` user (steps 1 to 3 from any `whr` shell, step 4 from a Terminal of its desktop session, step 2), in a checkout of the repository on a clean commit ([design D34](../design/decisions.md)):

1. **Install.** `make install` builds `whr`, the launcher `whr-shim` and the egress proxy `whr-proxy` (both for the guest, linux-arm64) from the current commit, with the version stamp, and installs them under `PREFIX` (default `~/.local`; the guest binaries go to `libexec/whr`). It refuses a dirty tree and a commit that is not on `origin/main` (run `git fetch origin` first), so the supervisor always runs approved, committed code (D34). It builds with `GOWORK=off` and an empty `GOFLAGS`, so a parent `go.work` or your environment cannot change the build. `whr version` shows the version and whether the tree was clean. `whr completion zsh` (or `bash`, `fish`) prints the shell completion.
2. **Fill the tool store.** `whr tools build -store <tool store> -shim ~/.local/libexec/whr/whr-shim-linux-arm64` downloads Claude Code at the version pinned in the repository, checks it against the pin and the vendor's manifest, stores it read-only and adds the launcher. A checksum mismatch stops it with nothing stored.
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

    Add `"agent_api_key_env_file": "/Users/whr/.config/whr/agent.env"` only for an API key. `agent_allowed_tools` is required while the agent runs without host approvals (issue #75): only the tools listed there run, and the list above is an example to adapt.
4. **Start it and create a workspace.** In `whr`'s desktop session, because the supervisor talks to Apple Container's per-session services (step 2): `whr serve` checks the whole configuration at start and lists every problem. In another terminal: `whr ws add <name> --path <empty folder below a workspace root> --repo <owner>/<repository> --role <role>` creates the workspace, seeds its agent clone and starts its environment; `whr agent add <workspace> <role>` adds an agent. Then `whr run <issue-url> --agent <workspace>/<role>`. Running `whr serve` as a launchd job that survives a restart comes with issue #38.

---
title: Prepare the Mac mini
description: A minimal, hardened host for workharbor on an Apple-silicon Mac mini.
weight: 1
toc: true
---

A checklist for the host, in order. Each step says why. Steps marked {{< status unverified >}} have not been tried on a real setup yet. The design decisions behind this page are D28 (software) and D29 (reachability) in the [design](../design.md).

## 1. Hardware and macOS

- An Apple-silicon Mac mini. **Recommended: M6 with 32 GB memory and 512 GB storage**, for about ten concurrent agent environments. **On a budget: 16 GB**, for about four, with 512 GB or with 256 GB plus an external SSD. 24 GB / 512 GB sits in between, for about six (D32, [design §8](../design.md#8-resources)).

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

- Use your own administrator account for setup only.
- Create a **standard user `whr`** for workharbor and Apple Container. Agents never run next to your own home directory, keychain or SSH keys.

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

Install [Homebrew](https://brew.sh), then the host packages from a `Brewfile`. Keep Homebrew from upgrading anything you did not ask for: `HOMEBREW_NO_AUTO_UPDATE` stops the automatic index refresh, `HOMEBREW_NO_INSTALL_UPGRADE` stops `brew install` from upgrading what is installed, and `brew pin` (step 6) holds a version through `brew upgrade`.

```bash
echo 'export HOMEBREW_NO_AUTO_UPDATE=1 HOMEBREW_NO_INSTALL_UPGRADE=1' >> ~/.zprofile
```

```ruby
# Brewfile: the whole host software for workharbor (D28)
brew "container"      # Apple Container; workharbor was measured with 1.5.0
brew "git"
cask "tailscale"      # only for the Tailscale option in step 7
# tap "wstein/tap"; brew "whr"   # from the first release (issue #62)
```

```bash
brew bundle --file=Brewfile
```

Do **not** install Claude Code, Codex CLI or other agent CLIs on the host for workharbor. workharbor fetches them into its own verified tool store and mounts them into each environment read-only (D19).

## 6. Apple Container

As `whr`, install the Linux kernel the containers boot once, then start the container system and check it:

```bash
container system kernel set --recommended
container system start --disable-kernel-install
container system status
```

`--disable-kernel-install` skips the interactive kernel prompt, which is why the kernel is installed first; without a kernel no container starts. After a restart the system does not start by itself; the workharbor launchd job will start it and then resume agents (issue #38). Pin the version you tested: `brew pin container`.

## 7. Reach it from your phone

workharbor listens on **loopback only**, and every request needs its API token (D29). A guest container reaches anything on the Mac's LAN address or on all interfaces, even from an isolated network, but not loopback (issue #69). Your phone reaches workharbor through a forwarder. Pick one option.

### Option A: Tailscale (default)

The quickest option. The Mac gets its own VPN interface and a stable name, and Tailscale can issue HTTPS certificates for that name, which the phone app (PWA) needs.

1. Install the Tailscale app on the Mac (step 5) and on the phone, and sign in on both.
2. Forward the Mac's Tailscale name to workharbor on loopback with HTTPS: `tailscale serve --bg <port>` (check the exact syntax with `tailscale serve --help`). workharbor itself stays on loopback.
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
- Screen Sharing over the VPN works once a user is logged in; it does not reach the FileVault unlock screen (step 3).

## 9. Backups

- Back up the `whr` user's workharbor state: the database, configuration and audit log (Time Machine or another backup).
- Exclude container volumes and images: they are rebuilt, and volumes grow sparsely (issue #54).

## 10. Phone notifications (optional)

Install the [ntfy](https://ntfy.sh) app and subscribe to the topic workharbor generates during onboarding ([design §9.4](../design.md#94-notifications)). A notification carries only a task ID, an event kind and a link; the link needs the VPN from step 7.

## 11. Build and configure whr (dogfood)

Three steps, as the `whr` user, in a checkout of the repository on a clean commit ([design D34](../design.md)):

1. **Install.** `make install` builds `whr`, the launcher `whr-shim` and the egress proxy `whr-proxy` (both for the guest, linux-arm64) from the current commit, with the version stamp, and installs them under `PREFIX` (default `~/.local`; the guest binaries go to `libexec/whr`). It refuses a dirty tree and a commit that is not on `origin/main` (run `git fetch origin` first), so the supervisor always runs approved, committed code (D34). It builds with `GOWORK=off` and an empty `GOFLAGS`, so a parent `go.work` or your environment cannot change the build. `whr version` shows the version and whether the tree was clean.
2. **Fill the tool store.** `whr tools build -store <tool store> -shim ~/.local/libexec/whr/whr-shim-linux-arm64` downloads Claude Code at the version pinned in the repository, checks it against the pin and the vendor's manifest, stores it read-only and adds the launcher. A checksum mismatch stops it with nothing stored.
3. **Write the configuration file.** One JSON file with the repositories, the cache, workspace and tool-store directories, the GitHub App ID and key file, the agent-login file, the API token file and a loopback listen address. Every secret is a path to a file with mode `0600`, never a value in the configuration. `whr serve` checks all of it at start and lists every problem.

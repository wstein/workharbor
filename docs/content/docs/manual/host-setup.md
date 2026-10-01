---
title: Prepare the Mac mini
description: A minimal, hardened host for workharbor on an Apple-silicon Mac mini.
weight: 1
toc: true
---

A checklist for the host, in order. Each step says why. Steps marked **unverified** have not been tried on a real setup yet. The design decisions behind this page are D28 (software) and D29 (reachability) in the [design](../design.md).

## 1. Hardware and macOS

- An Apple-silicon Mac mini; 16 GB of memory plans for about four concurrent agent environments ([design §8](../design.md#8-resources)).
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
- **After a power cut:** unlock the Mac once (screen sharing or a keyboard), then log in as `whr` so its services start.
- Whether workharbor runs as a LaunchAgent of `whr` or as a LaunchDaemon is still open (issue #38).

## 4. Power

A sleeping Mac pauses every agent.

```bash
sudo pmset -a sleep 0 disksleep 0 autorestart 1 womp 1
```

`autorestart 1` starts the Mac after a power cut; `womp 1` lets it wake for network access.

## 5. Software (Homebrew)

Install [Homebrew](https://brew.sh), then the host packages from a `Brewfile`. Keep Homebrew from upgrading on its own:

```bash
echo 'export HOMEBREW_NO_AUTO_UPDATE=1' >> ~/.zprofile
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

Start the container system once as `whr`, then check it:

```bash
container system start --disable-kernel-install
container system status
```

`--disable-kernel-install` skips an interactive prompt. After a restart the system does not start by itself; the workharbor launchd job will start it and then resume agents (issue #38). Pin the version you tested: `brew pin container`.

## 7. Reach it from your phone

workharbor listens on loopback and on exactly **one address you configure**, never on all interfaces: a container on the default network can reach any host service bound to all interfaces (D29). Pick one option.

### Option A: Tailscale (default)

The quickest option. The Mac gets its own VPN interface and a stable name, and Tailscale can issue HTTPS certificates for that name, which the phone app (PWA) needs.

1. Install the Tailscale app on the Mac (step 5) and on the phone, and sign in on both.
2. workharbor listens on the Mac's Tailscale address.
3. Optional: limit the phone to the workharbor port with a Tailscale access rule.

Tailscale's coordination server is a third party. [Headscale](https://github.com/juanfont/headscale) replaces it with a self-hosted one.

### Option B: WireGuard on your router (FRITZ!Box)

No VPN software on the Mac and no third party. A FRITZ!Box offers WireGuard from FRITZ!OS 7.50 (**unverified**: check your version).

1. On the FRITZ!Box: *Internet → Permit Access → VPN (WireGuard)*, add a connection for your phone, and import it into the WireGuard app with the QR code.
2. The phone then reaches the Mac at its LAN address. There is no VPN interface on the Mac, so the address alone does not tell your phone from any other device on the LAN.
3. So workharbor listens on the Mac's LAN address, and the macOS firewall admits only the addresses the FRITZ!Box gives VPN clients. How the FRITZ!Box numbers VPN clients is **unverified**: check the address your phone gets.
4. The phone app needs HTTPS: use your own certificate authority (installed on the phone) or a certificate for a domain you own.

A line without a public IPv4 address (DS-Lite, carrier-grade NAT) may not accept inbound WireGuard (**unverified**); Tailscale works there.

### Option C: WireGuard on the Mac

A VPN interface like Tailscale's, without a third party, but you forward a UDP port on the router to the Mac and manage keys yourself. Choose it only if you need both properties.

## 8. Firewall and SSH

- macOS firewall on, in stealth mode: *System Settings → Network → Firewall*.
- Remote Login (SSH) only for your administrator account, and only over the VPN, until workharbor's short-lived SSH certificates exist (issue #32).
- Screen Sharing over the VPN is enough for unlocking after a power cut.

## 9. Backups

- Back up the `whr` user's workharbor state: the database, configuration and audit log (Time Machine or another backup).
- Exclude container volumes and images: they are rebuilt, and volumes grow sparsely (issue #54).

## 10. Phone notifications (optional)

Install the [ntfy](https://ntfy.sh) app and subscribe to the topic workharbor generates during onboarding ([design §9.4](../design.md#94-notifications)). A notification carries only a task ID, an event kind and a link; the link needs the VPN from step 7.

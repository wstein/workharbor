---
title: Manual
description: Setting up and running workharbor on a Mac mini.
weight: 3
---

How to set up and run workharbor. workharbor is building release 1 and has no runnable service yet, so this manual covers what can be done today: preparing the host and installing from source, the security notes, and what the agent vendors' terms mean for your login. Command names are provisional and follow the [design](../design/_index.md).

{{< cards >}}
  {{< card link="host-setup" title="Prepare the Mac mini" subtitle="macOS, user, power, Homebrew, Apple Container, VPN, firewall, backups." icon="desktop-computer" >}}
  {{< card link="security" title="Security notes" subtitle="What you accept by running agents, and how to limit it." icon="shield-check" >}}
  {{< card link="vendor-terms" title="Agent vendor terms" subtitle="What a subscription login means under workharbor, with the sources." icon="scale" >}}
  {{< card link="run-the-supervisor" title="Run the supervisor" subtitle="whr serve, workspaces, passkeys, previews, doctor (draft, provisional)." icon="server" >}}
  {{< card link="daily-use" title="Daily use" subtitle="Start a task, follow it, answer, pause, resume (draft, provisional)." icon="play" >}}
{{< /cards >}}

Installing `whr`, signing in, daily use and operations follow with the first release, `v0.1.0` (issue #65).

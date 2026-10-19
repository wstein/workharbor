---
title: WorkHarbor
layout: hextra-home
---

{{< hextra/hero-badge link="docs/design/" >}}
  <span>Building release 1</span>
  {{< icon name="arrow-circle-right" attributes="height=14" >}}
{{< /hextra/hero-badge >}}

<div class="hx:mt-6 hx:mb-6">
{{< hextra/hero-headline >}}
  Supervise AI coding agents.&nbsp;<br class="hx:sm:block hx:hidden" />Stay in the loop.
{{< /hextra/hero-headline >}}
</div>

<div class="hx:mb-12">
{{< hextra/hero-subtitle >}}
  Self-hosted: agents work on your repository issues in isolated workspaces while you answer questions, intervene, review and approve every push from one dashboard on every device. The command-line tool is&nbsp;<code>whr</code>.
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx:mb-6">
{{< hextra/hero-button text="Read the design" link="docs/design/" >}}
{{< hextra/hero-button text="Set it up" link="docs/manual/" style="background: transparent; border: 1px solid currentColor; color: var(--whr-hero-secondary);" >}}
</div>

<div class="hx:mt-6"></div>

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="A supervisor with a dashboard, not an IDE"
    subtitle="Tasks, runs, workspaces and environments are separate objects you watch and steer from the dashboard on any device or the CLI. Attaching or detaching an editor never interrupts the agent."
    icon="adjustments"
  >}}
  {{< hextra/feature-card
    title="Human in the loop"
    subtitle="Agents raise decisions: questions, approvals and reviews. You answer them from your phone, the web app or the CLI."
    icon="chat-alt"
  >}}
  {{< hextra/feature-card
    title="Isolated by default"
    subtitle="Apple Container on an Apple-silicon Mac mini comes first: each workspace's environment, a lightweight VM that its agents share, out to the internet only through an allowlist proxy, with short-lived, per-run forge credentials."
    icon="shield-check"
  >}}
  {{< hextra/feature-card
    title="Approval boundaries are policy"
    subtitle="Agents commit inside their environment; the host never runs git there. Their commits leave as a bundle, are checked against the supervisor's mirror and are pushed only after you approve the exact commit. Merge, tag, release and deploy stay with you."
    icon="badge-check"
  >}}
  {{< hextra/feature-card
    title="One service layer"
    subtitle="whr and the web UI share the same service layer, so anything you can click you can script."
    icon="terminal"
  >}}
  {{< hextra/feature-card
    title="Traceable work"
    subtitle="Every commit can carry its issue, task, run and the AI model that helped, so history shows who or what did what."
    icon="clipboard-check"
  >}}
{{< /hextra/feature-grid >}}

## What goes wrong when agents run unsupervised

Each failure has an answer in the design. Release 1 is still being built: these are the intended behaviour, not a track record.

- **Nothing stops a push.** Here an agent's commits go to the forge only after you approve the exact commit; merge, tag, release and deploy stay with you. See [security](docs/manual/security.md).
- **No isolation.** Here each workspace has its own environment, and its agents reach the internet only through an allowlist proxy. See [the design](docs/design/).
- **Secrets are within reach.** Here forge credentials are short-lived and per run, and `whr` never handles a subscription login. See [vendor terms](docs/manual/vendor-terms.md).
- **No way to follow or stop a run.** Here runs and decisions show in the dashboard and the CLI, where you can steer or stop them. See [daily use](docs/manual/daily-use.md).
- **One login shared by many agents.** Here the agents of one workspace share that workspace's sign-in (you sign in once per environment), and only you start runs. Several sessions on one sign-in: {{< status open >}} (D42, #82). See [vendor terms](docs/manual/vendor-terms.md).

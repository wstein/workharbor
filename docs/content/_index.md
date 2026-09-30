---
title: workharbor
layout: hextra-home
---

{{< hextra/hero-badge link="docs/design/" >}}
  <span>Design phase</span>
  {{< icon name="arrow-circle-right" attributes="height=14" >}}
{{< /hextra/hero-badge >}}

<div class="hx:mt-6 hx:mb-6">
{{< hextra/hero-headline >}}
  Supervise AI coding agents.&nbsp;<br class="hx:sm:block hx:hidden" />Stay in the loop.
{{< /hextra/hero-headline >}}
</div>

<div class="hx:mb-12">
{{< hextra/hero-subtitle >}}
  Agents work on your repository issues in isolated workspaces while you answer questions, intervene, review and approve. The command-line tool is&nbsp;<code>whr</code>.
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx:mb-6">
{{< hextra/hero-button text="Read the design" link="docs/design/" >}}
</div>

<div class="hx:mt-6"></div>

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="Task supervision, not an IDE"
    subtitle="Tasks, workspaces, runs and environments are separate objects. Attaching or detaching an editor never interrupts the agent."
    icon="adjustments"
  >}}
  {{< hextra/feature-card
    title="Human in the loop"
    subtitle="Agents raise decisions: questions, approvals and reviews. You answer them from the CLI or the web UI."
    icon="chat-alt"
  >}}
  {{< hextra/feature-card
    title="Isolated by default"
    subtitle="Apple Container on an Apple-silicon Mac mini comes first, with default-deny networking and short-lived per-run credentials."
    icon="shield-check"
  >}}
  {{< hextra/feature-card
    title="Approval boundaries are policy"
    subtitle="Agents push branches and open pull requests. Merge, release and deploy stay with you."
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

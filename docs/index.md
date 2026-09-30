---
title: Home
layout: default
nav_order: 1
description: Self-hosted supervisor for AI coding agents.
image: /assets/images/social-preview.png
---

<div class="wh-hero">
  <span class="wh-eyebrow">Self-hosted · Go · Apple Container first</span>
  <h1>Supervise AI coding agents. <em>Stay in the loop.</em></h1>
  <p class="wh-lead">
    workharbor lets agents work on your repository issues independently, in isolated
    workspaces, while you answer questions, intervene, review and approve.
    The command-line tool is <code>whr</code>.
  </p>
  <a class="btn btn-primary fs-5 mb-4 mb-md-0 mr-2" href="{{ '/design/' | relative_url }}">Read the design</a>
  <a class="btn fs-5 mb-4 mb-md-0" href="https://github.com/wstein/workharbor">View on GitHub</a>
</div>

{: .warning }
There is no working implementation yet. Everything here describes the intended design and is subject to change.

## Why workharbor

<div class="wh-grid">
  <div class="wh-card">
    <h3>Task supervision, not an IDE</h3>
    <p>Tasks, workspaces, runs and environments are separate objects. Attaching or detaching an editor never interrupts the agent.</p>
  </div>
  <div class="wh-card">
    <h3>Human in the loop</h3>
    <p>Agents raise decisions: questions, approvals and reviews. You answer them from the CLI or the web UI.</p>
  </div>
  <div class="wh-card">
    <h3>Isolated by default</h3>
    <p>Apple Container on an Apple-silicon Mac mini comes first, with default-deny networking and short-lived per-run credentials. Other runtimes follow through adapters.</p>
  </div>
  <div class="wh-card">
    <h3>Approval boundaries are policy</h3>
    <p>Agents push branches and open pull requests. Merge, release and deploy stay with you.</p>
  </div>
  <div class="wh-card">
    <h3>One API</h3>
    <p><code>whr</code> and the web UI are clients of the same API, so anything you can click you can script.</p>
  </div>
  <div class="wh-card">
    <h3>Traceable work</h3>
    <p>Every commit can carry its issue, task, run and the AI model that helped, so history shows who or what did what.</p>
  </div>
</div>

## Planned CLI

```bash
whr login --server <url>
whr run <issue-url>
whr ls
whr logs <task> -f
whr say <task> "use the existing retry helper"
whr inbox
whr approve <decision>
```

Command names are provisional.

## Stack

Go, a single static binary, SQLite, and a server-rendered web UI built with `templ`, htmx and server-sent events.

## License

[EUPL-1.2](https://github.com/wstein/workharbor/blob/main/LICENSE)

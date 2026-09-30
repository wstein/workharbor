---
title: Home
layout: home
nav_order: 1
description: Self-hosted supervisor for AI coding agents.
image: /assets/images/social-preview.png
---

# workharbor

Let AI coding agents work on your repository issues independently, in isolated workspaces, while you stay in the loop to answer questions, intervene, review and approve. The command-line tool is **`whr`**.
{: .fs-6 .fw-300 }

[Read the design](design){: .btn .btn-primary .fs-5 .mb-4 .mb-md-0 .mr-2 }
[View on GitHub](https://github.com/wstein/workharbor){: .btn .fs-5 .mb-4 .mb-md-0 }

---

{: .warning }
**Design phase.** There is no working implementation yet. Everything here describes the intended design and is subject to change.

## Why workharbor

- **Task supervision, not an IDE.** Tasks, workspaces, runs and environments are separate objects. Attaching or detaching an editor never interrupts the agent.
- **Human in the loop.** Agents raise decisions (questions, approvals, reviews) and you answer them from the CLI or the web UI.
- **Isolated by default.** The first target is Apple Container on an Apple-silicon Mac mini, with default-deny networking and short-lived per-run credentials. Other runtimes follow through adapters.
- **Approval boundaries are policy.** Agents push branches and open PRs. Merge, release and deploy stay with you.
- **One API.** `whr` and the web UI are clients of the same API.

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

Go, a single static binary and SQLite.

## License

[EUPL-1.2](https://github.com/wstein/workharbor/blob/main/LICENSE)

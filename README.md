# workharbor

A self-hosted supervisor for AI coding agents. Agents work on repository issues independently in managed, isolated workspaces; you stay in the loop to answer questions, intervene, review and approve.

The command-line tool is **`whr`**.

> **Status:** design phase. There is no implementation yet. See [docs/design.md](docs/design.md).

## Concept

- **Task supervision, not an IDE.** Tasks, workspaces, runs and environments are separate objects; attaching or detaching an editor never interrupts the agent.
- **Human in the loop.** Agents raise decisions (questions, approvals, reviews); you answer them from the CLI or web UI.
- **Isolated by default.** First target is Apple Container on an Apple-silicon Mac mini, with default-deny networking and per-run credentials. Other runtimes follow through adapters.
- **Approval boundaries are policy.** Agents push branches and open PRs; merge, release and deploy stay with you.
- **One API.** The `whr` CLI and the web UI are clients of the same API.

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

Command names are provisional; see the design document.

## Stack

Go, single static binary, SQLite.

## License

[EUPL-1.2](LICENSE)

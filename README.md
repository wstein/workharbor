<h1 align="center">
  <img src="assets/banner.png" alt="workharbor: supervise AI coding agents, stay in the loop" width="100%">
</h1>

[![License: EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![Status: design phase](https://img.shields.io/badge/status-design%20phase-orange.svg)](docs/content/docs/design.md)
[![Platform: macOS (Apple silicon)](https://img.shields.io/badge/platform-macOS%20Apple%20silicon-lightgrey?logo=apple&logoColor=white)](docs/content/docs/design.md)

> [!WARNING]
> **Work in progress: design phase.** workharbor has no working implementation yet. The repository currently holds the [design](docs/content/docs/design.md), the skeleton of the Go module and the project tooling. Interfaces, command names and the architecture are provisional and will change. Do not use it to supervise real work. Documentation: <https://wstein.github.io/workharbor/>.

A self-hosted supervisor for AI coding agents. Agents work on repository issues independently in managed, isolated workspaces; you stay in the loop to answer questions, intervene, review and approve.

The command-line tool is **`whr`**.

## Concept

- **Task supervision, not an IDE.** Tasks, workspaces, runs and environments are separate objects; attaching or detaching an editor never interrupts the agent.
- **Human in the loop.** Agents raise decisions (questions, approvals, reviews); you answer them from the CLI or web UI.
- **Isolated by default.** First target is Apple Container on an Apple-silicon Mac mini, with default-deny networking and short-lived, per-run forge credentials. Other runtimes follow through adapters.
- **Approval boundaries are policy.** Agents commit in their own checkouts on the host; after your cleanup and approval the supervisor pushes the branch and opens the PR. Merge, release and deploy stay with you.
- **One service layer.** The `whr` CLI (over the JSON API) and the server-rendered web UI share the same service layer.

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

Go, a single static binary, SQLite, and a server-rendered web UI (`templ`, htmx, SSE).

## Status and roadmap

Design phase, with the domain model being built test first. The plan is tracked in GitHub milestones:

1. [M0 Decisions](https://github.com/wstein/workharbor/milestone/1): open design decisions and spike follow-ups
2. [R1 Slice](https://github.com/wstein/workharbor/milestone/2): a CLI-only vertical slice, `whr run <issue-url>` to an opened pull request
3. [R1 Complete](https://github.com/wstein/workharbor/milestone/3): the rest of release 1, including the web UI and phone client
4. [Later](https://github.com/wstein/workharbor/milestone/4): medium and long term

See [all milestones](https://github.com/wstein/workharbor/milestones), [open issues](https://github.com/wstein/workharbor/issues) and the [delivery plan](docs/content/docs/design.md#13-delivery) in the design.

## Contributing

Contributions are welcome, especially design reviews. Please read [CONTRIBUTING.md](CONTRIBUTING.md) and the [Code of Conduct](CODE_OF_CONDUCT.md). Report security problems privately as described in [SECURITY.md](SECURITY.md).

## License

[EUPL-1.2](LICENSE)

<h1 align="center">
  <img src="assets/banner.png" alt="workharbor: supervise AI coding agents, stay in the loop" width="100%">
</h1>

[![License: EUPL-1.2](https://img.shields.io/badge/license-EUPL--1.2-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.27-00ADD8?logo=go&logoColor=white)](go.mod)
[![Status: building release 1](https://img.shields.io/badge/status-building%20release%201-orange.svg)](docs/content/docs/design/_index.md)
[![Platform: macOS (Apple silicon)](https://img.shields.io/badge/platform-macOS%20Apple%20silicon-lightgrey?logo=apple&logoColor=white)](docs/content/docs/design/_index.md)

> [!WARNING]
> **Work in progress: no release yet.** The building blocks exist and are tested: the domain model and SQLite store, hardened host git, the Apple Container adapter with its egress proxy, the Claude Code adapter in degraded mode and the tool store. The supervisor service (`whr serve`) and the task commands below do not exist yet. Command names outside the stable set and parts of the architecture will change. Do not use it to supervise real work. Documentation: <https://wstein.github.io/workharbor/>.

A self-hosted supervisor for AI coding agents. Agents work on repository issues independently in managed, isolated workspaces; you stay in the loop to answer questions, intervene, review and approve.

The command-line tool is **`whr`**.

## Concept

- **Task supervision, not an IDE.** Tasks, workspaces, runs and environments are separate objects; attaching or detaching an editor never interrupts the agent.
- **Human in the loop.** Agents raise decisions (questions, approvals, reviews); you answer them from your phone, the web app or the CLI.
- **Isolated by default.** First target is Apple Container on an Apple-silicon Mac mini: each agent in its own lightweight VM, reaching the internet only through an allowlist proxy, with short-lived, per-run forge credentials. Other runtimes follow through adapters.
- **Your agent, your login, within its terms.** Claude Code first, with your own subscription or an API key. You sign in inside each environment; `whr` never handles a subscription login, and only you start runs ([vendor terms](docs/content/docs/manual/vendor-terms.md)).
- **Approval boundaries are policy.** Agents commit in their own checkouts on the host; after your cleanup and approval the supervisor pushes the branch and opens the PR. Merge, release and deploy stay with you.
- **One service layer.** The `whr` CLI (over the JSON API) and the server-rendered web UI share the same service layer.

## Planned CLI

```bash
whr serve                      # the supervisor: JSON API, web app, reconciler
whr run <issue-url>
whr ls
whr logs <task> -f
whr say <task> "use the existing retry helper"
whr inbox
whr approve <decision>
whr answer <decision> <option>
```

These are the stable commands of the first slice (design D37); none exists yet, and every other command is provisional.

## Build

```bash
make build         # bin/whr: whr version and whr tools build work today
make install       # whr, whr-shim and whr-proxy from a clean commit on origin/main
```

Setting up the host is in the [manual](https://wstein.github.io/workharbor/docs/manual/).

## Stack

Go, a single static binary, SQLite, and a server-rendered web UI (`templ`, htmx, SSE).

## Status and roadmap

Building release 1, dogfood first: workharbor develops workharbor as soon as it can run one issue end to end (D34). The plan is tracked in GitHub milestones:

1. [Dogfood](https://github.com/wstein/workharbor/milestone/5): the smallest set that runs a real workharbor issue through `whr`
2. [M0 Decisions](https://github.com/wstein/workharbor/milestone/1): open design decisions and spike follow-ups
3. [R1 Slice](https://github.com/wstein/workharbor/milestone/2): a CLI-only vertical slice, `whr run <issue-url>` to an opened pull request
4. [R1 Complete](https://github.com/wstein/workharbor/milestone/3): the rest of release 1, including the web app for phone and tablet
5. [Later](https://github.com/wstein/workharbor/milestone/4): medium and long term, such as API-key mode as a full peer (D41)

See [all milestones](https://github.com/wstein/workharbor/milestones), [open issues](https://github.com/wstein/workharbor/issues) and the [delivery plan](docs/content/docs/design/roadmap.md#13-delivery) in the design.

## Contributing

Contributions are welcome, especially design reviews and the open spikes. Please read [CONTRIBUTING.md](CONTRIBUTING.md) and the [Code of Conduct](CODE_OF_CONDUCT.md). Report security problems privately as described in [SECURITY.md](SECURITY.md).

## License

[EUPL-1.2](LICENSE)

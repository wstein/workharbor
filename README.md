<h1 align="center">
  <img src="assets/banner.png" alt="WorkHarbor: supervise AI coding agents, stay in the loop" width="100%">
</h1>

<p>
<a href="LICENSE"><img alt="License: EUPL-1.2" src="https://img.shields.io/badge/license-EUPL--1.2-blue.svg?style=flat-square"></a>
<a href="https://github.com/wstein/workharbor/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/wstein/workharbor/actions/workflows/ci.yml/badge.svg?branch=main"></a>
<a href="https://scorecard.dev/viewer/?uri=github.com/wstein/workharbor"><img alt="OpenSSF Scorecard" src="https://api.scorecard.dev/projects/github.com/wstein/workharbor/badge"></a>
<a href="https://www.bestpractices.dev/projects/15300"><img alt="OpenSSF Best Practices" src="https://www.bestpractices.dev/projects/15300/badge"></a>
<a href="go.mod"><img alt="Go" src="https://img.shields.io/badge/go-1.26.9%2B-00ADD8?logo=go&logoColor=white&style=flat-square"></a>
<a href="docs/content/docs/design/_index.md"><img alt="Platform: macOS (Apple silicon)" src="https://img.shields.io/badge/platform-macOS%20Apple%20silicon-lightgrey?logo=apple&logoColor=white&style=flat-square"></a>
</p>

WorkHarbor lets AI coding agents work on your repository issues in isolated workspaces on your own Mac, and nothing leaves your machine until you approve it. You follow and steer every agent from one dashboard on your phone, tablet or computer.

> [!WARNING]
> **Work in progress: pre-releases only.** The `v0.1.0-alpha.N` pre-releases are published on GitHub for dogfooding; `v0.1.0` is still the first release. The supervisor (`whr serve`), the CLI, the web UI and the Apple Container and Claude Code adapters exist and are tested, mostly against fakes; the first end-to-end run of a real issue (#28) is next. Command names outside the stable set and parts of the architecture will still change. Do not use it to supervise real work yet. Documentation: <https://wstein.github.io/workharbor/>.

A self-hosted supervisor for AI coding agents: isolated workspaces, your approval for every push, one dashboard on every device. Agents work on repository issues independently in managed, isolated workspaces; you stay in the loop to answer questions, intervene, review and approve.

The command-line tool is **`whr`**.

## Concept

- **A supervisor with a dashboard, not an IDE.** Tasks, runs, workspaces and environments are separate objects you watch and steer from the dashboard (web, phone) or the CLI; attaching or detaching an editor never interrupts the agent.
- **Workspaces with named agents.** A workspace is a folder with its own isolated environment; each named agent (`<workspace>/<role>`) works on its own branch there. A console environment gives you a shell next to them, without logging in to the host.
- **Human in the loop.** Agents raise Decisions (questions, approvals, reviews); you answer them from your phone, the web app or the CLI.
- **Isolated by default.** First target is Apple Container on an Apple-silicon Mac mini: each workspace gets its own environment, reaching the internet only through an allowlist proxy, with short-lived, per-run forge credentials. The agents of one workspace share that environment; put agents that must not touch each other in separate workspaces. Other runtimes follow through adapters.
- **Your agent, your login, within its terms.** Claude Code first, with your own subscription or an API key. You sign in inside each environment; `whr` never handles a subscription login, and only you start runs ([vendor terms](docs/content/docs/manual/vendor-terms.md)).
- **Approval boundaries are policy.** Agents commit inside their environment; the host never runs git there. An agent's commits leave as a git bundle, are checked on the host against a supervisor-owned mirror of the repository, and are pushed only after you approve the exact commit ("Ready to push?"). Merge, tag, release and deploy stay with you, enforced by the forge adapter, not by prompts.
- **One service layer.** The `whr` CLI (over the JSON API) and the server-rendered web UI share the same service layer.

## What goes wrong when agents run unsupervised

Each failure below has an answer in the design. The `v0.1.0-alpha.N` pre-releases are published and `v0.1.0` is the first release, but it is still being built, so these are the intended behaviour, not a track record.

- **Nothing stops a push.** An agent can push its own work. Here an agent's commits leave as a bundle and go to the forge only after you approve the exact commit; merge, tag, release and deploy stay forbidden for agents ([approval boundaries](#concept), [security](docs/content/docs/manual/security.md)).
- **No isolation.** An agent runs with your host's files and network. Here each workspace has its own environment, and its agents reach the internet only through an allowlist proxy ([isolated by default](#concept)).
- **Secrets are within reach.** Long-lived tokens end up where an agent can read them. Here forge credentials are short-lived and per run, and `whr` never handles a subscription login ([vendor terms](docs/content/docs/manual/vendor-terms.md)).
- **No way to follow or stop a run.** Here runs, decisions and events are visible in the dashboard and the CLI, and you can steer or stop a run from either (`whr logs`, `whr say`, [daily use](docs/content/docs/manual/daily-use.md)).
- **One login shared by many agents.** Here the agents of one workspace share that workspace's sign-in (you sign in once per environment), and only you start runs; several sessions on one sign-in is an open question, D42 and #82 ([vendor terms](docs/content/docs/manual/vendor-terms.md)).

## What you need

- An Apple-silicon Mac mini (or Mac) running macOS with [Apple Container](https://github.com/apple/container), the first runtime. Release 1 supports no other host.
- A GitHub App for the repositories the agents work on: `whr github app create` makes it from a manifest.
- An agent login: a Claude subscription, signed in inside the environment and never given to `whr`, or an API key, which `whr` keeps in a `0600` file.
- Go 1.26.9 or later, only to build from source. CI checks Go 1.26.9 and 1.27.2 separately. Documentation, release and documentation vulnerability tools require Go 1.27; the development image uses Go 1.27.1.

The [manual](https://wstein.github.io/workharbor/docs/manual/) walks through the host, the App and the first run. It is a draft until `v0.1.0`.

## CLI (provisional)

```bash
whr serve                      # the supervisor: JSON API, web app, reconciler
whr run <issue-url> --agent <workspace>/<role>
whr ls
whr logs <task> -f
whr say <task> "use the existing retry helper"
whr inbox
whr approve <decision>
whr answer <decision> <option>
```

These are the stable commands of the first slice (design D37); they exist, but have not run against a `v0.1.0` release yet (only the `v0.1.0-alpha.N` pre-releases are published). Others (`whr ws`, `whr agent`, `whr console`, `whr setup`, `whr doctor`, …) are provisional; see the [manual](https://wstein.github.io/workharbor/docs/manual/).

## Build

```bash
make build         # bin/whr
make install       # whr, whr-shim and whr-proxy from the local main (warns on a dirty tree or another HEAD)
```

The dogfood host installs a pre-release instead: download the release archive and `checksums.txt`, verify them and run `sudo ./install.sh <tag>` (no clone, no `gh`; the commands are in the release notes and on the [install page](https://wstein.github.io/workharbor/docs/manual/install-upgrade-release/)). `make install-release VERSION=<tag>` is the route from a clone. From `v0.1.0` on, `brew install wstein/tap/whr`.

Setting up the host is in the [manual](https://wstein.github.io/workharbor/docs/manual/); installing, upgrading and releasing are on its [install page](https://wstein.github.io/workharbor/docs/manual/install-upgrade-release/).

## Stack

Go, a single static binary, SQLite, and a server-rendered web UI (`templ`, htmx, SSE).

## Status and roadmap

Building release 1, dogfood first: WorkHarbor develops WorkHarbor as soon as it can run one issue end to end (D34). The plan is tracked in GitHub milestones:

1. [Dogfood](https://github.com/wstein/workharbor/milestone/5): the smallest set that runs a real WorkHarbor issue through `whr`
2. [M0 Decisions](https://github.com/wstein/workharbor/milestone/1) (closed): the design decisions and spike follow-ups that came before release 1; new decisions are tracked in the issue that needs them
3. [R1 Slice](https://github.com/wstein/workharbor/milestone/2): a CLI-only vertical slice, `whr run <issue-url>` to an opened pull request
4. [R1 Complete](https://github.com/wstein/workharbor/milestone/3): the rest of release 1, including the web app for phone and tablet
5. [Later](https://github.com/wstein/workharbor/milestone/4): medium and long term, such as API-key mode as a full peer (D41)

See [all milestones](https://github.com/wstein/workharbor/milestones), [open issues](https://github.com/wstein/workharbor/issues) and the [delivery plan](docs/content/docs/design/roadmap.md#13-delivery) in the design.

## Contributing

Contributions are welcome, especially design reviews and the open spikes. Please read [CONTRIBUTING.md](.github/CONTRIBUTING.md) and the [Code of Conduct](.github/CODE_OF_CONDUCT.md). Report security problems privately as described in [SECURITY.md](.github/SECURITY.md).

## License

WorkHarbor is free software under the European Union Public Licence, [EUPL-1.2](LICENSE).

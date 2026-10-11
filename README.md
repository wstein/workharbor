# WorkHarbor

[![Repository checks](https://github.com/wstein/workharbor-nxt/actions/workflows/check.yml/badge.svg)](https://github.com/wstein/workharbor-nxt/actions/workflows/check.yml)
[![Status: bootstrap](https://img.shields.io/badge/status-bootstrap-orange)](#first-implementation)
[![Python: 3.11+](https://img.shields.io/badge/python-3.11%2B-blue)](#start)
[![Go](https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white)](#baseline)
[![Elixir](https://img.shields.io/badge/Elixir-4B275F?logo=elixir&logoColor=white)](#baseline)
[![Phoenix](https://img.shields.io/badge/Phoenix-FD4F00?logo=phoenixframework&logoColor=white)](#baseline)
[![Symphony](https://img.shields.io/badge/Symphony-412991)](https://openai.com/index/open-source-codex-orchestration-symphony/)
[![Harness Engineering](https://img.shields.io/badge/Harness_Engineering-412991)](https://openai.com/index/harness-engineering/)
[![Spec Kit](https://img.shields.io/badge/Spec_Kit-181717?logo=github&logoColor=white)](https://github.com/github/spec-kit)

Self-hosted development workspaces: Docker first, `.devcontainer` native, browser
access, provider-neutral host agents, reproducible Git configuration and tool bundles.

**This repository is a Spec Kit project bootstrap, not a working platform.** Product
implementation remains pending. AI tooling is for development only.

## Start

```sh
git clone workharbor.bundle workharbor
cd workharbor
make check
python3 scripts/select_feature.py 001-workspace-lifecycle
```

Requires Git, Make, Python 3.11+ and Bash. On macOS the built-in Python may need a
newer installation. Open Codex in this repository; generated `$speckit-*` skills
live under `.agents/skills/`. Read [AGENTS.md](AGENTS.md), the
[constitution](.specify/memory/constitution.md) and [documentation index](docs/index.md).

## Baseline
Phoenix/Elixir/LiveView control plane, Go agent, SQLite initial metadata storage.
Single owner and remote Docker host first; Apple container, Podman, Kubernetes and
IDE integrations follow. Workspace configuration lives in Git with pinned project
submodules; Bundledock-style tools remain independent of users' language images.
Stack choices are bootstrap ADRs; resource efficiency requires measurement.

## First implementation
[Feature 001](specs/001-workspace-lifecycle/spec.md) defines enrollment → trusted
Git configuration → provision → browser terminal → stop/restart → recovery.
Its [plan](specs/001-workspace-lifecycle/plan.md) and
[tasks](specs/001-workspace-lifecycle/tasks.md) are pending. Start on
`001-workspace-lifecycle`, run `$speckit-analyze`, then implement focused slices.
Use `$speckit-converge` to assess gaps. Do not recreate an already specified feature
without reviewing its changes.

## Development orchestration
[Harness](docs/development/harness.md) supplies local/CI repository checks.
[Symphony](docs/development/symphony.md) supplies a pinned workflow and operator
activation procedure. It requires separate tracker/runtime configuration; nothing
has been installed on your host or activated. No product dependency on Symphony.

## Provenance and license
[Third-party provenance](third_party/README.md) records the exact Spec Kit snapshot
and its MIT notice. WorkHarbor's own project license awaits an owner decision.
[References](docs/references.md) distinguish inspiration from implementation.

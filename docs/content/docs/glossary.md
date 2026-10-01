---
title: Glossary
description: The words workharbor uses, each with a pointer to the design section that defines it.
weight: 4
---

The words workharbor uses for its objects and modes. Each entry gives the short meaning and points to the [design](design/_index.md) section that defines it; the design wins where the two differ.

## Work

Task
: One piece of work, usually from a forge issue: its instructions, Decisions, progress, results and pull request link. It ends completed, cancelled or failed. States in [§4.1](design/domain.md#41-state-machines).

Topic
: One line of work: one branch, `agent/<topic>`, with its own checkout on the host. Several topics run at once, each with its own environment and agent ([§4.5](design/domain.md#45-topics-checkouts-and-cleanup-before-push)).

Workspace
: The files a task works in: the topic's checkout, its branch, tool configuration and caches. A workspace may outlive several runs ([§4](design/domain.md#4-domain-model)).

Run
: One execution of an agent in an environment. A paused or interrupted run is relaunched from the agent's session; a stopped or failed run is never reused, so a retry or rework starts a new run on the same workspace and topic ([§4.1](design/domain.md#41-state-machines)).

Environment (`env`)
: The container or VM a run executes in. It can be stopped or recreated without losing the workspace, which lives on the host ([§4.4](design/domain.md#44-persistence-semantics), [§5.1](design/architecture.md#51-runtime-adapter)).

Event
: An append-only record of an instruction, observation, Decision or action. Events feed the UI and are the audit trail ([§5.4](design/architecture.md#54-events-idempotency-and-retention)).

## Review and approval

Decision
: A question, approval or review request raised to the developer, open until answered. A Decision that blocks a run pauses it; an approval fails closed on timeout ([§4.2](design/domain.md#42-decision-object)).

ReviewCandidate
: One prepared revision of a topic: the branch, the pinned commit SHA, then the pull request and the CI results for that SHA. It is created when cleanup pins the SHA, before the push ([§4](design/domain.md#4-domain-model), [§4.5](design/domain.md#45-topics-checkouts-and-cleanup-before-push)).

"Ready to push?"
: The review Decision for a ReviewCandidate. Approval is tied to its commit SHA; only then does the supervisor push and open the pull request. Agents never push (D18).

Autonomy table
: The policy that maps each action to `auto`, `ask` or `forbid`, with a floor that keeps merge, tag, release and deploy forbidden for agents ([§6](design/security.md#6-policy-and-autonomy), D36).

## Agents

Full mode
: An agent adapter with headless operation, structured events, mid-run message injection and host-routed approvals ([§5.2](design/architecture.md#52-agent-adapter)).

Degraded mode
: An agent adapter with structured events and headless operation only. A message to the running agent becomes a resumed turn, labelled in the UI, and tools run from a fixed allowlist (`dontAsk`) instead of asking the host ([§5.2](design/architecture.md#52-agent-adapter)).

Auth modes
: How an agent signs in, reported by its adapter and never assumed. `api-key`: the key stays in a host-side proxy and is issued per run. `subscription`: a consumer-plan login kept in a dedicated auth directory per environment, refreshed by the CLI itself ([§5.2](design/architecture.md#52-agent-adapter), [§7.3](design/security.md#7-security)).

## Host and environment

Tool store
: A content-addressed, read-only directory on the host that holds the agent CLIs and the supervisor's helpers, verified against pinned checksums and mounted into every environment. A profile picks a set of versions ([§5.6](design/architecture.md#56-tool-store), D19).

`whr-shim`
: A small launcher in the tool store. It runs agent and command processes in their own process group, so the host can cancel a whole process tree inside the guest (D25).

Egress sidecar
: A container next to each environment that runs `whr-proxy`, the logging allowlist proxy. It is the environment's only way out: hosts by name, ports 443 and 80, public addresses only ([§7](design/security.md#7-security), threat model T5).

Repository cache
: A bare repository per forge repository on the host. Each topic is a shared clone of it, and its objects are mounted read-only into the environment ([§4.5](design/domain.md#45-topics-checkouts-and-cleanup-before-push)).

Owner label
: The `workharbor.owner` label on everything a supervisor instance creates. An adapter lists, starts, stops and deletes only what carries its own owner ([§5.1](design/architecture.md#51-runtime-adapter)).

## Marking claims

The docs mark how firm a claim is: {{< status verified >}} measured on the target setup, {{< status unverified >}} taken from documentation or the original sources, {{< status decided >}} settled in the decision table, {{< status open >}} not decided yet. Measured results are on the [spike pages](spikes/_index.md).

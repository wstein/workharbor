# Product and scope

Status: accepted baseline, implementation pending.

WorkHarbor combines a Portainer-inspired host overview, DevPod-inspired portable
workspaces and Okteto-inspired remote development access. These are inspiration,
not code dependencies or a parity promise. Primary operator: one owner on a
homeserver, approximately 4–8 active workspaces; actual limits depend on workloads.

## MVP
Authenticated local owner; register a remote Docker host; import a Git workspace
configuration; validate `.devcontainer`; provision; access a browser terminal;
stop/restart with persistent files; diagnose/retry failure. Show operation progress,
observed state and actionable errors in a Phoenix LiveView dashboard.

## Sequence
001 lifecycle and terminal → 002 one Bundledock integration → 003 browser editor
and VS Code access → 004 JetBrains connection → 005 Apple container provider.
Podman, Kubernetes, multiple users, OAuth, Forgejo/Gitea/Codeberg/GitHub integration,
CI integration and a broader CLI follow demonstrated demand and adapter tests.

## Non-goals
AI features, agent management as a product, general container administration,
built-in Git hosting, billing, managed SaaS, collaborative editing and arbitrary
multi-tenant execution. A browser editor is planned, not delivered by feature 001.

## Workspace repository
A small Git repository owns `.devcontainer/devcontainer.json`, a platform manifest
and pinned project submodules under `projects/`. It can be stored locally, on an
existing minimal Git server or beside project repositories. Per-user forks are
optional provenance, not an authorization mechanism. MVP uses one config repo per
workspace. Imported repos are never implicitly rewritten or recursively cloned.

## Success criteria
The owner can create, inspect, access, stop and restart a workspace using only the
browser. Disconnected hosts display uncertainty. Restart retains committed and
uncommitted project files. No product path requires a tracker or AI subscription.

## Proposed future option

[ADR 0006](adr/0006-optional-ai-execution.md) proposes a later optional internal
execution board linking Spec Kit artifacts, Symphony scheduling and WorkHarbor
workspaces, resources, interventions and evidence. This is outside the accepted
baseline and the sequence above; AI features remain current non-goals. The
[concept paper](papers/optional-ai-execution.md) describes the owner experience.
Product implementation requires separate explicit owner scope and constitutional
approval, followed by an approved feature spec and plan. Saving this proposal
does not provide that authorization.

---
title: Native Codex supervision
description: "Pinned app-server integration, external native skills and roles, policy isolation and measurement gates."
weight: 6
toc: true
---

## 5.9 Native Codex supervision (D53)

{{< status decided >}} Werner requests full Codex support in both workharbor and
crewbook as P1, with Antigravity afterward. [crewbook #9](https://github.com/wstein/crewbook/issues/9)
records this batch and the D53 reservation. This amends D12's implementation
ordering and D52's minimal text-only loading path for Codex. It does not change
the release boundary, authorize publication or weaken policy. Existing Claude
discovery suppression remains required. Codex support is not implemented or
measured by this decision.

[D54's canonical role contract](native-bindings.md) specifies the versioned
package fields, host projection and effective native evidence. It does not
approve a project-policy authority or establish measured native support.

### Native protocol and capability gate

Use the unmodified, verified, exactly pinned Codex CLI's `app-server` over stdio
inside the managed environment, launched through `whr-shim`. The host runtime
exec channel is the only control transport; expose no app-server listener to
the guest network or host clients. Generate and retain the protocol schema from
that same binary. Reject incompatible versions before launch. Adapter contract
version, native CLI version, artifact digest and schema digest are distinct.

The [official app-server documentation](https://developers.openai.com/codex/app-server)
describes initialization, `thread/start`, `thread/resume`, `turn/start`,
`turn/steer`, `turn/interrupt`, typed items, host approval requests and usage.
It also documents `skills/extraRoots/set`, per-working-directory extra skill
roots, explicit skill input items and model discovery. These are documented
candidate interfaces, not evidence that a particular target binary supports
the required semantics. {{< status unverified >}} Target compatibility,
isolation, subscription access and end-to-end execution await #34/#241.

Correlate requests and events with the connection generation, request ID,
thread, turn and item. Bound message bytes, queues and pending requests; reject
malformed or contradictory required events. A completed process is not proof
of a completed turn: failed, interrupted and declined work retains its status.
Unknown required requests fail closed. Report steering delivery only after
native acceptance, and distinguish acceptance from the model processing it.
Stop cancels pending approvals, interrupts the turn and reaps the process tree
through the runtime. Disconnected control channels stop the agent rather than
leaving an unanswered request or an unsupervised child.

`agent.Capabilities.Mode()` remains the full/degraded/unsupported definition.
Full mode requires measured structured headless operation, steering and
host-routed approvals. Required resume, isolation and selected-role binding
gates also have to pass before a run launches. `exec --json` and `exec resume`
may remain a separately labelled degraded path if they enforce the fixed
allowlist; they are not the full-support implementation. Missing mandatory
controls are errors, not permission to fall back to unrestricted execution.

### External native skills and roles

D52's selected immutable text package and reviewed external pin remain the
only package source. Mount the selected validated directory read-only at its
recorded `/skills/<inventory-sha256>` root, outside the work repository. Native
discovery and invocation must use that exact root on start, resume and child
creation. A `skills/list` result, an injected text section or a literal root
variable alone does not prove actual native invocation.

For native skills, use version-checked external-root registration and explicit
skill input paths into the selected inventory. Measure whether registration
affects discovery and invocation, not merely the list response. Refuse missing,
duplicate or escaping entrypoints. Explicit `none` registers no package skills
or roles and still enforces all platform and project policy. A compatible
alternative needs no crewbook identity. Do not copy skills into the repository
or permit its `.agents/skills` to become a fallback.

The [official custom-agent documentation](https://learn.chatgpt.com/docs/agent-configuration/subagents#custom-agents)
describes native TOML roles and the precedence of explicit model and effort
settings; the [configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference#configtoml)
also documents role `config_file` bindings. Version-check the chosen mechanism
against the target schema and binary. Crewbook supplies declarative role names,
descriptions, instructions and model/effort intent from canonical content;
workharbor validates them and supplies the native binding to inventoried
read-only resources. Do not load arbitrary TOML settings from a package: native
role configuration is a projection of approved fields, not an exception to
D52's ban on package-supplied security configuration, hooks, tools or MCP.
If the binary cannot bind external roles without untrusted discovery or writable
role substitution, report the gap and refuse that mandatory role selection.

Crewbook #9 owns native `cb-dispatch`, `cb-review`, `cb-code` and `cb-delegate`
entrypoints with coordinator versus leaf responsibilities, alongside its other
canonical roles. No Claude-only file or command is a prerequisite for Codex.
Role instructions cannot authorize child spawning outside supervisor admission.
Record child ownership and exact bindings before execution; children inherit
the same enforced ceiling, not the parent's authority to widen it. Cancellation
and restart must account for every owned child. The selected external workflow
and project contribution rules own reviewer eligibility and author/reviewer
separation. workharbor records generic child identity and binding evidence and
enforces admission and capability ceilings without assigning authority to a
workflow name; human approval of the exact artifact remains a separate host gate.

### Trusted instructions, configuration and model binding

Supervisor policy is authoritative, followed by reviewed project instructions
and contribution rules, then package workflow defaults and task text. Read
project policy from a trusted reviewed revision under D38, separately from the
agent-writable checkout; bind its digest and full loaded content. Repository
`AGENTS.override.md`, nested policy, native configuration and the writable agent
home cannot silently replace it. Fail on missing required inputs, truncation,
unexpected instruction sources or conflicting required bindings. Keep native
instruction-source evidence separate from the composed text digest.

The [official skills guide](https://learn.chatgpt.com/docs/build-skills#where-to-save-skills)
documents repository and home discovery; the configuration reference documents
project trust and native settings layers. These defaults make isolation a
measurement requirement. Workharbor supplies supervisor-owned read-only config
and requirements in the environment, with only the selected native resources
enabled. Separate writable vendor auth/session state from immutable instruction,
role and policy configuration; a separate `CODEX_HOME` alone is not enforcement.
Suppress repository/home hooks, plugins, skills, MCP, apps and unapproved tools,
including automatic dependency installation and provider/config overrides.
Apply and verify the same restrictions on child creation and every resume.
If a pinned CLI cannot provide this split and suppression, it is incompatible.

Workflow recommendations belong to crewbook or another selected external skill
set. workharbor treats workflow names as opaque identifiers: it validates the
explicit requested and resolved client/model/effort tuple against operator
approval and generic capability constraints. A workflow name determines no
model grade, child admission, approval authority, concurrency or tool privilege.

Werner previously approved the following selections for this repository's
external development workflow. They record that guidance, not supervisor role
semantics or authorization for a new invocation:

| External development workflow | Requested model | Reasoning effort |
| --- | --- | --- |
| Authors and ordinary workers | sol6.1 | low |
| Independent reviewers and design | sol6.1 | medium |
| Helpers | luna | medium |

Resolve each explicitly requested selection to exact native identifiers with
the pinned CLI;
the official native examples use `gpt-6.1-sol` and `gpt-6-luna`, but examples
and model pages are not a compatibility or entitlement test. Persist requested
and resolved identifiers, explicit effort, role source digest and effective
native settings. No alias invention, default inheritance, automatic upgrade or
silent fallback. Validate supported effort and complete an authorized target
turn before claiming account access; catalog discovery alone is insufficient.
The historical development-workflow substitution changes no other providers'
bindings and grants no permission to edit protected paths.

### Approvals, subscription login and recovery

Use native correlated command/file approval requests with the existing human
Decision and `agent.Approver` path. An allow applies only to that pending request
within platform ceilings. Decline on timeout, cancellation, stale/replayed ID,
wrong thread/turn or unknown permission shape. Do not accept session-wide grants,
exec-policy amendments, permission widening or vendor automatic review as a
substitute for human approval. Exclude native out-of-sandbox shell/process APIs
from the adapter's exposed control surface. Forge merge, tag, release and deploy
remain denied by the forge adapter regardless of native approvals.

D40 is unchanged: the human signs in through the vendor CLI inside the
environment, and the vendor keeps its login on the environment's agent-home
volume. Workharbor never reads, copies, stores, relays or logs that credential.
No subscription token relay, supervisor token file, auth import or externally
managed token login is authorized. Sign-in uses the human's directly attached
terminal, not captured app-server auth messages. #281's landed preparation and
image-trust changes (dd48c4a, f3df0f6) have a clean independent review by Hooke
at `2e1c1c53639c32d37b29a1330541e71ce3c30242`, using Werner's explicitly approved
sol6.1 medium substitution for Opus. The design notification is satisfied;
no separate Claude/Opus review remains required for that scope. #282 still owns
the session-long hold, child and audit work. No live target claim follows from
those source changes.

Persist the thread/session and owned-child IDs together with D52 selection,
native tool/schema/config digests, exact model/effort and reviewed project policy
before executing work. Every restart/resume revalidates those records and sends
the D27 briefing about interrupted or uncertain tool effects and superseded
Decisions. Resume never grants an old pending approval or substitutes current
defaults. Missing content/session or incompatible settings refuse resume.
Auth expiry and exhausted quota pause with a human Decision; no account rotation
or automatic retry. Unknown usage, costs and reset times remain unknown.

### Implementation ownership and evidence

| Existing issue | Owner and file boundary | Gate |
| --- | --- | --- |
| #35 | `wh/runtime`: new `internal/agent/codex/` and focused `agenttest` coverage; shared agent-contract changes coordinated before edits | Pinned schema, bounded events, approvals, steering, cancel/resume and native isolation conformance |
| #164 | `wh/platform`: `internal/toolstore/`, existing pin resources and verifier tests | Exact binary/version/digest, Sigstore identity/issuer and safe extraction; no adapter edits |
| #283 | Serial platform/runtime workers: `internal/skillset/` fixes, then existing `ProjectInstructions`, `SkillBinding`, `PrepareSkills` paths in runtime/service/serve | Validated read-only mount, trusted policy, native loading and recorded selection; no cutover before review |
| #242 | `wh/platform`: selected-package native binding/drift and `internal/doctor/laneagents*` | Exact role/model/effort and truthful diagnostics; no independent role source |
| #34 / #241 | `wh/verify`: spike scripts and evidence on their spike branches, product spike reports | Native target protocol, instruction loading, model access, config/tool isolation and E2E |
| crewbook #9 | Crewbook author: canonical declarative skills/role metadata and ordinary GFM guides, reusing #1–#6 and #8 | Native discovery/invocation separately demonstrated; no executable runtime tools or paused Go8 draft edits |

Compose the real supervisor and CLI/API/UI through the same service methods
after the adapter interface lands. Serialize overlapping service, serve,
doctor and policy files; two code authors by default (no upper cap), with disjoint boundaries.
The P1 native-contract dependency supersedes the old after-#28 delay for these
Codex portions of #241/#242; unrelated Copilot work retains its existing order.
The historical advisory #227 is the routing record, not implementation evidence.
All new tracker items for this batch belong to crewbook; reuse host issues.

Evidence must cover pinned crewbook, a minimal alternative and explicit none;
native root/child discovery and invocation; missing/tampered skills or bindings;
planted repository/home config, hooks, plugins and tools; denied approvals and
channel loss; malformed events; steering, cancellation, child cleanup, restart
and resume after changed defaults; auth/quota and usage; and one issue through
prepared Git handover using the shared CLI/API/UI service policy. Commit harness
and sanitized evidence with exact versions, hashes and model/effort; identify
redaction and never call sanitized output raw. Offline fixtures do not establish
live access. Human sign-in remains inside the target environment. Live runs
require their own operator trigger; no paid evaluation is inferred here.

### Foundation eligibility

#35 is released from Blocked to Todo, P1, `wh/runtime`, by Werner's explicit
instruction. The bounded offline adapter foundation may be claimed and planned
now; source edits require this native specification's landed local-main SHA.
#34 measurements and #164 verification gate production support and full-support
claims, not offline construction against the implemented #20 agent contract.
The adapter-only boundary excludes service/serve integration, tool verification,
doctor, forge policy and crewbook drafts; dispatch owns the author start.

{{< status unverified >}} Native Codex compatibility, package loading, exact
bindings, config isolation and target E2E remain open. This specification does
not clear #283's findings or any protected Ready-to-push gate. #281's clean
scoped independent review is recorded above; live target claims stay unverified.

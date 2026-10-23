# 0015-host-session-and-local-shell: One host multiplexer session, host-local workspace shells

Date: 2026-10-23
Status: Proposed (owner request of 2026-10-23). It needs an independent review CLEAR for its exact head and then explicit owner
approval of that head, evidenced by a tracker or PR link. Nothing is implemented.

## Context

The owner connects from a notebook over SSH and VPN to the WorkHarbor host and attaches to one persistent terminal-multiplexer
session that runs on the host. In that session they want panes of two clearly distinguishable kinds: privileged shells on the host
itself, and shells inside a workspace container. `docker exec` does this today but is long to type and error-prone.

[Feature 006](../../specs/006-whr-cli/spec.md) does not cover this:

- FR-006 forbids a raw host shell and Docker socket access for `shell`, `console`, `ssh` and `agent attach`, because those commands
  reach the workspace through the control plane with single-use grants. That is right for a remote caller.
- FR-048 (proposed, never accepted) makes herdr the default backend of the in-container persistent console. With herdr already
  running on the host, a default herdr in the container nests two multiplexers in one pane.
- [ADR 0012](0012-persistent-workspace-console.md) says each workspace has one persistent console whose lifetime is independent of its
  connections. On the owner's host the host multiplexer session already provides that persistence for panes the owner opens there.
- Only `ls`, `rm`, `stage`, `promote` and `drift` have a local (`--local`) mode today (FR-066 to FR-069); there is no local `shell`.

## Decision

1. **Host multiplexer.** The owner's multiplexer (herdr) runs on the host only. `shell --local` never starts, requires or configures a
   multiplexer inside a workspace container; an explicit `console_mode: herdr` stays supported. Host panes are ordinary host shells opened by the multiplexer; `whr` never opens a host
   shell and has no host shell mode.
2. **Local shell mode.** `whr workspace shell --local <ws>` (short form `whr shell --local <ws>`) opens an interactive login shell
   in a running workspace container of the local Docker daemon, found by the label discovery of FR-066, without the control plane,
   login or grant. It is explicit (`--local` is never an automatic fallback), human-initiated and takes no command.
   [Feature 010](../../specs/010-host-and-workspace-panes/spec.md) specifies it.
3. **Relaxation of FR-006, limited to local mode.** FR-006 protects a remote caller from reaching the host through a workspace
   session. A local-mode caller can already run `docker exec` on this host (docker group member, or owner of a rootless, Docker Desktop or
   OrbStack daemon), so a single-use grant protects nothing there. The label rule of FR-066 is a usability guard, not a security
   boundary. Local mode adds no capability beyond what that caller already has, grants nothing to a
   container, the control plane or the browser, and changes nothing for remote commands. The relaxation applies to `shell --local`
   only; it does not extend to `console`, `ssh` or `agent attach`.
4. **Console default.** The default console mode becomes `plain`. herdr in the container stays selectable and optional, is never
   required, and `whr` gives no nesting guidance. FR-048 is amended accordingly.
5. **`whr console` and local mode.** `whr console <ws>` stays the remote, grant-based reattachable console of ADR 0012. There is no
   `console --local`; the flag is refused with the usage code and a message that names the host multiplexer. ADR 0012's lifetime
   rules apply only to the console.
6. **Optional multiplexer plugin.** A thin herdr plugin may list `whr ls --local --json` and open panes that run
   `whr shell --local <name>`. It is optional, outside the product dependencies and outside `whr`.

## Why a new ADR and not only an amendment of ADR 0012

ADR 0012 is Accepted for the lifetime rules, and its "Proposed backend selection" section is the only part that concerns backends.
The new decision touches Feature 006, the trust boundary of FR-006 and the command table, so it needs its own status and review.
ADR 0012 gets a short pointer: its proposed backend selection is superseded by this ADR once this ADR is accepted, and its accepted
lifetime rules are untouched.

## Consequences

- Feature 006 FR-006, FR-007, FR-007a, FR-019, FR-022a, FR-048, FR-058, FR-066, US0 scenarios 1 and 7, SC-015 and the assumptions are
  amended in the same change; the manifest default `console_mode` moves from `herdr` to `plain`. `contracts/workspace/` (schema and
  vectors), `contracts/cli/` (command table), the `whr` code and the wizard default follow through their owner-gated procedures;
  until they land, those artifacts still say `herdr`. That is a recorded inconsistency, not an approval.
- ADR 0014 is unchanged except that `shell` gains the `--local` flag through the table procedure of FR-052.
- Constitution principle IV is respected: no Docker socket is exposed to the browser, the control plane or a container; `whr` is a
  client of the socket its caller already can use.
- The security reasoning is bounded to the single-owner, own-host model of the constitution. It is no multi-tenant claim.

# SC-009 checkpoint A: owner runbook and result template

**UNVERIFIED — prepared, never executed by the agent.** Plugin absent.
Use the authoritative [checkpoint A procedure](../../../specs/010-host-and-workspace-panes/quickstart.md#checkpoint-a--after-p2-plugin-absent).
[Offline P2 evidence](p2.md) verifies fake Docker/PTY boundaries only.
Checkpoint B, real plugin identity and optional OSC 11 tint remain pending P4.

## Preconditions and evidence header

Owner runs this after accepting/merging P2 and separately authorizing host use.
Use an already-running disposable labelled fixture with no credentials/history.
This procedure authorizes no install, daemon start/stop, socket permission change,
provisioning, deletion or plugin activation. If a prerequisite is absent, record
NOT TESTED and let the owner decide the separate operation.

- Date / operator / host-run authorization reference: UNRECORDED
- Product full source SHA / binary provenance: UNRECORDED
- Host OS / architecture; Docker client and server versions: UNRECORDED
- Herdr version / existing session identity: UNRECORDED
- Notebook OS / terminal version / font: UNRECORDED
- Plugin: ABSENT; tint: DISABLED; manual titles and tab labels: REQUIRED
- Sanitized fixture full ID / name / expected shell / user / mount directory: UNRECORDED
- Owner acceptance reference and exact accepted source revision: UNRECORDED

Record controlled fixture assertions and sanitized versions only. Do not dump
session contents, environments, provider settings or Docker credential/context data.

## Steps and results

Attach through normal SSH/VPN to the existing host herdr session. Keep an ordinary
host pane and open two workspace panes using `whr shell --local <container-name>`.
Set BOTH pane title and tab label manually: `🟥 HOST`, plus distinct workspace
glyphs and full `ws:<name>` labels. Use the existing herdr UI; no plugin is needed.

| Check | Expected observation | Actual / evidence / verdict |
| --- | --- | --- |
| Shell/user/directory | Controlled fixture matches configured selection | UNVERIFIED |
| Resize | Container terminal observes new dimensions; shell remains usable | UNVERIFIED |
| Clear/rendering | HOST and workspace identities persist in pane title AND tab label, readable on the notebook font | UNVERIFIED |
| ctrl-p alone | Controlled raw key-display fixture receives byte 0x10 | UNVERIFIED |
| ctrl-q alone | Receives byte 0x11, no Docker default detach | UNVERIFIED |
| ctrl-space alone | Receives byte 0x00 | UNVERIFIED |
| ctrl-\ alone | Receives byte 0x1c rather than being swallowed | UNVERIFIED |
| ctrl-] alone | Receives byte 0x1d | UNVERIFIED |
| ctrl-] ctrl-^ | Docker detaches; an already-authorized observer confirms shell survives | UNVERIFIED |
| Exit 7 | Returns status 7 to the SAME ordinary host-parent prompt | UNVERIFIED |
| Clean exit / controlled signal status | Parent observes expected status; record actual signal convention | UNVERIFIED |
| SSH reconnect | Same herdr session, pane, directory, harmless shell variable, controlled running command and title survive | UNVERIFIED |
| Lifetime caveat | Owner acknowledges pane close/client loss does not guarantee container-process termination | UNVERIFIED |

Use a controlled raw key-display fixture, restoring terminal settings afterward;
shell editing behavior alone is insufficient evidence of delivered bytes. The
fake PTY checks are not a replacement for this actual Docker observation. If no
approved raw-key fixture/observer is available, record NOT TESTED rather than
installing tools or creating another exec implicitly.

For status checks, open a separate ordinary HOST shell pane through herdr. At its
prompt run `whr shell --local <container-name>` as a foreground child, then
`exit 7` in the container. Immediately run `echo $?` in bash/zsh or `echo $status`
in fish at the SAME host prompt. Do not use `exec whr`, a plugin entrypoint or a
pane whose top-level process is whr; those destroy the required parent prompt.
Leave reconnect fixture panes intact.

For SSH persistence, choose a known directory in a surviving workspace pane, set
a harmless fixture-local shell variable, and start a recognizable controlled
long-running command. Drop only notebook SSH, keeping host herdr running.
Reconnect to the SAME session and verify all listed observations. Stop only the
fixture command normally. The owner separately manages detached fixture shells.
Never stop the owner's daemon to manufacture a transport-loss test.

## Owner disposition

Base checkpoint A: **UNVERIFIED**. Failures / not-tested cases: UNRECORDED.
Owner acceptance: PENDING. Full SC-009 including checkpoint B: PENDING.
No observed failure authorizes changing settled decisions; report it for owner
disposition. Acceptance requires the exact tested revisions and actual results.

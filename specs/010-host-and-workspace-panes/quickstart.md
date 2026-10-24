# Validation guide: Host and workspace panes

Status: acceptance procedure, **not an execution record**. P1 discovery is merged in
[PR #220](https://github.com/wstein/workharbor/pull/220); P2 shell implementation and offline evidence are
[recorded separately](../../docs/quality/010-host-and-workspace-panes/p2.md). P3 configuration defaults are [implemented separately](../../docs/quality/010-host-and-workspace-panes/p3.md);
remote console session creation remains pending. The optional P4 plugin and its standalone
checks are [documented separately](../../integrations/herdr/README.md); SC-009 remains UNVERIFIED.
Requirements and settled answers are in [spec](spec.md); package order is in [plan](plan.md).
The [checkpoint A result template](../../docs/quality/010-host-and-workspace-panes/sc009-base.md) remains UNVERIFIED.

## Offline package validation

On the implementation branch, use `.tool-versions`, a clean isolated checkout and the existing Go test/PTY harness. Write failing
behavior cases first, implement the package, and retain actual command/output and exact source revision in its PR evidence.
The P1–P4 packages are merged; the following commands validate their offline scope:

```sh
python3 scripts/select_feature.py 010-host-and-workspace-panes
python3 -I -B scripts/check_repo.py
git diff --check
cd apps/whr
gofmt -l internal/localdocker internal/localshell internal/commands internal/scaffold internal/wizard
go test ./internal/localdocker ./internal/localshell ./internal/commands ./internal/scaffold ./internal/wizard ./internal/ptytest ./cmd/whr
```

For P1, omit not-yet-created `localshell`; check the command/schema/vector tests for that package's changed paths as well.
All Docker calls in offline tests go to explicit fake sockets/clients; no real socket, daemon, container, herdr session, token or network.
PTY cases must execute on supported native Linux/macOS and report skips honestly; fixture subprocesses use a temporary HOME/env.
Run `make check` from repository root under applicable authorization and record raw/qualified results separately per repository policy.
Standalone builds and real-host operation require their own owner authorization; planning does not authorize them.

| Package | Essential expected outcomes |
| --- | --- |
| P1 | Labelled running/stopped fixtures listed; role/label filter correct; foreign excluded; full required metadata; schema-valid JSON; fixed Unix endpoint; total timeout ≤5 s; zero mutation. |
| P2 | Exact argv; missing mount uses validated configured directory with an escaped pre-handover warning under merged FR-010/OQ-11 and SC-011; redirected stderr/no-fallback/invalid-fallback cases; invalid usage before Docker; no match 65, ambiguity 64, non-running 68; no token canaries in child env/argv/output; raw child bytes unchanged; statuses 0/1/7/126/127/137/255 pass with reachable daemon; lost daemon/deadline 72; no duplicate exec. |
| P3 | Omitted field on old/new manifest defaults plain; explicit herdr retained; wizard/scaffold/schema vectors agree; live console untouched; no local console enabled. |
| P4 | Separate fake-whr/herdr script tests; invalid names/cancel open nothing; deterministic marker/title/tab label; background-off zero OSC 11 and enabled exactly one before exec; SC-010 numeric constraints verified. |

P4 provides the standalone entrypoint `python3 -B integrations/herdr/check.py` for script/manifest/hash/palette tests.
It is separate from required repository CI and `make check`; actual results are recorded in the P4 evidence.

## SC-009 real-host owner gate

**Owner-operated or separately owner-authorized. Initially UNVERIFIED.** Offline tests and PR CI cannot close this gate.
Use a disposable labelled workspace fixture with no credentials/session history to capture. Confirm Docker/herdr are already
installed; do not install, start services, alter socket permissions or provision/delete containers as part of this runbook.

### Evidence header

Create a follow-up record under `docs/quality/010-host-and-workspace-panes/` with:

- Exact `whr` product SHA and plugin SHA (or plugin absent), date, operator and owner authorization/acceptance reference.
- Host OS/architecture, Docker client/server versions, herdr version; notebook OS, terminal version and font.
- Sanitized fixture container identity and expected shell/user/directory; manual-title or plugin mode; background enabled/disabled.
- A step table with expected result, observed result, pass/fail/not-tested and a controlled evidence reference.

Record only controlled fixture results and sanitized version information. Do not record session contents, environment dumps, tokens,
provider settings or credential-bearing Docker/context output. The application itself still logs no session content.

### Checkpoint A — after P2, plugin absent

1. Connect from the notebook through the normal SSH/VPN path; attach to the existing host herdr session.
   Keep one host pane and open two workspace panes using `whr shell --local <container-name>`.
   Set pane titles and tab labels manually with the accepted glyph/text policy; keep prompt configuration unchanged.
2. Confirm configured user, shell and workspace directory with controlled fixture assertions. Resize the terminal and confirm it
   remains usable. Clear the screen: persistent labels must still identify HOST and each workspace.
3. Use a controlled key-display fixture to verify each of **ctrl-p, ctrl-q, ctrl-space, ctrl-\ and ctrl-] alone** reaches the
   container process. Avoid interpreting a shell's editing action as proof that a byte arrived. Then verify the fixed sequence
   **ctrl-] ctrl-^** detaches, the exec shell remains running, and no alternative default Docker detach sequence steals ctrl-p/q.
   Use an already-authorized observer for process survival; `whr` offers no session reattach or cleanup command.
4. Open a separate ordinary HOST shell pane through herdr, leaving the reconnect fixture panes intact. At that host prompt run
   `whr shell --local <container-name>` as a foreground child; do not use `exec whr`, a plugin entrypoint or a pane whose top-level
   process is `whr`. Inside that workspace shell run `exit 7`; the command then returns to the SAME host prompt. Immediately inspect
   its status (`echo $?` in bash/zsh, `echo $status` in fish); it must be 7. Also record a clean exit and any controlled signal-status
   check in this host-parent setup. A daemon-loss probe, if desired, needs separate authorization; never stop the owner's daemon for
   this guide.
5. In a surviving workspace pane choose a known directory, set a harmless fixture-local shell variable and start a recognizable
   controlled long-running command. Drop only the notebook SSH connection, leaving host herdr running.
6. Reconnect over SSH and attach to the SAME herdr session. Verify the same pane, working directory, shell variable, running command
   and pane title remain. Stop only the fixture command normally. This verifies SSH persistence, not cleanup after pane closure.
7. Record that closing a pane/abnormal client loss is not guaranteed to kill its container process (FR-007); do not mark a stronger
   cleanup promise verified. The owner manages any explicitly detached fixture process separately.

Checkpoint A alone establishes only the base subset of SC-009. Record plugin-dependent steps as pending, not passing.

### Checkpoint B — after P4, separately installed/enabled by owner

1. Run the independent P4 checks (including SC-010), then use the picker to open the same controlled workspace in a tab and in a split.
   Repeat A's key, exit and SSH-reconnection checks using the plugin-created panes.
2. Verify workspace title AND tab label carry the same deterministic glyph and full `ws:<name>` identity; host title/label use
   `🟥 HOST`. Verify both survive `clear`; record what happens when a program sets its own title, without treating labels as trusted.
3. Check emoji rendering/readability on the owner's actual notebook font, over SSH, including narrow tab labels. Include multiple
   workspaces, including a hash collision, to show full text still distinguishes them.
4. With optional tint explicitly enabled, verify only the OPENED PANE changes background: the neighboring pane, tab and outer
   terminal must not change. Repeat with tint disabled: markers remain, no tint sequence is emitted.
5. Exercise a split sharing a tab: record the actual tab-wide label behavior and whether the pane titles still identify all panes.
   Do not claim that a single tab label is independently per-pane. Any failure of the spec's identity outcome is recorded for owner
   disposition before acceptance; it does not authorize quietly changing the decided marker policy.

### Gate result and handoff

SC-009 closes only after the owner accepts all applicable A/B observations at the exact tested source revisions. Record failures,
missing capabilities and not-tested cases explicitly. Without plugin evidence only A may close; full feature/plugin live verification
remains pending. Without testing optional tint, background behavior remains unverified even if other plugin observations pass.
An owner-approved text correction or SC-010-triggered palette change must update authoritative artifacts and refresh affected evidence.

An independently reviewed, green P1–P3 PR may be ready within its own accepted scope while P4 or real-host evidence remains pending;
that does not close SC-009, complete the whole feature, authorize merge, or authorize runtime/deployment.

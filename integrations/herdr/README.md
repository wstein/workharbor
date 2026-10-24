# Optional WorkHarbor local-pane plugin

This integration is independent of `whr`. It starts no containers, agents or
services, holds no credentials, and is never required by repository checks.
Requires Python 3.11+ (standard library, including curses), Herdr 0.9.3+ and the
reviewed P1–P3 `whr` on PATH. Linux and macOS only.

## Verify first

```sh
python3 -B integrations/herdr/check.py
```

The standalone suite uses fake whr/Herdr processes and native PTYs. Its palette
verifier checks SC-010 offline. This does not establish actual terminal rendering,
SSH persistence or installation readiness on the owner's host.

## Owner-only installation and operation

After the owner has merged the reviewed P4 and authorized installation, the OWNER
can link the absolute plugin directory on the Docker host:

```sh
herdr plugin link /absolute/path/to/workharbor/integrations/herdr
herdr plugin action invoke workharbor.local.pick-tab
```

No installation or enabling command is run by the checks or by this development
change. Follow Herdr's enabled-state controls for the owner's installation.
Invoke actions from an existing host pane. Available action suffixes:

| Action | Effect |
| --- | --- |
| `pick-tab` | Overlay picker, workspace opens in a new tab |
| `pick-split` | Overlay picker, workspace splits the original host pane |
| `pick-tab-tinted` / `pick-split-tinted` | Same, explicitly enabling pane background |
| `host-tab` | Ordinary host login shell in a new tab |
| `host-tab-tinted` | Same, explicitly enabling the host background |

Prefix each suffix with `workharbor.local.` when invoking it. In the picker use
arrow keys or j/k, Enter to select, Escape/q/Ctrl-C to cancel. It displays names
only, from `whr ls --local --json`. Only exactly `running` records with valid
lowercase 1–64-character names are offered; uppercase/underscore/dot names are
intentionally excluded. It reads no other workspace fields, adds no CLI state
filter and never queries Docker independently. A stale selection remains subject
to `whr shell --local` refusal; the plugin does not retry or start the workspace.

The selected name reaches the workspace entrypoint only in
`--env WHR_PANE_WORKSPACE=<name>`. The entrypoint validates it again and execs
exactly `whr shell --local <name>`; no command string is evaluated. Herdr-provided
pane/tab IDs identify the newly opened pane; a separate original target preserves
split placement when the transient overlay closes. Metadata failure stops launch.
Host actions exec the owner's absolute `SHELL` with `-l`, default `/bin/sh`, in
Herdr's plugin working directory. This is outside `whr`; it adds no host mode.

## Identity and optional background

Host title AND tab label: `🟥 HOST`. Workspace title AND tab label:
`<glyph> ws:<name>`. FNV-1a 32-bit over name bytes modulo five selects the fixed
`🔷 🟠 🟣 🟡 ⬜` glyph/tint pair. Collisions are expected; full name text identifies
the workspace. Text is the main cue; the marker is cosmetic and makes no
accessibility or security claim. A program may change its title/background.

Background is off by default. Tinted actions emit exactly one OSC 11 in the
opened pane before exec; `whr` emits no plugin sequence. Full-screen programs or
other colour tools may replace it. No reset is needed when that pane closes.
Herdr tabs have shared labels: opening a split renames the shared tab, so its
latest label does not identify every pane. Individual pane titles remain distinct.
Real shared-tab behavior, title persistence after clear, narrow emoji rendering,
pane-only tint, keys/detach and SSH reconnect remain UNVERIFIED until the owner
runs [SC-009 checkpoint B](../../specs/010-host-and-workspace-panes/quickstart.md).

The private entrypoint environment accepts `WHR_PANE_BACKGROUND=0|1`,
`WHR_PICKER_PLACEMENT=tab|split` and `WHR_PANE_TARGET=<original-pane-id>`;
a split picker requires the original target. Prefer the actions to manually
opening entrypoints. No plugin state file, provider token or agent command exists.

## Troubleshooting disappearing panes

A failed picker or workspace pane can close immediately with status 68; its
stderr line may disappear with it. Action commands are recorded in Herdr's plugin
command log:

```sh
herdr plugin log list --plugin workharbor.local --limit 20
```

Pane entrypoints are not action commands and may leave only a pane-exit event in
the server log. Run `herdr --help` to locate the active log directory, then inspect
its `herdr-server.log` for the pane ID and exit status. Do not expect that event to
contain the full stderr diagnostic. To keep a picker error visible, the owner can
run `python3 -B /absolute/plugin/path/picker.py` as a child of an existing host
shell with the current Herdr context; its error returns to that same prompt.
This is a diagnostic alternative to the transient overlay, not an install hook.

Placement is significant in Herdr 0.9.3: overlays target the active pane without
workspace/target flags; tabs accept workspace only; splits accept the original
target pane only. The corrected fake rejects incompatible combinations using the
same errors independently reproduced in the linked Opus review. Installation and
real owner-host behavior remain separate SC-009 gates.

## Contract and numeric sources

[Feature 010](../../specs/010-host-and-workspace-panes/spec.md),
[package evidence](../../docs/quality/010-host-and-workspace-panes/p4.md),
[Herdr plugin commands/environment](https://herdr.dev/docs/plugins/#commands-and-environment).
The manifest uses argv commands in the plugin working directory, native pane
entrypoints and Herdr-supplied context. Numeric references are documented in
`check_palette.py`: WCAG/W3C conversions, Machado 2009 severity-1 linear-sRGB
simulation matrices, and Sharma/Wu/Dalal CIEDE2000 equations/reference pairs.
Simulations are approximations; five tints and terminal/font limits remain.

# Local CLI and plugin contract design

Status: implementation design for [plan](../plan.md), not an edit or replacement of frozen
[`contracts/cli/commands.json`](../../../contracts/cli/commands.json). P1/P2 amendments require the established 006 FR-051/052
review procedure and mirror/freeze tests. Canonical requirements remain [spec](../spec.md) and 006 FR-066.

## P1: `workspace ls --local`

Canonical `whr workspace ls --local`; short form `whr ls --local`. Optional `--role <primary|staging|temp>`, `--label <key[=value]>`
and `--json` follow FR-066. Plain remote `ls` stays unavailable until its separate owner API exists.
Discovery returns all recognized matching containers, including stopped ones, within a total 5-second wall-clock budget.
The P1 output-schema amendment defines `localWorkspaceListResult` using the existing CLI envelope and all FR-066 fields:
`id`, `name`, `role`, `image`, `state`, `status`, `workspace_folder`, `labels`, `home_volume`, `protected_volumes`.
Freeze schema and fixture examples in that amendment before the plugin consumes them; this document does not invent a second JSON
wire shape. Human output escapes terminal controls; candidate display shortens ID to 12 characters without changing its internal ID.

Amended in part by Feature 009 owner decisions D-12, D-13, D-16 and D-18 (spec FR-002): the endpoint order becomes option `--container-endpoint`, config value, `DOCKER_HOST`, `/var/run/docker.sock`, with no candidate search and no context read in local mode. The merged P1 code still implements the previous order until T032 to T034; this text is updated by T034.

Socket admission reads only `DOCKER_HOST` and documented doctor candidates. No Docker context lookup or remote scheme is contacted.
Explicit `--host=<endpoint>` applies to every Docker call. Docker listing/inspection is a read-only adapter concern; its bounded
argv/metadata tests belong to P1 and its discovery work is separate from P2's post-selection inspect/exec call counts.
No `rm`, lifecycle promotion, volume deletion or drift command is activated by P1.

## P2: `workspace shell --local <ws>`

Canonical and short forms share one handler. Exactly one selector; no trailing command, `--json` or non-terminal stdin/stdout.
Reject those usage cases with 64 before contacting Docker. `console --local` returns 64 with shell/host-session guidance;
`ssh` and `agent attach` gain no local mode. Without `--local`, existing remote shell behavior remains unchanged.

After shared discovery, prefer exact container name; otherwise require a unique 12–64-hex ID prefix. Match before checking running
state. Preflight returns 65 for no labelled match, 64 for ambiguity, 68 for non-running state, missing Docker client,
unusable socket/scheme, failed validation or inspect; interrupt before handover returns 72; unclassified failure 74.
Names and values in diagnostics use the existing FR-022 escaping; never print endpoint credentials or environment values.

Manifest resolution reuses `apps/whr/internal/workspacecfg`: its no-follow reader, `MaxManifestBytes`, `CheckManifest` and validated
`Manifest.HumanShell` cover the existing size/JSON/shell rules. Since the file-reading helper is private, P2 must expose a narrow
manifest-only API or extract the shared helper, retaining its safety tests; do not write another parser or run full directory/Git
validation on shell startup. Missing `human_shell` retains the fish default; a rejected value is not silently defaulted.

FR-004 fixed vectors (each displayed token is one argv element except the annotated format expression):

```text
docker --host=<endpoint> container inspect --format {{json .}} <full-id>
docker --host=<endpoint> exec --interactive --tty --detach-keys=ctrl-],ctrl-^ --user=<user> --workdir=<dir> --env=TERM=<term> --env=WHR_LOCAL_WORKSPACE=<name> --env=WHR_LOCAL_MODE=1 <full-id> <shell> -l
```

`{{json .}}` is ONE argument; no shell evaluation. On nonzero client status only:

```text
docker --host=<endpoint> container inspect --format {{.State.Running}} <full-id>
```

That liveness inspect has its own 5-second deadline. Daemon reachability, not the boolean running value, determines whether client
status passes through. Test daemon-reachable container-not-found explicitly. Connection failure/deadline maps to 72 with a `whr:`
line; otherwise preserve status including 1, 126, 127, 137 and 255. No extra availability exec is allowed (OQ-6).
The local handler waits on the child; terminal descriptors go directly to Docker. Resize/signal behavior must be tested at the process
boundary and under SC-009, without a `whr` stream relay. Preserve the FR-010 start notice exactly, only on terminal stderr.

Working directory: choose the workspace-folder mount destination matching the host folder; if none is determinable, use the container's
configured working directory and retain a fallback reason for its required FR-005 warning. Validate the chosen directory before exec;
an invalid fallback is a 68 refusal, not a successful fallback. Escape the fallback path/reason in every diagnostic under FR-022.

**Merged requirement (OQ-11, SC-011):** [PR #216](https://github.com/wstein/workharbor/pull/216) has reconciled FR-005/FR-010.
Keep the byte-exact TTY-only notice. Only on a valid configured-working-directory fallback, write one additional stderr line after
the notice and before the child starts, even when stderr is redirected:

```text
whr: warning: no workspace folder mount in <name>; starting in <dir>
```

Escape the substituted name/directory per FR-022. A matched mount produces no warning. An invalid fallback (non-absolute, over 4096
bytes, NUL or newline) exits 68 naming the escaped value, starts no exec and emits no warning. No notice/fallback warning is deferred
into the shell stream after handover. No new warning amendment is needed.
P2's `TestLocalShellWorkingDirFallbackWarnsBeforeHandover` asserts exact text, notice → warning → child ordering, configured workdir,
redirected-stderr warning without notice, matched-mount silence, invalid-fallback/no-exec/no-warning, escaping and unchanged child bytes.

## P3: default preference

Missing `console_mode` resolves to `plain` at new-session creation, including existing manifests. Explicit `plain` or `herdr` is
retained; no live console migrates. Schema/vectors/scaffold/wizard/output documentation must agree after the gated P3 changes.
No additional CLI flag, local console or nested-multiplexer detection is added.

## P4: plugin boundary

The optional `integrations/herdr/` manifest provides an overlay picker and tab/split workspace pane entrypoint. It reads
`whr ls --local --json`, validates candidate names against 006 FR-058, and passes the selected name as
validated data through `WHR_PANE_WORKSPACE`/fixed argv, never a shell-evaluated command string. It reads no credentials or manifest
aliases. Cancellation opens no pane; stale or non-running selection is safely refused by P2.

P1 returns all states and has no running-state filter flag. The required FR-018/US3 amendment is now owner-approved and merged in
[PR #217](https://github.com/wstein/workharbor/pull/217), commit `82136e51c82ccf62ceaf7491667e23f59035847a` (OQ-12).
The picker reads only each result's `name` and `state`, uses state solely to decide eligibility, offers only exact `running` records
and displays the name only. It adds no CLI filter and reads no labels/provider metadata or independent Docker discovery.
P2 refuses stale/non-running selections with 68. The amendment gate is fulfilled; do not request it again or substitute an all-state
picker. Normal P4 artifact and prior-package merge gates still apply. Fake picker tests cover running, stopped, paused, restarting,
unknown/missing state and the name-only display without changing the settled name validation.

The workspace entrypoint ultimately replaces itself with `whr shell --local <name>`, allowing the parent `whr` process to supervise
its own Docker child. Fixed FNV-1a identity sets both pane title and tab label. Host configuration/action sets `🟥 HOST`; `whr` never
creates a host shell. Shared-tab label behavior must follow herdr's actual tab scope and be included in SC-009 evidence.

Optional background writes exactly one OSC 11 sequence for the selected fixed tint BEFORE exec; default background-off writes none.
`whr` emits no plugin escape sequence. P4's independent verifier checks FNV-1a cases, glyph exclusion, contrast ≥4.5 against `#c0c0c0`,
pairwise CIEDE2000 ≥8 normal and ≥6 under each Machado 2009 severity-1 simulation in linear sRGB. Keep palette constants unchanged
unless that SC-010 verifier fails. No plugin tests/install/runtime become a requirement of `make check`.

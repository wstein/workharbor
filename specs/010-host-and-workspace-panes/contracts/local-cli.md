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

## P3: default preference

Missing `console_mode` resolves to `plain` at new-session creation, including existing manifests. Explicit `plain` or `herdr` is
retained; no live console migrates. Schema/vectors/scaffold/wizard/output documentation must agree after the gated P3 changes.
No additional CLI flag, local console or nested-multiplexer detection is added.

## P4: plugin boundary

The optional `integrations/herdr/` manifest provides an overlay picker and tab/split workspace pane entrypoint. It reads
`whr ls --local --json`, offers only running candidates whose name validates against 006 FR-058, and passes the selected name as
validated data through `WHR_PANE_WORKSPACE`/fixed argv, never a shell-evaluated command string. It reads no credentials or manifest
aliases. Cancellation opens no pane; stale selection is safely refused by P2. Establish the running-only list view from P1's public
state/filter interface; the plugin reads no labels or provider metadata and implements no independent Docker discovery.

The workspace entrypoint ultimately replaces itself with `whr shell --local <name>`, allowing the parent `whr` process to supervise
its own Docker child. Fixed FNV-1a identity sets both pane title and tab label. Host configuration/action sets `🟥 HOST`; `whr` never
creates a host shell. Shared-tab label behavior must follow herdr's actual tab scope and be included in SC-009 evidence.

Optional background writes exactly one OSC 11 sequence for the selected fixed tint BEFORE exec; default background-off writes none.
`whr` emits no plugin escape sequence. P4's independent verifier checks FNV-1a cases, glyph exclusion, contrast ≥4.5 against `#c0c0c0`,
pairwise CIEDE2000 ≥8 normal and ≥6 under each Machado 2009 severity-1 simulation in linear sRGB. Keep palette constants unchanged
unless that SC-010 verifier fails. No plugin tests/install/runtime become a requirement of `make check`.

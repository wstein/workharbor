# Container runtime prerequisite

WorkHarbor needs an existing Docker-compatible runtime on the host. On Linux `whr`
never installs, configures or updates a container runtime. On macOS the host setup
wizard (`whr setup`, [Feature 009](../specs/009-host-setup-wizard/spec.md)) and
`whr doctor --fix` may install OrbStack through Homebrew when the owner selected it; `whr` never starts,
configures or signs in to the runtime application and never changes its Linux VM.
Plain `whr doctor` only looks for a socket that is already there and links to this page
when it finds none.

Status: guidance for the first `whr` increment ([Feature 006](../specs/006-whr-cli/spec.md),
FR-060). Nothing here is a verified platform claim; the verified host and client
matrices are published per release (FR-026).

- macOS: the owner's recommended runtime is [OrbStack](https://orbstack.dev/). Other
  Docker-compatible runtimes that expose a Docker socket may work but are not recommended
  or verified.
- Linux: Docker Engine, installed with the distribution's or Docker's own instructions.
  Membership of the `docker` group is host-privileged: it grants root-equivalent access
  and does not make a workspace a hostile-workload isolation boundary.
- Windows: unsupported until credential ACL and terminal evidence exist (plan).

After installing, run `whr doctor`. A passing `container.socket` check means a socket
accepted a connection; it does not mean the runtime version, storage or Dev Container
features are admitted by the WorkHarbor provider.

## Local workspace discovery

`whr ls --local` (also `whr workspace ls --local` or `whr ws ls --local`)
uses the same Unix socket selection as doctor: an explicit `DOCKER_HOST` wins;
otherwise the documented doctor candidates are tried in order. Non-Unix endpoints
are refused with exit 68; Docker contexts are not consulted. Socket selection and
Docker reads share one five-second budget (deadline exit 71, interruption 72).
Nothing is installed, started, stopped or removed.

The list includes running and stopped containers bearing `anchorage-staging`,
`anchorage-temp`, `devcontainer.local_folder`, `devcontainer.config_file` or the
existing provider label `workharbor.workspace`. Labels are a usability guard, not
a security boundary. Staging takes precedence over temp when both labels exist;
the remaining recognized containers have role primary.

Use `--role primary|staging|temp` and/or `--label key[=value]` to narrow the list.
A label without `=` tests presence; `key=` tests an empty value. Filtering never
admits foreign containers. There is no running-state filter.

`--json` returns `result.workspaces` under the canonical command `workspace.ls`;
the [output schema](../contracts/cli/output.schema.json) defines every field.
JSON preserves full container IDs; human output shows 12 characters and escapes
terminal control/bidirectional characters. Home and protected volumes are metadata
only. The host workspace folder comes from `devcontainer.local_folder`, otherwise
a unique bind source at `/workspace`, `/workspaces` or `/workspaces/...`; it is
empty when no unambiguous folder is identified. Docker child processes receive a
minimal environment and a fresh empty temporary config directory, preventing
host credentials, contexts and credential helpers from being loaded. Responses
larger than 4 MiB are refused rather than truncated.

Without `--local`, listing still refuses with exit 64 because the remote owner API
is pending. Local discovery does not implement removal or volume cleanup.

## Local workspace shell

From an existing host shell or host multiplexer pane, run
`whr shell --local <container-name>` (also `whr workspace shell --local` or
`whr ws shell --local`). An exact full name wins; otherwise use a unique
12–64-character container ID prefix. Matching includes stopped containers,
which are refused with exit 68. Ambiguous selectors exit 64; absent/foreign
containers exit 65. Labels are a usability guard for callers who already have
host Docker access, as explained in [ADR 0015](adr/0015-host-session-and-local-shell.md).

Both stdin and stdout must be terminals. JSON, trailing commands and `--shell`
are refused before Docker runs. `console --local` exits 64 and directs the caller
to this shell command inside the host multiplexer session. Remote shell access
remains unavailable; this local path needs no login, grant or control plane.

Preflight shares a five-second budget and pins one Unix endpoint and full
container ID through inspect, exec and any post-exit check. Operational preflight
failures exit 68; interruption exits 72. The host manifest is read through the
existing no-symlink, 64-KiB workspace reader. Its `human_shell` selects fish, bash,
zsh or nu; an absent manifest or shell preference uses fish, as does an absent
workspace-folder label. A label naming a missing host directory is intentionally
refused with exit 68: an unreadable/nonexistent directory cannot establish that
the manifest file is absent. Other manifest read/validation failures also refuse
entry. The container user comes from
`devcontainer.metadata` remoteUser, configured user, then captain. The working
directory is the container mount destination corresponding to the host workspace
folder, otherwise the validated configured container working directory.

On a terminal stderr the command prints its local-access/detach notice. A
working-directory fallback also prints, including with redirected stderr:

```text
whr: warning: no workspace folder mount in <name>; starting in <dir>
```

Names and directories in these messages are escaped. Invalid metadata is refused
before exec. Docker receives the original terminal descriptors: raw output is
unfiltered and unrecorded by whr. Only TERM and the optional prompt inputs
WHR_LOCAL_WORKSPACE/WHR_LOCAL_MODE are added to the container environment; no
prompt configuration is changed. Docker client credentials/settings are excluded
by the shared minimal environment and empty temporary config.

Use `ctrl-] ctrl-^` to detach; the container shell survives. Closing a pane or
losing the client does not guarantee process termination. Normal child exit
status passes through; a nonzero status triggers one bounded daemon-reachability
inspect. A reachable daemon preserves that status even if the container is gone;
unreachable/timeout exits 72 with a whr diagnostic.

For persistent visual identity, set BOTH pane title and tab label manually:
`🟥 HOST` for the host, and a distinct workspace glyph plus `ws:<name>` for each
workspace. No multiplexer is started by whr. Real Docker key/detach behavior,
resize, rendering and SSH persistence require the
[SC-009 owner checkpoint A](quality/010-host-and-workspace-panes/sc009-base.md);
offline tests do not establish those outcomes. Console defaults and the optional
Herdr plugin belong to later packages.

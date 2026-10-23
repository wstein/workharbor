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
is pending. Local shell entry is the next Feature 010 package; this discovery
slice does not implement removal, volume cleanup or the owner-host pane gates.

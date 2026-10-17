# Container runtime prerequisite

WorkHarbor needs an existing Docker-compatible runtime on the host. `whr` never
installs, configures or updates a container runtime; `whr doctor` only looks for a
socket that is already there and links to this page when it finds none.

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

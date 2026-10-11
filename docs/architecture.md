# Architecture

Status: target architecture; no application binaries exist yet.

## Components
Phoenix/Elixir control plane with LiveView owns owner login, workspace metadata,
desired state, durable operations, audit and session authorization. SQLite is the
initial single-instance metadata store; Ecto migrations and repository boundaries
must permit a later PostgreSQL deployment without claiming it is already supported.
A Go host agent owns provider access, local storage and process supervision. It
connects outbound to the control plane over an authenticated TLS channel.

Domain → application services → ports; adapters depend inward on ports. Web UI
calls application services only. Provider SDKs, Docker socket paths and Dev
Container CLI subprocesses are confined to host adapters. Contract types are
versioned independently from implementations. No common code may import an adapter.

## Intended tree
`apps/control_plane/`: Phoenix app (feature implementation creates it).
`apps/host_agent/`: Go executable (feature implementation creates it).
`contracts/`: protocol, provider and bundle boundaries.
`docs/`: decisions, runbooks, quality and execution records.
`specs/`: feature requirements, plans and tasks.

## Runtime and persistence
User → authenticated UI → operation service → host-scoped agent command → provider.
The browser terminal uses a one-time, expiring workspace/owner-bound session grant;
control plane relays framed PTY traffic to the agent. It never grants raw host shell
access. Bound buffers, dimensions, connection lifetimes and simultaneous sessions.
Workspace volume outlives RuntimeInstance. Control plane restart reads operations
and reconciles agent reports; stale leases must not imply a running workspace.

## Host protocol
Versioned messages include command ID, operation/idempotency key, host ID, workspace
ID, owner ID and configuration revision. Agent verifies target and authorization;
result identifies provider instance and observed generation. mTLS identifies peers;
application scopes authorize commands. Enrollment, rotation, revocation, reconnect,
replay handling and compatibility negotiation are first-feature acceptance work.

## Environment construction
Resolve the trusted config Git commit, validate submodule paths/URLs and capability
requirements, resolve pinned bundle artifacts, then provision via Dev Container CLI.
BundleResolver produces digest/platform/requirements; BundleAttacher chooses a
supported provider materialization strategy. Unsupported attachments fail before
provisioning. OCI bundle contents are not assumed to be runnable container images.
Lifecycle hooks and bundles execute only after workspace trust is granted.

## Isolation
Host agent can control Docker and is privileged infrastructure. A compromised
workspace must not obtain agent credentials, control sockets or other owners' data.
Use a dedicated host for untrusted workloads; microVM providers are a later option.
Runtime backups and Git configuration backups are separate recovery concerns.

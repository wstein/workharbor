# Anchorage bundle drift fixture

`anchorage-devcontainer.json` captures the actual consumer configuration from
`/workspaces/Anchorage/.devcontainer/devcontainer.json` observed on 2026-10-24.
Its six pinned BundleDock references, canonical volumes and two-string host-info
member are test inputs only. The snapshot contains no credentials. Tests read it
as data and never execute its hooks or contact a registry or Docker daemon.

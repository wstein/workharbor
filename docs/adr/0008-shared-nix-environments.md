# 0008-shared-nix-environments: Shared repository configuration, deferred integration

Date: 2026-10-12
Status: Future direction; implementation deferred pending an approved feature and plan

## Context and direction

The owner identifies a shared Nix configuration repository as the main value of
future devenv support. A common repository can define reusable development tools,
language defaults and service modules. Projects compose selected shared modules
with their own configuration and overrides, then use that declared environment
locally, in CI and as the source for project-specific OCI images.

Support is deferred until full integration can be designed and verified. Do not
add a standalone devenv executable to the toolbox or introduce a partial runtime
integration as an intermediate product feature. The current Docker-first,
`.devcontainer` baseline and independent BundleDock contracts remain authoritative.
This decision records direction only; it creates no implementation task, repository,
build, publication or deployment authorization.

## Shared configuration contract

- The shared repository owns reusable modules and their compatibility policy.
  Projects select modules and retain project-specific packages, services and overrides.
- Each project records the shared repository's immutable revision through its
  reviewed input configuration and `devenv.lock`. A shared repository update does
  not silently change every project; adoption is an explicit project change.
- Shared Nix modules are executable trusted inputs. Review the resolved shared and
  project revisions together, including hooks, services and build commands.
- Pin the Nix/devenv toolchain and build platform alongside locked inputs. Record
  the resulting OCI digest and build provenance separately from source identities.
- Keep credentials, caches and service data outside committed configuration and
  image layers. Local overrides must be recorded in effective execution evidence
  or rejected for approved CI/image builds; a lockfile alone does not bind them.

Upstream supports composing Nix modules from external inputs. Its documentation
distinguishes that from importing remote `devenv.yaml` files, which is not supported
by the documented composition path. A mutable sibling `path:` checkout is useful
for local experimentation but is insufficient as the sole immutable build identity.
See [composition](https://devenv.sh/composing-using-imports/),
[inputs](https://devenv.sh/inputs/) and [pinning](https://devenv.sh/pinning/).

## Later environment and OCI flow

1. Maintain shared modules in a common repository.
2. Compose them with each project's configuration and commit its reviewed lockfile.
3. On an approved Linux builder, use the pinned devenv version to build the project's
   shell or process image; validate its actual runtime requirements and entrypoint.
4. Copy the built image to an approved registry or Docker daemon, record its digest,
   and have WorkHarbor provision it through the established container boundary.
5. Verify the local, CI and container environments against the same declared inputs;
   document platform, process, secret and persistence differences explicitly.

Current upstream documents `devenv container build shell` producing an image
specification in the Nix store, followed by `devenv container run shell` for Docker
execution. The build result is not automatically a Docker archive suitable for
`docker load`; that command requires a separately produced compatible archive.
The `processes` image is a separate choice for starting declared services. macOS
image generation requires a Linux builder. These commands are upstream references,
not measured WorkHarbor procedures. See [containers](https://devenv.sh/containers/).

## Integration boundaries and acceptance

WorkHarbor would retain workspace ownership, provisioning, persistent volumes and
terminal access. devenv would own the composed project environment and its image
construction. BundleDock utility carriers remain separately resolved artifacts;
the later design must define overlapping tool/PATH ownership rather than assume
its read-only mounts work in every Nix-generated image.

Keep `.devcontainer` the reviewed workspace interface. Any generated configuration
must be reviewed before import; no runtime generation may silently overwrite it.
No host Nix installation or privileged daemon access is implied by consuming an
already built OCI image. Cache policy and permitted source builds belong to the
future builder contract, separate from BundleDock's vendor-prebuilt packaging policy.

Before claiming support, an approved feature must prove shared-input update and
rollback, exact version/provenance reporting, AMD64/ARM64 image compatibility,
non-root browser-terminal access, service persistence/recovery, bounded cache-miss
behavior and offline reuse of previously materialized artifacts. Identical declared
inputs do not by themselves prove identical behavior across operating systems or
deployment modes. No such integration evidence exists yet.

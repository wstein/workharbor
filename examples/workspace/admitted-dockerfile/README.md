# Admitted dockerfile configuration example

Copy this directory as the root of a dedicated configuration Git repository. Commit
it, pin every actual project submodule and explicitly approve that exact root and
submodule revision set before provisioning. The image is a digest-pinned Docker
Official Image; both execution users are explicit numeric UID:GID pairs.

The current provider requires rootful, unmapped Linux Docker and initial ownership
capability. UID/GID 1000:1000 must work in the actual image; real Linux nonroot Git,
chmod and retention proof remains an acceptance gate. These fixtures are selected
source inputs, not a claim that Docker acceptance has already passed.

See [the capability matrix](../../../docs/development/devcontainer-matrix.md).

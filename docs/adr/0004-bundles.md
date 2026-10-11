# 0004-bundles: Independent bundle resolution and attachment

Date: 2026-10-11
Status: Accepted contract; runtime integration deferred

## Context and decision
Separate BundleResolver from BundleAttacher. Resolve OCI artifacts by digest and platform, preserve tool paths and attachment requirements. Attachers advertise methods and reject unsupported methods before creating a runtime. Bundledock provides centrally pinned tools beside an unchanged language image; verify its real layout before implementing feature 002. Do not invent a Bundledock manifest API or equate OCI bundles with OCI runtime bundles.

## Consequences
Changes to this decision require an owner-reviewed superseding ADR and updated contracts/specs.

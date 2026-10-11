# 0003-workspace-git: Configuration repository with project submodules

Date: 2026-10-11
Status: Accepted design

## Context and decision
Use a dedicated workspace config Git repository with pinned project submodules. Record origin and exact commit per resolved configuration. Keep runtime files, credentials and audit out of Git. No embedded Git server in MVP. Validate relative paths, symlink traversal and approved URL schemes before explicitly initialized submodules. A config update never silently resets uncommitted project work. Per-user forks can follow later; owner_id remains the access authority.

## Consequences
Changes to this decision require an owner-reviewed superseding ADR and updated contracts/specs.

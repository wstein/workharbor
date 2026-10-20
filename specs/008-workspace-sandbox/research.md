# Feature 008 research

Status: researched choices for review; nothing claimed implemented. Inputs: IDEAS_NOTES sections 4 to 11 (2026-10-20; Anchorage branch
`docs/ideas-codex-delivery-plan`, unmerged, not in this repository), which are research, not requirements. Upstream facts below were inspected on 2026-10-20 and must be re-pinned before use.

- **Git worktree** shares repository metadata and refs; a scratch home does not protect shared control data
  ([git-worktree](https://git-scm.com/docs/git-worktree#_details)). Hence untrusted jobs get a private copy (FR-010) and the
  worktree mode is an acknowledged trusted convenience (FR-011).
- **Private copy**: `git clone --no-local --no-hardlinks` of the base commit, or `git archive` plus a generated `.git`, avoids
  alternates and shared objects; remotes are removed after cloning and `.git/config` is generated. Dirty content travels as a
  patch applied inside the copy. Transfer cost, cache trust and dirty submodules are handled by the copy policy.
- **Config rename**: git's `config.lock` rename is why a directory mount is required (owner decision 2026-10-20). Confirmation
  against real Docker bind-mount behavior is an owner gate; unit tests inject `EBUSY`.
- **Docker Sandboxes** documents microVM execution, an isolated daemon, private clone with a read-only source and mountless
  workspaces ([architecture](https://docs.docker.com/ai/sandboxes/architecture/), [security](https://docs.docker.com/ai/sandboxes/security/)).
  These motivate the capability fields; no adapter or equivalent protection is claimed.
- **Proxy variables** configure cooperative clients and do not enforce routing; allowlists reduce destinations without
  preventing exfiltration through permitted services. Hence `egress_enforcement` defaults to `none` and refuses requirements.
- **macOS container stacks** run Linux guests (the Docker VM); the Mac host kernel and the Docker VM kernel shared by containers must be distinguished when
  stating isolation.
- **Git identity**: `user.useConfigOnly=true` stops git guessing an identity; the environment variables supply it. A test
  (`TestCommitWithoutIdentityFails`) verifies the behavior with the installed git rather than assuming it.
- **Why no fallback**: a plain container satisfies none of the properties a microVM request is made for; silent substitution
  turns a stated protection into a false one (FR-014).

## Decisions still needed

OD-1 to OD-5 in [plan.md](plan.md#owner-decisions). Provider kinds beyond standard Docker, credential mediation,
egress enforcement and recovery need their own specifications and are not decided here.

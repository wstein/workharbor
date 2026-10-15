# Feature 007 implementation plan

Status: proposed for owner review; bounded direct-request implementation candidate.
No revision-bound Symphony dispatch approval is asserted.

Use the existing pilot host-agent Go module and standard library only. Depend on
pilot `832dd63000257bad01656154b639251d643f2a2d` (T009 runtime source
`90ea176`), which is not in main. Keep the main-target PR draft with this explicit
dependency. Do not change Feature 006 tasks, the T009 closed config/provider schema,
mount admission, Docker execution or transport fields.

Add `internal/workspaceshell` as a pure policy port. It validates caller-supplied
attachment description only; later trusted resolver/attacher establishes integrity,
authorization, and observed mount state. Never decode source data into authority.
The new-session launcher owns applying the returned environment before startup and
installing fixed guarded rc fragments in approved image paths. Reattachment to an
existing console cannot apply settings. T013/T014 have no accepted launch adapter
API yet, so this port deliberately requires later adoption review.

Write negative/positive tests before behavior, then implement environment planning.
Use fixed toolbox paths and shell snippets, copied environment maps and stable typed
errors. No shell-string interpolation, source execution, dependencies or activation.
Document no-prompt policy and full Nushell shortcuts opt-out. Keep executable policy
acceptance separate from full runtime acceptance.

Allowed paths: this feature directory; `contracts/workspace-shell-policy.md`;
`apps/host_agent/internal/workspaceshell/`; append only this package's required
IDs in `docs/development/ci-acceptance.json`; a focused evidence record and links in
`docs/index.md` and the issue-81 design paper. No existing accepted task edits.

Checks from host-agent: `gofmt -l internal/workspaceshell`,
`go test ./internal/workspaceshell`, `go vet ./internal/workspaceshell`.
From root: `make check`, `git diff --check`. Record actual commands/results.
Real Linux read-only mounts, image-specific system rc loading, bash/zsh/fish/Nushell
vendor startup and prompt-sentinel tests remain future integration acceptance;
fixtures and pure outputs cannot satisfy them. Independent exact-head review is
required before handoff; passing checks do not grant merge or activation.

# Archive of workharbor-old

Historical records of a superseded design. They are not current requirements,
decisions or threat model for this repository. This repository has a different
architecture (Go `whr` rewrite, Spec Kit); see [product](../../product.md),
[architecture](../../architecture.md) and the [ADR index](../../index.md).

## Read this first: names and numbers refer to workharbor-old

The old repository was renamed to `wstein/workharbor-old`, and on 2026-10-11 the
name `wstein/workharbor` was reused for the current repository, so GitHub no
longer redirects old links. Inside this archive, every
`https://github.com/wstein/workharbor/...` URL (196 links in 35 files) and every
bare `#N` issue or pull request reference (including `Refs: #538` in the
patches) means the same item in `wstein/workharbor-old`. Read such links with
`-old` substituted. For example, `.../workharbor/issues/7` in the archive is old
issue 7, "Route agent approvals from inside a container to the supervisor", while
the same URL now opens the current repository's #7, "T006: Implement strict
TaskContext validation and canonical digesting".

References to `AGENTS.md`, `.agents/*` and similar files, such as "the rules are in
AGENTS.md" in the manual, mean the old repository's files. They are not
instructions for the current repository, whose rules are in its own
[AGENTS.md](../../../AGENTS.md).

## Provenance

| Item | Value |
| --- | --- |
| Source repository | `wstein/workharbor-old` (archived, read-only) |
| Source commit | `89a612d6df29a81db5abed9542120b1d8ce36cbd` (main, 2026-10-15) |
| Copied | 2026-10-19 |
| Why | The old repository may later be deleted; these documents have no counterpart here |

All Markdown files under `docs/content/` were copied byte-for-byte to the same
relative path under `docs/content/` in this directory. No file was edited.
The copies keep their original links and Hugo front matter and shortcodes, so
`scripts/check_repo.py` does not check links below `docs/archive/workharbor-old/docs/content/`;
this README and any other authored file here are checked.

License and authorship: the documents were published under EUPL-1.2 in the old
repository, written by Werner Stein plus one Codex commit and one Dependabot
commit. Both repositories are EUPL-1.2. The two LICENSE files differ byte-wise
(13,792 versus 13,827 bytes; line wrapping and list-marker formatting only).

## Contents (original path = path relative to `docs/content/docs/`)

- [Requirements catalog v3](docs/content/docs/requirements.md) (`requirements.md`)
- [Decision log D1 to D62](docs/content/docs/design/decisions.md) (`design/decisions.md`)
- [Threat model](docs/content/docs/threat-model.md) (`threat-model.md`)
- [Glossary](docs/content/docs/glossary.md)
- [Design documents](docs/content/docs/design/_index.md) (`design/`)
- [Operations manual](docs/content/docs/manual/_index.md) (`manual/`)
- [Spike reports](docs/content/docs/spikes/_index.md) (`spikes/`)
- `patches/`: see below

## Unmerged old-history patches

`patches/` holds `git format-patch` output of five commits that existed only as
unreachable objects in a local repository (they would disappear on garbage
collection), which is why the patches are kept. They patch
the old Go monolith (`internal/*`, module `github.com/wstein/workharbor`) and
are historical text, never applied here.

| Patch | Subject | Status |
| --- | --- | --- |
| [973c007a](patches/973c007a.patch) | feat(cli): inspect agent topic push forge controls | Never merged into old main; design reference |
| [521c64ea](patches/521c64ea.patch) | docs(manual): explain agent push doctor diagnostics | Never merged; documents 973c007a |
| [e7958c31](patches/e7958c31.patch) | feat(web): allow an explicit VPN listen address | Pre-fix snapshot of old PR #553 (merged) |
| [aa900e9f](patches/aa900e9f.patch) | docs(design): permit an explicit web listener address | Pre-fix snapshot of old PR #553 (merged) |
| [6ddbca79](patches/6ddbca79.patch) | docs(manual): describe the WireGuard web listener | Pre-fix snapshot of old PR #553 (merged) |

`973c007a` and `521c64ea` implement `whr doctor --agent-topic-push
[--agent-app-id N]`: the flags, the `inspectAgentTopicPush()` wiring of the
inspector merged in old PR #551, tests, and the manual text. The
`agent-push-ruleset` check reports `warn` for missing policy, an agent bypass or
unconfirmed enforcement, and `not_verified` for an unreachable forge, unknown
identity or incomplete visibility; neither fails the exit code alone. The
diagnostic does not waive R-30. This was never merged into
the old main. Treat it as a design reference for a future spec in this
repository, not as code to port: the new `whr` uses a different structure.

The three #553 patches are earlier states of work that was merged in the old
repository with additional authentication and serve enforcement; the merged
version is a superset and lives in `wstein/workharbor-old`.

## Not archived

- GitHub issues (481 in `wstein/workharbor-old`) and pull requests (76): not in git, not exported.
- Unmerged branches of the old repository: `feat/533-docker-quota-proof`,
  `feat/533-docker-runtime`, `feat/535-tool-watchdog`, `fix/534-existing-runtime`,
  `codex/console-shared-shell-layer` and the `spike/*` branches.
- Code, schemas, `docs/releases/`, `design/mock`, `CHANGELOG.md`, root `AGENTS.md` and `README.md`,
  `.agents/*.md` (old agent role prompts), root `spikes/`, `.github/*.md` community files,
  `assets/BRAND.md`, and the Hugo site configuration, layouts and static assets.
- The LICENSE files (see above).

All of these remain in the old repository.

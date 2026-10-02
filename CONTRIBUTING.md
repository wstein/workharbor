# Contributing to workharbor

Thank you for your interest. workharbor is **building release 1** and has no release yet: the most valuable contributions right now are reviews of the [design](docs/content/docs/design/_index.md) and the [threat model](docs/content/docs/threat-model.md), spikes against the open decisions in §12, and bug reports against the existing packages and tooling.

Please read the [Code of Conduct](CODE_OF_CONDUCT.md) first. [AGENTS.md](AGENTS.md) holds the full conventions and applies to humans as well as AI agents.

## Before you start

- **Open an issue first** for anything beyond a typo. Commits of type `feat`, `fix`, `perf` and `refactor` must reference an issue, so there is always one to link.
- For architecture changes, discuss in the issue. Decisions are recorded as numbered rows (D1, D2, ...) in the design document.
- Report security problems privately, as described in [SECURITY.md](SECURITY.md).

## Development setup

You need Go (see `go.mod`) and, for the docs, a C++ compiler the first time Hugo is built. Nothing else needs installing: tools run through pinned `go run` or `npx` commands.

```bash
git clone https://github.com/wstein/workharbor
cd workharbor
make hooks   # enable the pre-commit and commit-msg hooks and the commit template
make check   # format, vet, lint, editorconfig and tests
make docs    # build the documentation site into _site
```

## Making a change

1. Branch from `main`. Keep the change small and focused.
2. Add or update tests for domain logic and policy. Update the design or docs when behaviour or decisions change.
3. Run `make check`. The hooks and CI run the same checks.
4. Open a pull request and fill in the template. Pull requests are merged with **rebase merge**, so each commit lands on `main` as written, with its trailers; squash and merge commits are disabled.

For open-ended questions and ideas, use [Discussions](https://github.com/wstein/workharbor/discussions). Issues labelled `design` concern the architecture and the design document.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/) with metadata in Git trailers, enforced by `commitlint`:

```text
feat(domain): add run interrupted state

Why the change was made.

Refs: #12
Assisted-by: Claude Code:claude-sonnet-5-5
```

- `Refs: #N` or `Closes: #N` is required for `feat`, `fix`, `perf` and `refactor`.
- `Assisted-by: <tool>:<model-id>` discloses AI assistance (see below).
- `Signed-off-by` is for humans only.
- One commit per finished logical change, not per attempt: iterate in your working tree and commit once the result is final. Amend or fixup your own unpushed commits instead of adding "fix previous commit" commits.
- Do not bypass the hooks with `--no-verify`.

## Releases

Only the maintainer releases (design D24). The pipeline is built but dormant until `v0.1.0`.

1. **Prepare.** On a branch: `make release-prep VERSION=vX.Y.Z` regenerates `CHANGELOG.md` and commits it as `chore(release): prepare vX.Y.Z`. Merge it and wait for CI on `main`.
2. **Tag.** The maintainer signs and annotates it: `git tag -s vX.Y.Z` on that commit, then pushes the tag. The workflow stops unless the tag is annotated, signed by a key in `.github/release-signers` (SSH signatures), on `main` and green in CI. Agents never tag.
3. **Check the draft.** The `release` workflow builds `whr` (darwin/arm64, linux/arm64, linux/amd64) and the guest helpers, checksums, an SBOM and provenance attestations, with notes from git-cliff, into a **draft** release. Download the assets, `gh attestation verify` one, and read the notes.
4. **Publish.** Publish the draft by hand. Nothing in the workflow publishes.

To test the pipeline before a tag, run the `release` workflow by hand (a snapshot build that creates no release), or `make release-snapshot` locally.

## AI-assisted contributions

AI tools are welcome, including coding agents. You remain responsible for everything you submit:

- review and understand every line before you open the pull request
- disclose the tool and model with an `Assisted-by` trailer
- never add `Signed-off-by` on behalf of an agent
- do not submit work you could not explain or that you have not run

## License

workharbor is released under the [EUPL-1.2](LICENSE). By contributing you agree that your contribution is licensed under the same terms.

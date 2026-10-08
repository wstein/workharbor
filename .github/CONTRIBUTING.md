# Contributing to workharbor

Thank you for your interest. workharbor is **building release 1** and has no release yet: the most valuable contributions right now are reviews of the [design](../docs/content/docs/design/_index.md) and the [threat model](../docs/content/docs/threat-model.md), spikes against the open decisions in §12, and bug reports against the existing packages and tooling.

Please read the [Code of Conduct](CODE_OF_CONDUCT.md) first. [AGENTS.md](../AGENTS.md) holds the full conventions and applies to humans as well as AI agents.

## Before you start

- **Open an issue first** for anything beyond a typo. Commits of type `feat`, `fix`, `perf` and `refactor` must reference an issue, so there is always one to link.
- For architecture changes, discuss in the issue. Decisions are recorded as numbered rows (D1, D2, ...) in the design document.
- Report security problems privately, as described in [SECURITY.md](SECURITY.md).

## Development setup

You need Go (see `go.mod`) and, for the docs, `curl` and `shasum` (macOS: also `pkgutil`). Hugo needs no install: the first `make docs` downloads the pinned prebuilt Hugo v0.165.0 extended release into `.cache/hugo` and verifies its SHA-256 (`.config/hugo.sha256`) before use. Other tools run through pinned `go run` or `npx` commands.

```bash
git clone https://github.com/wstein/workharbor
cd workharbor
make hooks   # enable the pre-commit and commit-msg hooks and the commit template
make check   # format, vet, lint, editorconfig and tests
make docs    # build the documentation site into _site
```

## Where things live

- Tool configuration is under [`.config/`](../.config): `cliff.toml` (changelog), `gitmessage` (commit template), `golangci.yml` (lint), `goreleaser.yaml` (release), `lychee.toml` (link check) and `typos.toml` (spelling).
- The community files are under [`.github/`](.): this file, `SECURITY.md`, `CODE_OF_CONDUCT.md`, the issue and pull request templates, `dependabot.yml`, `renovate.json`, `release-signers` and the workflows.
- Nothing of these is at the repository root any more, so link to them through these paths (for example `.github/SECURITY.md`).

## Making a change

1. Branch from `main`. Keep the change small and focused.
2. Add or update tests for domain logic and policy. Update the design or docs when behaviour or decisions change.
3. Run `make check`. The hooks and CI run the same checks.
4. Open a pull request and fill in the template. Pull requests are merged with **rebase merge**, so each commit lands on `main` as written, with its trailers; squash and merge commits are disabled.

For open-ended questions and ideas, open an [issue](https://github.com/wstein/workharbor/issues/new/choose); GitHub Discussions is not enabled. Issues labelled `design` concern the architecture and the design document.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/) with metadata in Git trailers, enforced by `commitlint`:

```text
feat(domain): add run interrupted state

Why the change was made.

Refs: #12
Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>
```

- `Refs: #N` or `Closes: #N` is required for `feat`, `fix`, `perf` and `refactor`.
- `Co-Authored-By: <tool> <model-id> <attribution-email>` discloses AI assistance (see below).
- `Signed-off-by` is for humans only.
- One commit per finished logical change, not per attempt: iterate in your working tree and commit once the result is final. Amend or fixup your own unpushed commits instead of adding "fix previous commit" commits.
- Do not bypass the hooks with `--no-verify`.

Use the actual model exposed by the session (`unknown` if unavailable), never a guessed model. The example applies only when that is the actual model. [AGENTS.md](../AGENTS.md) defines the attribution identities, preserves legitimate human coauthors and accepts validated historical `Assisted-by` trailers without rewriting history.

## Releases

Only the maintainer releases (design D24). Until `v0.1.0`, dogfood builds are prerelease tags `v0.1.0-alpha.N` whose draft is never published: skip step 1 but not step 0, tag a green commit of `main` (step 2), and install the draft on the host with `make install-release VERSION=v0.1.0-alpha.N` ([Install, upgrade and release](https://wstein.github.io/workharbor/docs/manual/install-upgrade-release/) in the manual).

0. **Summary.** Commit `docs/releases/vX.Y.Z.md` (from `docs/releases/TEMPLATE.md`) on `main` first, also for alpha tags. The release job prepends it to the notes and fails without it (missing or empty file), and a tag cannot be moved.
1. **Prepare.** On a branch: `make release-prep VERSION=vX.Y.Z` regenerates `CHANGELOG.md` and commits it as `chore(release): prepare vX.Y.Z`. Merge it and wait for CI on `main`.
2. **Tag.** The maintainer signs and annotates it: `git tag -s vX.Y.Z` on that commit, then pushes the tag. It may be pushed together with `main`: the workflow waits for CI on that commit (polling every 15 seconds, for up to 15 minutes) and stops if CI fails, is cancelled, does not finish in time or never started. It also waits up to 5 minutes for the tagged commit to appear on `main`, and stops unless the tag is annotated, signed by a key in `.github/release-signers` (SSH signatures) and on `main`. Agents never tag.
3. **Check the draft.** The `release` workflow builds `whr` (darwin/arm64, linux/arm64, linux/amd64) and the guest helpers, checksums, an SBOM and provenance attestations, with notes from git-cliff, into a **draft** release. Download the assets, `gh attestation verify` one, and read the notes.
4. **Publish.** Publish the draft by hand. Nothing in the workflow publishes.
5. **Tap.** Publishing a final release (not a prerelease) runs the `tap` workflow: it checks the assets' checksums and attestations, renders `Formula/whr.rb` with `scripts/homebrew-formula.sh` and pushes it to [`wstein/homebrew-tap`](https://github.com/wstein/homebrew-tap). Once: create that repository, add a deploy key with write access to it, and store the private key as the secret `TAP_DEPLOY_KEY` in an environment `homebrew-tap` of this repository (limit the environment to the `main` branch and the `v*` tags). Run the workflow by hand with a tag to render the formula again.

To test the pipeline before a tag, run the `release` workflow by hand (a snapshot build that creates no release), or `make release-snapshot` locally.

## AI-assisted contributions

AI tools are welcome, including coding agents. You remain responsible for everything you submit:

- review and understand every line before you open the pull request
- disclose the actual tool and the model name as exposed by the session (or `unknown`) with a `Co-Authored-By` trailer
- never add `Signed-off-by` on behalf of an agent
- do not submit work you could not explain or that you have not run

## License

workharbor is released under the [EUPL-1.2](../LICENSE). By contributing you agree that your contribution is licensed under the same terms.

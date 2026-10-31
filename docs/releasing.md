# Releasing whr

The `whr` CLI is released by `.github/workflows/release.yml`. An annotated tag that the
repository owner pushes to a commit of `main` is the approval to publish; nothing else publishes.
Merging, a schedule, a bot or a manual run does not. This is the same pattern as the SourceSteward
release workflow. Nothing is signed with a certificate or notarized: the publish job attests build
provenance and writes `SHA256SUMS`.

## Versions

- A release is `vMAJOR.MINOR.PATCH`, for example `v0.1.0`.
- The first release is `v0.1.0`; its notes read "First release." because no earlier release tag
  exists on the remote.
- Pre-releases are supported for later use: `vMAJOR.MINOR.PATCH-alpha.N`, `-beta.N` or `-rc.N` with
  no leading zero in `N`, for example `v0.2.0-rc.1`. A pre-release tag publishes a GitHub
  pre-release; a release tag publishes a normal release. No other suffix and no build metadata is
  accepted. A final release is the notes baseline above its own pre-releases.
- The binary is stamped without the leading `v` (`whr version` prints `whr 0.1.0`), its
  revision is the tagged commit and its build date is the commit time. The archive is
  `whr_<version without v>_<os>_<arch>.tar.gz`.
- Never move or re-create a tag. A wrong tag is abandoned and the next version used.

## Release notes

There is no CHANGELOG file. `.github/scripts/release-notes.sh` generates the notes from the commit
subjects between the previous release tag and the tagged commit, and keeps them short:

- **Features (N)**: every `feat` commit.
- **Breaking changes (N)**: every commit with `!` after its type (for example `docs!:` or `fix!:`)
  that is not a `feat`, so nothing breaking is hidden. A `feat!` stays under Features. Only the
  subject is read; a `BREAKING CHANGE:` footer alone is not detected, so mark the subject with `!`.
- **Fixes (N)**: every `fix` commit, in a collapsed `<details>` block.
- **Other changes (N)**: `perf` and `refactor` commits, only when there are any.
- A closing line counts the documentation commits and all remaining commits (tests, style, build, CI,
  chores, reverts and subjects that are not Conventional Commits) without listing them, and links the
  full comparison `https://github.com/wstein/workharbor/compare/<previous>...<tag>`.

A group without commits is omitted. The first release prints "First release." and a range without
commits prints "No changes."

## Shipped platforms

linux/amd64, linux/arm64 and darwin/arm64, each built with CGO disabled and run natively on a
runner of its own platform (`ubuntu-24.04`, `ubuntu-24.04-arm`, `macos-15`) before publication.
Windows and darwin/amd64 are explicitly not shipped: whr uses Unix file-system and terminal primitives, and the
repository evidence is Linux and Apple-silicon macOS. Add darwin/amd64
(a `macos-15-intel` runner) if Intel Macs matter.

## Stages

1. **verify** (read-only): the version is strict; for a tag run the tag is an annotated tag whose
   commit is reachable from `main` and is the commit of the run; a successful `check.yml` push run
   exists for that commit (`release-verify.sh ci-green` retries the head_sha query and then filters
   the newest 500 successful push runs by hand, because the API has answered inconsistently; an
   older commit must re-run CI first); no release for the tag exists; the run was started by the
   repository owner; `go mod verify`, `go vet`, `scripts/check_repo.py` and the workflow tests pass.
2. **build**: a matrix job per target builds twice with separate empty caches and compares the bytes
   (`-trimpath`, `-buildvcs=false`, `GOWORK=off`, `-mod=readonly`), then packages a deterministic
   archive with `LICENSE`, `README.md` and the third-party notices of the linked modules.
3. **smoke**: the shipped archive is checked against its checksum and member list and run natively
   (`whr version`, `whr --version`, `whr version --json`, `whr --help`, and the stamped revision).
   Nothing else is started: no Docker, no workspace.
4. **publish** (tag runs only, environment `release`): rechecks the owner, that no release (draft
   or not) exists, and that the tag still names the verified commit; writes `SHA256SUMS`; attests
   provenance (`id-token` and `attestations` permissions exist only in this job); creates the
   release with `gh release create --verify-tag`, with `--prerelease` for a pre-release version.
   Set the repository variable `WHR_RELEASE_DRAFT` to `true` to publish a draft first.

## Dry run

Run the `release` workflow by hand (`workflow_dispatch`) with the default `v0.0.0-dryrun` or another
`-dryrun` label. Verify, build and smoke run for every target; publish never runs. A dry run is a
build, so it needs the owner's approval like any other.

## Owner steps for a release

1. Check that `check.yml` is green on the `main` commit to release. Before the first tag, create
   the environment `release` (Settings, Environments) with a required reviewer or a tag
   restriction; the publish job refers to it and GitHub would otherwise create it unprotected.
2. Optionally run the dry run above.
3. Push the annotated tag on that commit, for example
   `git tag -a v0.1.0 -m "WorkHarbor v0.1.0" <commit> && git push origin v0.1.0`.
4. Wait for the run. If it fails before publish, fix the cause and use the next version.

## Verifying a download

```sh
sha256sum --check --ignore-missing SHA256SUMS              # Linux
grep ' ARCHIVE$' SHA256SUMS | shasum -a 256 --check          # macOS, one archive
gh attestation verify ARCHIVE --repo wstein/workharbor
```

## Tests

`apps/whr/internal/releaseworkflow` tests the scripts under `.github/scripts` (with a fake `gh` and
mutants of the CI guard) and the structure of the workflow (pinned actions, permissions, matrix,
gates). The tests build no binary: the scripts are exercised with a stand-in for `whr`, and the real
binary is exercised by the smoke job.

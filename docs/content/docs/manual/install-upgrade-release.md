---
title: Install, upgrade and release
description: Install whr from a draft release or the tap, upgrade it, and what the operator checks before a release is published.
weight: 4
toc: true
---

For the operator: the person who installs `whr` on the Mac mini and cuts releases. **A draft: nothing on this page has been run against a release yet** ({{< status unverified >}}; the first install is issue #62, the first release `v0.1.0` follows the slice demo, issue #28). The design is [D24](../design/decisions.md) and [Releases](../design/roadmap.md).

## Where `whr` comes from

The supervisor always runs an installed binary built by CI from a signed tag on `main`, never a working tree (D34). The installer puts three files in a prefix that the `whr` user cannot write, so nothing running as `whr`, an agent's escape included, can replace the binary:

| File | Role |
| --- | --- |
| `<prefix>/bin/whr` | the CLI and `whr serve` (macOS, arm64) |
| `<prefix>/libexec/whr/whr-shim-linux-arm64` | the in-guest launcher |
| `<prefix>/libexec/whr/whr-proxy-linux-arm64` | the egress allowlist proxy |

## Before `v0.1.0`: a draft release

Until the first release, a signed prerelease tag `v0.1.0-alpha.N` on a green commit of `main` gives a dogfood build. Its draft is never published. Install it as the **administrator**, not as `whr`:

```bash
make install-release VERSION=v0.1.0-alpha.1            # prefix /opt/whr
make install-release VERSION=v0.1.0-alpha.1 PREFIX=/some/prefix
```

You need `gh` (`brew install gh`), signed in as a writer of the repository: a draft can be downloaded only by a writer. The script downloads the macOS archive, the guest archive and `checksums.txt`, checks both archives against the checksums and against the build-provenance attestation of this repository's release workflow, and installs **nothing** unless every check passes. It refuses anything but macOS on Apple silicon. It reads the installed version from `<prefix>/libexec/whr/VERSION`, which it writes after a verified install, and never runs the installed `whr` before the checks; an older tag, or an install without that file, needs `--allow-downgrade` (`make install` from source removes that file, so the version after a source install is unknown) (`make install-release ... ALLOW_DOWNGRADE=1`). `WHR_RELEASE_REPO=owner/name` changes whose attestations are trusted (a fork); the script refuses it unless you also pass `--trust-release-repo` to `scripts/install-release.sh`.

Then, as `whr`, build the tool store with the guest launcher (the script prints the exact command):

```bash
/opt/whr/bin/whr tools build -store <tool store> -shim /opt/whr/libexec/whr/whr-shim-linux-arm64
```

`make install` builds from your own checkout instead. It refuses a dirty tree and a commit that is not on `origin/main`, and is for a developer's machine, not the supervisor.

## From `v0.1.0`: the tap

A published, non-prerelease release updates the Homebrew tap, so the tap never points at a draft:

```bash
brew install wstein/tap/whr
```

The formula installs `whr`, the guest binaries in `libexec/whr` and the shell completions. The formula route is {{< status unverified >}} until #62 installs it. The host's Homebrew packages are pinned and not upgraded unasked (`HOMEBREW_NO_AUTO_UPDATE=1`, see [Prepare the Mac mini](host-setup.md)); upgrade `whr` on purpose.

## Upgrade

1. Read the release notes and the upgrade notes below.
2. Back up the state directory (the database and the configuration, see Backups in [Prepare the Mac mini](host-setup.md)).
3. Install the new version as the administrator (`make install-release` or `brew upgrade whr`).
4. Restart the supervisor: `whr service uninstall` then `whr service install` rewrites the plist for the new binary path; if the path did not change, `launchctl kickstart` of the job is enough. {{< status unverified >}}
5. Run `whr doctor`, then `whr version`.

The database migrates on the first start of a new version. There is no downgrade: keep the backup of step 2. Versions are `0.x` until the API, the adapter contract and the migrations are stable, so read every upgrade note.

### Upgrade note: the integration branch is part of the workflow policy (migration 0016)

Since migration `0016`, the recorded workflow of a repository includes its **integration branch** (`main` or `develop`) next to the preset. On the first start after the upgrade, a branch that was recorded as empty and is now read from the configuration counts as a **policy change**, and `whr serve` stops with a message that names the repository and both values. Check them, then do one of:

- start once with `whr serve --accept-workflow-change`; or
- with a passkey enrolled, start normally and confirm the change on the web page *Changes*; it applies at the next start.

Tasks already started keep the policy they started under. Repository names are now matched without regard to case, so a name written in another case is the same repository.

## Cut a release (the maintainer)

Only a human tags, signs and publishes (D24, §6); an agent never does.

1. `make release-prep VERSION=vX.Y.Z` regenerates `CHANGELOG.md` and commits it as `chore(release)`. It does not tag.
2. After CI is green on that commit of `main`, push a **signed, annotated** tag `vX.Y.Z`. The release workflow checks the signature against `.github/release-signers`, that the commit is on `main` and that CI passed, then builds into a **draft**: `whr`, the guest binaries, `checksums.txt`, an SBOM and a build-provenance attestation.
3. Check the draft: install it on your own prefix with `make install-release VERSION=vX.Y.Z`, which verifies the checksums and the attestation, and read the notes.
4. Publish it. For a release (not a prerelease) the `tap` workflow renders the formula and pushes it to `wstein/homebrew-tap`; a prerelease never updates the tap.

`make release-snapshot` builds the artifacts locally into `dist/` without publishing, to test the pipeline.

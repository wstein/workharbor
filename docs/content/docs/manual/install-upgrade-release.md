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

### Verify a download yourself

The release signature is the keyless Sigstore build-provenance attestation the release job makes (D24): it binds each file to the release workflow, the tag and the tagged commit. The attestation bundle is attached to the release as `whr_<tag>.intoto.jsonl`. Pin the workflow, the tag and the commit, not just the repository: without `--source-ref` and `--source-digest`, an older release's archive with its own `checksums.txt` passes (a downgrade).

```bash
tag=v0.1.0-alpha.1
commit=$(gh api repos/wstein/workharbor/commits/refs/tags/$tag --jq .sha)
gh attestation verify <file> --repo wstein/workharbor \
  --signer-workflow wstein/workharbor/.github/workflows/release.yml \
  --source-ref refs/tags/$tag --source-digest "$commit" \
  --deny-self-hosted-runners
# offline, with the attached bundle:
gh attestation verify <file> --repo wstein/workharbor --bundle whr_$tag.intoto.jsonl \
  --signer-workflow wstein/workharbor/.github/workflows/release.yml \
  --source-ref refs/tags/$tag --source-digest "$commit" \
  --deny-self-hosted-runners
```


Also check the file against `checksums.txt` (`shasum -a 256 -c`). `make install-release` does both. The exact `gh` flags (`--source-ref`, `--source-digest`, `--deny-self-hosted-runners` included), the offline check with `--bundle` and whether OpenSSF Scorecard counts the attached bundle as a signature are {{< status unverified >}} until a real draft release has been checked (#180).

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

`brew upgrade whr` is not blocked by `HOMEBREW_NO_INSTALL_UPGRADE`: Homebrew's manual says that variable only stops `brew install` from upgrading an installed formula. `brew pin` does hold a formula through `brew upgrade`, so if `whr` was pinned, run `brew unpin whr` first and pin it again afterwards (the manual page of `brew`; not run against a `whr` formula, {{< status unverified >}}).

The formula has `depends_on arch: :arm64`, so on an Intel Mac Homebrew stops with its own message about the unsupported architecture ({{< status unverified >}}: read from the formula, not run). Release 1 supports Apple silicon only.

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
2. After CI is green on that commit of `main`, push a **signed, annotated** tag `vX.Y.Z`. The release workflow checks the signature against `.github/release-signers`, that the commit is on `main` and that CI passed, then builds into a **draft**: `whr`, the guest binaries, `checksums.txt`, an SBOM, a build-provenance attestation and its bundle (`whr_<tag>.intoto.jsonl`).
3. Check the draft: install it on your own prefix with `make install-release VERSION=vX.Y.Z`, which verifies the checksums and the attestation, and read the notes.
4. Publish it. For a release (not a prerelease) the `tap` workflow renders the formula and pushes it to `wstein/homebrew-tap`; a prerelease never updates the tap.

### Set up the tap once (the maintainer)

The `tap` workflow pushes the formula with a deploy key, which issue #62 installs. {{< status unverified >}} until #62 is done, and the details below come from `.github/workflows/tap.yml`, not from a run:

- The private half is the secret `TAP_DEPLOY_KEY` in the `homebrew-tap` environment of `wstein/workharbor`, so only that workflow job can read it.
- The public half is a deploy key with write access on `wstein/homebrew-tap` and on no other repository.
- To rotate it, generate a new key pair, add the new public key to `wstein/homebrew-tap`, replace the secret, run the workflow once, then delete the old deploy key. Never put the private key in a file in the repository or in a chat.

`make release-snapshot` builds the artifacts locally into `dist/` without publishing, to test the pipeline.

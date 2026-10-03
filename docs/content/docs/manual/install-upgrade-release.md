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

You need `gh` (`brew install gh`), signed in as a writer of the repository: a draft can be downloaded only by a writer. The script downloads the macOS archive (`whr_<tag>_darwin_arm64.tar.gz`), the guest archive (`whr-guest_<tag>_linux_arm64.tar.gz`) and `checksums.txt`, checks both archives against the checksums and against the build-provenance attestation of this repository's release workflow, and installs **nothing** unless every check passes. It refuses anything but macOS on Apple silicon. It reads the installed version from `<prefix>/libexec/whr/VERSION`, which it writes after a verified install, and never runs the installed `whr` before the checks; an older tag, or an install without that file, needs `--allow-downgrade` (`make install` from source removes that file, so the version after a source install is unknown) (`make install-release ... ALLOW_DOWNGRADE=1`). `WHR_RELEASE_REPO=owner/name` changes whose attestations are trusted (a fork); the script refuses it unless you also pass `--trust-release-repo` to `scripts/install-release.sh`.

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

`make install` builds from the source tree of a clone of the repository instead. It refuses a dirty tree and a commit that is not on `origin/main`, and is for a developer's machine, not the supervisor.

## From `v0.1.0`: the tap

A published, non-prerelease release updates the Homebrew tap, so the tap never points at a draft:

```bash
brew install wstein/tap/whr
```

The formula installs `whr`, the guest binaries in `libexec/whr` and the shell completions. The formula route is {{< status unverified >}} until #62 installs it. The host's Homebrew packages are pinned and not upgraded unasked (`HOMEBREW_NO_AUTO_UPDATE=1`, see [Prepare the Mac mini](host-setup.md)); upgrade `whr` on purpose.

`brew upgrade whr` is not blocked by `HOMEBREW_NO_INSTALL_UPGRADE`: Homebrew's manual says that variable only stops `brew install` from upgrading an installed formula. `brew pin` does hold a formula through `brew upgrade`, so if `whr` was pinned, run `brew unpin whr` first and pin it again afterwards (the manual page of `brew`; not run against a `whr` formula, {{< status unverified >}}).

The formula has `depends_on arch: :arm64`, so on an Intel Mac Homebrew stops with its own message about the unsupported architecture ({{< status unverified >}}: read from the formula, not run). Release 1 supports Apple silicon only.

## Back up, upgrade and restore

One procedure serves all three: stop, copy or replace, start. Run it as the `whr` user, from its desktop session ([Prepare the Mac mini](host-setup.md), step 2). The paths below are the defaults; where the configuration sets `state_dir` or other roots, use those.

### What to copy

| What | Where | Rule |
| --- | --- | --- |
| Configuration directory | `~/.config/whr` (the configuration file) | A plain copy. It also holds secret files by default (below). |
| State directory | `~/.local/state/whr`, or `state_dir` | Holds the database `workharbor.db` **with** `workharbor.db-wal` and `workharbor.db-shm`. The database runs in WAL mode, so a copy of `workharbor.db` alone, or of any of the three taken while `whr serve` runs, can be inconsistent. Copy all three with `whr serve` stopped. The audit log is stored with the database; whether a separate audit file exists is {{< status unverified >}}. `api.sock` in the same directory is a socket: skip it. The same directory holds `topics` (the supervisor's own repositories: the commits the supervisor has imported from an agent's exported bundle, D42, [design §4.5](../design/domain.md); commits the agent made after the last export are not in it), `mirrors` (the cache of the forge repositories: rebuildable, can be large, so skipping it is fine) and the editor copies (`open`, the copies you open in your editor). Back up `topics` with the database. Whether to keep the editor copies is your call ({{< status unverified >}}: their directory name is read from the code, not from a restore). |
| Secret files | Every file the configuration names: `api_token_file`, `github.key_file`, `agent_api_key_env_file`, `console.ssh_ca_key_file`, the `ntfy` files | **Only into an encrypted backup** (a FileVault volume or an encrypted disk image). They may live outside `~/.config/whr`, so read the paths from the configuration. Keep mode `0600` when you restore them. |
| Workspace folders | Every folder below `roots.workspaces` | The agents' working folders and their session state. An agent's commits live in the workspace's agent clone until the supervisor exports them as a bundle into `topics` (above), so these folders hold the unexported commits as well as uncommitted changes and the session state; `topics` alone can miss committed work. Restore them from the same backup as the database, so session and database agree ({{< status unverified >}}). |
| Tool store | `roots.tool_store` | **Not copied.** Rebuildable with `whr tools build`, and `whr serve` verifies it at start against the hashes recorded when it was built ([design §5.6](../design/architecture.md)); it refuses to start when it does not verify. |
| Not copied | The agent-home volumes and container images | **Exclude them from every backup.** An agent-home volume holds the agents' sessions and the subscription logins (D40); no `whr` command copies one, and you should not either ([design §7.3](../design/security.md)). Images are rebuilt. |

### The procedure

1. **Stop the supervisor.** `whr service uninstall` unloads the job (the logs stay); if you run `whr serve` by hand, stop that. Check that no `whr serve` process is left. Stopping the supervisor does not stop the environments: an agent can keep running in its environment while `whr serve` is down, so the workspace folders and `topics` may change under your copy ([design §4.1](../design/domain.md)). Stop the environments first (`whr` has no single command for it in this draft; stop each workspace's environment, {{< status unverified >}}), or accept that the copy may be mid-change. Runs resume after the start ({{< status unverified >}}).
2. **Back up** every path in the table, to an encrypted destination for the secret files. Prefer a manual copy of the paths above while the supervisor is stopped. Time Machine cannot be timed to a stopped supervisor, and whether its snapshot covers the database and its two WAL files at one instant is not measured ({{< status unverified >}}).
3. **Upgrade (skip for a plain backup).** Read the release notes and the upgrade notes below, then install the new version as the administrator (`make install-release` or `brew upgrade whr`).
4. **Restore (skip unless restoring).** With the supervisor still stopped, put back the configuration directory, the state directory (the database file with its `-wal` and `-shm` files, together from one backup, or none of the three), the secret files at the paths the configuration names, and the workspace folders. Remove stale `-wal` and `-shm` files that do not belong to the restored database.
5. **Start the supervisor.** `whr service install` (the `whr service` group is provisional, like every command here) writes the plist for the current binary; the database migrates on the first start of a new version.
6. **Check.** `whr doctor`, then `whr version`.

### What a restore loses

The agent-home volumes are not in the backup, so a restore on the same Mac keeps the volumes that still exist, and a restore on another Mac or after the volumes were removed has none. Then:

- **Agent logins are gone.** Sign in again inside each environment, through the vendor's own flow (D40); `whr` never stored the login.
- **Logins, not sessions, are in the agent home.** Session state (transcript and agent session ID) lives in the workspace folder ([design §4.3](../design/domain.md)), so it comes back with the folder. It is lost only where the folder is not restored or the agent no longer knows the session: a run whose session the agent cannot resume (`ErrNoSession`), or that never reported one, ends `failed` and waits on a retry-or-cancel Decision ([design §5.3](../design/architecture.md)). Start a new run from the workspace; the agent's files and commits in the folder are still there.
- **Runs and tasks come back as the database recorded them** at the time of the backup, so a run that finished later is unknown to the restored supervisor. A Decision that was open in the backup may be superseded by the restart rule ([design §4.2](../design/domain.md)); the agent asks again.
- **Resume after a restore.** A run recorded `starting`, `running` or `paused` becomes `interrupted` and resumes from its session, if the restored workspace folder still holds it, with a briefing from the supervisor (D27); otherwise it ends `failed` as above. The restored database may be older than the workspace folder and its session, so check the workspace before you let the agent repeat anything.

The restore itself, and what resumes afterwards, has not been rehearsed on the real setup: {{< status unverified >}}.

The database migrates on the first start of a new version. There is no downgrade: keep the backup from the procedure above. Versions are `0.x` until the API, the adapter contract and the migrations are stable, so read every upgrade note.

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

---
title: Install, upgrade and release
description: Install whr from the release archive, a clone or the tap, upgrade it, and what the operator checks before a release is published.
weight: 3
toc: true
---

For the operator: the person who installs `whr` on the Mac mini and cuts releases. **A draft: the install has not been run on a clean Mac yet** ({{< status unverified >}}; the first install is issue #62, the first release `v0.1.0` follows the slice demo, issue #28). Measured so far: the script tests and the attestation check of `v0.1.0-alpha.3` (below). The design is [D24](../design/decisions.md) and [Releases](../design/roadmap.md).

## Where `whr` comes from

The managed dogfood or reference-host supervisor runs an installed binary built by CI from a signed tag on `main`, never a working tree (D34); in the alpha `whr` does not refuse to run from elsewhere (issue #493), and the doctor only warns. The installer puts three files in a prefix that the `workharbor` user cannot write, so nothing running as `workharbor`, an agent's escape included, can replace the binary:

| File | Role |
| --- | --- |
| `<prefix>/bin/whr` | the CLI and `whr serve` (macOS, arm64) |
| `<prefix>/libexec/whr/whr-shim-linux-arm64` | the in-guest launcher |
| `<prefix>/libexec/whr/whr-proxy-linux-arm64` | the egress allowlist proxy |

## Before `v0.1.0`: pre-releases

Until the first release, a signed prerelease tag `v0.1.0-alpha.N` on a green commit of `main` gives a dogfood build. Its release is published as a pre-release (the `v0.1.0-alpha.N` pre-releases are, per `gh release list`, 2026-10-08), and the tap ignores it. Install it as the **administrator**, not as `workharbor`. The first route is the release archive below: it needs no clone, no `gh` and no Command Line Tools. `make install-release` is the second route, from a clone.

### Install from the release archive (the first install)

From the release after `v0.1.0-alpha.4` (alpha.4 and earlier keep the old layout: separate host and guest archives, and for alpha.4 a loose `install-release.sh`) a release is **one archive**, `whr_<version>_darwin_arm64.tar.gz`, next to `checksums.txt`, the SBOM and the attestation bundle. The archive holds `bin/whr`, `guest/whr-shim-linux-arm64` and `guest/whr-proxy-linux-arm64` (Linux binaries, payload for the guests, never run on the Mac), `install.sh` (the same script as `scripts/install-release.sh`), `LICENSE` and `README.md`. `checksums.txt` lists that one archive (and the SBOM), so one checksum line and one attestation cover both the host and the guest binaries, built from the same commit. The first install needs only what stock macOS ships: `curl`, `tar`, `shasum`, `install` and `sudo`; no `gh`, no Homebrew; `whr setup host --only homebrew` then installs Homebrew after your confirmation (see [Prepare the Mac mini](host-setup.md), step 5; a clean Mac is {{< status unverified >}}). Replace `<tag>` (zsh reads a literal `<tag>` as a redirect) and run it as the administrator. The `install.sh` step uses `sudo`, so the account must be an administrator; on a Mac with one account that account is both the administrator and the one `whr` runs as (supported, not recommended):

```bash
cd "$(mktemp -d)"
tag=<tag>; base=https://github.com/wstein/workharbor/releases/download/$tag
f=whr_${tag#v}_darwin_arm64.tar.gz
curl -fsSLO "$base/$f" -O "$base/checksums.txt" &&
grep " $f\$" checksums.txt | shasum -a 256 -c -
```

The first block's last command prints the archive name and `OK`; any other output is a failure, do not go on.

If `gh` is installed and signed in (an upgrade: `whr setup` installs it), check who built the archive **before** `sudo ./install.sh`: the unpacked mode never checks the attestation, even when `gh` works, so without this an upgrade installs as root on the checksum and TLS alone. A first install has no `gh` yet: skip this block, it trusts the checksums and TLS, and can run the check afterwards. In a new shell, `cd` back into the folder that holds the archive and set `tag` and `f` again as in the first block before you run it.

```bash
commit=$(gh api repos/wstein/workharbor/commits/refs/tags/$tag --jq .sha) &&
gh attestation verify "$f" --repo wstein/workharbor \
  --signer-workflow wstein/workharbor/.github/workflows/release.yml \
  --source-ref refs/tags/$tag --source-digest "$commit" \
  --deny-self-hosted-runners
```

Then install:

```bash
tar -xzf "$f" &&
sudo ./install.sh "$tag"
```

`install.sh` unpacks the archive next to it (the one for its tag) and installs those files, and downloads nothing; the prefix is `/opt/whr` unless you add a path. It warns, and goes on, about an existing prefix directory (a symlink is judged by its target) that is group- or world-writable or that the user running it does not own (alpha policy, issues #493 and #504): whoever can write the prefix can replace the binary that root runs, so keep it writable by the administrator only.

The tag of `install.sh <tag>` names the version written to `<prefix>/libexec/whr/VERSION`. When `checksums.txt` sits next to `install.sh` (the flow above), the archive of that tag must sit there too and match its checksum line, or the script stops before it installs anything or writes `VERSION`, and it installs the files of that archive, not of a loose `bin/` beside it; without `checksums.txt` the tag cannot be checked, and the script says so. The unpacked mode checks no attestation: that is the `gh` check above. The script also runs piped as `cat install.sh | bash -s -- <tag>` from the unpacked directory (the tested form); `/bin/bash -s` piped is not tested and may fall into download mode. Measured: the script's tests (`go test ./scripts`: install from an unpacked archive without `gh`, the tag matched against the archive next to the script, `cat install.sh | bash -s` in an unpacked directory, checksum mismatch, downgrade, writable (warned) or symlinked prefix, a `gh` that cannot read the tag, the pasted blocks in stock zsh) and a GoReleaser snapshot (`make release-snapshot`) whose archive lists `bin/whr`, `guest/whr-*-linux-arm64` and `install.sh`. {{< status unverified >}} until a release carries it: the upload and the attestation of the single archive, the Homebrew formula built from it, and an install on a clean Apple-silicon Mac with no `gh`, Homebrew or Command Line Tools (#489).

**Next, as the administrator:** `/opt/whr/bin/whr setup host`. `/opt/whr/bin` is not on the administrator's `PATH` (the installer changes no shell file), so the administrator's commands here spell the full path: `/opt/whr/bin/whr version` shows the tag, and `/opt/whr/bin/whr setup host --only homebrew` installs Homebrew. If you would rather type `whr`, add `export PATH=/opt/whr/bin:$PATH` to the administrator's `~/.zprofile`. After `whr setup host`, `workharbor` runs `whr setup` in its desktop session ([Prepare the Mac mini](host-setup.md)). The order of a first install is: this archive, `/opt/whr/bin/whr setup host` as the administrator, then `whr setup` as `workharbor`.

### From a clone: `make install-release`

This route needs a clone, `git` and `make` (Command Line Tools); with `gh` signed in (`brew install gh`; a draft release, not yet published, can be downloaded only by a writer) it also verifies the attestation, and it is how you verify a draft release. Run it as the **administrator**; the first command uses the prefix `/opt/whr`, the second another one:

```bash
make install-release VERSION=<tag>
make install-release VERSION=<tag> PREFIX=/some/prefix
```

`gh` is optional: with it the script also verifies the attestation, without it only the checksums. A `gh` that is installed but cannot read the tag (not signed in, or no login under `sudo`) counts as absent: the script prints that and checks the checksums only. The fallback and the `grep ... | shasum` step are covered by script tests with stubbed tools; a run on a clean Mac (no `gh`, Homebrew or Command Line Tools) is {{< status unverified >}}. This single-archive script installs releases after `v0.1.0-alpha.4`; for `v0.1.0-alpha.4` and earlier, use the script of that tag (`git show <tag>:scripts/install-release.sh`). From a clone the script downloads the release archive (`whr_<version>_darwin_arm64.tar.gz`) and `checksums.txt`, checks the archive against the checksums and, when `gh` is present, against the build-provenance attestation of this repository's release workflow, and installs **nothing** unless every check passes. It refuses anything but macOS on Apple silicon. It reads the installed version from `<prefix>/libexec/whr/VERSION`, which it writes after a verified install, and never runs the installed `whr` before the checks; an older tag, or an install without that file, needs `--allow-downgrade` (`make install` from source removes that file, so the version after a source install is unknown) (`make install-release ... ALLOW_DOWNGRADE=1`). `WHR_RELEASE_REPO=owner/name` changes whose attestations are trusted (a fork); the script refuses it unless you also pass `--trust-release-repo` to `scripts/install-release.sh`.


### Verify a download yourself

The release signature is the keyless Sigstore build-provenance attestation the release job makes (D24): it binds each file to the release workflow, the tag and the tagged commit. The attestation bundle is attached to the release as `whr_<tag>.intoto.jsonl` (from `v0.1.0-alpha.3`; alpha.1 and alpha.2 have none, so use the online form for them; the second command below is the offline form with the attached bundle). The attestation covers the archive and the SBOM (alpha.4 also the loose `install-release.sh`), all listed in `checksums.txt`, not `checksums.txt` itself: verify an archive, and check `checksums.txt` only with `shasum`. Pin the workflow, the tag and the commit, not just the repository: without `--source-ref` and `--source-digest`, an older release's archive with its own `checksums.txt` passes (a downgrade).

```bash
tag=v0.1.0-alpha.3
file=whr_0.1.0-alpha.3_darwin_arm64.tar.gz
commit=$(gh api repos/wstein/workharbor/commits/refs/tags/$tag --jq .sha)
gh attestation verify "$file" --repo wstein/workharbor \
  --signer-workflow wstein/workharbor/.github/workflows/release.yml \
  --source-ref refs/tags/$tag --source-digest "$commit" \
  --deny-self-hosted-runners
gh attestation verify "$file" --repo wstein/workharbor --bundle whr_$tag.intoto.jsonl \
  --signer-workflow wstein/workharbor/.github/workflows/release.yml \
  --source-ref refs/tags/$tag --source-digest "$commit" \
  --deny-self-hosted-runners
```


Also check the file against `checksums.txt` (`shasum -a 256 -c`). `make install-release` does both. {{< status verified >}} on 2026-10-08 with `tag=v0.1.0-alpha.3` and `whr_0.1.0-alpha.3_darwin_arm64.tar.gz`: both forms above exit 0 (the repository is public, so the public Sigstore instance applies and the offline form needs no extra trust root), and the same command on `checksums.txt` fails with HTTP 404 because no attestation exists for its digest. Whether OpenSSF Scorecard counts the attached bundle as a signature is {{< status unverified >}} (#180).

Then, as `workharbor`, build the tool store with the guest launcher (the script prints the exact command):

```bash
store=/Users/workharbor/tools
/opt/whr/bin/whr tools build -store "$store" -shim /opt/whr/libexec/whr/whr-shim-linux-arm64
```

### Development installation from source

{{< status decided >}} Werner's development-source exception (issue #299) lets
ordinary `make install` build from the current local `main`, including a
commit not yet published to `origin/main`. No extra install target or development
flag is required. Before installing, obtain independent review of the exact
commit you will build. The installer expects `HEAD` to equal `refs/heads/main`
exactly and warns about a dirty tree or any different commit, including an older
`main` commit, a topic commit or one ahead of local `main` (alpha policy, issue #504). A detached checkout
or a differently named branch at the identical current `main` commit passes;
the branch label does not change the code being installed. Equality with a local
ref cannot prove that a review took place. The
installer neither fetches nor updates a remote-tracking ref to pass this check.
This source route is for a developer's machine. The managed dogfood and reference
host retain the signed release route above; a development install is not evidence
that either host is ready.

`make install` builds each binary into a temporary directory next to its destination, signs the macOS `whr` ad hoc (`codesign --force --sign -`, macOS only) and moves it into place by rename, so a running or cached binary is never overwritten in place. `whr-shim` and `whr-proxy` are Linux files and are renamed, not signed. It ends by running `whr version` and fails with a message naming `codesign -v` and `xattr -l` if that does not run. Whether an invalid signature after an in-place overwrite is what killed `whr` on the real host is {{< status unverified >}} until the output of those two commands is known (issue #393); see [Troubleshooting](troubleshooting.md).

`make install` defaults to `$HOME/.local`. An explicit `PREFIX=/absolute/path`
chooses another prefix. The prefix must already exist, be a directory and be
writable by you. The installer refuses `/`, your home or a directory above your
home, the source checkout being built from and Git metadata. It resolves symlinks
and the nearest existing parent before creating a missing prefix, and checks the
actual binary destinations too: an existing symlink must not redirect a write
elsewhere, and a destination must be a regular single-link file or a directory.
Alpha policy (issue #493): where the prefix lies and who owns it do not refuse
the install. A prefix in `/opt/whr`, `/opt/homebrew` or `/usr/local`, in another
Git working tree, or not owned by you or open to group and other writers prints
a warning on standard error and goes on; this is revisited at beta
{{< status unverified >}} on a real host. A dirty tree or a `HEAD` that is not the
local `main` prints a warning too (issue #504): what gets installed is then not a
reviewed commit. Root must still use `make install-release`.
`make install DESTDIR=/absolute/stage` stages the install: every file is written
under `$DESTDIR$PREFIX`, and nothing is written at the real `PREFIX`. `DESTDIR`
must be an existing absolute, clean directory path (no trailing slash, no `.`
or `..`, no newline, carriage return or tab) and not `/`. A symlink in any
existing component between `DESTDIR` and `DESTDIR/PREFIX` is refused, because
`mkdir -p` would follow it out of the stage. `PREFIX=/` with a `DESTDIR` stages
to `$DESTDIR/bin` and `$DESTDIR/libexec/whr`. The checks above apply to `PREFIX`
itself and, for existence and writability, to the staged location, or to the
deepest existing directory above it when `mkdir -p` has to create the prefix.
`DESTDIR` is never embedded in a binary or in the messages, which name `PREFIX`
only. Unset or empty `DESTDIR` installs straight to `PREFIX`, as before.
`make install-release` does not take `DESTDIR`.
A refused destination is an error, never a fallback to another prefix.

The source installer builds all three binaries with `GOWORK=off` and empty
`GOFLAGS`, stamps the source commit, and removes the release-only
`libexec/whr/VERSION` marker. These are developer-built binaries without release
provenance. Implementation and live installation of the amended source gate are
{{< status unverified >}} until a run on the reference host records its
evidence.

There is no development mode: `whr setup`, `whr doctor` and `whr service` take
no `--dev` or `--managed` flag, and no environment variable or configuration key
selects an installation. A `development_prefix` key left in an older
`config.json` is accepted and ignored. Use the same installation the usual way,
with `--prefix` when it is not `/opt/whr`
(provisional; {{< status unverified >}} on the reference host):

```bash
whr setup --prefix "$HOME/.local" --user "$USER" --only config-base --dry-run
whr setup --prefix "$HOME/.local" --user "$USER" --only config-base
whr doctor --user "$USER"
```

`whr setup host --user "$USER"` creates this configuration itself as its first step (`config-first`, the same code as `config-base`, issue #394), so the commands above are needed first only to run the user part alone or to preview it. Without `--user`, `workharbor` is the account and the administrator does not write its configuration: the steps that read it are reported as not reachable, with the command to run as `workharbor`.

`config-base` writes the usual `~/.config/whr/config.json`; it does not install
or start a service. Remove `--only config-base` to run the other setup steps.

Alpha policy (issue #493): `whr` runs from wherever it lies, also from a Git
working tree, a directory you can write or a prefix the account owns. Setup,
`whr service install` and `whr offboard` refuse only root and a file that is not
an executable regular file after symbolic links. `whr doctor` reports where
`whr` runs from, and who owns or can write the prefix, as one `warn` that does not change
the exit code; a supervisor the account can replace lacks the replacement
protection of an administrator-owned prefix, and that weaker point is accepted
for the dogfood account and revisited at beta. `whr serve` looks for the guest
helpers next to the running binary, in `<dir>/../libexec/whr`, so a `whr`
copied alone does not start: the doctor looks there and reports the tool-store
step as not reachable until they are in place.

When setup reaches `service-install`, it runs `whr service install --config <file>`
with the running executable. The LaunchAgent retains that executable path, so
stopping and starting it needs no flag. The default production procedure remains
the administrator-owned release installation above.


## From `v0.1.0`: the tap

A published, non-prerelease release updates the Homebrew tap, so the tap never points at a draft:

```bash
brew install wstein/tap/whr
```

The formula installs `whr`, the guest binaries in `libexec/whr` and the shell completions. The formula route is {{< status unverified >}} until #62 installs it. The host's Homebrew packages are pinned and not upgraded unasked (`HOMEBREW_NO_AUTO_UPDATE=1`, see [Prepare the Mac mini](host-setup.md)); upgrade `whr` on purpose.

`brew upgrade whr` is not blocked by `HOMEBREW_NO_INSTALL_UPGRADE`: Homebrew's manual says that variable only stops `brew install` from upgrading an installed formula. `brew pin` does hold a formula through `brew upgrade`, so if `whr` was pinned, run `brew unpin whr` first and pin it again afterwards (the manual page of `brew`; not run against a `whr` formula, {{< status unverified >}}).

The formula has `depends_on arch: :arm64`, so on an Intel Mac Homebrew stops with its own message about the unsupported architecture ({{< status unverified >}}: read from the formula, not run). Release 1 supports Apple silicon only.

## Back up, upgrade and restore

One procedure serves all three: stop, copy or replace, start. Run it as the `workharbor` user, from its desktop session ([Prepare the Mac mini](host-setup.md), step 2). The paths below are the defaults; where the configuration sets `state_dir` or other roots, use those.

### What to copy

| What | Where | Rule |
| --- | --- | --- |
| Configuration directory | `~/.config/whr` (the configuration file) | A plain copy. It also holds secret files by default (below). |
| State directory | `~/.local/state/whr`, or `state_dir` | Holds the database `workharbor.db` **with** `workharbor.db-wal` and `workharbor.db-shm`. The database runs in WAL mode, so a copy of `workharbor.db` alone, or of any of the three taken while `whr serve` runs, can be inconsistent. Copy all three with `whr serve` stopped. The audit log is stored with the database; whether a separate audit file exists is {{< status unverified >}}. `api.sock` in the same directory is a socket: skip it. The same directory holds `topics` (the supervisor's own repositories: the commits the supervisor has imported from an agent's exported bundle, D42, [design §4.5](../design/domain.md); commits the agent made after the last export are not in it), `mirrors` (the cache of the forge repositories: rebuildable, can be large, so skipping it is fine) and the editor copies (`open`, the copies you open in your editor). Back up `topics` with the database. Whether to keep the editor copies is your call ({{< status unverified >}}: their directory name is read from the code, not from a restore). |
| Secret files | Every file the configuration names: `api_token_file`, the `token_file` of each `api_clients` entry, `github.key_file`, `agent_api_key_env_file`, `bot_signing_key_file`, `console.ssh_ca_key_file`, the `ntfy` files | **Only into an encrypted backup** (a FileVault volume or an encrypted disk image). They may live outside `~/.config/whr`, so read the paths from the configuration. Keep mode `0600` when you restore them. |
| Workspace folders | Every folder below `roots.workspaces` | The agents' working folders and their session state. An agent's commits live in the workspace's agent clone until the supervisor exports them as a bundle into `topics` (above), so these folders hold the unexported commits as well as uncommitted changes and the session state; `topics` alone can miss committed work. Restore them from the same backup as the database, so session and database agree ({{< status unverified >}}). |
| Tool store | `roots.tool_store` | **Not copied.** Rebuildable with `whr tools build`, and `whr serve` verifies it at start against the hashes recorded when it was built ([design §5.6](../design/architecture.md)); it refuses to start when it does not verify. |
| Not copied | The agent-home volumes and container images | **Exclude them from every backup.** An agent-home volume holds the agents' sessions and the subscription logins (D40); no `whr` command copies one, and you should not either ([design §7.3](../design/security.md)). Images are rebuilt. |

### The procedure

1. **Stop the supervisor.** `whr service uninstall` unloads the job (the logs stay); if you run `whr serve` by hand, stop that. Check that no `whr serve` process is left. Stopping the supervisor does not stop the environments: an agent can keep running in its environment while `whr serve` is down, so the workspace folders and `topics` may change under your copy ([design §4.1](../design/domain.md)). Stop the environments first (`whr` has no single command for it in this draft; stop each workspace's environment, {{< status unverified >}}), or accept that the copy may be mid-change. Runs resume after the start ({{< status unverified >}}).
2. **Back up** every path in the table, to an encrypted destination for the secret files. Prefer a manual copy of the paths above while the supervisor is stopped. Time Machine cannot be timed to a stopped supervisor, and whether its snapshot covers the database and its two WAL files at one instant is not measured ({{< status unverified >}}).
3. **Upgrade (skip for a plain backup).** Read the release notes and the upgrade notes below, then install the new version as the administrator: the archive block of [Install from the release archive](#install-from-the-release-archive-the-first-install) with the new tag (the tap never gets a prerelease), `make install-release` from a clone, or `brew upgrade whr` from `v0.1.0` on.
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

## Uninstall

There is no `make uninstall` target and no `whr uninstall` command; removal is by hand, in this order. Run the first steps as `workharbor` (or the account that runs `whr`, `--user` in a development setup) and the prefix removal as the administrator.
{{< status unverified >}} No uninstall has been run against a release; each path below is read from the code.

### What is installed

| Route | Files placed |
| --- | --- |
| `make install-release` (default `PREFIX=/opt/whr`) | `<prefix>/bin/whr`, `<prefix>/libexec/whr/whr-shim-linux-arm64`, `<prefix>/libexec/whr/whr-proxy-linux-arm64` and the marker `<prefix>/libexec/whr/VERSION` (`scripts/install-release.sh`). |
| `make install` (default `PREFIX=$HOME/.local`) | The same three binaries; it deletes `libexec/whr/VERSION` instead of writing it (`Makefile`). |
| `brew install wstein/tap/whr` | `bin/whr`, `libexec/whr/whr-shim-linux-arm64` and `libexec/whr/whr-proxy-linux-arm64` in the formula's Cellar, linked under the Homebrew prefix, plus the shell completions (`scripts/homebrew-formula.sh`). |

The installers create no other file: no launchd job, no configuration, no state.

### What stays behind

Removing the binaries leaves all of this, because `whr` keeps it outside the prefix (paths are the defaults; the configuration may set others, see [Back up, upgrade and restore](#back-up-upgrade-and-restore)):

- the launchd job `~/Library/LaunchAgents/io.github.wstein.workharbor.plist` and its logs in `~/Library/Logs/whr/` (`whr.out.log`, `whr.err.log`, never rotated);
- the configuration directory `~/.config/whr` (with `config.json.bak` after a setup run) and every secret file the configuration names (API token, GitHub App key, signing keys, ntfy files);
- the state directory `~/.local/state/whr`, or `state_dir`: the database `workharbor.db` with `-wal` and `-shm`, `topics`, the API socket;
- the workspace folders below `roots.workspaces`, the tool store (`roots.tool_store`), and the agent-home volumes and container images of Apple Container, which hold the agents' vendor logins ([design §7.3](../design/security.md));
- the `workharbor` account and the host settings of [Prepare the Mac mini](host-setup.md).

### Order

1. **Back up anything you want to keep**, as in [Back up, upgrade and restore](#back-up-upgrade-and-restore). Removal below is not undoable.
2. **Stop the service**: `whr service uninstall` unloads the job and removes the plist; the logs stay. Stop a hand-run `whr serve` too, and check that no `whr serve` process is left. This must come first: with the binary gone, launchd restarts a failing job at most every 30 seconds ([Run the supervisor](run-the-supervisor.md)). Stop the workspace environments as well; `whr` has no single command for that in this draft ({{< status unverified >}}).
3. **Delete the data you no longer want**, as `workharbor`: `rm -r ~/.local/state/whr ~/.config/whr ~/Library/Logs/whr`, the secret files the configuration named if they lie elsewhere, and the workspace folders (they hold unexported agent commits). Removing the agent-home volumes and images uses Apple Container's own commands ({{< status unverified >}}: `whr` has no command for it).
4. **Remove the binaries**, as the administrator. Homebrew: `brew uninstall whr` (unpin first with `brew unpin whr` if it was pinned), then `brew untap wstein/tap` if you no longer want the tap. Release or source install: `rm -r <prefix>/bin/whr <prefix>/libexec/whr`, then the prefix directory itself if it is empty (`/opt/whr` is created by the installer).
5. **Remove the account, optionally**: [Remove the WorkHarbor account](host-setup.md#remove-the-workharbor-account) (`whr offboard host`, a dry run unless `--delete`). It deletes the account and its home folder, so run it after step 3 or after the backup. It does not unload launchd jobs or touch the prefix or workspace volumes, so do steps 2 and 4 first.

## Cut a release (the maintainer)

Only a human tags, signs and publishes (D24, §6); an agent never does.

0. Write the release summary `docs/releases/vX.Y.Z.md` from `docs/releases/TEMPLATE.md` (4-6 lines: highlights, what users can do now, what is known broken or unverified, how to verify provenance; then the `## Install` block) and commit it on `main`, reviewed like any file. The release workflow prepends it to the generated lists. If the file is missing or empty the release job fails before it builds anything; the tag cannot be moved, so fix `main` and create a new tag. The generated lists (features, bug fixes, ...) are folded in `<details>` with counts.
1. `make release-prep VERSION=vX.Y.Z` regenerates `CHANGELOG.md` and commits it as `chore(release)`. It does not tag. It accepts any version tag, a prerelease such as `v0.1.0-alpha.5` included, and needs a clean tree. The release workflow does not read `CHANGELOG.md` (it runs git-cliff itself for the notes), so the step is not a gate for an alpha; no `chore(release)` commit exists in the history and the committed `CHANGELOG.md` still has only an `Unreleased` section, so none of `v0.1.0-alpha.1` to `.4` used it.
2. After CI is green on that commit of `main`, push a **signed, annotated** tag `vX.Y.Z`. The release workflow checks the signature against `.github/release-signers`, that the commit is on `main` and that CI passed, then builds into a **draft**: one archive (`whr`, the guest binaries and `install.sh`), `checksums.txt`, an SBOM, a build-provenance attestation and its bundle (`whr_<tag>.intoto.jsonl`).

    Sign with the signing key, the one whose public half is in `.github/release-signers`, not with your login key, and check the tag locally before you push it:

    ```sh
    git -c gpg.format=ssh -c user.signingkey=$HOME/.ssh/id_ed25519_signing.pub \
      tag -s vX.Y.Z -m vX.Y.Z <commit>
    git -c gpg.format=ssh -c gpg.ssh.allowedSignersFile=.github/release-signers \
      tag -v vX.Y.Z
    ```

    The error `No principal matched` means a key outside `.github/release-signers` signed the tag. The workflow reads `.github/release-signers` from the **tagged** commit, so a tag that failed stays failed: fix the cause on `main`, wait for green CI, and create a **new** tag on the fixed commit. Only a human tags; an agent never does.
3. Check the draft: install it on your own prefix with `make install-release VERSION=vX.Y.Z`, which verifies the checksums and the attestation, and read the notes.
4. Publish it. For a release (not a prerelease) the `tap` workflow renders the formula and pushes it to `wstein/homebrew-tap`; a prerelease never updates the tap.

### Release profile

The values a release checklist (or the `crewbook-release` lane) needs, each read from the file named:

| Item | Value |
| --- | --- |
| Previous tag | the highest `v0.1.0-alpha.N` below the new tag (a rule for the alphas; `v0.1.0` and later need their own, not decided here) |
| Notes | `docs/releases/<tag>.md`, from `docs/releases/TEMPLATE.md`; the install block equals the template's apart from the tag |
| Signer file | `.github/release-signers`, read from the tagged commit |
| Workflow | `.github/workflows/release.yml` (draft release; the `tap` workflow runs only for a published non-prerelease) |
| Archive | `whr_<version>_darwin_arm64.tar.gz` (`<version>` is the tag without `v`), with `bin/whr`, `guest/` and `install.sh`; next to `checksums.txt` and the SBOM `whr_<version>_sbom.cdx.json` |
| Provenance | `whr_<tag>.intoto.jsonl`, attached to the draft |

Rules: the job fails before building if `docs/releases/<tag>.md` is missing or empty; the tag is annotated, signed by a key in the tagged commit's signer file, on `main` with CI green on that commit; a prerelease never updates the tap; a failed tag stays failed and needs a new tag.

The notes of `v0.1.0-alpha.5`, the first release built with the single archive, must say under **Known limits** that the upload and attestation of the single archive, the Homebrew formula built from it and the install on a clean Apple-silicon Mac are unverified; the markers on this page stay {{< status unverified >}} until a release has carried them.

### Set up the tap once (the maintainer)

The `tap` workflow pushes the formula with a deploy key, which issue #62 installs. {{< status unverified >}} until #62 is done, and the details below come from `.github/workflows/tap.yml`, not from a run:

- The private half is the secret `TAP_DEPLOY_KEY` in the `homebrew-tap` environment of `wstein/workharbor`, so only that workflow job can read it.
- The public half is a deploy key with write access on `wstein/homebrew-tap` and on no other repository.
- To rotate it, generate a new key pair, add the new public key to `wstein/homebrew-tap`, replace the secret, run the workflow once, then delete the old deploy key. Never put the private key in a file in the repository or in a chat.

`make release-snapshot` builds the artifacts locally into `dist/` without publishing, to test the pipeline.

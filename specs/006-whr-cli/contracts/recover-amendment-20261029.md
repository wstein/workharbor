# Contract amendment: whr workspace recover (2026-10-29)

Owner direction: a recovery container that carries the workspace home and a
BundleDock toolbox, so that sessions and files can be rescued from a broken or
retired workspace without a development environment.

## Command

```text
whr workspace recover --local <exact-name|full-id> [--toolbox latest|<tag>|current|<volume-id>] [--rw] [--net] [--dry-run]
```

Command ID `workspace.recover`. Terminal-only: `--json` is a usage error, and
a run without `--dry-run` needs terminal stdin and stdout.

## Behavior

1. The workspace is selected like `home clone` (exact name or full ID, verified
   ownership, home volume from the container mounts).
2. The base image `whr-recover:<hash>` (Alpine, git, openssh-client, less, ncdu,
   rsync, tar, zstd, jq) is built from an embedded Dockerfile on first use. The
   tag is the hash of the Dockerfile. The build needs network once.
3. The toolbox comes from `--toolbox`:
   - `latest` (default) or a tag: `quay.io/bundledock/toolbox:<tag>` is pulled and
     a `bundledock-toolbox-<image id>` volume is created and filled once, like
     BundleDock's `bundle-volume.sh`;
   - `current`: the toolbox volume mounted in the selected workspace;
   - a hex prefix or full name: a local `bundledock-toolbox-*` volume.
   On a terminal without `--toolbox` the command asks, and Enter takes `latest`.
4. One `docker run --rm --interactive --tty` starts the container: home at
   `/recover/home` (read-only unless `--rw`), toolbox read-only at
   `/opt/bdock/toolbox`, network none unless `--net`, read-only root file system,
   `/tmp` as tmpfs, all capabilities dropped except CHOWN, DAC_OVERRIDE, FOWNER
   and FSETID, no-new-privileges. The shell is the toolbox `fish`, else `sh`.
5. The exit status of the shell is the exit status of the command.

## Safety

Only fixed argument vectors are admitted. Nothing is deleted automatically: an
incomplete toolbox volume is reported as a residual with the command to remove
it. `--dry-run` has no pull, build, volume or container effect.

## Not in scope

Mounting further bundles (claude, codex, node) into the container; a
`--with` option is a possible follow-up.

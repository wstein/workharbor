# Contract amendment: whr workspace recover (2026-10-29)

Owner direction: a recovery container that carries the workspace home, the
BundleDock toolbox and nixbox, so that sessions and files can be rescued from a
broken or retired workspace without a development environment.

Revised 2026-10-30: no image is built and no package manager runs. The container
is the stock pinned Debian plus the two mounted bundles.

## Command

```text
whr workspace recover --local <exact-name|full-id> [--toolbox <choice>] [--nixbox <choice>] [--rw] [--net] [--dry-run]
```

`<choice>` is `latest` (default), a tag, `sha256:<digest>`, `current` (the volume
mounted in the selected workspace) or a local volume id (hex prefix or full name).

Command ID `workspace.recover`. Terminal-only: `--json` is a usage error, and
a run without `--dry-run` needs terminal stdin and stdout.

## Behavior

1. The workspace is selected like `home clone` (exact name or full ID, verified
   ownership, home volume from the container mounts).
2. The image is the stock
   `docker.io/library/debian@sha256:f6b08b09a05725f9431502d3b5714f5c4576e82c1a84fa1debe0c24a7e0c3ec4`
   (trixie-slim, so the glibc bundles run). It is pulled once when missing and never
   built. No `apt`, no package manager and no Dockerfile run in the recovery.
3. Each bundle comes from its flag:
   - `latest` (default), a tag or a digest: `quay.io/bundledock/toolbox` or
     `quay.io/bundledock/nixbox` is pulled and a `bundledock-<name>-<image id>`
     volume is created and filled once by the carrier image (offline, as root, the
     volume at `/bundle`), like BundleDock's `bundle-volume.sh`;
   - `current`: the volume mounted in the selected workspace (`/opt/bdock/toolbox`
     or `/nix`);
   - a hex prefix or full name: a local `bundledock-<name>-*` volume.
   On a terminal without `--toolbox` the command asks for the toolbox, and Enter
   takes `latest`. nixbox is not asked; `--nixbox` selects it.
4. One `docker run --rm --interactive --tty` starts the container: home at
   `/recover/home` (read-only unless `--rw`), toolbox read-only at
   `/opt/bdock/toolbox`, nixbox read-only at `/nix`, network none unless `--net`,
   read-only root file system, `/tmp` as tmpfs, all capabilities dropped except
   CHOWN, DAC_OVERRIDE, FOWNER and FSETID, no-new-privileges. Environment:
   `PATH=/nix/profile/bin:/opt/bdock/toolbox/bin:<system>`,
   `BDOCK_TOOLBOX=/opt/bdock/toolbox`,
   `SSL_CERT_FILE=/nix/profile/etc/ssl/certs/ca-bundle.crt`. Tools: nixbox gives git
   (no Perl), rsync, zstd, less, ssh and CA certificates; the toolbox gives jq, rg, jj,
   fastfetch (through nixbox shims) and its other tools; Debian gives tar, find and the
   base utilities. The shell is the toolbox `fish`, else `sh`.
5. The exit status of the shell is the exit status of the command.

## Safety

Only fixed argument vectors are admitted: the pulls (the pinned Debian, toolbox and
nixbox references only), the volume creates (labels `workharbor.recover=toolbox|nixbox`,
matching names only), the populate runs and the one interactive run; everything else
is rejected. Nothing is deleted automatically: an incomplete volume is reported as
a residual with the command to remove it. `--dry-run` lists the planned pulls and
volumes and has no pull, volume or container effect.

## Not in scope

Mounting further bundles (claude, codex, node) into the container; a
`--with` option is a possible follow-up.

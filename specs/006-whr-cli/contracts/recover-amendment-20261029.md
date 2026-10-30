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
mounted in the selected workspace) or a local volume id (full name, or a hex prefix
of 12 or more characters or with a letter a-f; a short all-digit string such as the
date tag `20261029` is a tag).

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
     volume is created and filled by the carrier image (offline, the volume at
     `/bundle`, no `--user`, so the image's default user, which is root in the
     published toolbox image), like BundleDock's `bundle-volume.sh`. The carrier
     writes `.ready` last and holds `.initializing` while it runs (it removes it after
     an ordinary failure, so a partial fill has neither marker; it stays after a hard
     kill or while another run is in progress). An existing volume is reused only
     after a read-only offline probe in the pinned Debian image finds `.ready` and no
     `.initializing`. Otherwise the carrier runs once more; it fills an empty volume or
     re-verifies a ready one, but refuses a nonempty or locked volume and never repairs
     a partly filled one. In that case the volume is reported as a residual that may be
     incomplete or still being initialized (a concurrent carrier run looks the same),
     exit 73, and is never mounted; the owner removes it (docker refuses to remove a
     volume that is in use) and runs recover again;
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
   `SSL_CERT_FILE=/nix/profile/etc/ssl/certs/ca-bundle.crt`,
   `XDG_DATA_DIRS=/opt/bdock/toolbox/share:/nix/profile/share:/usr/share`. Tools: nixbox gives git
   (no Perl), rsync, zstd, less, ssh and CA certificates; the toolbox gives jq, rg, jj,
   fastfetch (through nixbox shims) and its other tools; Debian gives tar, find and the
   base utilities. The shell is the toolbox `fish` when `fish -c true` succeeds, else `sh`.
5. The exit status of the shell is the exit status of the command.

## Safety

Only fixed argument vectors are admitted: the pulls (the pinned Debian, toolbox and
nixbox references only), the volume creates (labels `workharbor.recover=toolbox|nixbox`,
matching names only), the populate runs, the read-only completeness probe and the one interactive run; everything else
is rejected. Nothing is deleted automatically: a volume that may be incomplete is
reported as a residual with the command to remove it. `--dry-run` lists the planned pulls and
volumes and has no pull, volume or container effect.

## Not in scope

Mounting further bundles (claude, codex, node) into the container; a
`--with` option is a possible follow-up.

# Home clone and backup amendment

Status: **DECIDED by the owner, 2026-10-29** (scope relayed to the implementing
agent in the session request of that date, revised the same day: the helper is
a pinned minimal base image running `tar`, not an rsync image). This document
records that decision and the implementation contract. It claims no real-host
acceptance: every test of this increment uses fake Docker executables.

## Owner decision and precedence

An owner wants Claude/Codex sessions and other work in a workspace home to
continue in a new container without copying large caches. Approved scope, and
only this scope:

1. `whr workspace home clone` and `whr workspace home backup`, copying an
   existing workspace home volume into a **new** volume with `tar` inside a
   digest-pinned minimal base image.
2. An additive optional `workharbor.json` field pinning that base image by
   digest, written by `whr workspace init` only when explicitly given.
3. Documentation replacing the "no built-in backup" statements that this
   amendment supersedes.

For **workspace home volumes**, this supersedes the sentence of the
[throwaway staging amendment](throwaway-staging-amendment-20261025.md) and of
[local-stage-promote.md](local-stage-promote.md) that there is no
volume-backup feature: `home backup` is that feature. The throwaway amendment
otherwise stays unchanged and authoritative: staging remains throwaway, there
is still no keep flag, promotion and removal still dispose of every candidate
artifact, and neither command adds an automatic backup to stage, promote or
`rm`. The complete `anchorage-home-*` guard, FR-067 protections, D1/D2,
fingerprint trust and original-home preservation stay decided.

**Out of scope** for this increment: any change to stage, promote, `rm`,
candidate adoption (no `adopt-candidate`), restore into an existing volume,
starting a container on the new volume, deleting or modifying the source,
building or publishing an image, and any remote/control-plane form.

## Interaction

Canonical forms, with the existing `ws` group alias and no short forms:

```text
whr workspace home clone  --local [--to <new-volume>] [--exclude <path>]... [--include-credentials] [--dry-run] [--yes] [--trust <fingerprint>] <id|name>
whr workspace home backup --local [--to <new-volume>] [--dry-run] [--yes] [--trust <fingerprint>] <id|name>
```

Canonical command IDs are `workspace.home.clone` and `workspace.home.backup`.
`--local` is required and boolean. Exactly one positional selector: the exact
container name or full 64-hex container ID of a recognized workspace container
(FR-066 roles primary, staging or temp, running or stopped). No prefix, label or
role batch selection. The selected container must have a home volume mount
(`/home/captain`, else `/home/vscode`, as discovery reports it).

| Mode | Default excludes | Credentials | Extra `--exclude` |
| --- | --- | --- | --- |
| clone | `./.cache` and the fixed credential list below | excluded unless `--include-credentials` | allowed, repeatable |
| backup | `./.cache` only | included (it is a backup) | not offered |

Fixed credential list (clone): `./.claude.json`, `./.claude/.credentials.json`,
`./.codex/auth.json`, `./.config/gh`, `./.docker/config.json`,
`./.git-credentials`, `./.gnupg`, `./.netrc`, `./.ssh`. `./.cache` cannot be
re-included in this increment.

An `--exclude` value names a path relative to the home root, written `x/y`,
`./x/y` or `/x/y`; all three canonicalize to `./x/y`. It has no empty, `.` or
`..` component after the prefix, no trailing `/`, at most 256 bytes, and only
the characters `A-Z a-z 0-9 . _ - + @ /`; no wildcard, option or filter syntax.
At most 32 user excludes. Values are deduplicated and sorted with the defaults.
A violation is 64 before any I/O.

`--to` names the target volume. It must match the local volume-name grammar
(`[A-Za-z0-9][A-Za-z0-9_.-]*`, at most 128 bytes; otherwise 64), must not start
with the protected prefixes `anchorage-home-`, `whr-stage-home-` or
`bundledock-`, and must not exist on the admitted daemon (68). The default is
`whr-home-<mode>-<container-name>-<n>` with the smallest `n >= 1` that does not
exist, so the same daemon state yields the same name in dry-run and apply. A
container name that is not a valid volume-name fragment refuses 68 and asks
for `--to`.

## Helper base image pin

`workharbor.json` (schema_version 2 only) gains the optional field:

```json
"helper_images": { "base": "docker.io/library/busybox@sha256:<64 hex>" }
```

`helper_images` is a closed object; `base` is its only member. The value is a
minimal base image providing `/bin/sh`, `tar` and `find` (for example busybox or
alpine). It **must** end in `@sha256:<64 lowercase hex>` (an optional tag may
precede the digest), at most 512 bytes. The lowercase repository path needs at
least one `/` (write `docker.io/library/<name>` rather than a bare Docker Hub
name), so a value can never be read as a Docker option. Anything else is the
existing `manifest.schema` finding. The field is absent unless the owner sets
it. schema_version 1 refuses it (`needs schema_version 2`). The workspace's own
local image ID would be an acceptable alternative pin; it is not admitted in
this increment.

`whr workspace init [dir] --helper-base-image <ref>` validates the reference,
shows it in the normal preview (setting `helper_images` with origin `flag`), and
writes it only through the existing init consent: dry-run, `--yes` or the
interactive confirmation, and `--overwrite workharbor.json` for an existing
manifest. An existing pin is preserved when other settings change. Init never
pulls, inspects or runs the image.

## Preview, trust and confirmation

Resolution and refusal before any effect, in this order:

1. Bad flags/arguments: 64. Missing `--local`: 64.
2. Admitted local Unix endpoint (FR-066); five-second budget for all reads.
3. Selector: no match 65, ambiguous 64, unrecognized/foreign container or no
   home volume 68.
4. Helper pin: read `workharbor.json` from the selected container's
   `devcontainer.local_folder` label through the bounded no-follow reader. A
   missing label/manifest, manifest findings, or an absent or non-digest pin
   refuses **68** with init remediation. Nothing is pulled.
5. Target: grammar 64; protected prefix, existing volume or source collision 68.

The preview (dry-run and before confirmation) shows: mode, source container
ID/name/role, source volume, target volume, the helper image reference with its
digest, the sorted excludes, whether credentials are included, an estimated
size and the fingerprint. The estimate is the source volume size the daemon
reports through `docker system df --verbose` before excludes; it is `null`
when the daemon does not report one, which is not an error. The preview also
says to pause writers in the source (agents, editors) for a consistent copy;
the source is never stopped by `whr`.

The fingerprint is `sha256:` plus the hex SHA-256 of a canonical JSON document
binding the endpoint, mode, source container ID, source volume, target volume,
helper reference, excludes, credential choice and the fixed copy template.
Dry-run needs neither trust nor consent and reports the fingerprint for a later
apply.

Consent follows the [stage contract](local-stage-promote.md#source-trust-and-confirmation):

| Case | Exit |
| --- | --- |
| Unattended/JSON apply without `--yes` | 70 |
| Unattended/JSON apply without `--trust <fingerprint>` (`--yes` alone grants nothing) | 70 |
| Interactive fingerprint or `y/N` confirmation declined | 70 |
| `--trust` differs from the freshly computed fingerprint, or the plan changed since preview | 68 |
| Interrupt/cancellation before the first effect | 72 |

## Apply and protections

Apply re-resolves the endpoint, selection, pin and target, recomputes the
fingerprint and refuses 68 on any difference. Effects, in order, all through
fixed admitted Docker argv with the private empty Docker configuration:

1. `image pull --quiet -- <ref@digest>`. A failed pull refuses 68 with no volume
   created (cached layers may remain; they are not workspace resources).
2. `volume create --label workharbor.home-copy=<mode> --label
   workharbor.home-copy.operation=<op> --label workharbor.home-copy.source=<source> -- <target>`,
   where `<op>` is 16 random hex digits. Ownership is proved by reading back the
   operation label (`volume inspect --format {{json .Labels}} -- <target>`); a
   pre-existing volume of that name never carries it, so a race refuses 68
   without touching it.
3. The copy, the only writing helper run (common prefix `H`):

   ```text
   H = docker run --rm --pull never --network none --read-only
         --security-opt no-new-privileges --cap-drop ALL
         --cap-add CHOWN --cap-add DAC_OVERRIDE --cap-add FOWNER --cap-add FSETID
         --user 0:0 --name whr-home-<copy|verify>-<op> --label workharbor.home-copy.operation=<op>
   H -v <source>:/from:ro -v <target>:/to --entrypoint /bin/sh <ref@digest>
     -c 'set -eu -o pipefail; cd /from; tar -cpf - --numeric-owner "$@" . | tar -xpf - --numeric-owner -C /to'
     whr-home-copy --exclude=./<path>...
   ```

4. Verification, a second helper run with **both** volumes read-only
   (`-v <target>:/to:ro`), the same prefix and image, and a fixed `find`
   template counting regular files, directories and symbolic links on each side
   with the same excludes pruned, plus `.claude/projects/**/*.jsonl` and
   `.codex/sessions/**/*.jsonl` when present. Its exclude arguments are the
   relative paths (`x/y`), again positional.

The shell templates are constants. Every exclude reaches the shell only as a
positional argument after the fixed `$0` name and is expanded as `"$@"` or a
quoted variable; nothing is interpolated into the script. The admission
function accepts only these vectors with validated volume names, digest-pinned
reference, operation ID and exclude grammar; any other argv is refused before
a process starts.

The source volume is mounted read-only in every helper run and is never a
`volume create` target. **Nothing is deleted automatically**: after a failure
the only new resource is the operation's target volume (and, after an
interruption, possibly a still-running `whr-home-copy-<op>` helper container
that `--rm` removes when it exits). Both are reported for the owner to inspect
and remove. Existing volumes, primary homes, `whr-stage-home-*`,
`anchorage-home-*` and `bundledock-*` volumes are never targets.

## Result and exit codes

JSON uses the existing envelope with closed result `localHomeCopyResult` in
`contracts/cli/output.schema.json`; no operation ID. The verification report
lists, for source and target: regular files, directories, symbolic links,
`.claude/projects/**/*.jsonl` and `.codex/sessions/**/*.jsonl` counts, and
whether they match. Human output shows the session counts when either side has
any. Counts are not a content attestation.

| Code | Case |
| --- | --- |
| 0 | Copy verified (all counts equal), or valid dry-run. |
| 64 | Bad arguments, flags or selector ambiguity. |
| 65 | No container matches the selector. |
| 68 | Refusal before effects: no home, missing/unpinned helper, protected or existing target, changed plan, trust mismatch, failed pull. |
| 70 | Missing or declined `--yes`, trust or confirmation. |
| 71 | Read deadline with nothing applied. |
| 72 | Interrupt before effects. |
| 73 | Any failure after the target volume exists: copy failure, interruption, failed verification run, or counts that differ. The target is kept and reported. Takes precedence over 68/71/72 after effects. |
| 74 | Unclassified internal failure only. |

## Non-binding future extension

When the BundleDock toolbox ships `rsync`, `whr` may optionally run
`/opt/bdock/toolbox/bin/rsync` from the workspace's own pinned toolbox volume,
mounted read-only into the same base image, for resumable or incremental runs.
This is a possible later decision only; nothing here specifies or implements it.

## Acceptance (fixtures, then owner host)

Red-first tests with fake Docker executables must cover: manifest field vectors
(valid, non-digest, unknown member, v1); init preview/write/preserve of the pin
and no image access; exact copy/verify argv and admission refusal of other
vectors; source read-only in every run; excludes only as positional arguments;
default and explicit target naming, protected prefixes and existing-volume
refusal; missing pin refusal without a pull; dry-run zero effects; 70/68
consent outcomes; copy failure keeping only the reported target with no
deletion; verification mismatch 73; credential excludes and opt-in; command
table, help and JSON schema. Linux Docker and macOS+OrbStack host runs remain
owner gates and are not claimed here.

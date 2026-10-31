# Colima acceptance run: how to start it and what it measures

Date: 2026-10-31. Status: **the workflow has NOT been run yet.** This page holds no
measurement and supports no claim about Colima. Colima stays an alternative runtime
whose support wording waits for evidence; it becomes the default only after a green
matrix (owner decision, Feature 009 D-10).

## Start it

The owner starts it: GitHub, Actions, workflow `colima-acceptance`, Run workflow
(`workflow_dispatch` is the only trigger; there is no schedule, push or pull request
trigger, and agents never start it). Source:
[`.github/workflows/colima-acceptance.yml`](../../../.github/workflows/colima-acceptance.yml).
It has `contents: read`, three matrix jobs with `continue-on-error`, and never gates
a merge. `whr` never starts or configures Colima, so the job does it on a throwaway
runner VM: `brew install colima docker` (Homebrew core only), then
`colima start --runtime docker --vm-type ... --mount-type ...` with two bounded attempts.

| Job | Runner | Colima flags |
| --- | --- | --- |
| `intel-qemu-sshfs` | `macos-15-intel` | `--vm-type qemu --mount-type sshfs` |
| `intel-qemu-9p` | `macos-15-intel` | `--vm-type qemu --mount-type 9p` |
| `arm-vz-virtiofs` | `macos-15` (Apple Silicon) | `--vm-type vz --mount-type virtiofs` |

Whether the Apple Silicon row can start at all (nested virtualization on GitHub arm
runners) is unknown. A failed start is a result, not a hidden error: the job summary
says "Colima did NOT start", keeps the start logs and the Lima host-agent log tail,
and records every dependent probe as `failed: colima did not start`.

## What it records

Each probe is `observed` or `failed: <reason>`; a failing probe never fails the job.
Only checkout, the Go setup, the `whr` build and the Homebrew install can. Output goes
to the job summary and to the artifact `colima-acceptance-<job>` (`results.json`,
`logs/`, 14 days).

- Runner facts: architecture, macOS version, CPU, memory, `kern.hv_support`.
- Colima: `colima version`, `status`, `status --json`, `list`; Homebrew versions of
  colima, lima, docker, qemu.
- Layout: `ls -la ~/.colima ~/.colima/default`, whether `colima.yaml` sits next to
  `docker.sock`, `ls -l /var/run/docker.sock`, `df -k` of the volume holding `~/.colima`.
- Effective configuration: the keys `runtime`, `vmType`, `mountType`, `cpu`, `memory`,
  `disk`, `rootDisk`, `arch`, `autoActivate` and the `mounts` block of `colima.yaml`.
- Docker: `docker context ls`; `docker info` fields Server Version, OperatingSystem,
  Driver, DockerRootDir, Name, Architecture, SecurityOptions.
- Image: `docker pull` and `docker run` of the alpine digest that `whr` already pins in
  `tools.Dockerfile` (chosen over `hello-world`: whr uses it, so it proves a real dependency).
- Bind mounts: a host directory under `$RUNNER_TEMP`, one under `$HOME`, and one under
  `$TMPDIR`; each is checked for visibility, writability and ownership from a container,
  and whether a container write reaches the host.
- A named-volume write and read round trip.
- A 200 MiB tar pipe between volumes, timed in a single container and across two
  `docker run` calls through the host.
- `whr` built from source, run without `DOCKER_HOST`: `doctor --json`, `ls --local --json`
  (before and after creating a throwaway labeled container with a home volume),
  `workspace home clone --local --dry-run`, a real `home clone` and `home backup`
  followed by reading back the seed file, and `workspace recover --local --dry-run`.
  All throwaway objects carry the run id and are removed in an `always()` step, which
  also stops and deletes the Colima VM.

## Mapping to the results

- Matrix rows: the three jobs fill the runtime/mount-type rows of the verification
  matrix proposed in the Colima support audit
  (`colima-support-audit-20261031.md` on the unmerged branch `docs/colima-support-audit`). A row is green only when
  start, the mount and volume probes, the `whr` probes and the home copy all say
  `observed` with sensible content.
- Audit assumptions: A-1 (socket path), A-2 (context, `/var/run/docker.sock`), A-3 and
  A-4 (mounts, mount types), A-5 (rootful daemon, uids), A-9 (default resources and disk),
  A-10 (architecture) and A-12 (build fallback via the home clone) get observed values.
- Feature 009 OG-1 (facts the owner's Mac must confirm): the workflow answers
  `colima.yaml` next to `docker.sock` (FR-028), the `colima version` output for
  `version_args`/`version_regex`, the `disk` and `rootDisk` keys and unit, and the
  Colima data directory (FR-028). It is CI evidence on runner images, so it does not
  replace the owner's Mac for `min_version` or for a Mac with Docker Desktop or OrbStack
  also present. Results from the `colima.yaml` that appears only after the first start
  are visible here because the job always starts first.

## Limits

The runner images are not the owner's Mac. Nested virtualization, runner speed and
Homebrew's current Colima version can all change a result between runs; record the run
URL and the commit with any conclusion drawn from it.

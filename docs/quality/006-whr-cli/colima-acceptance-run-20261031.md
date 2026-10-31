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
runner VM: `brew install colima docker` (Homebrew core only; plus `brew install qemu` for the qemu rows,
because Colima depends on Lima only), then `colima start --runtime docker --vm-type ... --mount-type ...`
with two bounded attempts. Destructive Colima commands and removals run only on a GitHub-hosted runner
(`GITHUB_ACTIONS=true` and `RUNNER_ENVIRONMENT=github-hosted`); elsewhere they refuse.

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
The steps that can fail the job are checkout, the Go setup, the `whr` build, the Homebrew
install, and any step that exceeds its own timeout (see the budget below). The measure, `whr`,
cleanup, summary and upload steps run even then, so a start that failed or timed out is written
as "Colima did NOT start". Every probe is bounded by a watchdog that kills its whole process
group (TERM, then KILL); an alarm would not work because Go programs ignore SIGALRM. Output goes
to the job summary and to the artifact `colima-acceptance-<job>` (`results.json`,
`logs/`, 14 days).

- Runner facts: architecture, macOS version, CPU, memory, `kern.hv_support`.
- After each failed start, before anything is deleted: the tail of the Lima host-agent logs,
  `colima.yaml`, and listings of `~/.colima/_lima` and `~/.colima/default`. No delete follows
  the last attempt, so the later probes see the failed state.
- Before the first start: whether `~/.colima`, `~/.colima/default`, `~/.config/colima` and a
  `colima.yaml` exist at all.
- Colima: `colima version`, `status`, `status --json`, `list`; Homebrew versions of
  colima, lima, docker, qemu.
- Layout: `ls -la ~/.colima ~/.colima/default`, whether `colima.yaml` sits next to
  `docker.sock`, `ls -l /var/run/docker.sock`, `df -k` of the volume holding `~/.colima`.
- VM disk: `du -sk` of `~/.colima/_lima` and its subdirectories, files over 1 MiB with their
  allocated size, and `df -k` of the volume holding `~/.colima/_lima`.
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
  also deletes the Colima VM (hosted runners only).
- On the Intel qemu/sshfs row only, a second profile: `colima start -p whr-acc` with the same flags and
  small resources, then `colima status -p whr-acc`, `ls ~/.colima/whr-acc`, the socket path, `docker context ls`, and
  `whr doctor --json` once without `DOCKER_HOST` and once with `DOCKER_HOST` set to that profile's socket.

## Mapping to the results

- Matrix rows: the three jobs fill the runtime/mount-type rows of the verification
  matrix proposed in the Colima support audit
  (`colima-support-audit-20261031.md` on the unmerged branch `docs/colima-support-audit`). A row is green only when
  start, the mount and volume probes, the `whr` probes and the home copy all say
  `observed` with sensible content.
- Audit assumptions: A-1 (socket path), A-2 (context, `/var/run/docker.sock`), A-3 and
  A-4 (mounts, mount types), A-5 (rootful daemon, uids), A-9 (default resources and disk),
  A-10 (architecture) and A-12 (build fallback via the home clone) get observed values.
- Feature 009 OG-1 (facts the owner's Mac must confirm): the workflow records, on runner images,
  whether `colima.yaml` exists before the first start and sits next to `docker.sock` (FR-028),
  the `colima version` output (input for `version_args` and `version_regex`), the `disk` and
  `rootDisk` keys as written in `colima.yaml`, and where the VM disk lives and how large it is
  (FR-028). The second-profile probe gives evidence for OQ-1 (profiles other than `default`) on one
  row only. It does not decide `min_version`, the unit of `disk`, or behavior on a Mac that also has
  Docker Desktop or OrbStack; those stay with the owner's Mac.

## Time budget

The job cap is 50 minutes, which leaves five minutes for runner setup and the post steps. The step timeouts add up to 45. A test pins that sum, that every step
has a timeout, and that for each step the sum of its probe and `bounded` bounds, each plus the
5 s watchdog grace of a call that hits its bound, fits the step timeout. The start loop counts
twice; the cleanup counts up to 2 containers and 8 volumes. There is no slack, so a slow runner
fails a step on its timeout rather than overrunning the job.

| Step | Minutes |
| --- | --- |
| checkout, setup-go, build, helpers | 1, 2, 2, 1 |
| install (colima, docker, qemu on qemu rows) | 5 |
| start (two attempts of 200 s, diagnostics, one delete between) | 10 |
| measure the runtime | 7 |
| measure `whr` | 6 |
| second profile (one row) | 5 |
| cleanup, summary, upload | 4, 1, 1 |

## Limits

The runner images are not the owner's Mac. Nested virtualization, runner speed and
Homebrew's current Colima version can all change a result between runs; record the run
URL and the commit with any conclusion drawn from it.

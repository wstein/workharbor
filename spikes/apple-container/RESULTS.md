# Results

Measured on 1 October 2026 on the Mac mini (Apple silicon, 16 GiB, macOS 26.6.2) with `container` CLI 1.5.0, guest Linux 6.12.28 on the `fedora:latest` image. Tracks [issue #2](https://github.com/wstein/workharbor/issues/2). Raw output of every script is in `results/`. All objects were named `whspike-*` and removed afterwards; the two containers that already existed were not touched.

## Summary

| Item | Status | Headline |
| --- | --- | --- |
| 1. Lifecycle and limits | Measured | Each container is its own VM. CPU and memory limits are enforced. Use `--init`, or a stop takes 5 s |
| 2. Storage | Measured | Volumes and bind mounts survive a rebuild; the rootfs does not. Bind mounts are about 5x slower for many small files. A volume attached read-write is exclusive to one container |
| 3. Isolation | Measured | Mounted unix sockets are unusable. The runtime accepts any host path, so the adapter must reject. The default network reaches the LAN, the internet, other containers and host services |
| 4. Default-deny egress | Measured, corrected | `--internal` networks block the internet, the LAN and other containers, but not the host (issue #69). A dual-homed proxy sidecar gives a logging allowlist |
| 5. Agent in a container | Measured | A real, authenticated run works: the harness on the host drives Claude Code in a container on an internal network, through the proxy. Streaming, mid-run messages and resume after a container restart all work. Cancel and approvals need work |
| 6. Recovery | Measured, except a reboot | No restart policy. After a crash or a `system stop` and `start`, containers come back `stopped` with their data. Volumes, networks and images survive. A reboot was not triggered |
| 7. Stock image plus a shared read-only tool store | Measured | Works. glibc and musl need separate builds; Codex's static musl binary runs everywhere. Startup is the same from a bind mount, a volume or a copy |
| 8. Memory at 1 and 4 containers | Not started | |
| 9. Repositories on the host, mounted in (worktrees) | Measured | Works in three layouts. A shared `.git` exposes sibling tasks and runs guest-planted hooks and config on the host unless host git is hardened; a per-task clone with read-only alternates is the safest layout. A bind-mounted checkout costs a cold first status of 0.3 to 1.1 s and then about 120 ms |
| 10. A real project build, volume versus bind mount | Measured | Keeping the checkout on a bind mount costs nothing measurable. Keeping the Go module and build caches on a bind mount roughly doubles warm builds and adds a quarter to a cold one |

## 1. Lifecycle and limits

- **Timings** (image already local): `run -d` 1.1 s, `exec` ready after about 100 ms, `start` 0.6 s, `rm` 0.14 s. `stop` took **5.3 s** with `sleep` as PID 1, because it ignores SIGTERM; with `--init` it took **145 ms**. Always pass `--init`.
- **Isolation unit.** Each container is a separate lightweight VM: the guest reports its own kernel (`Linux 6.12.28` against `Darwin 25.6.0`) and one `container-runtime-linux` host process runs per container.
- **Limits.** `--cpus 2` gives `nproc` of 2; four busy loops used at most two CPUs' worth of time. `--memory 512M` is enforced by a memory cgroup inside the VM (`oom_memcg=/container/...`): a 700 MB allocation was killed with exit 137 and the container survived. The guest shows a larger `MemTotal` (629 MB for 512M), so the cgroup, not `MemTotal`, is the limit. The inspect output has a `cpuOverhead` of 1. Defaults are 4 CPUs and 1 GiB.
- **Addresses.** A container's IP changes across recreate and restart; read it with `container inspect` each time. The `.dns` domain list is empty.
- **Sharp edge.** `container rm --all` deletes every container, including ones you did not create. The scripts only remove by exact `whspike-` name.

## 2. Storage

| | Rootfs | Named volume | Bind mount |
| --- | --- | --- | --- |
| Survives stop and start | Yes | Yes | Yes |
| Survives delete and recreate | **No** | Yes | Yes (it is a host directory) |
| 3000 small files | 225 ms | 198 ms | **1172 ms** |
| 200 MB sequential write | 313 ms | 492 ms | 567 ms |
| Read 3000 small files | | 9 ms | 349 ms |

- A named volume is an ext4 image file (`volume.img`, virtual size 512 GiB, sparse) under `~/Library/Application Support/com.apple.container/volumes/`. The host cannot browse it directly.
- A bind mount syncs both ways at once. Files the guest root writes appear on the host owned by the host user (uid 501) with the mode kept. A symlink to `$HOME` inside a mount is just a dangling link in the guest.
- **Recommendation for §4.4:** keep the repository checkout and the agent home on a volume; use a bind mount only for hand-over to the host, and expect git-heavy work on it to be slow.
- **Superseded by items 9 and 10.** The design (§4.4, D16) keeps repositories on the host and bind-mounts them, because item 10 measured no cost for a bind-mounted checkout in a real build once the caches stay on the volume, and item 9 settled the checkout layout. Only the agent home and the caches stay on the volume.
- **A volume is exclusive while it is writable.** While a container has a named volume read-write, no other container can attach it, not even read-only: the second `run` fails with "The storage device attachment is invalid" (a Virtualization.framework error) and no container is created. Several containers can attach one volume **read-only** at the same time. A volume is free again as soon as its holder stops (`09b-volume-exclusive.sh`). A bind mount has no such limit. So one writable volume per environment, and a rebuild must stop the old container before the new one starts.

## 3. Isolation and escape tests

- **The runtime does not reject mounts.** It mounted `/etc` read-only without complaint, so rejecting `$HOME`, `~/.ssh`, home's parents and runtime sockets is the adapter's job. `03-isolation.sh` has a deny-list function that resolves symlinks first (so a symlink to `$HOME` is rejected) and rejects unix sockets; it is a seed for the conformance suite.
- **Mounted unix sockets are unusable.** A host socket in a bind-mounted directory could not be listed (`Operation not supported`) or connected to, and the host listener saw no connection. The same held for the real Socktainer socket (`~/.socktainer/container.sock`): no escape that way.
- **Network, default network** (everything below succeeded from a guest):
  - the internet (`1.1.1.1:443`, `https://example.com`);
  - the LAN default gateway (one TCP connect to port 80);
  - the Mac's own LAN address, and the host gateway `192.168.64.1`, **when the host service listens on all interfaces**; a service bound only to `127.0.0.1` was not reachable;
  - other containers on the network, both ways, and the host can reach container IPs.
- **Hardening works.** `--read-only --cap-drop ALL --user 1000:1000 --tmpfs /tmp` gave an empty capability set, a read-only root filesystem, a writable `/tmp`, and failing `mount`.
- **Not tested on purpose:** `--ssh` forwards the host ssh-agent into the container, which would let it sign with your keys. The adapter must never pass it. `--publish-socket`, `--virtualization` and `--rosetta` were not exercised.
- **VM boundary.** The guest has `/dev/vsock` and `/dev/vfio` nodes; the host-guest channels (vsock, the exec path) were not probed beyond the tests above.

## 4. Default-deny egress

- `container network create --internal NAME` ("host-only") blocks **everything** from a container on it: the internet, DNS (names do not resolve), the LAN, the host through every address, IPv6, and containers on other networks. Nothing leaks.
  - **Corrected by issue #69** (branch `spike/host-reachability`): an `--internal` guest does reach host listeners bound to the Mac's LAN address or to all interfaces, through the network's gateway. Only loopback-only listeners are unreachable. This spike's "the host through every address" did not test a listener on the LAN address; design §7.2 and D29 follow #69.
- **The host cannot serve it.** The host gets no interface on an internal network, so a proxy on the host cannot bind to its gateway address or be reached.
- **Working design:** a **sidecar** container attached to both networks (`--network default --network NAME`, the flag repeats) runs the logging allowlist proxy. Agent containers live only on the internal network and reach the sidecar's internal IP on port 3128. Measured with the probe's proxy:
  - allowed hosts returned 200 (`example.com`, `proxy.golang.org`); denied hosts got 403 (HTTPS through CONNECT and plain HTTP); a CONNECT to a raw IP was denied too;
  - `curl` with `HTTPS_PROXY` works, and the **proxy** resolves the name, so the guest needs no DNS (no DNS exfiltration path);
  - direct connections stayed blocked; the proxy was not reachable from the default network; every decision was logged with time, verdict, method, host and source.
- **Agents on one internal network can reach each other.** One internal network per task or environment, not one shared network.
- **Limits of the test:** the allowlist matches the hostname in CONNECT, so it does not defeat domain fronting, and the sidecar has full egress and is trusted.

## 5. Agent inside a container

- **Install.** `curl -fsSL https://claude.ai/install.sh | bash` inside a container on the internal network, through the proxy, installed Claude Code 2.1.286 (a native Linux aarch64 build, 230 MB) into a volume in about 11 s. The install needed only `claude.ai` and `downloads.claude.ai`.
- **Hosts contacted.** An unauthenticated headless run contacted only `api.anthropic.com`. A minimal allowlist for Claude Code is therefore `api.anthropic.com`, `claude.ai` and `downloads.claude.ai`; further hosts may appear once a real login and tools are used.
- **State survives.** A marker in `/root/.claude` (the volume) survived stop and start and a delete and recreate of the container, so an auth directory and agent session kept on a volume survive the same events.
- **Memory.** A container with an idle agent in `stream-json` mode used about 277 MiB (including page cache) of a 2 GiB limit, with 11 processes.
- **An authenticated run** (`05c-agent-run.sh`; Claude Code 2.1.286 from the tool store, a stock fedora image, an `--internal` network, the allowlist proxy sidecar). The login was a long-lived subscription token from `claude setup-token`, passed per exec with `container exec --env-file`, so it never sat in the container's own configuration, on a command line or in the repository. It was deleted afterwards (and the owner revokes it).
  - A headless `claude -p` inside the container answered through the proxy.
  - **Allowlist.** The proxy saw `api.anthropic.com` (allowed) and `http-intake.logs.us5.datadoghq.com` (Claude Code's telemetry, **denied**). The denial broke nothing, so the minimal allowlist is `api.anthropic.com` alone. `curl` inside the container obeys the proxy variables, so a check without `--noproxy` tests the proxy, not the network; the real direct paths (to `api.anthropic.com` and to a raw IP, with `--noproxy '*'`) both failed.
  - **Harness in the loop.** The spike #1 harness stayed on the host and ran `container exec -i ... claude` as the agent process through a one-line wrapper (`-claude`). Live typed events, token deltas and a message injected 4 s into a running tool (`done` plus `BANANA`) all worked exactly as on the host.
  - **Memory.** The container used about 293 MiB while the agent ran a tool (13 processes) and about 288 MiB idle, of a 2 GiB limit.
  - **Resume after a container restart.** After `container stop` and `start`, the same agent session (same session ID) resumed from `/root/.claude` on the home volume and still remembered the earlier instruction. The token is supplied again on each exec, so a stopped and restarted environment recovers fully. The working directory is a second volume (`/work`), since a volume has one writer.
- **Cancel does not work through `container exec`.** The harness sends SIGINT to its child; here that child is the `container exec` client, which failed to forward it ("failed to send signal ... invalidArgument") and the agent kept running until the container stopped. An adapter needs another cancel route, such as signalling the process from inside the container with `exec`, or running the agent under a small launcher that writes its PID.
- **Approvals from inside the container are still open.** This run used `-approvals=false`. The MCP approve helper of spike #1 would run in the guest and call the supervisor on the host, which an internal network cannot reach. Options: an HTTP MCP server reached through the sidecar, or a TCP relay in the sidecar to a supervisor listener bound to the bridge address (not loopback).

## 6. Recovery

- **Process model.** launchd jobs `com.apple.container.apiserver`, `...container-core-images`, one `container-network-vmnet.<network>` per network and one `container-runtime-linux.<container>` per container, all children of launchd.
- **No restart policy.** Among the `run` flags only `--init` matches; nothing restarts a container.
- **Runtime crash.** Killing the host-side runtime process of one container left it `stopped`; `exec` failed with "not running"; the volume state was intact and `start` brought it back with its files. A reconciler has to detect and restart it (design §5.3).
- **After a reboot.** No LaunchAgent or LaunchDaemon plist for the container services exists on disk, so nothing starts automatically; `container system start` has to run after login. A reboot was not triggered.
- **`container system stop` and `start`** (approved by the owner; `09-system-restart.sh`, with a running container, a stopped one, a container on an internal network, a volume, a custom network and a bind mount):
  - `stop` took 0.4 s and ended every container VM at once (no runtime processes left); the CLI then failed with an XPC "connection invalid" error until `start`.
  - `start --disable-kernel-install --timeout 90` took 0.4 s and needed no prompt. Without `--disable-kernel-install` it asks about the kernel (the default); that prompt was not exercised, so unattended boot should pass the flag.
  - **Every container came back `stopped`**, including the ones that were running. Nothing restarted by itself.
  - **Survived:** all containers and their root filesystems, the volume, the custom internal network (its launchd job was re-created), the images, the bind-mounted directory. A file in the rootfs, one in the volume and one in the bind mount were all intact after starting the containers again.
  - **Lost:** every process inside a container (a background process was gone), and the container IPs changed (for example `192.168.64.55` to `192.168.64.2`).
  - **Networks kept their rules:** after the restart the default network reached the internet and the internal network stayed blocked.
  - The owner's two containers and all five images were untouched.
- **Still not done:** an actual reboot (documented from the facts above, not triggered).
- Killing PID 1 from inside the guest had no effect, because the init process ignores it.

## 7. Stock image plus a shared read-only tool store

Instead of installing an agent in every container (item 5: 11 s and 230 MB each), the agent CLIs live once in a versioned, immutable **tool store** on the host and are mounted read-only into stock images, the way a Nix store is shared. `build-store.sh` builds it; `07b-toolstore.sh` tests it.

**Layout** (a tiny Nix-like store, 949 MB for four entries):

```
store/<hash8>-<name>-<version>-<platform>/bin/<name>     content-addressed, never modified
profiles/<profile>/bin/<name> -> ../../../store/.../bin/<name>
```

Claude Code is downloaded from the vendor's release URL and verified against its SHA-256 manifest; Codex CLI is the static musl build from the GitHub release. Relative symlinks inside one mount keep working. Profiles `default` (glibc Claude Code 2.1.286 and Codex), `musl` and `pinned` (Claude Code 2.1.285) were used.

**One store, four stock images** (`--mount type=bind,source=STORE,target=/opt/store,readonly`):

| Stock image | libc | `default` Claude Code (glibc) | `musl` Claude Code | Codex CLI (static musl) |
| --- | --- | --- | --- | --- |
| fedora:latest | glibc | runs | `required file not found` | runs |
| debian:bookworm-slim | glibc | runs | `not found` | runs |
| ubuntu:24.04 | glibc | runs | `not found` | runs |
| alpine:3 | musl | `not found` | runs | runs |

The "not found" errors are the missing dynamic loader, not a missing file. So a store needs **one build per libc** (the vendor's installer picks the musl build the same way, by looking for the musl loader), and a static binary such as Codex runs in any image. The adapter picks the profile from the image's libc. A tool that needs shared libraries beyond libc would need its closure in the store, which is what Nix does; none of the three needed it.

**Where the store lives**, startup cost of `claude --version` on fedora (three runs, ms):

| Source | Runs | Notes |
| --- | --- | --- |
| Bind mount (virtiofs), read-only | 127, 120, 103 | Shared by all containers with no copy |
| Rootfs copy | 107, 116, 109 | Copying the 230 MB binary took 443 ms per container |
| ext4 named volume, read-only | 187, 108, 100 | Populating it from the store took 3.5 s, once |

- **No meaningful startup difference.** The bind mount wins on simplicity: nothing to copy, one directory to update. A volume is read-only capable (`-v NAME:/opt/store:ro` works and is enforced) and a volume can be attached **read-only** to several running containers at once (tested with two). Only a read-only volume can be shared; a volume attached read-write is exclusive to one container (item 2), so a shared tool volume must be read-only everywhere.
- **Immutable from inside.** `touch`, `rm`, appending to a tool, `chmod` and replacing a symlink all failed with "Read-only file system". Re-hashing every store entry afterwards showed no change.
- **Several containers, one store.** Four containers started at once, each ran both tools and exited, in 3.0 to 4.5 s total, including VM start.
- **Two versions side by side.** Environment A (profile `default`) ran Claude Code 2.1.286 and environment B (profile `pinned`) ran 2.1.285 at the same time, each resolving to its own store entry.
- **Writable state is separate.** With `HOME` on a per-environment volume, the agent's files (`.claude`, `.claude.json`) landed in that home and not in the store, and the two homes were independent.
- **Cost comparison.** A per-container install was 11 s and 230 MB each; the store costs one download (949 MB for four entries) and zero per container.

**Design consequences**
- The agent adapter declares its tool entry (name, version, platform, hash) and the runtime adapter mounts the matching profile read-only. A new agent version is a new store entry plus a profile change, and a rollback is a profile change.
- Updates, pinning and audit are central: one place verifies checksums, one place records which version ran a task (record the store hash in the run's audit entry).
- It also removes network need at start-up: the egress allowlist no longer has to permit `downloads.claude.ai`.
- The harness and helper binaries (for example the spike's approve helper) can live in the same store.

## 9. Repositories on the host, mounted into environments

Direction from the owner: clones live outside containers and volumes, on the host, and are mounted in; worktrees give parallel topics that are merged and cleaned up before anything is pushed. `10-git-worktree.sh` and `10b-git-tuning.sh` test the mechanics on a synthetic repository (5000 files, git 2.56 on the host, `alpine/git` in the guest, pulled and removed afterwards).

**Four layouts**

| Layout | Does git work in the guest? | What the guest can reach |
| --- | --- | --- |
| Worktree (absolute pointer), only the worktree mounted | **No**: `fatal: not a git repository`. The `.git` file holds `gitdir: <host path>/cache.git/worktrees/t1` | |
| Worktree created with `--relative-paths`, worktree and shared repo mounted with the same relative layout | Yes | The whole shared repo |
| Worktree, shared repo mounted read-write at its host path | Yes | **All branches, the shared hooks and config, and every other task's worktree metadata** |
| **Per-task clone** (`git clone --shared`) with the cache's objects mounted read-only at their host path | Yes | Only its own repository. The cache objects are read-only, and other tasks are invisible |

**What a hostile guest can do to the host**

- **Guest-planted hook.** A `pre-commit` hook written into the shared repo from inside the guest ran on the **host** when the host later ran a plain `git commit`.
- **Guest-planted config.** A `core.fsmonitor` command set in the shared config ran on the host at the next plain `git status`.
- **Both were neutralised** by host-side `git -c core.hooksPath=/dev/null -c core.fsmonitor=false`. Other repo-config keys of the same class were not tested: `core.sshCommand`, `core.pager`, `core.editor`, `credential.helper`, `diff.external`, `gpg.program`, aliases and clean/smudge filters. A hardened `-c` list is therefore a floor, not a guarantee. Safer is to not run host git inside agent-writable trees at all and to `git fetch` their branches into a supervisor-owned repository; that route was not tested.
- **The same hook trick works in the per-task clone**, but only against the task's own repository. It reaches the host only if the host runs plain git there.
- **Branch protection exists but is partial.** Deleting a branch that a worktree has checked out was refused (`used by worktree`). Other branches of a shared repo were not tried and are reachable.

**Speed of a bind-mounted checkout** (5000 files, 300 edited):

| | Bind-mounted clone | Clone in a volume |
| --- | --- | --- |
| First `git status` after fresh edits | 0.3 to 1.1 s | 0.12 s |
| Later `git status` | about 100 to 130 ms | 75 to 118 ms |
| `git add` of 300 files | 363 ms | 120 ms |
| `git commit` | 667 ms | 334 ms |
| Clone into the volume from the read-only cache | | 1.6 s |

Git tuning (`core.untrackedCache`, `feature.manyFiles`, `core.preloadIndex`, `core.checkStat=minimal`) changed steady-state `status` by almost nothing (about 100 to 130 ms in every case), because the cost is the cold first stat of every file, not repeated work. For much larger repositories the cold pass grows with the file count and was not measured.

**Consequences**
- Repositories on the host are workable. The agent's home and caches stay on the volume; the checkout is a host directory mounted read-write.
- Prefer the **per-task clone with a read-only object cache** over worktrees of one shared repository: it removes the sibling-task and shared-hook exposure and needs no pointer fix. Worktrees stay attractive for the developer's own parallel topics on the host, where no agent writes into the shared `.git`.
- The host must treat every agent-writable repository as hostile: hardened git invocation or fetch into a trusted repository, never plain git in the agent's tree.
- A relative worktree (`--relative-paths`) fixes the pointer only if the mounts keep the relative layout.

## 10. A real project build: volume versus bind mount

`11-build-bench.sh` times the steps of this repository's `make check` (build, vet, test, and golangci-lint and editorconfig-checker compiled from source with `go run`) in a stock fedora image with the Go 1.27.1 toolchain mounted read-only, 4 CPUs and 4 GiB. The caches it builds are large: 390 MB in about 25,700 module files and 697 MB in about 7,800 build-cache files. One run per cell, on one machine; the cold run includes downloading modules from the network, so it varies.

| Layout | Cold (empty caches) | Warm | One file changed |
| --- | --- | --- | --- |
| **A** checkout and caches on one **volume** | 45.3 s | 2.34 s | 2.60 s |
| **B** checkout on a **bind mount**, caches on a volume | 41.1 s | 2.68 s | 2.65 s |
| **C** checkout and caches on a **bind mount** | 56.4 s | 4.65 s | 4.83 s |

Per step, warm: build 239 / 257 / 318 ms, vet 170 / 180 / 267 ms, test 172 / 201 / 283 ms, golangci-lint 1.41 / 1.46 / 3.32 s (A / B / C). The slowest cold step was compiling golangci-lint: 35.6 s on a volume, 32.7 s with the checkout on a bind mount and 45.9 s with everything on a bind mount.

- **The checkout on a bind mount (B) is as fast as on a volume (A).** The differences are within the run-to-run noise (B's cold run was even faster, which is network variance). A project's own sources are few files, so the bind mount's cost per file does not show.
- **The caches are what hurt (C).** Putting the module cache (about 25,700 small files) and the build cache on a bind mount made warm builds about 2x slower and the cold build about 25% slower, with golangci-lint's warm run taking 3.3 s instead of 1.4 s.
- **A project with many small files in its working tree behaves like C.** `node_modules` is the obvious case and was not measured.
- Memory: the container peaked at about 1.4 GiB during the build.

**Consequences:** keep repositories on the host (section 9) and bind-mount them; keep every cache and build output (`GOMODCACHE`, `GOCACHE`, package-manager caches, build directories) on the per-environment volume, outside the checkout, by environment variables set in the environment. The adapter, not the agent, sets them.

## Consequences for the design

- **§5.1 runtime adapter.** Report: isolation boundary is a VM per container; `--internal` networks supported; no restart policy; no suspend or checkpoint observed.
- **§4.4 persistence.** Repository and agent home on a volume; bind mounts for hand-over only; the rootfs is disposable.
- **§7.2 egress.** Per-environment `--internal` network plus a dual-homed proxy sidecar is a verified way to get default-deny egress with a log.
- **§7.4 isolation policy.** The adapter rejects mounts (resolve symlinks first); unix sockets cannot be mounted usefully; never pass `--ssh`; use `--init`, `--read-only`, `--cap-drop ALL` and a non-root user where possible.
- **§5.1 and §5.2, tool store.** Prefer stock images plus a shared read-only, content-addressed tool store mounted into each environment (item 7): one build per libc, profiles for versions, the store hash recorded per run. No per-container install.
- **§5.3 reconciler.** Detect `stopped` containers and restart them; state lives on the volume. After `container system start` or a crash, every container is `stopped` and every process is gone, so the reconciler resumes the agent from its session (design §5.3) after starting the container. Container IPs change, so never store them; and run `container system start --disable-kernel-install` when starting the services unattended.
- **§4.4 persistence.** One writable volume per environment (exclusive); stop the old container before starting a replacement that uses the same volume.
- **§12.** Replace the "Apple Container network isolation controls are unverified" marks with these measurements; keep reboot behaviour, the authenticated agent run and approvals from inside the container open.

## Not tested

Much larger repositories on a bind mount, fetching from an agent's repository into a trusted one, a reboot, the interactive kernel-install prompt of `container system start`, approvals from inside the container, a working cancel through `container exec`, memory at 1 and 4 containers, behaviour under memory pressure on the host, `--publish-socket`, `--virtualization`, Rosetta, and Socktainer beyond its socket.

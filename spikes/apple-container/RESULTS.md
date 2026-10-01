# Results

Measured on 1 October 2026 on the Mac mini (Apple silicon, 16 GiB, macOS 26.6.2) with `container` CLI 1.5.0, guest Linux 6.12.28 on the `fedora:latest` image. Tracks [issue #2](https://github.com/wstein/workharbor/issues/2). Raw output of every script is in `results/`. All objects were named `whspike-*` and removed afterwards; the two containers that already existed were not touched.

## Summary

| Item | Status | Headline |
| --- | --- | --- |
| 1. Lifecycle and limits | Measured | Each container is its own VM. CPU and memory limits are enforced. Use `--init`, or a stop takes 5 s |
| 2. Storage | Measured | Volumes and bind mounts survive a rebuild; the rootfs does not. Bind mounts are about 5x slower for many small files. A volume attached read-write is exclusive to one container |
| 3. Isolation | Measured | Mounted unix sockets are unusable. The runtime accepts any host path, so the adapter must reject. The default network reaches the LAN, the internet, other containers and host services |
| 4. Default-deny egress | Measured | `--internal` networks block everything. A dual-homed proxy sidecar gives a logging allowlist |
| 5. Agent in a container | Partly | Claude Code installs and runs through the proxy; state survives stop, start and rebuild. Not logged in, so no real agent run yet |
| 6. Recovery | Measured, except a reboot | No restart policy. After a crash or a `system stop` and `start`, containers come back `stopped` with their data. Volumes, networks and images survive. A reboot was not triggered |
| 7. Stock image plus a shared read-only tool store | Measured | Works. glibc and musl need separate builds; Codex's static musl binary runs everywhere. Startup is the same from a bind mount, a volume or a copy |
| 8. Memory at 1 and 4 containers | Not started | |

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
- **Not done yet:** a real run, because it needs a login inside the container (a subscription token the owner must create). Approvals from inside the container also need a path from the guest to the supervisor: the MCP helper of spike #1 runs in the guest and the supervisor is on the host, which an internal network cannot reach. Options: an HTTP MCP server reached through the sidecar, or a relay.

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

A reboot, the interactive kernel-install prompt of `container system start`, an authenticated agent run, approvals from inside the container, memory at 1 and 4 containers, behaviour under memory pressure on the host, `--publish-socket`, `--virtualization`, Rosetta, and Socktainer beyond its socket.

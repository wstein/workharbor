# Guest package verification (Node and Docker)

Owner decisions H50:b and H52:a. The host agent does not hash the Node or Docker
CLI binaries. Each is verified inside the disposable Linux guest by exact version
plus the package or archive verification below, and the result is recorded as
evidence that Feature 006 T009 runtime acceptance cites. The
[provider contract](../../specs/006-whr-cli/contracts/devcontainer-provider.md)
and [tool-pins.json](../../apps/host_agent/internal/providers/docker/tool-pins.json)
are authoritative for pinned versions and digests; this page copies none of them.

Status: procedure written, NOT yet exercised on a real guest. Do not report it as
measured until an evidence record exists from a real run.

Run these steps as the sole guest-mutation helper, inside the guest, once per fresh
guest, before any workload. Any failed check aborts preparation; the guest is then
deleted per the exec plan. Never continue with an unverified tool.

## Docker Engine and CLI

1. Download the Docker apt signing key from
   <https://download.docker.com/linux/ubuntu/gpg> (see
   <https://docs.docker.com/engine/install/ubuntu/>) into a staging file, not yet
   into `/etc/apt/keyrings`. Treat it as untrusted.
2. Verify the key BEFORE trusting it: run
   `gpg --show-keys --with-colons <staging-file>`. Require exactly one `pub` record
   whose first following `fpr` record equals `docker_engine.apt_gpg_fingerprint`
   in tool-pins.json (`9DC858229FC7DD38854AE2D88D81803C0EBFCD88`, uid "Docker
   Release (CE deb)"), compared as the full 40-hex string, never a short ID. The
   subkey `D3306A018370199E527AE7997EA0A9C3F273FCD8` is expected; any other `pub`
   record or a different primary fingerprint aborts.
3. Only then install the key to `/etc/apt/keyrings/docker.asc`, write the source
   entry for Ubuntu 24.04 (`noble`) over HTTPS with `signed-by=/etc/apt/keyrings/docker.asc`,
   and run `apt-get update`. Never use `apt-key` or `[trusted=yes]`.
4. Resolve the exact package version for `docker_engine.version` in tool-pins.json
   with `apt-cache madison docker-ce`; install `docker-ce` and `docker-ce-cli` pinned
   to that single full version string (`docker-ce=<version-string>`). Refuse if no
   candidate matches. `containerd.io` has its own version numbering: install it
   from the same signed source without the Engine string and record its installed
   version as evidence only (no pin exists for it).
5. Confirm apt verified signatures: `apt-get update` must report no
   `NO_PUBKEY`, `not signed` or `KEYEXPIRED` warnings for the Docker source; record
   `signature_status` as `verified` only when `apt-cache policy docker-ce` shows the
   candidate from the signed source and `dpkg -s docker-ce` reports the pinned
   version. No other trust path is accepted.
6. Check the installed version: `docker version --format '{{.Server.Version}}'` and
   `docker version --format '{{.Client.Version}}'` must both equal the pinned
   Engine version; record `dpkg-query -W -f='${Version}\n' docker-ce docker-ce-cli containerd.io`.

Fingerprint provenance (two agreeing sources, checked 2026-10-16): the archived
Docker docs line "9DC8 5822 9FC7 DD38 854A E2D8 8D81 803C 0EBF CD88"
(<https://web.archive.org/web/20200601id_/https://docs.docker.com/engine/install/ubuntu/>;
current install pages no longer publish it) and the primary fingerprint of the key
served at <https://download.docker.com/linux/ubuntu/gpg>.

## Node

1. Map the guest architecture to the Node file name: `amd64` is `linux-x64`,
   `arm64` is `linux-arm64`, giving `node-v<version>-<mapped>.tar.xz`. Require
   `xz-utils` in the guest. Download that archive and `checksums_url`
   (`SHASUMS256.txt`) named in tool-pins.json under `node`, into a new empty
   directory. Treat both as untrusted.
2. Verify `SHASUMS256.txt` itself: its SHA256 must equal the digest pinned for it.
   TODO(owner): tool-pins.json pins archive digests (`linux_arm64_sha256`,
   `linux_amd64_sha256`) but no digest for the SHASUMS256 file; add a
   `checksums_sha256` field (as crane has) in a follow-up pin amendment, or
   explicitly accept the archive digest alone. Until then record
   `shasums_digest_status` as `not-pinned`.
3. Compute the archive SHA256 and require it to equal the pinned archive digest for
   the guest architecture, and to equal the SHASUMS256 line whose filename field is
   exactly the archive name (exact field match, not a substring).
   Optionally verify the detached `SHASUMS256.txt.sig`/`.asc` against the Node release
   keys when the owner pins those keys; record `signature_status` accordingly.
4. Extract only after the digest passes, into a new directory; install under
   `/opt/node-<version>` and prepend its `bin` to PATH.
5. Run the extracted binary by absolute path: `<node>/bin/node --version` must print
   `v` plus the pinned `node.version`. This is a version check, not a binary hash;
   the pinned digests are release-archive digests.

## Evidence record

Write one JSON file per guest, mode 0600, to the T009 bounded evidence directory
for that run (`<evidence-dir>/guest-package-verification.json`), and cite it in the
T009 acceptance record. No secrets, no raw environment.

| Field | Content |
| --- | --- |
| `schema_version` | `1` |
| `guest_id` | Immutable created guest identity |
| `arch` | `arm64` or `amd64` |
| `recorded_at` | UTC timestamp |
| `tool_pins_sha256` | SHA256 of the tool-pins.json used |
| `docker.apt_source` | Repository URL and suite |
| `docker.gpg_fingerprint` | Full fingerprint observed |
| `docker.gpg_fingerprint_match` | `true` only if equal to the pinned value |
| `docker.package_versions` | `docker-ce`, `docker-ce-cli` dpkg versions (pinned) and `containerd.io` (evidence only) |
| `docker.server_version`, `docker.client_version` | From `docker version` |
| `docker.signature_status` | `verified` or `failed` |
| `node.archive_name`, `node.archive_sha256` | As downloaded |
| `node.archive_sha256_match` | `true` only if equal to the pinned digest |
| `node.shasums_sha256`, `node.shasums_digest_status` | Observed digest; `matched`, `mismatch` or `not-pinned` |
| `node.signature_status` | `verified`, `not-checked` or `failed` |
| `node.reported_version` | Output of `node --version` |
| `result` | `pass` only when `docker.gpg_fingerprint_match` is `true`, `docker.signature_status` is `verified`, the `docker-ce` and `docker-ce-cli` versions plus `docker.server_version` and `docker.client_version` equal the pinned Engine version, `node.archive_sha256_match` is `true`, `node.reported_version` equals `v` plus the pinned Node version, and `node.shasums_digest_status` is `matched` (`not-pinned` does not pass) |

A record with `result` other than `pass`,
cannot support acceptance of the H50:b guarantee.

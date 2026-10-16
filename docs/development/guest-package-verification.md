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

1. Add the Docker apt repository for Ubuntu 24.04 (`noble`) over HTTPS using the
   upstream instructions at <https://docs.docker.com/engine/install/ubuntu/>.
   Install its signing key to `/etc/apt/keyrings/docker.asc` and reference it with
   `signed-by=` in the source entry. Never use `apt-key` or `[trusted=yes]`.
2. Verify the key fingerprint before trusting it: compute
   `gpg --show-keys --with-fingerprint /etc/apt/keyrings/docker.asc` and require the
   primary fingerprint to equal the pinned value
   `DOCKER_APT_GPG_FINGERPRINT` below, compared as a full 40-hex string, not a
   short ID.
3. Resolve the exact package version for `docker_engine.version` in tool-pins.json
   with `apt-cache madison docker-ce`; install `docker-ce`, `docker-ce-cli` and
   `containerd.io` pinned to that single full version string (for example
   `docker-ce=<version-string>`). Refuse if no candidate matches the pinned version.
4. Confirm apt verified signatures: `apt-get update` must report no
   `NO_PUBKEY`, `not signed` or `KEYEXPIRED` warnings for the Docker source; record
   `signature_status` as `verified` only when `apt-cache policy docker-ce` shows the
   candidate from the signed source and `dpkg -s docker-ce` reports the pinned
   version. No other trust path is accepted.
5. Check the installed version: `docker version --format '{{.Server.Version}}'` and
   `docker version --format '{{.Client.Version}}'` must both equal the pinned
   Engine version; record `dpkg-query -W -f='${Version}\n' docker-ce docker-ce-cli`.

TODO(owner): confirm `DOCKER_APT_GPG_FINGERPRINT`. The published value is listed at
<https://docs.docker.com/engine/install/ubuntu/> (Docker's "Official GPG key
fingerprint"). It was not verified offline when this page was written, so no value
is pinned here. Record the confirmed 40-hex fingerprint and its retrieval date in
this page (replace this TODO) before the first real run.

## Node

1. Download, into a new empty directory, the release archive for the guest
   architecture and `checksums_url` (`SHASUMS256.txt`) named in tool-pins.json
   under `node`. Treat both as untrusted.
2. Verify `SHASUMS256.txt` itself: its SHA256 must equal the digest pinned for it.
   TODO(owner): tool-pins.json pins archive digests (`linux_arm64_sha256`,
   `linux_amd64_sha256`) but no digest for the SHASUMS256 file; add a
   `checksums_sha256` field (as crane has) in a follow-up pin amendment, or
   explicitly accept the archive digest alone. Until then record
   `shasums_digest_status` as `not-pinned`.
3. Compute the archive SHA256 and require it to equal the pinned archive digest for
   the guest architecture, and to match the archive's line in `SHASUMS256.txt`.
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
| `docker.package_versions` | `docker-ce`, `docker-ce-cli`, `containerd.io` dpkg versions |
| `docker.server_version`, `docker.client_version` | From `docker version` |
| `docker.signature_status` | `verified` or `failed` |
| `node.archive_name`, `node.archive_sha256` | As downloaded |
| `node.archive_sha256_match` | `true` only if equal to the pinned digest |
| `node.shasums_sha256`, `node.shasums_digest_status` | Observed digest; `matched`, `mismatch` or `not-pinned` |
| `node.signature_status` | `verified`, `not-checked` or `failed` |
| `node.reported_version` | Output of `node --version` |
| `result` | `pass` only when every required match is `true`/`verified` |

A record with `result` other than `pass`, or with an owner TODO above still open,
cannot support acceptance of the H50:b guarantee.

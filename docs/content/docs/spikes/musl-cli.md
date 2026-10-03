---
title: "Spike: musl builds of the agent CLIs"
description: "Whether Claude Code, Codex CLI and Antigravity publish linux-arm64 musl builds, and what the common toolchains say about musl, as the evidence for Alpine as a first-class agent base (D43)."
weight: 14
---

> Source: `spike/musl-cli` at `3672938` (a local branch until it is pushed; link it then), with `probe.sh` and its output `results.txt`. Tracks [#152](https://github.com/wstein/workharbor/issues/152). Probed on 3 October 2026, without any account or credential.

## Summary

All three agent CLIs publish a linux-arm64 musl build today. What is not measured is that any of them runs under `agenttest` on Alpine: Apple Container's service was not running on the probing host, so no container was started and no `--version` check ran. Alpine therefore stays best-effort for agent environments (D43) until that run is made.

## The agent CLIs

| CLI | linux-arm64 musl build | Evidence | Status |
| --- | --- | --- | --- |
| Claude Code 2.1.285 | `linux-arm64-musl` in the release manifest (232,077,120 bytes, sha256 `31efc413...`), next to `linux-arm64`; same `downloads.claude.ai` base and manifest as the pinned glibc build. The first 4 KiB of the file is an ELF with interpreter `/lib/ld-musl-aarch64.so.1`: dynamically linked against musl, so it needs `libgcc`, `libstdc++` and, for search, `ripgrep` with `USE_BUILTIN_RIPGREP=0` | The vendor's [advanced setup](https://code.claude.com/docs/en/setup) lists Alpine 3.19+ and those packages; the manifest and ELF header were fetched by `probe.sh` | {{< status verified >}} that it is published; {{< status unverified >}} that it runs and passes `agenttest` |
| Codex CLI `rust-v0.160.0` | `codex-aarch64-unknown-linux-musl.tar.gz` (with `.sigstore`) in the [GitHub release](https://github.com/openai/codex/releases/latest); the binary is statically linked | `file` on the extracted binary; spike #2 had found the static musl binary running on the Fedora guest | {{< status verified >}} that it is published and static; runs on Fedora per spike #2; {{< status unverified >}} on Alpine |
| Antigravity 1.2.16 | The installer detects musl and asks for `manifests/linux_arm64_musl.json`, which now answers 200 with a `cli_linux_arm64_musl.tar.gz` and a sha512; the extracted binary is statically linked. A forum report of the manifest's 404 on musl hosts ([Google AI forum](https://discuss.ai.google.dev/t/bug-antigravity-cli-install-fails-on-musl-hosts-segfault-on-cpu-that-not-support-aes-ni/169775)) is therefore out of date; the vendor's documented requirements still name glibc, so the musl build is not a documented, supported platform | `probe.sh` fetched the manifest and the archive and checked the sha512 prefix | {{< status verified >}} that it is published; {{< status unverified >}} that the vendor supports it or that it runs on Alpine |

The Antigravity manifest host is the installer's own download URL, not a documented API; it may change without notice.

## Toolchains on musl

Documented only, from the sources named; none was run.

| Toolchain | musl status | Source |
| --- | --- | --- |
| Python wheels | The `musllinux_1_x_<arch>` platform tag exists (PEP 656) and pip selects it on Alpine; a package that publishes no musllinux wheel (common for aarch64 with large native extensions) is built from source, which needs a compiler and headers in the base | [PEP 656](https://peps.python.org/pep-0656/); per-package wheel availability {{< status unverified >}} |
| Node native modules | Node.js does not ship musl builds itself; they come from the project's [unofficial-builds](https://github.com/nodejs/unofficial-builds) (`linux-arm64-musl`, needs `libstdc++`), or from Alpine's `nodejs` package. Modules with prebuilt glibc binaries fall back to `node-gyp`, again needing a compiler | unofficial-builds README; per-module prebuilds {{< status unverified >}} |
| Go | Pure Go builds are unaffected; cgo needs a musl-targeting C compiler, and Alpine's `go` package provides one | Go documentation; {{< status unverified >}} here |
| Rust | `aarch64-unknown-linux-musl` is Tier 2 with host tools (rustc and cargo run natively); `rust:alpine` is the official image | [platform support](https://doc.rust-lang.org/nightly/rustc/platform-support.html); {{< status unverified >}} here |

## What is missing for a decision

1. A run on Apple Container: Alpine (pinned by digest), each CLI's `--version` from a tool-store mount, then `agenttest`'s conformance suite for the Claude adapter; labelled temporary containers only, no sign-in. Needs a host with the container service running (`wh/verify`).
2. What remains in the tool store: Claude Code's `linux-arm64-musl` build is pinned since #162 (`platformRe` accepts the `-musl` suffix, `whr tools build -platform linux-arm64-musl` makes a `-musl` profile), and Antigravity's glibc and musl builds are pinned since #164 (its sha512 manifest checked against the pin and the archive, then a safe extraction; the manifest host is undocumented, so the pin carries the archive URL and a change fails the download). The Codex pins still wait on a verifier for the sigstore bundle on the tarball, which needs a library for the Fulcio chain and the Rekor proof (#164), and nothing is weakened to fit. Selecting the build by the base's libc waits for an Alpine agent base; an environment on a glibc base, Fedora or Ubuntu, keeps the glibc profile. The run on Alpine is #161.

## Proposal for D43

Keep "Alpine follows later" and replace its reason: musl builds of all three CLIs are published (Claude Code dynamic against musl, Codex and Antigravity static) and the store already pins Claude Code's, so what remains is the verifiers for the Codex and Antigravity pins (#164), the build choice by the base's libc once an Alpine agent base exists, and the run on Alpine (#161). Make Alpine first-class for agent environments once that run passes and the store pins all three.

## Updating a pin

`whr tools build` trusts the pins in `internal/toolstore/pins.json` and nothing fetched at run time for Antigravity: the vendor's manifest names only the latest release, so reading it would break a pinned build the day a newer one appears. The provenance is checked once, when a pin is written or updated, by a person or CI, never by `whr`.

- **Antigravity.** Run `scripts/antigravity-pin-check.sh` (curl and jq, https only, no credential). It compares the manifest's version, archive URL and sha512 with each pin; a difference means a newer release to re-pin. Hash the archive and the extracted file yourself for the new pin.
- **Codex.** Pin the release asset's sha256 in the same file (run-time check: the pin). When writing the pin, verify the release's sigstore bundle against the repository's workflow identity and the GitHub Actions OIDC issuer with an external, version-pinned verifier (`cosign verify-blob-attestation --bundle` or `gh attestation verify`), and record the verified identity in the commit message. {{< status unverified >}} No Codex pin exists yet: `cosign` was not installed on the host and `gh attestation verify` needs a `gh auth` login, which the project forbids for agents.

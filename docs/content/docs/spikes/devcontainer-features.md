---
title: "Spike #108: devcontainer features on Apple Container"
description: "Whether common devcontainer features can be fetched from ghcr.io and built into an image with container build on the Fedora and Ubuntu bases."
weight: 10
---

> Source: `spike/devcontainer-features` at `33196f7`, with `run.sh`, the second run's `results.txt` and `RESULTS.md` (the first run's timings). A local branch until it is pushed. Tracks [#108](https://github.com/wstein/workharbor/issues/108). Recorded on 2 October 2026.

Measured on the Mac mini with Apple `container` 1.5.0. The script fetches the `node` (1.7.1) and `python` (1.8.0) features from `ghcr.io/devcontainers/features` as OCI artifacts with an anonymous pull token (no account, no login), checks each blob against its digest, and builds `FROM <base>`, `COPY` the feature, `RUN ./install.sh` as root with the options as upper-case environment variables and `_REMOTE_USER=root`, the way the devcontainer CLI's generated Dockerfile does. The bases are the Fedora and Ubuntu 24.04 images pinned in `internal/baseimage`.

| Build | Time (first run) | Result |
| --- | --- | --- |
| `node` (`version=lts`) on Ubuntu | 78 s | node v24.21.0, npm 11.19.0 |
| `node` on Fedora | 103 s | node v24.21.0, npm 11.19.0 |
| `python` (`version=os-provided`) on Ubuntu | 88 s | Python 3.12.3 |
| `python` on Fedora | 94 s | Python 3.14.7 |

## What it shows

{{< status verified >}} (this spike, on the target setup): both features build and run on both bases with `container build`, from a context that holds only the feature. Two points matter for applying them.

- **A feature's tool is on `PATH` only through its `containerEnv`.** The first run built `node` but `node` was not found: the feature installs it under `/usr/local/share/nvm` and sets `NVM_DIR`, `NVM_SYMLINK_CURRENT` and `PATH` in `containerEnv`. Written into the image as `ENV` lines (including `PATH=…:${PATH}`), it works. D38 reserves `PATH`, `HOME` and the other names for the supervisor in the *spec* (`runtime.ReservedEnv`); a feature's `containerEnv` belongs in the image layer instead, so the reserved-name rule must apply to the repository's own `containerEnv` and to build arguments, not to the `ENV` lines the supervisor writes for a feature.
- **The build has the network the builder has.** `install.sh` downloads from `github.com`, `nodejs.org` and package mirrors during the build, outside the egress allowlist, which is the accepted risk of §7.2 for a repository's own Dockerfile and is the same here. A feature is code from a registry that runs as root in the builder.

Neither feature declares `privileged`, `mounts`, `capAdd`, `securityOpt`, `init`, `entrypoint` or a lifecycle command, so the refusal rules of D38 were not exercised; the script prints those keys so a third feature can be checked the same way.

## Limits

{{< status unverified >}}: two features, one version each, on two bases. `installsAfter` (`common-utils`, `oryx`) was not applied and no `common-utils` user setup was run; options beyond `version` were not tried; a feature that needs a lifecycle hook, a mount or a privilege was not found among these and was not built; other registries than `ghcr.io` and a feature's own dependencies on other features were not tried. The time is the builder's with a cold cache; a second build of the same context took 9 to 14 s from the cache.

# Spike #108: devcontainer features on Apple Container

`run.sh` fetches the `node` (1.7.1) and `python` (1.8.0) features from ghcr.io, builds each
on the pinned Fedora and Ubuntu 24.04 bases with `container build` 1.5.0, and runs the tool.
`results.txt` is the second run (its builds were cached, hence 9 to 14 s).

First run, before the feature's `containerEnv` was written into the image as `ENV` lines:

| Build | Time | Result |
| --- | --- | --- |
| node on Ubuntu | 78 s | built; `node: not found` (no `PATH`) |
| node on Fedora | 103 s | built; `node: command not found` |
| python (`version=os-provided`) on Ubuntu | 88 s | Python 3.12.3 |
| python (`version=os-provided`) on Fedora | 94 s | Python 3.14.7 |

Second run, with `ENV NVM_DIR`, `NVM_SYMLINK_CURRENT` and `PATH` from the feature's
`containerEnv`: node v24.21.0 and npm 11.19.0 on both bases.

Limits: two features, one version each, run as root in the builder with direct network
access (the builder is outside the egress allowlist, §7.2), `installsAfter` (common-utils,
oryx) not applied, no `common-utils` user setup, no feature with `privileged`, `mounts`,
`capAdd` or a lifecycle hook (neither of these two has one).

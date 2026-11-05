# Spike 319 evidence

Setup: run `scripts/lab-setup.sh` with `T` set to an empty scratch directory (may be relative) and `CLAUDE_JOB_DIR` set so that `$CLAUDE_JOB_DIR/tmp` equals
`T` (the scripts read `env.sh`, the lab tree and `lin/img.env` from there). It downloads mise 2026.10.25 (macOS and Linux arm64, checksum
verified), copies the scripts to `$T/scripts` (they call each other there), writes `env.sh`, builds the hostile, own, five-layer and lock fixtures, and names the container image (`IMG`, default
`debian:trixie`; the spike used the Anchorage devcontainer image, Debian 13 arm64, root, with bash). Scripts that need container installs
(`dk.sh`, `ct.sh`, `bind.sh`, `shared.sh`, `bench.sh`) also expect the container workspace tree `$T/lin/ct/ws` (mounted as `/ws`):
`repos/hostile/mise.toml` and `repos/own/mise.toml` (`lab-setup.sh` builds both from the fixture with canary paths under `/canary`, and runs
`git init` in each) and a whr-layer file `/ws/mise.toml` (`$T/lin/ct/ws/mise.toml`), which you create by hand (any valid `[tools]` file; it is the
config path the scripts trust). `$T/lin/ct/canary` is recreated by the scripts. They further expect an installed Linux data directory in `$T/lin/data` and the
project in `$T/lin/proj`; create them with `dk.sh rw rw 'mise lock; mise install --locked'` after copying a `mise.toml` there. The benchmark
also needs `$T/bench/ws/mise.toml` (8 tools, see `bench.sh`). Network access and `gh` login are required.

Stored output: `layers.out` (ADR section 1), `lock.out` (section 4), `bench.out` (section 6). Other results in the ADR were observed from
these scripts and are not stored as output.

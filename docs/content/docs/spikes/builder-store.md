---
title: "Spike (#133): does the builder see the host image store"
description: "Whether container build resolves a FROM from the host's local image store, with a whr.invalid name and with a bare one, with and without --pull."
weight: 11
---

> Source: `spike/builder-store` at `f0af05e`, with `run.sh`, `results.txt` and `RESULTS.md`. A local branch until it is pushed. Tracks [#133](https://github.com/wstein/workharbor/issues/133). Recorded on 2 October 2026.

Measured on the Mac mini with Apple `container` 1.5.0. The script builds an image X (`alpine` plus `/marker`) and tags it `whr.invalid/whtmp/x:1` and `whtmp/x:1`; neither was ever pushed. It then builds `FROM <tag>` plus `RUN cat /marker` in a fresh context for each name, without and with `--pull`.

| Base | Without `--pull` | With `--pull` |
| --- | --- | --- |
| `whr.invalid/whtmp/x:1` | exit 0 in 3 s, the local image is used | exit 1 in 10 s: `failed to resolve either repository hostname whr.invalid` |
| `whtmp/x:1` | exit 0 in 7 s, the local image is used | exit 1 in 1 s: `401 Unauthorized` from `registry-1.docker.io` |

A second part uses the same image with the two other ways a Dockerfile names an image, again without and with `--pull`.

| Dockerfile | Without `--pull` | With `--pull` |
| --- | --- | --- |
| `FROM scratch` plus `COPY --from=whr.invalid/whtmp/x:1 /marker /marker` | exit 0 in 2 s, the local image is used | exit 1 in 10 s: `failed to resolve either repository hostname whr.invalid` |
| `FROM alpine` plus `RUN --mount=type=bind,from=whr.invalid/whtmp/x:1,target=/m cat /m/marker` | exit 0 in 4 s, `marker-X` is printed | exit 1 in 11 s: the same error |

## What it shows

{{< status verified >}} (this spike, on the target setup): the builder resolves a `FROM` from the host's image store, by default, whatever the name is. `COPY --from=` and `RUN --mount=...,from=` resolve a local `whr.invalid/` image the same way (the COPY case is judged by its exit status, since a `scratch` build prints nothing). A repository's Dockerfile that names a `whr.invalid/` image another repository's build left in the store builds on it, so the text scan of D38 is the only guard. `--pull` goes to the registry for either name and fails; the supervisor's own features build also uses a local `FROM` ({{< status unverified >}}: whether passing `--pull` is possible there was not tried).

## Limits

{{< status unverified >}}: one image and one run. The marker line was absent from the build log of the second case because the `RUN` layer came from the cache. Not measured: `ONBUILD` in a base image, and a custom `# syntax=` frontend (refused by `RefuseSyntaxDirective` without being run).

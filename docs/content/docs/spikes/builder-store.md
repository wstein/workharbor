---
title: "Spike (#133): does the builder see the host image store"
description: "Whether container build resolves a FROM from the host's local image store, with a whr.invalid name and with a bare one, with and without --pull."
weight: 11
---

> Source: `spike/builder-store` at `37a9cb3`, with `run.sh`, `results.txt` and `RESULTS.md`. A local branch until it is pushed. Tracks [#133](https://github.com/wstein/workharbor/issues/133). Recorded on 2 October 2026.

Measured on the Mac mini with Apple `container` 1.5.0. The script builds an image X (`alpine` plus `/marker`) and tags it `whr.invalid/whtmp/x:1` and `whtmp/x:1`; neither was ever pushed. It then builds `FROM <tag>` plus `RUN cat /marker` in a fresh context for each name, without and with `--pull`.

| Base | Without `--pull` | With `--pull` |
| --- | --- | --- |
| `whr.invalid/whtmp/x:1` | exit 0 in 3 s, the local image is used | exit 1 in 10 s: `failed to resolve either repository hostname whr.invalid` |
| `whtmp/x:1` | exit 0 in 7 s, the local image is used | exit 1 in 1 s: `401 Unauthorized` from `registry-1.docker.io` |

## What it shows

{{< status verified >}} (this spike, on the target setup): the builder resolves a `FROM` from the host's image store, by default, whatever the name is. A repository's Dockerfile that names a `whr.invalid/` image another repository's build left in the store builds on it, so the text scan of D38 is the only guard. `--pull` goes to the registry for either name and fails; the supervisor's own features build also uses a local `FROM` ({{< status unverified >}}: whether passing `--pull` is possible there was not tried).

## Limits

{{< status unverified >}}: one image and one run. The marker line was absent from the build log of the second case because the `RUN` layer came from the cache. Whether a `COPY --from` or a `RUN --mount=from=` of a local image resolves the same way was not measured, nor was a custom `# syntax=` frontend (refused by `RefuseSyntaxDirective` without being run).

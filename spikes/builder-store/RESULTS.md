# Spike (#133): can the builder see the host's local image store?

`run.sh` builds X (alpine + `/marker`), tags it `whr.invalid/whtmp/x:1` and `whtmp/x:1` (never pushed),
then builds `FROM <tag>` in a fresh context with `container build` 1.5.0. `results.txt` is the run.

| Base | No flag | `--pull` |
| --- | --- | --- |
| `whr.invalid/whtmp/x:1` | exit 0, 3 s, local image used | exit 1, `failed to resolve either repository hostname whr.invalid` |
| `whtmp/x:1` | exit 0, 7 s, local image used | exit 1, `401 Unauthorized` from registry-1.docker.io |

The builder resolves a FROM from the host store by default; the name does not matter, only `--pull` forces the registry.
Limits: one image, one run; the marker line is missing from the log when the RUN layer came from the cache.

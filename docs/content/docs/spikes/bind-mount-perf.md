---
title: "Spike #40: bind mount versus volume"
description: "git status, read, copy, remove and build times for a node_modules-style tree and a 153 000-file repository, on a virtiofs bind mount and on an ext4 volume."
weight: 6
---

> Source: `spike/bind-mount-perf` at `2d29ecdeedeaa1937e89b8ab6d9863e22c854d89` (local branch until it is pushed), with `run.sh`, `gen.py`, `guest.sh` and the raw `results.txt`. Tracks [#40](https://github.com/wstein/workharbor/issues/40). Recorded on 1 October 2026.

Measured on a Mac mini (Apple silicon), Apple `container` 1.5.0, a `fedora` guest with 4 CPUs and 4 GB, git, gcc and tar installed with `dnf`. Two generated workloads, both committed to git so `git status` has every file to check: `nodemods` (40 000 small files, the shape of a `node_modules` tree) and `large` (153 000 files plus 3 000 C sources). Each ran once on a bind mount of a host directory (virtiofs) and once on a volume (ext4 image) seeded from the host copy. Times are seconds; the first row of each kind is the first run, with cold caches in the guest.

| Measurement | nodemods bind | nodemods volume | large bind | large volume |
| --- | --- | --- | --- | --- |
| `git status`, first run | 11.8 | 0.45 | 40 | 1.5 |
| `git status`, warm | 1.8 | 0.04 | 8 | 0.11 |
| read the whole tree (`tar`) | 1.2 | 0.07 | 9.1 | 0.17 |
| copy the tree (`cp -a`) | 35 | 0.43 | 128 | 1.5 |
| remove the tree | 7.2 | 0.16 | 28 | 0.30 |
| build, 3 000 C files, 4 in parallel | | | 8.2 | 7.5 |
| build, 1 111 C files in sequence | | | 9.5 | 10.1 |
| seed the volume from the host copy | | 18 | | 68 |

## What it shows

- Work that touches many files is **20 to 100 times slower on a bind mount**: `git status`, a tree walk, a copy and a removal. The warm `git status` of the large repository takes 8 s on a bind mount and 0.1 s on a volume.
- A **CPU-bound build is the same** on both (about 8 to 10 s): the cost is per file operation, not throughput.
- Putting a tree on a volume costs a **one-time copy** (18 s and 68 s here).

## Limits

One machine, one image, one run per cell (the first run is the cold one; a second run in `results.txt` repeats it warm for the bind mount). The trees are generated, not a real `node_modules` or a real repository, and the host side was not measured. The design owner decided from it: dependency directories go on a volume and the checkout stays on the host ([D39](../design/decisions.md)), and a volume checkout for very large repositories stays open (§12).

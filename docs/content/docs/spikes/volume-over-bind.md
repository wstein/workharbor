---
title: "Spike #80: a volume over a bind mount"
description: "Whether a named volume mounted at a subdirectory of a bind-mounted checkout works on Apple Container 1.5.0, and what the mount point does in the guest."
weight: 7
---

> Source: [`spike/volume-over-bind`](https://github.com/wstein/workharbor/tree/spike/volume-over-bind/spikes/volume-over-bind) at `161daccbbd1cccf087c51177d3661693520c2788` (measured at `a75a747`), with `run.sh` and the unedited `results.txt`. Tracks [#80](https://github.com/wstein/workharbor/issues/80). Recorded on 1 October 2026.

Measured on the Mac mini with Apple `container` 1.5.0 and a `fedora` guest. A temporary host directory is bind-mounted at `/work`, and a named volume at `/work/node_modules`, as D39 first proposed for a dependency directory inside a checkout.

| Check | Result |
| --- | --- |
| The guest sees the volume | yes: `/work` on virtiofs, `/work/node_modules` on the volume's ext4 |
| The host sees | only an empty mount-point directory; the guest's writes elsewhere in `/work` reach the host |
| Contents after stop and start | kept |
| Contents after delete and rebuild | kept; the runtime recreates the mount point if the host removed it |
| A host file at that path | hidden in the guest, which sees the volume's files and `lost+found`; the host file is untouched |
| `mv`, `rmdir` of the mount point in the guest | fail with "Device or resource busy" |
| `rm -rf` of the mount point in the guest | empties it, then exits 1 on the directory itself |

## What it shows

A volume can cover a dependency directory of a bind-mounted checkout, but a tool that deletes or renames that directory fails on the busy mount point, a fresh volume shows `lost+found`, and a tracked file there would be hidden. So the design moves output directories through the tool's own setting where one exists and mounts over the checkout only where a tool cannot move it ([D39](../design/decisions.md)).

## Limits

One run on one machine. How npm copes with the busy mount point (`npm ci`, `npm install`, `rm -rf node_modules && npm install`) is not measured yet; it is a criterion of #80.

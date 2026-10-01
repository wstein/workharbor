# Results: a volume over a bind-mounted subdirectory

Issue #80, for design D39. Run on 1 October 2026 on the Mac mini with Apple `container` 1.5.0 and a `fedora` guest. `run.sh` is the whole experiment and `results.txt` its unedited output; the numbers below refer to its lines.

A temporary host directory is bind-mounted at `/work`, and a named volume at `/work/node_modules`, as D39 proposes for a dependency directory inside a checkout.

## Measured

- **It mounts (1, 2).** The guest has `/work` on virtiofs and `/work/node_modules` on the volume's ext4. The guest reads what it wrote to the volume; the host sees only an empty mount-point directory there, and the guest's writes elsewhere in `/work` reach the host.
- **It keeps its contents (3, 4).** The volume's file survives a stop and start, and a delete and rebuild of the container. After the host removed the mount point, the runtime created it again.
- **It hides host files (6).** A file the host has at that path is not visible in the guest, which sees the volume's `dep.txt` and `lost+found` instead; the host file is untouched.
- **The mount point is busy in the guest (7, 8).** `mv` and `rmdir` of `/work/node_modules` fail with "Device or resource busy"; `rm -rf` empties it and then exits 1 on the directory itself. Creating directories inside it works.

## Not measured

How `npm ci`, `npm install` and `rm -rf node_modules && npm install` cope with the busy mount point (a criterion of #80); other tools that delete their output directory; another runtime.

## Consequence (design owner)

D39 relocates an output directory to a volume through the tool's own setting where one exists (`CARGO_TARGET_DIR`, `UV_PROJECT_ENVIRONMENT`), and mounts over the checkout only where a tool cannot move it (`node_modules`), never over a tracked path.

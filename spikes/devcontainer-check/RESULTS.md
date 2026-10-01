# Issue #77: `make check` inside the repository's devcontainer

`run.sh .` builds `.devcontainer/Dockerfile` with `container build`, starts a container from it (4 CPUs, 6 GB, user `agent`) with a copy of the tree bind-mounted at `/workspace` and the Go caches (`GOCACHE`, `GOMODCACHE`, the golangci-lint cache, `HOME`) on a volume at `/cache`, and runs `make check` twice. `results.txt` is the unedited output of the run on Apple Container 1.5.0.

Result: `make check` exits 0 on the first run (81 s, cold caches, tools fetched through the Go module proxy) and on the second (3 s, caches on the volume).

An earlier run with the plain `golang:1.27.1-trixie` image and uid 1000 failed in the tests because that uid has no passwd entry and `ssh-keygen` refuses it; that is why the Dockerfile adds the `agent` user. A run on a full disk failed with I/O errors in the guest and is not evidence either way.

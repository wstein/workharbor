# Apple Container spike

Throwaway scripts for [issue #2](https://github.com/wstein/workharbor/issues/2). Not shipping code. Findings are in [RESULTS.md](RESULTS.md); raw output is in `results/`.

## Safety

- Every object is named `whspike-*`, and only those are ever removed (`cleanup` in `lib.sh`). **Never use `container rm --all`**: it deletes containers you did not create.
- The tests probe loopback, the host gateway, the Mac's own LAN address, one TCP connect to the default gateway, and listeners started by the scripts. Nothing else on the network is touched.
- `container system stop` and `start` are not run by any script.
- Nothing here needs `sudo`. No credentials are used or stored.

## Run

Needs `container` 1.5.0 or newer, `python3`, Go, and the `fedora:latest` image (or set `IMG`).

```bash
cd spikes/apple-container
(cd probe && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ../bin-probe-linux . \
  && CGO_ENABLED=0 go build -o ../bin-probe-host .)
S=/some/scratch/dir
./01-lifecycle-limits.sh
BIND=$S/bind ./02-storage.sh
SCRATCH=$S ./03-isolation.sh
SCRATCH=$S ./04-egress.sh && SCRATCH=$S ./04b-egress-sidecar.sh
SCRATCH=$S ./05a-agent-install.sh && SCRATCH=$S ./05b-agent-persist.sh
SCRATCH=$S ./06-recovery.sh
./cleanup-all.sh
```

`probe/` is a small static Go helper (listeners, dial tests and a logging allowlist proxy) that runs on the host and, cross-compiled, inside the containers, so the guest image needs no extra tools.

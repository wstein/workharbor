---
title: "Spike #69: host reachability"
description: "What a guest on Apple Container can reach on the host's addresses, `--internal` networks included."
weight: 5
---

> Source: `spike/host-reachability` at `0c78d94f86cb5d659e993714fe4d728c0dc9fe50` (not on GitHub yet), with the scripts and raw output next to the results. Tracks [#69](https://github.com/wstein/workharbor/issues/69). Published as recorded on 1 October 2026.

Issue #69, for design D29 and §7.2. Run on 2026-10-01 on the Mac mini: macOS 26.6.2, Apple `container` 1.5.0, the macOS Application Firewall **disabled** (state 0), no VPN. `run.sh` is the whole experiment and `run.out` is its unedited output.

## Method

Four host listeners (`python3 -m http.server`) on ports 18080 to 18083: bound to loopback, to the LAN address (`192.168.1.205`), to every interface, and to the host's address on the container network (`192.168.64.1`, which exists only while a container runs). A guest on the default network (as the egress proxy sidecar is) and a guest on an `--internal` network (`192.168.128.0/24`) each curl every listener through the LAN address and through the container-network address, then the internet and the router (another LAN device).

## Measured

HTTP status; `000` means no connection within 4 s.

| Target (guest connects to) | Listener bound to | Default-network guest | `--internal` guest |
| --- | --- | --- | --- |
| LAN address `:18080` | loopback | 000 | 000 |
| LAN address `:18081` | LAN address | **200** | **200** |
| LAN address `:18082` | all interfaces | **200** | **200** |
| LAN address `:18083` | `192.168.64.1` | 000 | 000 |
| `192.168.64.1:18080` | loopback | 000 | 000 |
| `192.168.64.1:18081` | LAN address | 000 | 000 |
| `192.168.64.1:18082` | all interfaces | **200** | 000 |
| `192.168.64.1:18083` | `192.168.64.1` | **200** | 000 |
| internet (`https://1.1.1.1`) | n/a | 301 | 000 |
| router `192.168.1.1:80` (another LAN device) | n/a | 200 | 000 |

Controls: the host itself reached all four listeners on the addresses they are bound to (200).

## What it shows

- **Loopback is safe.** A listener bound only to `127.0.0.1` was unreachable from both guests.
- **A listener on the LAN address, or on every interface, is reachable from both guests, including the `--internal` one.** Binding the API to the Mac's LAN address, as D29 allows, therefore does not keep a guest out: the guest reaches the host through its own LAN address. This confirms the review's concern.
- **`--internal` does not mean "host unreachable".** Spike #2 concluded that an `--internal` network blocks everything and that the host has no interface on it. The internet and another LAN device (the router) are indeed blocked, but the host's own LAN address answers. The route is through the network's gateway to the host's own address. Treat the host's addresses as reachable from every guest; only the loopback-only and gateway-bound listeners are not reachable from an internal guest.
- **A listener bound to the container network's address (`192.168.64.1`) is reachable from default-network guests only.** That is the sidecar's side, so such a bind would expose the API to the sidecar and to the default-network guests, and to no internal guest.
- **The Application Firewall was off,** so these are the results with no filtering at all. With it on, the answers may differ; that was not measured.

## Not measured

- A Tailscale address: Tailscale is not installed on this Mac.
- The `pf` rule that admits only a source range to one port, tested from a LAN device and from the sidecar: it needs `sudo` and a second device. The commands to try are in the issue's second criterion.
- The Application Firewall enabled.
- A reboot, or other networks (`--internal` guests were created with the default subnet).
- The real sidecar: a default-network `fedora` guest stood in for it.

## Consequence for D29 (for the design owner)

The API's token must be the guard against a guest or the sidecar reaching it, not the bind address. Any non-loopback bind is reachable from every guest, so the token is checked on every request regardless of the address. Binding to loopback only keeps guests out entirely, at the cost of the remote-control use case (§1).

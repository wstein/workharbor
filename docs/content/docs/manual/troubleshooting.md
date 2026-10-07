---
title: Troubleshooting
description: What to check when whr fails to start.
weight: 17
toc: true
---

## `zsh: killed` with no output

Running `whr` (for example `whr --dev setup host`) prints only `zsh: killed`. The suspected cause is a macOS code signature that is invalid or stale after the binary was copied or rewritten in place over a previous one. That cause is {{< status unverified >}} until `codesign -v` and `xattr -l` output from the affected host confirms it (issue #393). Check the binary:

```text
codesign -v <path>     # no output and exit 0 means the signature is valid
xattr -l <path>        # lists extended attributes such as com.apple.quarantine
```

Fix: rebuild with `make install` from the clean current `main`. It signs the new `whr` ad hoc and replaces the old file by rename.

## The GitHub App link times out

`whr github app create` prints a link built from `public_url`, not from the loopback listener. A timeout points at the name or at the forwarder: a name the device cannot resolve, or `tailscale serve` not mapping the name to `listen` (default `127.0.0.1:8787`). The macOS application firewall does not filter loopback, so it is not the cause on the Mac itself. Run `whr doctor` (step `public-url`), or open the link of `whr github app create --local` in a browser on the Mac. See [host setup, step 11](host-setup.md#11-the-github-app).
